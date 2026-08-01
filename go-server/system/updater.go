package system

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// UpdateConfig 是 7 步流水线的运行配置，由 cmd/update-runner 从 flag/env 组装。
type UpdateConfig struct {
	RepoRoot    string // 仓库根目录（git -C、compose -f 的基准）
	ComposeFile string // 相对仓库的 compose 文件，默认 deploy/docker-compose.yml
	ProjectName string // compose 项目名，默认 deploy（compose 文件所在目录名）
	SessionID   string
	LogFile     string // 会话日志文件
	DoneFile    string // done marker 文件
	NtfyURL     string // ntfy 通知地址（为空则不发通知）
	BackupDir   string // DB 备份共享目录（为空则用仓库内 .hermes/backups）

	// RunnerImage 为空时在预检里用 `compose config --images server` 解析。
	RunnerImage string

	UpdateTimeout   time.Duration // 更新阶段看门狗
	RollbackTimeout time.Duration // 回滚阶段独立预算

	Force      bool
	DryRun     bool
	NoRollback bool
}

// composeAbs 返回 compose 文件绝对路径。
func (c *UpdateConfig) composeAbs() string {
	if filepath.IsAbs(c.ComposeFile) {
		return c.ComposeFile
	}
	return filepath.Join(c.RepoRoot, c.ComposeFile)
}

// project 返回 compose 项目名，空时回退到 compose 文件所在目录名。
func (c *UpdateConfig) project() string {
	if c.ProjectName != "" {
		return c.ProjectName
	}
	base := filepath.Base(filepath.Dir(c.composeAbs()))
	if base == "." || base == string(filepath.Separator) || base == "" {
		return "deploy"
	}
	return base
}

// Step 是流水线的一个步骤，标题文本必须与 update.sh 完全一致（I2 契约）。
// 回滚触发点/迁移阻塞语义以元数据落表，由通用执行器统一分派。
type Step struct {
	Num   int
	Title string
	Run   func(ctx context.Context) error
	// RollbackOn 表示该步失败是否触发回滚（§6.1：构建失败与 git pull 失败不回滚）。
	RollbackOn bool
	// SkipWhen 可选：返回 true 时跳过本步（迁移仅在受影响时执行由步骤内部判断，此处保留能力）。
	SkipWhen func(a *Affected) bool
}

// errNoChanges 表示流水线在 step 3/4 检测到无变更，应静默成功退出（不回滚）。
var errNoChanges = errors.New("pipeline-stop: no changes")

// healthCheckServices 是全栈健康检查的服务列表（与 update.sh 一致）。
var healthCheckServices = []string{
	"postgres", "influxdb", "grafana", "ntfy",
	"epics-gateway", "ioc", "py-agent-interpret", "server", "py-agent",
}

// fullServiceList 是全量构建/回滚的服务列表（与 update.sh ALL 分支一致）。
var fullServiceList = []string{
	"server", "py-agent", "py-agent-interpret", "epics-gateway", "ioc", "migrate",
}

// stepTitles 供 contract_test 断言 I2：7 步标题与 stepRe 可解析且顺序/总数正确。
var stepTitles = []string{"预检", "记录当前状态", "git pull", "变更检测", "构建镜像", "滚动更新", "全栈健康检查"}

// Pipeline 是 7 步流水线执行器：纯逻辑、不依赖 Service，可被 cmd/update-runner 与测试复用。
type Pipeline struct {
	cfg        *UpdateConfig
	cmds       CmdRunner
	log        *Logger
	oldSHA     string
	newSHA     string
	backupFile string
	affected   *Affected
}

// NewPipeline 构造流水线。
func NewPipeline(cfg *UpdateConfig, cmds CmdRunner, log *Logger) *Pipeline {
	return &Pipeline{cfg: cfg, cmds: cmds, log: log}
}

// steps 返回 7 步流水线（标题、失败语义、回滚策略都是数据）。
func (p *Pipeline) steps() []Step {
	return []Step{
		{Num: 1, Title: "预检", Run: p.stepPreflight},
		{Num: 2, Title: "记录当前状态", Run: p.stepRecord},
		{Num: 3, Title: "git pull", Run: p.stepPull},
		{Num: 4, Title: "变更检测", Run: p.stepDetect},
		{Num: 5, Title: "构建镜像", Run: p.stepBuild},
		{Num: 6, Title: "滚动更新", Run: p.stepDeploy, RollbackOn: true},
		{Num: 7, Title: "全栈健康检查", Run: p.stepHealth, RollbackOn: true},
	}
}

// Run 执行完整流水线，返回进程退出码；defer 内写 done marker（I3 契约）。
func (p *Pipeline) Run(ctx context.Context) (code int) {
	defer func() {
		m := DoneMarker{ExitCode: code, OldSHA: p.oldSHA, NewSHA: p.newSHA, EndedAt: nowUTC()}
		if err := WriteDoneMarker(p.cfg.DoneFile, m); err != nil {
			p.log.Linef("[ERROR] 写 done marker 失败: %v", err)
		}
	}()

	p.log.Linef("[UPDATE] 开始更新 (session=%s, engine=go)", p.cfg.SessionID)

	updateCtx := ctx
	if p.cfg.UpdateTimeout > 0 {
		var cancel context.CancelFunc
		updateCtx, cancel = context.WithTimeout(ctx, p.cfg.UpdateTimeout)
		defer cancel()
	}

	for _, st := range p.steps() {
		if st.SkipWhen != nil && st.SkipWhen(p.affected) {
			continue
		}
		p.log.Linef("[UPDATE] ===== 步骤 %d/%d：%s =====", st.Num, len(p.steps()), st.Title)
		if err := st.Run(updateCtx); err != nil {
			if errors.Is(err, errNoChanges) {
				return 0
			}
			p.log.Linef("[ERROR] 步骤 %d 失败: %v", st.Num, err)
			if st.RollbackOn && !p.cfg.NoRollback {
				// 回滚用独立预算 + 脱离更新阶段看门狗，禁止回滚中途被 kill（§6.2）。
				rbCtx := context.Background()
				var rbcancel context.CancelFunc
				if p.cfg.RollbackTimeout > 0 {
					rbCtx, rbcancel = context.WithTimeout(context.WithoutCancel(ctx), p.cfg.RollbackTimeout)
				}
				p.rollback(rbCtx)
				if rbcancel != nil {
					rbcancel()
				}
			}
			if errors.Is(err, context.Canceled) {
				return 130
			}
			return 1
		}
	}
	return 0
}

// ---------- 步骤实现 ----------

// stepPreflight 步骤 1：docker/compose/git 可用、磁盘、secrets、runner 镜像、项目名断言。
func (p *Pipeline) stepPreflight(ctx context.Context) error {
	if _, _, err := p.cmds.Run(ctx, "docker", "version"); err != nil {
		return errors.New("Docker 未安装或不在 PATH")
	}
	if _, _, err := p.cmds.Run(ctx, "docker", "compose", "version"); err != nil {
		return errors.New("docker compose (v2) 不可用")
	}
	if _, _, err := p.cmds.Run(ctx, "git", "-C", p.cfg.RepoRoot, "version"); err != nil {
		return errors.New("git 不可用")
	}

	if avail, err := p.diskFreeKB(ctx); err == nil && avail < 2097152 {
		p.log.Linef("[WARN]  磁盘可用空间 < 2 GB，请清理后重试")
	}

	for _, sec := range []string{"db_password", "jwt_key", "influxdb_token", "agent_password"} {
		path := filepath.Join(p.cfg.RepoRoot, "deploy", "secrets", sec+".txt")
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("缺少 secret 文件: deploy/secrets/%s.txt", sec)
		}
	}

	if p.cfg.RunnerImage == "" {
		img, err := p.resolveRunnerImage(ctx)
		if err != nil {
			return fmt.Errorf("runner 镜像解析失败: %w", err)
		}
		p.cfg.RunnerImage = img
		p.log.Linef("[UPDATE] runner 镜像: %s", img)
	}

	p.assertProjectName(ctx)
	p.assertRepoWritable()
	return nil
}

// stepRecord 步骤 2：记录 OLD_SHA 并备份数据库。
func (p *Pipeline) stepRecord(ctx context.Context) error {
	p.oldSHA = p.gitRevParse(ctx, "HEAD")
	if p.oldSHA == "" {
		return errors.New("git rev-parse HEAD 失败")
	}
	p.log.Linef("[UPDATE] 当前 commit: %s", shortSHA(p.oldSHA))

	if p.cfg.DryRun {
		return nil
	}

	name := fmt.Sprintf("lab-db-backup-%s-%s.sql", shortSHA(p.oldSHA), time.Now().Format("20060102_150405"))
	dir := p.cfg.BackupDir
	if dir == "" {
		dir = filepath.Join(p.cfg.RepoRoot, ".hermes", "backups")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		p.log.Linef("[WARN]  备份目录不可写，跳过备份: %v", err)
		return nil
	}
	p.backupFile = filepath.Join(dir, name)
	p.log.Linef("[UPDATE] 备份数据库 → %s", p.backupFile)

	// pg_dump 输出流式写入备份文件，避免全量读进内存
	f, err := os.Create(p.backupFile)
	if err != nil {
		p.log.Linef("[WARN]  创建备份文件失败: %v", err)
		p.backupFile = ""
		return nil
	}
	if err := p.cmds.RunStream(ctx, f, "docker", p.composeArgs("exec", "-T", "postgres", "pg_dump", "-U", "lab", "lab")...); err != nil {
		_ = f.Close()
		p.log.Linef("[WARN]  数据库备份失败（postgres 未运行？），继续…")
		p.backupFile = ""
		return nil
	}
	if err := f.Close(); err != nil {
		p.log.Linef("[WARN]  写备份文件失败: %v", err)
		p.backupFile = ""
	}
	return nil
}

// stepPull 步骤 3：git fetch + pull --ff-only；失败时 --force 继续，否则中止（不回滚）。
func (p *Pipeline) stepPull(ctx context.Context) error {
	if _, _, err := p.cmds.Run(ctx, "git", "-C", p.cfg.RepoRoot, "fetch", "origin"); err != nil {
		return fmt.Errorf("git fetch 失败: %w", err)
	}
	before := p.gitRevParse(ctx, "HEAD")

	if _, _, err := p.cmds.Run(ctx, "git", "-C", p.cfg.RepoRoot, "pull", "--ff-only", "origin", "main"); err == nil {
		p.newSHA = p.gitRevParse(ctx, "HEAD")
		p.log.Linef("[UPDATE] 已更新: %s → %s", shortSHA(before), shortSHA(p.newSHA))
	} else if p.cfg.Force {
		p.log.Linef("[WARN]  git pull 失败，但 --force 模式继续（使用当前 HEAD）")
		p.newSHA = before
	} else {
		p.log.Linef("[ERROR] git pull 失败，中止。请手动解决冲突。")
		return errors.New("git pull 失败")
	}

	if before == p.newSHA && !p.cfg.Force {
		p.log.Linef("[UPDATE] 代码无变更，跳过更新。")
		return errNoChanges
	}
	return nil
}

// stepDetect 步骤 4：变更检测；dry-run / none 时提前成功退出。
func (p *Pipeline) stepDetect(ctx context.Context) error {
	changed := p.gitDiffNameOnly(ctx, p.oldSHA, p.newSHA)
	if len(changed) == 0 && !p.cfg.Force {
		p.log.Linef("[UPDATE] 无文件变更，跳过构建。")
		return errNoChanges
	}

	aff := DetectServices(changed)
	if p.cfg.Force {
		aff = Affected{All: true}
		p.log.Linef("[UPDATE] --force: 强制全量重建")
	}
	p.affected = &aff

	p.log.Linef("[UPDATE] 变更文件数: %d", len(changed))
	p.log.Linef("[UPDATE] 受影响服务: %s", aff.String())

	if p.cfg.DryRun {
		p.log.Linef("")
		p.log.Linef("===== DRY RUN 变更详情 =====")
		for _, f := range changed {
			p.log.Linef("  %s", f)
		}
		p.log.Linef("")
		p.log.Linef("受影响服务: %s", aff.String())
		p.log.Linef("===== DRY RUN 结束 =====")
		return errNoChanges
	}

	if aff.IsNone() {
		p.log.Linef("[UPDATE] 无服务需要更新。")
		return errNoChanges
	}

	// 迁移变更回滚会被阻塞（§6.1），没有备份就执行迁移风险不可接受 → 中止流水线。
	// dry-run 与无变更路径已在上方提前退出，不受影响。
	if p.affectedHas("migrate") && p.backupFile == "" {
		p.log.Linef("[ERROR] 本次更新含数据库迁移，但数据库备份失败/缺失，中止。请先解决备份问题再重试。")
		return errors.New("含迁移变更但无数据库备份")
	}
	return nil
}

// stepBuild 步骤 5：按依赖顺序构建（epics-gateway → ioc → py-agent-interpret → server；
// py-agent 与 py-agent-interpret 共用镜像，不重复构建）。
func (p *Pipeline) stepBuild(ctx context.Context) error {
	if p.affected.All {
		p.log.Linef("[WARN] compose 文件或 Dockerfile 变更，执行全量重建")
		_, _, err := p.cmds.Run(ctx, "docker", p.composeArgs("build", "--pull",
			"server", "py-agent", "py-agent-interpret", "epics-gateway", "ioc", "migrate")...)
		return err
	}

	for _, svc := range []string{"epics-gateway", "ioc", "py-agent-interpret"} {
		if p.affectedHas(svc) {
			p.log.Linef("[UPDATE] 构建 %s …", svc)
			if _, _, err := p.cmds.Run(ctx, "docker", p.composeArgs("build", svc)...); err != nil {
				return err
			}
		}
	}

	if p.affectedHas("server") {
		p.log.Linef("[UPDATE] 构建 server（含前端）…")
		if _, _, err := p.cmds.Run(ctx, "docker", p.composeArgs("build", "server")...); err != nil {
			return err
		}
	}

	if p.affectedHas("py-agent") {
		p.log.Linef("[UPDATE] py-agent 与 py-agent-interpret 共用镜像，已构建。")
	}
	return nil
}

// stepDeploy 步骤 6：滚动更新 + 迁移 + server/py-agent。
func (p *Pipeline) stepDeploy(ctx context.Context) error {
	restart := func(svc string, maxWait int) error {
		p.log.Linef("[UPDATE] 重启 %s …", svc)
		if _, _, err := p.cmds.Run(ctx, "docker", p.composeArgs("up", "-d", "--no-deps", svc)...); err != nil {
			return err
		}
		if err := p.sleep(ctx, 2*time.Second); err != nil {
			return err
		}
		for i := 1; i <= maxWait; i++ {
			state, err := p.serviceState(ctx, svc)
			if err == nil && (state == "healthy" || state == "running") {
				p.log.Linef("[UPDATE] %s 健康 (%ds)", svc, i*2)
				return nil
			}
			if err := p.sleep(ctx, 2*time.Second); err != nil {
				return err
			}
		}
		return fmt.Errorf("%s 健康检查超时", svc)
	}

	for _, svc := range []string{"epics-gateway", "ioc", "py-agent-interpret"} {
		if !p.affectedHas(svc) {
			continue
		}
		if err := restart(svc, 30); err != nil {
			p.log.Linef("[ERROR] %s 重启失败: %v", svc, err)
			return err
		}
	}

	if state, _ := p.serviceState(ctx, "postgres"); state != "healthy" {
		p.log.Linef("[WARN] postgres 不健康，尝试重启…")
		if _, _, err := p.cmds.Run(ctx, "docker", p.composeArgs("up", "-d", "postgres")...); err != nil {
			return err
		}
		if err := p.sleep(ctx, 5*time.Second); err != nil {
			return err
		}
	}

	if p.affectedHas("migrate") {
		p.log.Linef("[UPDATE] 运行数据库迁移 …")
		if _, _, err := p.cmds.Run(ctx, "docker", p.composeArgs("run", "--rm", "migrate")...); err != nil {
			p.log.Linef("[ERROR] 迁移失败！")
			return err
		}
		p.log.Linef("[UPDATE] 迁移成功")
	}

	if p.affectedHas("server") {
		if err := restart("server", 15); err != nil {
			p.log.Linef("[ERROR] server 启动失败，查看日志:")
			out, _, _ := p.cmds.Run(ctx, "docker", p.composeArgs("logs", "--tail", "50", "server")...)
			for _, ln := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
				if ln != "" {
					p.log.Linef("[server] %s", ln)
				}
			}
			return err
		}
	}

	if p.affectedHas("py-agent") {
		if err := restart("py-agent", 30); err != nil {
			p.log.Linef("[ERROR] py-agent 启动失败")
			return err
		}
	}
	return nil
}

// stepHealth 步骤 7：全栈健康检查。
func (p *Pipeline) stepHealth(ctx context.Context) error {
	allHealthy := true
	var bad []string
	for _, svc := range healthCheckServices {
		state, err := p.serviceState(ctx, svc)
		if err == nil && (state == "healthy" || state == "running") {
			p.log.Linef("  OK  %s: %s", svc, state)
			continue
		}
		if err != nil {
			p.log.Linef("  BAD %s: %s", svc, "missing")
		} else {
			p.log.Linef("  BAD %s: %s", svc, state)
		}
		allHealthy = false
		bad = append(bad, svc)
	}

	p.log.Linef("")
	if allHealthy {
		p.log.Linef("==========================================")
		p.log.Linef("  更新成功！%s → %s", shortSHA(p.oldSHA), shortSHA(p.newSHA))
		p.log.Linef("==========================================")
		p.notify(ctx, "系统更新成功", "default", "white_check_mark",
			fmt.Sprintf("Updated %s → %s", shortSHA(p.oldSHA), shortSHA(p.newSHA)))
		return nil
	}
	p.log.Linef("[ERROR] 部分服务不健康，请检查！")
	return fmt.Errorf("不健康服务: %v", bad)
}

// ---------- 回滚 ----------

// rollback 与 update.sh 的 rollback() 逐条对齐：git checkout OLD_SHA；
// 受影响含 migrate → 迁移阻塞分支（通知 + 手动回滚，不改代码/不重建）；否则重建受影响服务。
func (p *Pipeline) rollback(ctx context.Context) {
	p.log.Linef("")
	p.log.Linef("[WARN] ========== 开始回滚 ==========")

	if p.oldSHA == "" {
		p.log.Linef("[ERROR] 无旧 commit 记录，无法回滚。")
		return
	}
	oldShort := shortSHA(p.oldSHA)
	p.log.Linef("[UPDATE] 恢复代码到 %s …", oldShort)
	if _, _, err := p.cmds.Run(ctx, "git", "-C", p.cfg.RepoRoot, "checkout", p.oldSHA); err != nil {
		p.log.Linef("[ERROR] git checkout %s 失败: %v", p.oldSHA, err)
		return
	}

	if p.affected != nil && p.affected.Has("migrate") {
		p.log.Linef("[ERROR] ========== 迁移变更，回滚阻塞！ ==========")
		p.log.Linef("[ERROR] 数据库 schema 可能已被新迁移修改，自动回滚存在风险。")
		p.log.Linef("[ERROR] 请先手动回滚迁移，然后重新执行回滚或部署：")
		p.log.Linef("[ERROR]   1. 运行 migrate down 回退 schema")
		p.log.Linef("[ERROR]   2. 验证服务状态后重新部署")
		if p.backupFile != "" {
			p.log.Linef("[ERROR] 数据库备份: %s", p.backupFile)
		}
		p.log.Linef("[ERROR] ==============================================")
		p.notify(ctx, "系统更新失败-迁移变更阻塞", "urgent", "warning,skull",
			fmt.Sprintf("Rollback to %s blocked: schema may have changed. Manual migrate down required.", oldShort))
		// 仓库停在 OLD_SHA（与当前运行的旧镜像一致）；若切回 main，
		// 工作区是新代码而运行的是旧镜像，下次更新的 diff 检测会空转。
		p.cmds.Run(ctx, "git", "-C", p.cfg.RepoRoot, "reset", "--hard", p.oldSHA)
		return
	}

	if p.backupFile != "" {
		p.log.Linef("[WARN] 数据库备份保留: %s", p.backupFile)
	}

	p.log.Linef("[UPDATE] 用旧代码重建受影响服务 …")
	for _, svc := range p.rollbackServices() {
		p.log.Linef("[UPDATE] 回滚重建 %s …", svc)
		if _, _, err := p.cmds.Run(ctx, "docker", p.composeArgs("build", svc)...); err != nil {
			p.log.Linef("[ERROR] 回滚构建 %s 失败: %v", svc, err)
			continue
		}
		if _, _, err := p.cmds.Run(ctx, "docker", p.composeArgs("up", "-d", "--no-deps", svc)...); err != nil {
			p.log.Linef("[ERROR] 回滚重启 %s 失败: %v", svc, err)
		}
	}

	p.notify(ctx, "系统更新失败-已回滚", "urgent", "warning",
		fmt.Sprintf("Rollback to %s after update %s failed", oldShort, shortSHA(p.newSHA)))

	// 同上：仓库与运行的旧镜像保持一致（OLD_SHA），不 checkout main 防止后续更新空转。
	p.cmds.Run(ctx, "git", "-C", p.cfg.RepoRoot, "reset", "--hard", p.oldSHA)

	p.log.Linef("[WARN] ========== 回滚完成 ==========")
	p.log.Linef("[WARN] 请检查服务状态并排查失败原因。")
}

// rollbackServices 返回回滚重建的服务列表：ALL 时用全量列表，否则用受影响列表。
func (p *Pipeline) rollbackServices() []string {
	if p.affected != nil && p.affected.All {
		return fullServiceList
	}
	if p.affected != nil {
		return p.affected.Services
	}
	return nil
}

// ---------- 工具函数 ----------

func (p *Pipeline) composeArgs(args ...string) []string {
	out := []string{"compose", "-f", p.cfg.composeAbs(), "-p", p.cfg.project()}
	return append(out, args...)
}

func (p *Pipeline) affectedHas(svc string) bool {
	if p.affected == nil {
		return false
	}
	return p.affected.Has(svc)
}

func (p *Pipeline) gitRevParse(ctx context.Context, rev string) string {
	out, _, err := p.cmds.Run(ctx, "git", "-C", p.cfg.RepoRoot, "rev-parse", rev)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func (p *Pipeline) gitDiffNameOnly(ctx context.Context, oldSHA, newSHA string) []string {
	out, _, err := p.cmds.Run(ctx, "git", "-C", p.cfg.RepoRoot, "diff", "--name-only", oldSHA, newSHA)
	if err != nil {
		return nil
	}
	var files []string
	for _, ln := range strings.Split(out, "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			files = append(files, ln)
		}
	}
	return files
}

func (p *Pipeline) serviceState(ctx context.Context, svc string) (string, error) {
	out, _, err := p.cmds.Run(ctx, "docker", p.composeArgs("ps", "--format", "json", svc)...)
	if err != nil {
		return "missing", err
	}
	containers, perr := parseComposePS([]byte(out))
	if perr != nil || len(containers) == 0 {
		return "missing", nil
	}
	return containerHealth(containers[0]), nil
}

// diskFreeKB 查询仓库所在磁盘可用空间（KB），解析 `df -Pk` 输出。
// POSIX `-Pk` 在 GNU coreutils 与 busybox（alpine runner 镜像）下都可用；
// GNU 专属的 `df --output=avail` 在 busybox 中不存在，会导致磁盘检查静默失效。
// 输出末行格式：Filesystem 1024-blocks Used Available Capacity Mounted on
func (p *Pipeline) diskFreeKB(ctx context.Context) (int64, error) {
	out, _, err := p.cmds.Run(ctx, "df", "-Pk", p.cfg.RepoRoot)
	if err != nil {
		return 0, err
	}
	lines_out := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines_out) == 0 {
		return 0, errors.New("df 无输出")
	}
	fields := strings.Fields(lines_out[len(lines_out)-1])
	if len(fields) < 4 {
		return 0, fmt.Errorf("df 输出格式异常: %q", out)
	}
	avail, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		return 0, err
	}
	return avail, nil
}
func (p *Pipeline) resolveRunnerImage(ctx context.Context) (string, error) {
	out, _, err := p.cmds.Run(ctx, "docker", p.composeArgs("config", "--images", "server")...)
	if err != nil {
		return "", err
	}
	images, perr := parseComposeImages([]byte(out))
	if perr != nil || len(images) == 0 {
		return "", errors.New("compose config --images server 无输出")
	}
	return images[0], nil
}

// assertProjectName 用 compose ls 断言项目名，失败仅告警（避免误伤首次部署）。
func (p *Pipeline) assertProjectName(ctx context.Context) {
	out, _, err := p.cmds.Run(ctx, "docker", "compose", "ls", "--format", "json")
	if err != nil {
		p.log.Linef("[WARN]  compose ls 失败，跳过项目名断言: %v", err)
		return
	}
	projects, perr := parseComposeProjects([]byte(out))
	if perr != nil {
		p.log.Linef("[WARN]  解析 compose ls 失败: %v", perr)
		return
	}
	found := false
	for _, pr := range projects {
		if pr.Name == p.cfg.project() {
			found = true
			break
		}
	}
	if !found {
		p.log.Linef("[WARN]  未发现运行中的 compose 项目 %q（首次部署可忽略）", p.cfg.project())
	}
}

// assertRepoWritable 验证 runner uid 对仓库可写，失败提示一次性 chown（§8.2）。
func (p *Pipeline) assertRepoWritable() {
	dir := filepath.Join(p.cfg.RepoRoot, ".hermes", "updates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		p.log.Linef("[ERROR] 仓库不可写: %v", err)
		p.log.Linef("[ERROR] 请执行: chown -R <UPDATE_RUN_UID>:<UPDATE_RUN_GID> %s", p.cfg.RepoRoot)
		return
	}
	testFile := filepath.Join(dir, ".writetest")
	if err := os.WriteFile(testFile, []byte("ok"), 0o644); err != nil {
		p.log.Linef("[ERROR] 仓库不可写: %v", err)
		p.log.Linef("[ERROR] 请执行: chown -R <UPDATE_RUN_UID>:<UPDATE_RUN_GID> %s", p.cfg.RepoRoot)
		return
	}
	_ = os.Remove(testFile)
}

func (p *Pipeline) sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// notify 发送 ntfy 通知。runner 镜像（alpine）没有 curl，用 Go net/http 直发，5s 超时。
func (p *Pipeline) notify(ctx context.Context, title, priority, tags, body string) {
	if p.cfg.NtfyURL == "" {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.NtfyURL, strings.NewReader(body))
	if err != nil {
		p.log.Linef("[WARN]  ntfy 通知构造失败: %v", err)
		return
	}
	req.Header.Set("Title", title)
	req.Header.Set("Priority", priority)
	req.Header.Set("Tags", tags)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		p.log.Linef("[WARN]  ntfy 通知发送失败: %v", err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
}
