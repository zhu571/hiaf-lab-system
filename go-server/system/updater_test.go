package system

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// pipelineHandler 是 updater 测试用的命令处理器：按命令分派，字段可覆盖各失败注入点。
type pipelineHandler struct {
	rev  func() string
	diff string
	// pullErr/fetchErr + stderr 文本：git 环节失败注入（fetch 失败是历史盲区，R3/R16）。
	pullErr     error
	fetchErr    error
	fetchStderr string
	migrateErr  error
	dumpErr     error
	buildErr    map[string]error
	// buildOut：构建命令的模拟 stdout（R3 行流转发测试）。
	buildOut string
	unhealth map[string]bool
	// flipHealthyAt：第 N 次 compose ps 之后全部转健康（R14 慢启动重试测试；0=不翻转）。
	flipHealthyAt int
	psCount       int
	images        string
	projects      string
	services      string
	// origin：git remote get-url origin 的返回（空 = 非 SSH 场景，跳过凭据自检）。
	origin string
	// sshOK/sshStderr：git ls-remote 凭据自检结果。
	sshOK     bool
	sshStderr string
	// missingImages：docker image inspect 报缺失的镜像（R7 base 镜像预检）。
	missingImages map[string]bool
	// schemaVersion：psql 查询 schema_migrations 的模拟返回（R9）。
	schemaVersion string
	// embedMissing：server 容器内前端产物缺失（R9 白屏回归检测）。
	embedMissing bool
	// repoRootInvalid：git rev-parse --git-dir 失败（R6 仓库路径错配）。
	repoRootInvalid bool
	onPull          func()
	ntfyTitles      []string
}

func (h *pipelineHandler) handle(c Call) (string, string, error) {
	switch c.Name {
	case "df":
		return "Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/root 12345678 1234567 1234567890 1% /opt\n", "", nil
	case "git":
		return h.git(c)
	case "ssh":
		return h.ssh()
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
		if hasArg(args, "--git-dir") && h.repoRootInvalid {
			return "", "fatal: not a git repository", errors.New("exit status 128")
		}
		return h.rev() + "\n", "", nil
	case hasArg(args, "remote"):
		if h.origin == "" {
			return "\n", "", nil
		}
		return h.origin + "\n", "", nil
	case hasArg(args, "ls-remote"):
		if h.sshOK {
			return "newsha\tHEAD\n", "", nil
		}
		stderr := h.sshStderr
		if stderr == "" {
			stderr = "git@github.com: Permission denied (publickey)."
		}
		return "", stderr, errors.New("exit status 128")
	case hasArg(args, "fetch"):
		if h.fetchErr != nil {
			return "", h.fetchStderr, h.fetchErr
		}
		return "", "", nil
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
	default: // checkout / reset
		return "", "", nil
	}
}

// ssh 模拟 `ssh -o BatchMode=yes -T git@github.com`：认证成功与失败退出码都是 1，
// 判定依据是 stderr 报文（与实现一致）。
func (h *pipelineHandler) ssh() (string, string, error) {
	err := errors.New("exit status 1")
	if h.sshOK {
		return "", "Hi deploy! You've successfully authenticated, but GitHub does not provide shell access.", err
	}
	return "", "git@github.com: Permission denied (publickey).", err
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
	case hasArg(args, "image") && hasArg(args, "inspect"):
		img := args[len(args)-1]
		if h.missingImages[img] {
			return "", "Error response from daemon: No such image: " + img, errors.New("exit status 1")
		}
		return "[]\n", "", nil
	case hasArg(args, "pull"):
		// R7：预检对本地缺失的 base 镜像先尝试拉取；镜像源故障（403）时拉取也失败
		img := args[len(args)-1]
		if h.missingImages[img] {
			return "", "Error response from daemon: pull access denied for " + img + " (docker.m.daocloud.io 403 Forbidden)", errors.New("exit status 1")
		}
		return "", "", nil
	case hasArg(args, "config") && hasArg(args, "--services"):
		if h.services == "" {
			// 默认与硬编码列表（healthCheckServices ∪ fullServiceList）一致：无漂移告警
			h.services = "postgres\nmigrate\nepics-gateway\ninfluxdb\ngrafana\nioc\nserver\npy-agent\npy-agent-interpret\nntfy\n"
		}
		return h.services, "", nil
	case hasArg(args, "config") && hasArg(args, "--images"):
		if h.images == "" {
			h.images = "repo-server\n"
		}
		return h.images, "", nil
	case hasArg(args, "ps"):
		svc := args[len(args)-1]
		h.psCount++
		if h.unhealth[svc] && (h.flipHealthyAt == 0 || h.psCount < h.flipHealthyAt) {
			return `{"Name":"lab-` + svc + `","Service":"` + svc + `","State":"exited","Health":"unhealthy"}`, "", nil
		}
		return `{"Name":"lab-` + svc + `","Service":"` + svc + `","State":"running","Health":"healthy"}`, "", nil
	case hasArg(args, "exec"):
		if hasArg(args, "pg_dump") {
			if h.dumpErr != nil {
				return "", "pg_dump failed", h.dumpErr
			}
			return "DUMPDATA\n", "", nil
		}
		if hasArg(args, "psql") {
			if h.schemaVersion != "" {
				return h.schemaVersion + "\n", "", nil
			}
			return "\n", "", nil
		}
		// 其余 exec = server 容器内前端产物核对（sh -c ls）
		if h.embedMissing {
			return "", "", errors.New("exit status 1")
		}
		return "/app/static/assets/index-embed01.js\n", "", nil
	case hasArg(args, "run") && hasArg(args, "migrate"):
		if h.migrateErr != nil {
			return "", "migrate failed", h.migrateErr
		}
		return "", "", nil
	case hasArg(args, "build"):
		if svc := buildService(args); svc != "" && h.buildErr[svc] != nil {
			return h.buildOut, "failed to solve: docker.m.daocloud.io 403 Forbidden", h.buildErr[svc]
		}
		if h.buildOut != "" {
			return h.buildOut, "", nil
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
	p := NewPipeline(cfg, fake, logger)
	p.healthRetryDelay = 0 // 测试关闭健康重试等待（R14），失败用例不被 3×5s 拖慢
	return p, fake, logPath, donePath
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

// TestPipelineBuildFailureNoRollback 构建失败不回滚服务（尚未重建任何容器），
// 但仓库必须退回 OLD_SHA 并回到分支（R4：防下次更新「代码无变更」假成功空转）。
func TestPipelineBuildFailureNoRollback(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:      defaultRev(rev, "oldsha", "newsha"),
		diff:     "go-server/main.go\n",
		buildErr: map[string]error{"server": context.DeadlineExceeded},
		onPull:   func() { rev.after = true },
	}
	p, fake, logPath, donePath := newTestPipeline(t, nil, h)

	if code := p.Run(context.Background()); code != 1 {
		t.Fatalf("Run = %d, want 1", code)
	}
	calls := fake.callsSnapshot()
	if containsCall(calls, "git", "checkout", "oldsha") {
		t.Error("构建失败不应走回滚分支（checkout oldsha）")
	}
	if hasTitle(h.ntfyTitles, "系统更新失败-已回滚") {
		t.Error("构建失败不应发已回滚通知")
	}
	// R4：仓库退回 OLD_SHA（checkout 分支 + reset --hard），HEAD 与运行镜像保持一致
	if !containsCall(calls, "git", "checkout", "main") {
		t.Errorf("构建失败应 checkout 分支准备回退: %v", callNames(calls))
	}
	if !containsCall(calls, "git", "reset", "--hard", "oldsha") {
		t.Errorf("构建失败应 reset --hard 回 OLD_SHA: %v", callNames(calls))
	}
	if containsCall(calls, "docker", "compose", "up", "-d") {
		t.Errorf("构建失败不应重启任何服务: %v", callNames(calls))
	}
	data, _ := os.ReadFile(logPath)
	if !strings.Contains(string(data), "仓库回退") {
		t.Error("缺少仓库回退日志")
	}
	data, _ = os.ReadFile(donePath)
	var m DoneMarker
	_ = json.Unmarshal(data, &m)
	if m.ExitCode != 1 {
		t.Errorf("marker exit_code = %d, want 1", m.ExitCode)
	}
	// 构建失败不得落已部署标记（deployed-sha 不存在）
	if _, err := os.Stat(deployedSHAFile(p.cfg.RepoRoot)); !os.IsNotExist(err) {
		t.Errorf("构建失败不应写 deployed-sha 标记")
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
	// 通知迁移阻塞 + 仓库回 main 分支并 reset --hard 停在 OLD_SHA（与运行的旧镜像一致）
	if !hasTitle(h.ntfyTitles, "系统更新失败-迁移变更阻塞") {
		t.Errorf("缺少迁移阻塞通知: %v", h.ntfyTitles)
	}
	if !containsCall(calls, "git", "checkout", "main") {
		t.Errorf("阻塞分支应 git checkout main: %v", callNames(calls))
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
	if !containsCall(calls, "git", "checkout", "main") {
		t.Errorf("回滚完成应 git checkout main（回到分支）: %v", callNames(calls))
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

// TestPipelineNoChangesEarlyExit 变更提交但文件 diff 为空、且已部署标记与 HEAD
// 一致 → 步骤 4 提前成功退出（标记缺失时按 R4 严格语义走全量对齐重建，见
// TestPipelineMissingDeployedMarkerRebuilds）。
func TestPipelineNoChangesEarlyExit(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:    defaultRev(rev, "oldsha", "newsha"),
		diff:   "",
		onPull: func() { rev.after = true },
	}
	p, fake, logPath, donePath := newTestPipeline(t, nil, h)
	if err := writeDeployedSHA(p.cfg.RepoRoot, "newsha"); err != nil {
		t.Fatalf("预置 deployed-sha: %v", err)
	}

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
	if err := writeDeployedSHA(p.cfg.RepoRoot, "oldsha"); err != nil {
		t.Fatalf("预置 deployed-sha: %v", err)
	}

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

// ---------- R4：deployed-sha 部署事实源 ----------

// TestPipelineSuccessWritesDeployedMarker 成功完成后落 deployed-sha 标记（R4）。
func TestPipelineSuccessWritesDeployedMarker(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:    defaultRev(rev, "oldsha", "newsha"),
		diff:   "go-server/main.go\n",
		onPull: func() { rev.after = true },
	}
	p, _, _, _ := newTestPipeline(t, nil, h)

	if code := p.Run(context.Background()); code != 0 {
		t.Fatalf("Run = %d, want 0", code)
	}
	sha, ok := readDeployedSHA(p.cfg.RepoRoot)
	if !ok || sha != "newsha" {
		t.Errorf("deployed-sha = %q ok=%v, want newsha", sha, ok)
	}
}

// TestPipelineNoChangeAlignedSkips HEAD 与 deployed-sha 一致 → 正常跳过（既有行为保留）。
func TestPipelineNoChangeAlignedSkips(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:  defaultRev(rev, "oldsha", "oldsha"),
		diff: "",
	}
	p, fake, logPath, _ := newTestPipeline(t, nil, h)
	if err := writeDeployedSHA(p.cfg.RepoRoot, "oldsha"); err != nil {
		t.Fatalf("预置 deployed-sha: %v", err)
	}

	if code := p.Run(context.Background()); code != 0 {
		t.Fatalf("Run = %d, want 0", code)
	}
	data, _ := os.ReadFile(logPath)
	if !strings.Contains(string(data), "代码无变更，跳过更新") {
		t.Error("对齐状态下应正常跳过")
	}
	if containsCall(fake.callsSnapshot(), "docker", "compose", "build") {
		t.Error("对齐状态下不应构建")
	}
}

// TestPipelineNoChangeDeployedStaleRebuilds R4 核心场景：build 曾失败（HEAD 前进、
// 镜像未重建，deployed-sha 停在旧版本）→ 再次更新不得假成功跳过，必须全量对齐重建。
func TestPipelineNoChangeDeployedStaleRebuilds(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:  defaultRev(rev, "oldsha", "oldsha"),
		diff: "",
	}
	p, fake, logPath, _ := newTestPipeline(t, nil, h)
	if err := writeDeployedSHA(p.cfg.RepoRoot, "stale0sha"); err != nil {
		t.Fatalf("预置 deployed-sha: %v", err)
	}

	if code := p.Run(context.Background()); code != 0 {
		t.Fatalf("Run = %d, want 0", code)
	}
	data, _ := os.ReadFile(logPath)
	if strings.Contains(string(data), "代码无变更，跳过更新") || strings.Contains(string(data), "无文件变更，跳过构建") {
		t.Error("部署状态不一致时不得假成功跳过")
	}
	if !strings.Contains(string(data), "HEAD 与已部署版本不一致") {
		t.Error("缺少部署状态不一致告警")
	}
	if !containsCall(fake.callsSnapshot(), "docker", "compose", "build", "--pull") {
		t.Errorf("应全量对齐重建: %v", callNames(fake.callsSnapshot()))
	}
	// 对齐重建成功后标记应更新到当前 HEAD
	if sha, ok := readDeployedSHA(p.cfg.RepoRoot); !ok || sha != "oldsha" {
		t.Errorf("重建后 deployed-sha = %q ok=%v, want oldsha", sha, ok)
	}
}

// TestPipelineMissingDeployedMarkerRebuilds R4 反例实测：手动删掉 deployed-sha
// 标记再触发 → 不得「跳过 + 重写标记」把部署状态永久掩盖，必须全量对齐重建
// 自愈，成功后标记重新落盘（严格语义：标记缺失 ≠ 已对齐）。
func TestPipelineMissingDeployedMarkerRebuilds(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:  defaultRev(rev, "oldsha", "oldsha"),
		diff: "",
	}
	p, fake, logPath, _ := newTestPipeline(t, nil, h)
	// 不预置 deployed-sha（等价于手动删除）

	if code := p.Run(context.Background()); code != 0 {
		t.Fatalf("Run = %d, want 0", code)
	}
	data, _ := os.ReadFile(logPath)
	if strings.Contains(string(data), "代码无变更，跳过更新") || strings.Contains(string(data), "无文件变更，跳过构建") {
		t.Error("标记缺失时不得假成功跳过（会把部署状态永久掩盖成空转）")
	}
	if !strings.Contains(string(data), "HEAD 与已部署版本不一致") {
		t.Error("缺少部署状态不一致告警")
	}
	if !containsCall(fake.callsSnapshot(), "docker", "compose", "build", "--pull") {
		t.Errorf("标记缺失应全量对齐重建: %v", callNames(fake.callsSnapshot()))
	}
	// 自愈闭环：重建成功后标记恢复
	if sha, ok := readDeployedSHA(p.cfg.RepoRoot); !ok || sha != "oldsha" {
		t.Errorf("重建后 deployed-sha = %q ok=%v, want oldsha", sha, ok)
	}
}

// ---------- R5：migrate 先于业务容器重启 ----------

// TestPipelineMigrateRunsBeforeServiceRestarts 断言步骤 6 内部顺序：migrate 运行
// 必须先于任何业务容器的 up -d 重启（postgres 检查/重启不算业务容器）。
func TestPipelineMigrateRunsBeforeServiceRestarts(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:    defaultRev(rev, "oldsha", "newsha"),
		diff:   "migrations/003_x.up.sql\npy-agent/worker.py\n",
		onPull: func() { rev.after = true },
	}
	p, fake, _, _ := newTestPipeline(t, nil, h)

	if code := p.Run(context.Background()); code != 0 {
		t.Fatalf("Run = %d, want 0", code)
	}
	calls := fake.callsSnapshot()
	migrateIdx, upIdx := -1, -1
	for i, c := range calls {
		if c.Name != "docker" {
			continue
		}
		if hasArg(c.Args, "run") && hasArg(c.Args, "migrate") {
			migrateIdx = i
		}
		if upIdx == -1 && hasArg(c.Args, "up") && hasArg(c.Args, "-d") && !hasArg(c.Args, "postgres") {
			upIdx = i
		}
	}
	if migrateIdx < 0 {
		t.Fatalf("缺少迁移调用: %v", callNames(calls))
	}
	if upIdx < 0 {
		t.Fatalf("缺少服务重启调用: %v", callNames(calls))
	}
	if migrateIdx > upIdx {
		t.Errorf("migrate(第 %d 次调用) 必须先于业务容器重启(第 %d 次)，I4 契约", migrateIdx, upIdx)
	}
}

// ---------- R3：失败原因透传 / 行流转发 ----------

// TestPipelineFetchFailureLogsStderrTail git fetch 失败时 stderr 尾部落进会话日志
// （历史盲区：fake 里 fetch 恒成功；真实事故「Host key verification failed」在 SSE 里蒸发）。
func TestPipelineFetchFailureLogsStderrTail(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:         defaultRev(rev, "oldsha", "oldsha"),
		fetchErr:    errors.New("exit status 128"),
		fetchStderr: "Host key verification failed.\nfatal: Could not read from remote repository.",
	}
	p, _, logPath, donePath := newTestPipeline(t, nil, h)

	if code := p.Run(context.Background()); code != 1 {
		t.Fatalf("Run = %d, want 1", code)
	}
	data, _ := os.ReadFile(logPath)
	for _, want := range []string{"git fetch 失败", "输出尾部", "Host key verification failed", "Could not read from remote repository"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("日志缺少 %q", want)
		}
	}
	if m := readMarker(t, donePath); m.ExitCode != 1 {
		t.Errorf("marker exit_code = %d, want 1", m.ExitCode)
	}
}

// TestPipelineBuildStreamsOutputAndFailureReason 构建输出按行流转发（R3）：
// 成功时进度行实时可见；失败时 daocloud 403 等真实原因随行流落日志，不再是裸 exit status 1。
func TestPipelineBuildStreamsOutputAndFailureReason(t *testing.T) {
	t.Run("success streams build output", func(t *testing.T) {
		rev := &struct{ after bool }{}
		h := &pipelineHandler{
			rev:      defaultRev(rev, "oldsha", "newsha"),
			diff:     "go-server/main.go\n",
			buildOut: "[+] Building 12.3s (8/12)\n => [internal] load metadata for docker.m.daocloud.io\n",
			onPull:   func() { rev.after = true },
		}
		p, _, logPath, _ := newTestPipeline(t, nil, h)
		if code := p.Run(context.Background()); code != 0 {
			t.Fatalf("Run = %d, want 0", code)
		}
		data, _ := os.ReadFile(logPath)
		if !strings.Contains(string(data), "  | [+] Building 12.3s") {
			t.Error("构建输出应按行流写入会话日志")
		}
	})
	t.Run("failure surfaces stderr detail", func(t *testing.T) {
		rev := &struct{ after bool }{}
		h := &pipelineHandler{
			rev:      defaultRev(rev, "oldsha", "newsha"),
			diff:     "go-server/main.go\n",
			buildErr: map[string]error{"server": errors.New("exit status 1")},
			onPull:   func() { rev.after = true },
		}
		p, _, logPath, _ := newTestPipeline(t, nil, h)
		if code := p.Run(context.Background()); code != 1 {
			t.Fatalf("Run = %d, want 1", code)
		}
		data, _ := os.ReadFile(logPath)
		if !strings.Contains(string(data), "docker.m.daocloud.io 403 Forbidden") {
			t.Error("构建失败的镜像源 403 细节应随行流可见（R3/R7 事故根因）")
		}
	})
}

// ---------- R2：SSH 凭据预检 ----------

// TestPipelineSSHPrecheckFailsFast origin 为 SSH 且 ls-remote 报凭据不可用 → 步骤 1 就以可操作
// 报错中止（而不是步骤 3 的裸 exit 128）。
func TestPipelineSSHPrecheckFailsFast(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:    defaultRev(rev, "oldsha", "newsha"),
		diff:   "go-server/main.go\n",
		origin: "git@github.com:zhu571/hiaf-lab-system.git",
		sshOK:  false,
	}
	p, fake, logPath, _ := newTestPipeline(t, nil, h)

	if code := p.Run(context.Background()); code != 1 {
		t.Fatalf("Run = %d, want 1", code)
	}
	data, _ := os.ReadFile(logPath)
	for _, want := range []string{"SSH 凭据自检失败", "Permission denied (publickey)", "runner-home/README.md", "步骤 1 失败"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("日志缺少 %q", want)
		}
	}
	if containsCall(fake.callsSnapshot(), "git", "fetch") {
		t.Error("凭据自检失败后不应再执行 git fetch")
	}
	for _, c := range fake.callsSnapshot() {
		if c.match("git", "ls-remote", "origin", "HEAD") && !c.hasDeadline {
			t.Error("git ls-remote 预检必须受单命令超时保护")
		}
		if c.Name == "ssh" {
			t.Error("预检不应直接调用 ssh")
		}
	}
}

// TestPipelineSSHPrecheckPasses git ls-remote 成功 → 流水线正常走完。
func TestPipelineSSHPrecheckPasses(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:    defaultRev(rev, "oldsha", "newsha"),
		diff:   "go-server/main.go\n",
		origin: "git@github.com:zhu571/hiaf-lab-system.git",
		sshOK:  true,
		onPull: func() { rev.after = true },
	}
	p, fake, logPath, _ := newTestPipeline(t, nil, h)

	if code := p.Run(context.Background()); code != 0 {
		t.Fatalf("Run = %d, want 0", code)
	}
	data, _ := os.ReadFile(logPath)
	if !strings.Contains(string(data), "SSH 凭据自检通过 (git@github.com)") {
		t.Error("缺少凭据自检通过日志")
	}
	calls := fake.callsSnapshot()
	if !containsCall(calls, "git", "ls-remote", "origin", "HEAD") {
		t.Error("凭据自检应执行 git ls-remote origin HEAD")
	}
	if containsCall(calls, "ssh") {
		t.Error("凭据自检不应直接调用 ssh")
	}
}

func TestPipelineSSHPrecheckNetworkErrorHasNoDeployKeyAdvice(t *testing.T) {
	h := &pipelineHandler{
		rev:       func() string { return "oldsha" },
		origin:    "git@github.com:zhu571/hiaf-lab-system.git",
		sshStderr: "ssh: Could not resolve hostname github.com: Temporary failure in name resolution",
	}
	p, _, logPath, _ := newTestPipeline(t, nil, h)
	if code := p.Run(context.Background()); code != 1 {
		t.Fatalf("Run = %d, want 1", code)
	}
	data, _ := os.ReadFile(logPath)
	if !strings.Contains(string(data), "请检查网络、DNS 与防火墙") {
		t.Error("网络失败应给出网络排查指引")
	}
	if strings.Contains(string(data), "runner-home/README.md") {
		t.Error("网络失败不应误报 deploy key 配置指引")
	}
}

// ---------- R6：仓库路径错配预检 ----------

// TestPipelinePreflightRepoRootMismatch RepoRoot 不是 git 仓库 → 步骤 1 报「仓库路径
// 错配」（compose 挂载漂移时 docker 自动建空目录，后续报「缺少 secret」是误导）。
func TestPipelinePreflightRepoRootMismatch(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:             defaultRev(rev, "oldsha", "newsha"),
		repoRootInvalid: true,
	}
	p, _, logPath, _ := newTestPipeline(t, nil, h)

	if code := p.Run(context.Background()); code != 1 {
		t.Fatalf("Run = %d, want 1", code)
	}
	data, _ := os.ReadFile(logPath)
	if !strings.Contains(string(data), "仓库路径错配") || !strings.Contains(string(data), "REPO_ROOT") {
		t.Errorf("缺少仓库路径错配报错: %s", data)
	}
}

// ---------- R7：base 镜像预检 ----------

// TestPipelineBaseImagePreflightAborts 构建前发现 base 镜像缺失（daocloud 403 拉不到）
// → 开头就报离线导入指引中止，不进入分钟级构建；仓库退回 OLD_SHA（R4）。
func TestPipelineBaseImagePreflightAborts(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:           defaultRev(rev, "oldsha", "newsha"),
		diff:          "deploy/Dockerfile.migrate\n",
		missingImages: map[string]bool{"migrate/migrate:v4.17.0": true},
		onPull:        func() { rev.after = true },
	}
	p, fake, logPath, _ := newTestPipeline(t, nil, h)
	// 在测试仓库内造最小 compose + Dockerfile，让预检能解析出 base 镜像
	deployDir := filepath.Join(p.cfg.RepoRoot, "deploy")
	if err := os.MkdirAll(deployDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deployDir, "docker-compose.yml"),
		[]byte("services:\n  migrate:\n    build:\n      context: ..\n      dockerfile: deploy/Dockerfile.migrate\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deployDir, "Dockerfile.migrate"),
		[]byte("FROM migrate/migrate:v4.17.0 AS migrate\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := p.Run(context.Background()); code != 1 {
		t.Fatalf("Run = %d, want 1", code)
	}
	data, _ := os.ReadFile(logPath)
	for _, want := range []string{"基础镜像缺失", "migrate/migrate:v4.17.0", "deploy/scripts/README.md", "离线导入"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("日志缺少 %q", want)
		}
	}
	calls := fake.callsSnapshot()
	// R7：本地缺失先尝试 docker pull（在线环境不误伤），拉取失败（403）才判缺失
	if !containsCall(calls, "docker", "pull", "migrate/migrate:v4.17.0") {
		t.Errorf("缺失镜像应先尝试拉取: %v", callNames(calls))
	}
	if !strings.Contains(string(data), "pull access denied") {
		t.Error("拉取失败原因（403）应随行流可见")
	}
	if containsCall(calls, "docker", "compose", "build") {
		t.Error("base 镜像缺失时不应进入构建")
	}
	if !containsCall(calls, "git", "reset", "--hard", "oldsha") {
		t.Errorf("预检失败也应退回 OLD_SHA（R4）: %v", callNames(calls))
	}
}

// TestParseComposeServicesGolden 以真实 deploy/docker-compose.yml 为 golden：
// 解析出全部服务的 build/image 定义，且 baseImages 覆盖全部 FROM。
func TestParseComposeServicesGolden(t *testing.T) {
	composePath := "../../deploy/docker-compose.yml"
	data, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("read compose: %v", err)
	}
	specs := parseComposeServices(string(data))
	byName := map[string]composeServiceSpec{}
	for _, s := range specs {
		byName[s.Service] = s
	}
	if len(specs) < 10 {
		t.Errorf("解析服务数 = %d, want >= 10", len(specs))
	}
	pg := byName["postgres"]
	if pg.HasBuild || pg.Image != "postgres:16-alpine" {
		t.Errorf("postgres = %+v", pg)
	}
	srv := byName["server"]
	if !srv.HasBuild || srv.Context != ".." || srv.Dockerfile != "deploy/Dockerfile" {
		t.Errorf("server build spec = %+v", srv)
	}
	ioc := byName["ioc"]
	if !ioc.HasBuild || ioc.Context != "../py-agent/ioc" || ioc.Dockerfile != "" {
		t.Errorf("ioc build spec = %+v", ioc)
	}
	eg := byName["epics-gateway"]
	if !eg.HasBuild || eg.Context != "../go-server/epics-gateway" {
		t.Errorf("epics-gateway build spec = %+v", eg)
	}
	pa := byName["py-agent"]
	if !pa.HasBuild || pa.Context != "../py-agent" || pa.Dockerfile != "Dockerfile" {
		t.Errorf("py-agent build spec = %+v", pa)
	}
	for _, want := range []string{"node:20-alpine", "golang:1.22-alpine", "alpine:3.20", "binwiederhier/ntfy:v2.27.0"} {
		if !sliceContains(srv.baseImages(composePath), want) {
			t.Errorf("server base 镜像缺少 %s: %v", want, srv.baseImages(composePath))
		}
	}
	mig := byName["migrate"].baseImages(composePath)
	for _, want := range []string{"migrate/migrate:v4.17.0", "python:3.12-alpine"} {
		if !sliceContains(mig, want) {
			t.Errorf("migrate base 缺少 %s: %v", want, mig)
		}
	}
	if !sliceContains(byName["ioc"].baseImages(composePath), "docker.m.daocloud.io/library/python:3.11-slim") {
		t.Errorf("ioc base 缺少 daocloud python（R7：镜像源 403 高危面）: %v", byName["ioc"].baseImages(composePath))
	}
	if !sliceContains(byName["epics-gateway"].baseImages(composePath), "python:3.11-slim") {
		t.Errorf("epics-gateway base 缺少 python:3.11-slim: %v", byName["epics-gateway"].baseImages(composePath))
	}
}

// TestDockerfileFromImages FROM 解析：多阶段、--platform 前缀、去重。
func TestDockerfileFromImages(t *testing.T) {
	text := "FROM node:20-alpine AS a\nFROM --platform=$BUILDPLATFORM golang:1.22-alpine AS b\nFROM alpine:3.20\nFROM alpine:3.20\n"
	got := dockerfileFromImages(text)
	want := []string{"node:20-alpine", "golang:1.22-alpine", "alpine:3.20"}
	if len(got) != len(want) {
		t.Fatalf("FROM 解析 = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("FROM[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

// ---------- R9：附加验证（schema 版本 / 前端 embed 产物） ----------

func writeTestMigrations(t *testing.T, repoRoot string, n int) {
	t.Helper()
	dir := filepath.Join(repoRoot, "migrations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= n; i++ {
		p := filepath.Join(dir, "00"+strconv.Itoa(i)+"_x.up.sql")
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestPipelineHealthSchemaMismatch schema 版本不匹配 → 计入不健康触发回滚（R9）。
func TestPipelineHealthSchemaMismatch(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:           defaultRev(rev, "oldsha", "newsha"),
		diff:          "go-server/main.go\n",
		schemaVersion: "1", // 期望 2
		onPull:        func() { rev.after = true },
	}
	p, fake, logPath, _ := newTestPipeline(t, nil, h)
	writeTestMigrations(t, p.cfg.RepoRoot, 2)

	if code := p.Run(context.Background()); code != 1 {
		t.Fatalf("Run = %d, want 1", code)
	}
	data, _ := os.ReadFile(logPath)
	if !strings.Contains(string(data), "schema 版本不匹配") || !strings.Contains(string(data), "期望 2，实际 1") {
		t.Errorf("缺少 schema 不匹配日志: %s", data)
	}
	if !containsCall(fake.callsSnapshot(), "git", "checkout", "oldsha") {
		t.Error("schema 不匹配应触发回滚")
	}
}

// TestPipelineHealthSchemaOK schema 版本一致 → 成功（防止附加验证误杀正常更新）。
func TestPipelineHealthSchemaOK(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:           defaultRev(rev, "oldsha", "newsha"),
		diff:          "go-server/main.go\n",
		schemaVersion: "2",
		onPull:        func() { rev.after = true },
	}
	p, _, logPath, _ := newTestPipeline(t, nil, h)
	writeTestMigrations(t, p.cfg.RepoRoot, 2)

	if code := p.Run(context.Background()); code != 0 {
		t.Fatalf("Run = %d, want 0", code)
	}
	data, _ := os.ReadFile(logPath)
	if !strings.Contains(string(data), "schema 版本 OK") {
		t.Error("缺少 schema OK 日志")
	}
}

// TestPipelineHealthEmbedMissing 容器内前端产物为空（白屏风险）→ 不健康触发回滚（R9）。
func TestPipelineHealthEmbedMissing(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:          defaultRev(rev, "oldsha", "newsha"),
		diff:         "go-server/main.go\n",
		embedMissing: true,
		onPull:       func() { rev.after = true },
	}
	p, fake, logPath, _ := newTestPipeline(t, nil, h)

	if code := p.Run(context.Background()); code != 1 {
		t.Fatalf("Run = %d, want 1", code)
	}
	data, _ := os.ReadFile(logPath)
	if !strings.Contains(string(data), "白屏") {
		t.Errorf("缺少白屏风险日志: %s", data)
	}
	if !containsCall(fake.callsSnapshot(), "git", "checkout", "oldsha") {
		t.Error("前端产物缺失应触发回滚")
	}
}

// TestPipelineHealthEmbedDrift 容器内产物与工作区 web-ui/dist 不一致 → 不健康（R9）。
func TestPipelineHealthEmbedDrift(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:    defaultRev(rev, "oldsha", "newsha"),
		diff:   "go-server/main.go\n",
		onPull: func() { rev.after = true },
	}
	p, _, logPath, _ := newTestPipeline(t, nil, h)
	dist := filepath.Join(p.cfg.RepoRoot, "web-ui", "dist", "assets")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "index-DIFFERENT.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := p.Run(context.Background()); code != 1 {
		t.Fatalf("Run = %d, want 1", code)
	}
	data, _ := os.ReadFile(logPath)
	if !strings.Contains(string(data), "前端产物不一致") || !strings.Contains(string(data), "index-DIFFERENT.js") {
		t.Errorf("缺少产物不一致日志: %s", data)
	}
}

// ---------- R14：健康检查重试窗口 ----------

// TestPipelineHealthRetryRecoversSlowService 慢启动服务（py-agent start_period）第一轮
// 不健康、第二轮恢复 → 不得误判触发回滚。
func TestPipelineHealthRetryRecoversSlowService(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:           defaultRev(rev, "oldsha", "newsha"),
		diff:          "go-server/main.go\n",
		unhealth:      map[string]bool{"py-agent": true},
		flipHealthyAt: 12, // 第 12 次 compose ps 起恢复（第一轮检查结束后）
		onPull:        func() { rev.after = true },
	}
	p, fake, logPath, _ := newTestPipeline(t, nil, h)

	if code := p.Run(context.Background()); code != 0 {
		t.Fatalf("Run = %d, want 0（慢启动应经重试恢复）", code)
	}
	data, _ := os.ReadFile(logPath)
	if !strings.Contains(string(data), "等待重试") {
		t.Error("缺少重试等待日志")
	}
	if containsCall(fake.callsSnapshot(), "git", "checkout", "oldsha") {
		t.Error("重试窗口内恢复不应回滚")
	}
}

// ---------- R15：compose 服务表差集告警 ----------

// TestPipelineWarnServicesDrift compose 服务表与硬编码列表双向差集 → WARN 不阻断。
func TestPipelineWarnServicesDrift(t *testing.T) {
	rev := &struct{ after bool }{}
	h := &pipelineHandler{
		rev:      defaultRev(rev, "oldsha", "oldsha"),
		diff:     "",
		services: "postgres\nserver\nnewsvc\n", // 缺 ioc 等硬编码项，多 newsvc
	}
	p, _, logPath, _ := newTestPipeline(t, nil, h)

	if code := p.Run(context.Background()); code != 0 {
		t.Fatalf("Run = %d, want 0（漂移只告警不阻断）", code)
	}
	data, _ := os.ReadFile(logPath)
	if !strings.Contains(string(data), `"newsvc" 不在更新系统硬编码列表内`) {
		t.Error("缺少新服务漂移告警")
	}
	if !strings.Contains(string(data), `"ioc" 不在 compose 服务表中`) {
		t.Error("缺少过期硬编码项告警")
	}
}

// ---------- 纯函数单测（R3 脱敏 / R2 主机解析） ----------

// TestSanitizeOutput URL 内嵌凭据与 Authorization 头脱敏（R3）。
func TestSanitizeOutput(t *testing.T) {
	in := "postgres://lab:secret123@host:5432/lab\nAuthorization: Bearer tok_abc123\nplain line"
	out := sanitizeOutput(in)
	if strings.Contains(out, "secret123") || strings.Contains(out, "tok_abc123") {
		t.Errorf("脱敏失败: %s", out)
	}
	if !strings.Contains(out, "lab:***@host") || !strings.Contains(out, "Bearer ***") {
		t.Errorf("脱敏后丢失结构: %s", out)
	}
	if !strings.Contains(out, "plain line") {
		t.Error("普通行不应被改写")
	}
}

// TestTailLines 尾部行提取：非空、保序、截断。
func TestTailLines(t *testing.T) {
	in := "l1\n\nl2\nl3\nl4\n"
	got := tailLines(in, 2)
	if len(got) != 2 || got[0] != "l3" || got[1] != "l4" {
		t.Errorf("tailLines = %v, want [l3 l4]", got)
	}
	if all := tailLines(in, 10); len(all) != 4 {
		t.Errorf("tailLines all = %v", all)
	}
}

// TestSshHostOf 从 git remote URL 提取 SSH 主机；https 返回空。
func TestSshHostOf(t *testing.T) {
	cases := map[string]string{
		"git@github.com:zhu571/hiaf-lab-system.git": "github.com",
		"ssh://git@github.com:22/zhu571/repo.git":   "github.com",
		"ssh://git@gitlab.example.com/repo.git":     "gitlab.example.com",
		"https://github.com/zhu571/repo.git":        "",
		"/local/path/repo.git":                      "",
	}
	for origin, want := range cases {
		if got := sshHostOf(origin); got != want {
			t.Errorf("sshHostOf(%q) = %q, want %q", origin, got, want)
		}
	}
}

// TestRunLoggedSanitizesRealStderr R3 反例实测（真子进程，非 fake）：失败命令的
// stderr 中部携带 Authorization: Bearer / URL 内嵌凭据，logCmdTail 落日志前必须
// 脱敏——脱敏作用于全量 stderr 后再截尾，token 在中间而非尾部也不泄漏。
func TestRunLoggedSanitizesRealStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("真子进程 sh 用例仅限 unix")
	}
	logPath := filepath.Join(t.TempDir(), "session.log")
	logger, err := NewLogger(logPath, nil)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	defer func() { _ = logger.Close() }()
	p := NewPipeline(&UpdateConfig{RepoRoot: "/r"}, NewExecRunner("", nil), logger)

	script := `{ echo "line before"; echo "Authorization: Bearer tok_REALSECRET123"; echo "db url postgres://lab:pw_REAL456@db:5432/lab"; echo "line after"; } >&2; exit 7`
	if _, err := p.runLogged(context.Background(), "sh", "-c", script); err == nil {
		t.Fatal("命令应失败（exit 7）")
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	got := string(data)
	if strings.Contains(got, "tok_REALSECRET123") || strings.Contains(got, "pw_REAL456") {
		t.Errorf("真实凭据泄漏进会话日志:\n%s", got)
	}
	if !strings.Contains(got, "Bearer ***") || !strings.Contains(got, "lab:***@db") {
		t.Errorf("脱敏后应保留结构（Bearer *** / lab:***@db）:\n%s", got)
	}
	if !strings.Contains(got, "line before") || !strings.Contains(got, "line after") {
		t.Errorf("非敏感行应保留:\n%s", got)
	}
}

func sliceContains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
