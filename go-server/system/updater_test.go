package system

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// pipelineHandler 是 updater 测试用的命令处理器：按命令分派，字段可覆盖各失败注入点。
type pipelineHandler struct {
	rev        func() string
	diff       string
	pullErr    error
	migrateErr error
	dumpErr    error
	buildErr   map[string]error
	unhealth   map[string]bool
	images     string
	projects   string
	onPull     func()
	ntfyTitles []string
}

func (h *pipelineHandler) handle(c Call) (string, string, error) {
	switch c.Name {
	case "df":
		return "Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/root 12345678 1234567 1234567890 1% /opt\n", "", nil
	case "git":
		return h.git(c)
	case "docker":
		return h.docker(c)
	}
	return "", "", nil
}

func (h *pipelineHandler) git(c Call) (string, string, error) {
	args := c.Args
	switch {
	case hasArg(args, "version"):
		return "", "", nil
	case hasArg(args, "rev-parse"):
		return h.rev() + "\n", "", nil
	case hasArg(args, "pull"):
		if h.pullErr != nil {
			return "", "pull failed", h.pullErr
		}
		if h.onPull != nil {
			h.onPull()
		}
		return "", "", nil
	case hasArg(args, "diff"):
		return h.diff, "", nil
	default: // fetch / checkout
		return "", "", nil
	}
}

func (h *pipelineHandler) docker(c Call) (string, string, error) {
	args := c.Args
	switch {
	case hasArg(args, "compose") && hasArg(args, "version"):
		return "", "", nil
	case hasArg(args, "version"):
		return "", "", nil
	case hasArg(args, "ls"):
		if h.projects == "" {
			h.projects = `[{"Name":"deploy","Status":"running","ConfigFiles":"x.yml"}]`
		}
		return h.projects, "", nil
	case hasArg(args, "config") && hasArg(args, "--images"):
		if h.images == "" {
			h.images = "repo-server\n"
		}
		return h.images, "", nil
	case hasArg(args, "ps"):
		svc := args[len(args)-1]
		if h.unhealth[svc] {
			return `{"Name":"lab-` + svc + `","Service":"` + svc + `","State":"exited","Health":"unhealthy"}`, "", nil
		}
		return `{"Name":"lab-` + svc + `","Service":"` + svc + `","State":"running","Health":"healthy"}`, "", nil
	case hasArg(args, "exec"):
		if h.dumpErr != nil {
			return "", "pg_dump failed", h.dumpErr
		}
		return "DUMPDATA\n", "", nil
	case hasArg(args, "run") && hasArg(args, "migrate"):
		if h.migrateErr != nil {
			return "", "migrate failed", h.migrateErr
		}
		return "", "", nil
	case hasArg(args, "build"):
		if svc := buildService(args); svc != "" && h.buildErr[svc] != nil {
			return "", "build failed", h.buildErr[svc]
		}
		return "", "", nil
	default: // up / logs
		return "", "", nil
	}
}

func buildService(args []string) string {
	for i, a := range args {
		if a == "build" && i+1 < len(args) && args[i+1] != "--pull" {
			return args[i+1]
		}
	}
	return ""
}

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func newTestPipeline(t *testing.T, mutate func(cfg *UpdateConfig), h *pipelineHandler) (*Pipeline, *fakeCmdRunner, string, string) {
	t.Helper()
	dir := t.TempDir()
	repoRoot := filepath.Join(dir, "repo")
	secrets := filepath.Join(repoRoot, "deploy", "secrets")
	for _, f := range []string{"db_password.txt", "jwt_key.txt", "influxdb_token.txt", "agent_password.txt"} {
		if err := os.MkdirAll(secrets, 0o755); err != nil {
			t.Fatalf("mkdir secrets: %v", err)
		}
		if err := os.WriteFile(filepath.Join(secrets, f), []byte("x"), 0o644); err != nil {
			t.Fatalf("write secret: %v", err)
		}
	}

	// ntfy 通知走 Go net/http：起 httptest 捕获 Title 头（替代原 curl 命令拦截）
	ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ntfyTitles = append(h.ntfyTitles, r.Header.Get("Title"))
	}))
	t.Cleanup(ntfy.Close)

	logPath := filepath.Join(dir, "session.log")
	donePath := filepath.Join(dir, "session.done")
	cfg := &UpdateConfig{
		RepoRoot:        repoRoot,
		ComposeFile:     "deploy/docker-compose.yml",
		ProjectName:     "deploy",
		SessionID:       "upd_test000000",
		LogFile:         logPath,
		DoneFile:        donePath,
		NtfyURL:         ntfy.URL,
		UpdateTimeout:   0,
		RollbackTimeout: 0,
	}
	if mutate != nil {
		mutate(cfg)
	}
	logger, err := NewLogger(logPath, nil)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	// Windows 上打开中的日志文件无法删除，必须在 TempDir 清理前关闭句柄。
	t.Cleanup(func() { _ = logger.Close() })
	fake := &fakeCmdRunner{fn: h.handle}
	return NewPipeline(cfg, fake, logger), fake, logPath, donePath
}

func defaultRev(p *struct{ after bool }, oldSHA, newSHA string) func() string {
	return func() string {
		if p.after {
			return newSHA
		}
		return oldSHA
	}
}

// TestPipelineSuccessFullRun 成功路径：7 步全跑、server 受影响、全栈健康、ntfy 成功、marker exit 0。
func TestPipelineSuccessFullRun(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:    defaultRev(rev, "oldsha", "newsha"),
		diff:   "go-server/main.go\n",
		onPull: func() { rev.after = true },
	}
	p, fake, logPath, donePath := newTestPipeline(t, nil, h)

	code := p.Run(context.Background())
	if code != 0 {
		t.Fatalf("Run = %d, want 0", code)
	}

	if order := stepOrder(t, logPath); !equalInts(order, []int{1, 2, 3, 4, 5, 6, 7}) {
		t.Errorf("step 顺序 = %v, want [1..7]", order)
	}

	var m DoneMarker
	data, _ := os.ReadFile(donePath)
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("marker 解析失败: %v", err)
	}
	if m.ExitCode != 0 || m.OldSHA != "oldsha" || m.NewSHA != "newsha" {
		t.Errorf("marker = %+v", m)
	}

	calls := fake.callsSnapshot()
	if !containsCall(calls, "docker", "compose", "build", "server") {
		t.Errorf("缺少 server 构建调用: %v", callNames(calls))
	}
	if !containsCall(calls, "docker", "compose", "up", "-d", "--no-deps", "server") {
		t.Errorf("缺少 server 重启调用: %v", callNames(calls))
	}
	if !containsCall(calls, "docker", "compose", "exec", "pg_dump") {
		t.Errorf("缺少 pg_dump 调用: %v", callNames(calls))
	}
	found := false
	for _, title := range h.ntfyTitles {
		if title == "系统更新成功" {
			found = true
		}
	}
	if !found {
		t.Errorf("缺少成功 ntfy: %v", h.ntfyTitles)
	}
	// pg_dump 输出应流式写入备份文件（不经内存缓冲）
	backups, _ := filepath.Glob(filepath.Join(p.cfg.RepoRoot, ".hermes", "backups", "lab-db-backup-*.sql"))
	if len(backups) != 1 {
		t.Errorf("备份文件数 = %d, want 1", len(backups))
	} else if data, _ := os.ReadFile(backups[0]); !strings.Contains(string(data), "DUMPDATA") {
		t.Errorf("备份文件内容缺少 pg_dump 输出: %q", data)
	}
	if !rev.after {
		t.Errorf("pull 后未解析新 SHA")
	}
}

// TestPipelineBuildFailureNoRollback 构建失败不回滚（尚未重建任何容器）。
func TestPipelineBuildFailureNoRollback(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:      defaultRev(rev, "oldsha", "newsha"),
		diff:     "go-server/main.go\n",
		buildErr: map[string]error{"server": context.DeadlineExceeded},
		onPull:   func() { rev.after = true },
	}
	p, fake, _, donePath := newTestPipeline(t, nil, h)

	if code := p.Run(context.Background()); code != 1 {
		t.Fatalf("Run = %d, want 1", code)
	}
	calls := fake.callsSnapshot()
	if containsCall(calls, "git", "checkout", "oldsha") {
		t.Error("构建失败不应回滚")
	}
	if hasTitle(h.ntfyTitles, "系统更新失败-已回滚") {
		t.Error("构建失败不应发已回滚通知")
	}
	data, _ := os.ReadFile(donePath)
	var m DoneMarker
	_ = json.Unmarshal(data, &m)
	if m.ExitCode != 1 {
		t.Errorf("marker exit_code = %d, want 1", m.ExitCode)
	}
}

// TestPipelineMigrateFailureBlocksRollback 迁移失败 → 迁移阻塞分支：通知 + 手动回滚，不重建服务。
func TestPipelineMigrateFailureBlocksRollback(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:        defaultRev(rev, "oldsha", "newsha"),
		diff:       "migrations/002_alter.sql\ngo-server/main.go\n",
		migrateErr: os.ErrPermission,
		onPull:     func() { rev.after = true },
	}
	p, fake, _, donePath := newTestPipeline(t, nil, h)

	if code := p.Run(context.Background()); code != 1 {
		t.Fatalf("Run = %d, want 1", code)
	}
	calls := fake.callsSnapshot()
	// 必须先 checkout 旧代码，再进迁移阻塞分支
	if !containsCall(calls, "git", "checkout", "oldsha") {
		t.Errorf("迁移失败应先 checkout 旧代码: %v", callNames(calls))
	}
	// 阻塞分支：不得重建/重启任何服务
	if containsCall(calls, "docker", "compose", "up", "-d") {
		t.Errorf("迁移阻塞分支不应重建服务: %v", callNames(calls))
	}
	// 通知迁移阻塞 + 仓库 reset --hard 停在 OLD_SHA（与运行的旧镜像一致）
	if !hasTitle(h.ntfyTitles, "系统更新失败-迁移变更阻塞") {
		t.Errorf("缺少迁移阻塞通知: %v", h.ntfyTitles)
	}
	if !containsCall(calls, "git", "reset", "--hard", "oldsha") {
		t.Errorf("阻塞分支应 git reset --hard oldsha: %v", callNames(calls))
	}
	data, _ := os.ReadFile(donePath)
	var m DoneMarker
	_ = json.Unmarshal(data, &m)
	if m.ExitCode != 1 {
		t.Errorf("marker exit_code = %d, want 1", m.ExitCode)
	}
}

// TestPipelineHealthFailureRollsBack 健康检查失败 → 常规回滚：checkout 旧代码 + 重建受影响服务。
func TestPipelineHealthFailureRollsBack(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:      defaultRev(rev, "oldsha", "newsha"),
		diff:     "go-server/main.go\n",
		unhealth: map[string]bool{"grafana": true},
		onPull:   func() { rev.after = true },
	}
	p, fake, logPath, _ := newTestPipeline(t, nil, h)

	if code := p.Run(context.Background()); code != 1 {
		t.Fatalf("Run = %d, want 1", code)
	}
	calls := fake.callsSnapshot()
	if !containsCall(calls, "git", "checkout", "oldsha") {
		t.Errorf("健康检查失败应回滚代码: %v", callNames(calls))
	}
	if !containsCall(calls, "docker", "compose", "build", "server") {
		t.Errorf("回滚应重建受影响服务 server: %v", callNames(calls))
	}
	if !containsCall(calls, "docker", "compose", "up", "-d", "--no-deps", "server") {
		t.Errorf("回滚应重启受影响服务 server: %v", callNames(calls))
	}
	if !containsCall(calls, "git", "reset", "--hard", "oldsha") {
		t.Errorf("回滚完成应 git reset --hard oldsha: %v", callNames(calls))
	}
	if !hasTitle(h.ntfyTitles, "系统更新失败-已回滚") {
		t.Errorf("缺少已回滚通知: %v", h.ntfyTitles)
	}
	if s := stepOrder(t, logPath); !equalInts(s, []int{1, 2, 3, 4, 5, 6, 7}) {
		t.Errorf("回滚也应走完 7 步头部: %v", s)
	}
}

// TestPipelineDryRun dry-run 只检测不执行。
func TestPipelineDryRun(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:    defaultRev(rev, "oldsha", "newsha"),
		diff:   "go-server/main.go\n",
		onPull: func() { rev.after = true },
	}
	p, fake, logPath, donePath := newTestPipeline(t, func(c *UpdateConfig) { c.DryRun = true }, h)

	if code := p.Run(context.Background()); code != 0 {
		t.Fatalf("Run = %d, want 0", code)
	}
	calls := fake.callsSnapshot()
	if containsCall(calls, "docker", "compose", "build") || containsCall(calls, "docker", "compose", "up") {
		t.Errorf("dry-run 不应执行 build/up: %v", callNames(calls))
	}
	data, _ := os.ReadFile(logPath)
	if !strings.Contains(string(data), "DRY RUN 变更详情") {
		t.Error("dry-run 应打印变更详情")
	}
	if m := readMarker(t, donePath); m.ExitCode != 0 {
		t.Errorf("dry-run marker exit_code = %d, want 0", m.ExitCode)
	}
}

// TestPipelineNoChangesEarlyExit 变更提交但文件 diff 为空 → 步骤 4 提前成功退出。
func TestPipelineNoChangesEarlyExit(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:    defaultRev(rev, "oldsha", "newsha"),
		diff:   "",
		onPull: func() { rev.after = true },
	}
	p, fake, logPath, donePath := newTestPipeline(t, nil, h)

	if code := p.Run(context.Background()); code != 0 {
		t.Fatalf("Run = %d, want 0", code)
	}
	calls := fake.callsSnapshot()
	if containsCall(calls, "docker", "compose", "build") {
		t.Errorf("无变更不应构建: %v", callNames(calls))
	}
	data, _ := os.ReadFile(logPath)
	if !strings.Contains(string(data), "无文件变更，跳过构建") {
		t.Error("缺少跳过日志")
	}
	if m := readMarker(t, donePath); m.ExitCode != 0 {
		t.Errorf("marker exit_code = %d, want 0", m.ExitCode)
	}
}

// TestPipelineForceFullRebuild --force 跳过变更检测，全量 build --pull。
func TestPipelineForceFullRebuild(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:    defaultRev(rev, "oldsha", "oldsha"),
		diff:   "",
		onPull: func() { rev.after = true },
	}
	p, fake, logPath, _ := newTestPipeline(t, func(c *UpdateConfig) { c.Force = true }, h)

	if code := p.Run(context.Background()); code != 0 {
		t.Fatalf("Run = %d, want 0", code)
	}
	calls := fake.callsSnapshot()
	if !containsCall(calls, "docker", "compose", "build", "--pull") {
		t.Errorf("--force 应全量 build --pull: %v", callNames(calls))
	}
	if !containsCall(calls, "docker", "compose", "up", "-d", "--no-deps", "server") {
		t.Errorf("--force 应重启 server: %v", callNames(calls))
	}
	if order := stepOrder(t, logPath); !equalInts(order, []int{1, 2, 3, 4, 5, 6, 7}) {
		t.Errorf("step 顺序 = %v", order)
	}
}

// TestPipelinePullFailureAborts git pull 失败（非 force）→ 中止，不回滚。
func TestPipelinePullFailureAborts(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:     defaultRev(rev, "oldsha", "oldsha"),
		diff:    "go-server/main.go\n",
		pullErr: os.ErrPermission,
	}
	p, fake, logPath, donePath := newTestPipeline(t, nil, h)

	if code := p.Run(context.Background()); code != 1 {
		t.Fatalf("Run = %d, want 1", code)
	}
	calls := fake.callsSnapshot()
	if containsCall(calls, "git", "checkout", "oldsha") {
		t.Error("pull 失败不应回滚")
	}
	data, _ := os.ReadFile(logPath)
	if !strings.Contains(string(data), "git pull 失败") {
		t.Error("缺少 pull 失败日志")
	}
	if m := readMarker(t, donePath); m.ExitCode != 1 {
		t.Errorf("marker exit_code = %d, want 1", m.ExitCode)
	}
}

// TestPipelineRollbackSurvivesCanceledCtx 回滚阶段脱离更新看门狗：即使 ctx 已取消，回滚照常执行。
func TestPipelineRollbackSurvivesCanceledCtx(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:      defaultRev(rev, "oldsha", "newsha"),
		diff:     "go-server/main.go\n",
		unhealth: map[string]bool{"server": true},
		onPull:   func() { rev.after = true },
	}
	p, fake, _, _ := newTestPipeline(t, func(c *UpdateConfig) {
		c.RollbackTimeout = 30 // 秒，足够
	}, h)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 模拟看门狗已到点取消更新阶段

	if code := p.Run(ctx); code != 130 {
		t.Fatalf("Run = %d, want 130（取消时按 130 退出）", code)
	}
	calls := fake.callsSnapshot()
	if !containsCall(calls, "git", "checkout", "oldsha") {
		t.Errorf("ctx 取消后回滚仍应执行: %v", callNames(calls))
	}
}

// TestPipelineNoCodeChangeEarlyExitAtStep3 pull 无新提交（before==after）→ 步骤 3 提前退出。
func TestPipelineNoCodeChangeEarlyExitAtStep3(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:    defaultRev(rev, "oldsha", "oldsha"),
		diff:   "go-server/main.go\n",
		onPull: func() { rev.after = true },
	}
	p, _, logPath, _ := newTestPipeline(t, nil, h)

	if code := p.Run(context.Background()); code != 0 {
		t.Fatalf("Run = %d, want 0", code)
	}
	data, _ := os.ReadFile(logPath)
	if !strings.Contains(string(data), "代码无变更，跳过更新") {
		t.Error("缺少代码无变更日志")
	}
}

// ---------- helpers ----------

func hasTitle(titles []string, want string) bool {
	for _, t := range titles {
		if t == want {
			return true
		}
	}
	return false
}

func stepOrder(t *testing.T, logPath string) []int {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	var order []int
	for _, ln := range strings.Split(string(data), "\n") {
		if m := stepRe.FindStringSubmatch(ln); m != nil {
			n, _ := strconv.Atoi(m[1])
			order = append(order, n)
		}
	}
	return order
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsCall(calls []Call, name string, substrings ...string) bool {
	for _, c := range calls {
		if c.Name != name {
			continue
		}
		ok := true
		for _, s := range substrings {
			if !hasArg(c.Args, s) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func callNames(calls []Call) []string {
	var out []string
	for _, c := range calls {
		out = append(out, c.Name+" "+strings.Join(c.Args, " "))
	}
	return out
}

func readMarker(t *testing.T, path string) DoneMarker {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	var m DoneMarker
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse marker: %v", err)
	}
	return m
}

// TestDiskFreeKB 解析 df -Pk 输出（busybox 兼容格式），验证可用空间取第 4 列。
func TestDiskFreeKB(t *testing.T) {
	fake := &fakeCmdRunner{fn: func(c Call) (string, string, error) {
		return "Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/root 1000000 500000 2097151 50% /opt\n", "", nil
	}}
	p := &Pipeline{cfg: &UpdateConfig{RepoRoot: "/opt"}, cmds: fake}
	avail, err := p.diskFreeKB(context.Background())
	if err != nil {
		t.Fatalf("diskFreeKB: %v", err)
	}
	if avail != 2097151 {
		t.Errorf("avail = %d, want 2097151", avail)
	}
}

// TestDiskFreeKBBadFormat df 输出异常时返回错误而非误读。
func TestDiskFreeKBBadFormat(t *testing.T) {
	fake := &fakeCmdRunner{fn: func(c Call) (string, string, error) {
		return "no fields\n", "", nil
	}}
	p := &Pipeline{cfg: &UpdateConfig{RepoRoot: "/opt"}, cmds: fake}
	if _, err := p.diskFreeKB(context.Background()); err == nil {
		t.Error("expected error for malformed df output")
	}
}

// TestPipelineMigrateWithoutBackupAborts 含迁移变更但备份失败 → 步骤 4 中止，不跑迁移。
func TestPipelineMigrateWithoutBackupAborts(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:     defaultRev(rev, "oldsha", "newsha"),
		diff:    "migrations/002_alter.sql\n",
		dumpErr: os.ErrPermission,
		onPull:  func() { rev.after = true },
	}
	p, fake, logPath, donePath := newTestPipeline(t, nil, h)

	if code := p.Run(context.Background()); code != 1 {
		t.Fatalf("Run = %d, want 1（含迁移但无备份必须中止）", code)
	}
	calls := fake.callsSnapshot()
	if containsCall(calls, "docker", "compose", "run", "--rm", "migrate") {
		t.Errorf("备份缺失时不应执行迁移: %v", callNames(calls))
	}
	data, _ := os.ReadFile(logPath)
	if !strings.Contains(string(data), "备份失败/缺失") {
		t.Error("缺少备份缺失中止日志")
	}
	if m := readMarker(t, donePath); m.ExitCode != 1 {
		t.Errorf("marker exit_code = %d, want 1", m.ExitCode)
	}
}
