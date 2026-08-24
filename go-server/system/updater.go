package system

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
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

	// RunnerImage 为空时在预检里从运行中的 lab-server 容器解析。
	RunnerImage string

	// Branch 更新分支（R12：默认 main，UPDATE_BRANCH 覆盖；pull/回滚/behind 统一来源）。
	Branch string

	UpdateTimeout   time.Duration // 更新阶段看门狗
	RollbackTimeout time.Duration // 回滚阶段独立预算

	Force      bool
	DryRun     bool
	NoRollback bool
}

// branch 返回更新分支（空值回退 main）。
func (c *UpdateConfig) branch() string {
	if c.Branch != "" {
		return c.Branch
	}
	return "main"
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
	// healthRetryDelay 健康检查轮次间隔（R14：慢启动服务重试窗口；测试可置 0 加速）。
	healthRetryDelay time.Duration
}

// NewPipeline 构造流水线。
func NewPipeline(cfg *UpdateConfig, cmds CmdRunner, log *Logger) *Pipeline {
	return &Pipeline{cfg: cfg, cmds: cmds, log: log, healthRetryDelay: 5 * time.Second}
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
// 成功（含无变更提前退出且非 dry-run）时落 deployed-sha 标记（R4 部署事实源）。
func (p *Pipeline) Run(ctx context.Context) (code int) {
	defer func() {
		if code == 0 && !p.cfg.DryRun && p.newSHA != "" {
			if err := writeDeployedSHA(p.cfg.RepoRoot, p.newSHA); err != nil {
				p.log.Linef("[WARN]  写已部署版本标记失败: %v", err)
			}
		}
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

// stepPreflight 步骤 1：docker/compose/git 可用、仓库有效性、SSH 凭据自检、磁盘、
// secrets、runner 镜像、项目名断言、compose 服务表差集告警。
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
	// R6：RepoRoot 不是有效 git 仓库 = 路径错配（compose 挂载漂移时 docker 会自动
	// 建空目录），在开头报清晰错误，而不是后续步骤报「缺少 secret 文件」等误导信息。
	if _, err := p.gitRun(ctx, "-C", p.cfg.RepoRoot, "rev-parse", "--git-dir"); err != nil {
		return fmt.Errorf("仓库路径错配: %s 不是有效 git 仓库（检查 REPO_ROOT / 仓库挂载路径）", p.cfg.RepoRoot)
	}

	// R2：origin 为 SSH 远程时预检 deploy key 可用，凭据问题在步骤 1 就以可操作
	// 报错暴露，而不是步骤 3 的裸 exit status 128。
	if err := p.checkGitSSH(ctx); err != nil {
		return err
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
	p.warnServicesDrift(ctx)
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
		p.log.Linef("[WARN]  数据库备份失败（postgres 未运行？），继续…: %s", sanitizeOutput(err.Error()))
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
// 「无变更跳过」以 deployed-sha 标记为部署事实源：HEAD 与标记不一致（上次更新
// 未完成）时不得假成功跳过（R4）。
func (p *Pipeline) stepPull(ctx context.Context) error {
	if _, err := p.gitRun(ctx, "-C", p.cfg.RepoRoot, "fetch", "origin"); err != nil {
		return fmt.Errorf("git fetch 失败: %w", err)
	}
	before := p.gitRevParse(ctx, "HEAD")

	if _, err := p.gitRun(ctx, "-C", p.cfg.RepoRoot, "pull", "--ff-only", "origin", p.cfg.branch()); err == nil {
		p.newSHA = p.gitRevParse(ctx, "HEAD")
		p.log.Linef("[UPDATE] 已更新: %s → %s", shortSHA(before), shortSHA(p.newSHA))
	} else if p.cfg.Force {
		p.log.Linef("[WARN]  git pull 失败，但 --force 模式继续（使用当前 HEAD）")
		p.newSHA = before
	} else {
		p.log.Linef("[ERROR] git pull 失败，中止。请依次检查：网络连通、GitHub 凭据（.hermes/runner-home/README.md）、本地是否偏离分支。")
		return errors.New("git pull 失败")
	}

	if before == p.newSHA && !p.cfg.Force {
		if p.deployedAligned() {
			p.log.Linef("[UPDATE] 代码无变更，跳过更新。")
			return errNoChanges
		}
		p.log.Linef("[WARN]  无新提交，但 HEAD 与已部署版本不一致（上次更新未完成），继续部署。")
	}
	return nil
}

// stepDetect 步骤 4：变更检测；dry-run / none 时提前成功退出。
func (p *Pipeline) stepDetect(ctx context.Context) error {
	changed, err := p.gitDiffNameOnly(ctx, p.oldSHA, p.newSHA)
	if err != nil {
		return fmt.Errorf("git diff 失败: %w", err)
	}

	var aff Affected
	switch {
	case len(changed) == 0 && p.cfg.Force:
		aff = Affected{All: true}
		p.log.Linef("[UPDATE] --force: 强制全量重建")
	case len(changed) == 0:
		// R4：diff 为空 ≠ 已部署。HEAD 与已部署标记不一致说明上次更新中断在构建
		// 之前（pull 已前进、镜像未重建），必须全量对齐而不是假成功退出。
		if p.deployedAligned() {
			p.log.Linef("[UPDATE] 无文件变更，跳过构建。")
			return errNoChanges
		}
		aff = Affected{All: true}
		p.log.Linef("[WARN]  无文件变更，但 HEAD 与已部署版本不一致（上次更新未完成），执行全量对齐重建。")
	default:
		aff = DetectServices(changed)
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

// stepBuild 步骤 5：先预检构建所需 base 镜像（R7），再按依赖顺序构建
// （epics-gateway → ioc → py-agent-interpret → server；py-agent 与 py-agent-interpret
// 共用镜像，不重复构建）。构建输出行流转发（R3）；失败时仓库退回 OLD_SHA（R4）。
func (p *Pipeline) stepBuild(ctx context.Context) error {
	if err := p.checkBaseImages(ctx); err != nil {
		return p.revertOnBuildFailure(err)
	}
	if err := p.doBuild(ctx); err != nil {
		return p.revertOnBuildFailure(err)
	}
	return nil
}

// doBuild 执行实际构建命令（build --pull 全量或按受影响服务）。
func (p *Pipeline) doBuild(ctx context.Context) error {
	if p.affected.All {
		p.log.Linef("[WARN] compose 文件或 Dockerfile 变更，执行全量重建")
		return p.streamLogged(ctx, "docker", p.composeArgs("build", "--pull",
			"server", "py-agent", "py-agent-interpret", "epics-gateway", "ioc", "migrate")...)
	}

	for _, svc := range []string{"epics-gateway", "ioc", "py-agent-interpret"} {
		if p.affectedHas(svc) {
			p.log.Linef("[UPDATE] 构建 %s …", svc)
			if err := p.streamLogged(ctx, "docker", p.composeArgs("build", svc)...); err != nil {
				return err
			}
		}
	}

	if p.affectedHas("server") {
		p.log.Linef("[UPDATE] 构建 server（含前端）…")
		if err := p.streamLogged(ctx, "docker", p.composeArgs("build", "server")...); err != nil {
			return err
		}
	}

	if p.affectedHas("py-agent") {
		p.log.Linef("[UPDATE] py-agent 与 py-agent-interpret 共用镜像，已构建。")
	}
	return nil
}

// revertOnBuildFailure 构建失败时把仓库退回 OLD_SHA 并回到分支（R4）：保证 HEAD
// 永远与正在运行的镜像一致。否则 pull 已前进、build 失败后，下次更新会因
// 「代码无变更」假成功跳过，新代码永远不被部署。不重建任何服务（非回滚）。
func (p *Pipeline) revertOnBuildFailure(err error) error {
	if p.oldSHA != "" && p.newSHA != "" && p.oldSHA != p.newSHA {
		p.log.Linef("[WARN]  构建未完成，仓库回退到 %s（保持 HEAD 与运行镜像一致，下次更新重新拉取）", shortSHA(p.oldSHA))
		p.returnToBranchAt(context.Background(), p.oldSHA)
	}
	return err
}

// stepDeploy 步骤 6：滚动更新 + 迁移。顺序与 update.sh 对齐（R5/I4 契约）：
// postgres 检查 → migrate → epics/ioc/interpret → server → py-agent。
// migrate 必须先于所有业务容器重启：避免新代码对旧 schema 运行一个窗口。
func (p *Pipeline) stepDeploy(ctx context.Context) error {
	restart := func(svc string, maxWait int) error {
		p.log.Linef("[UPDATE] 重启 %s …", svc)
		if _, err := p.runLogged(ctx, "docker", p.composeArgs("up", "-d", "--no-deps", svc)...); err != nil {
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

	if state, _ := p.serviceState(ctx, "postgres"); state != "healthy" {
		p.log.Linef("[WARN] postgres 不健康，尝试重启…")
		if _, err := p.runLogged(ctx, "docker", p.composeArgs("up", "-d", "postgres")...); err != nil {
			return err
		}
		if err := p.sleep(ctx, 5*time.Second); err != nil {
			return err
		}
	}

	if p.affectedHas("migrate") {
		p.log.Linef("[UPDATE] 运行数据库迁移 …")
		if err := p.streamLogged(ctx, "docker", p.composeArgs("run", "--rm", "migrate")...); err != nil {
			p.log.Linef("[ERROR] 迁移失败！")
			return err
		}
		p.log.Linef("[UPDATE] 迁移成功")
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

// stepHealth 步骤 7：全栈健康检查（服务状态 + schema 版本 + 前端 embed 产物）。
// 服务状态给 3 轮重试窗口（R14：慢启动服务如 py-agent start_period 30s 在边界
// 上可能被单轮误杀）；附加验证（R9）失败同样计入不健康触发回滚。
func (p *Pipeline) stepHealth(ctx context.Context) error {
	var bad []string
	for round := 1; round <= healthRetryRounds; round++ {
		bad = nil
		for _, svc := range healthCheckServices {
			state, err := p.serviceState(ctx, svc)
			if err == nil && (state == "healthy" || state == "running") {
				if round == 1 || round == healthRetryRounds {
					p.log.Linef("  OK  %s: %s", svc, state)
				}
				continue
			}
			stateStr := "missing"
			if err == nil {
				stateStr = state
			}
			if round == healthRetryRounds {
				p.log.Linef("  BAD %s: %s", svc, stateStr)
			} else {
				p.log.Linef("  ... %s: %s（等待重试 %d/%d）", svc, stateStr, round+1, healthRetryRounds)
			}
			bad = append(bad, svc)
		}
		if len(bad) == 0 {
			break
		}
		if round < healthRetryRounds {
			if err := p.sleep(ctx, p.healthRetryDelay); err != nil {
				return err
			}
		}
	}

	var failures []string
	if len(bad) > 0 {
		failures = append(failures, bad...)
	} else {
		// 附加验证仅在全部服务健康后执行（都依赖 postgres/server 就绪）。
		if !p.verifySchemaVersion(ctx) {
			failures = append(failures, "schema版本")
		}
		if len(failures) == 0 && !p.verifyFrontendEmbed(ctx) {
			failures = append(failures, "前端产物")
		}
	}

	p.log.Linef("")
	if len(failures) == 0 {
		p.log.Linef("==========================================")
		p.log.Linef("  更新成功！%s → %s", shortSHA(p.oldSHA), shortSHA(p.newSHA))
		p.log.Linef("==========================================")
		p.notify(ctx, "系统更新成功", "default", "white_check_mark",
			fmt.Sprintf("Updated %s → %s", shortSHA(p.oldSHA), shortSHA(p.newSHA)))
		return nil
	}
	p.log.Linef("[ERROR] 部分服务不健康或附加验证失败，请检查！")
	return fmt.Errorf("不健康项: %v", failures)
}

// verifySchemaVersion 核对 migrations 文件数与 schema_migrations 最大版本（R9，
// 移植 update.sh 附加验证 1）。migrations 目录缺失/为空时跳过（本地开发仓库）。
func (p *Pipeline) verifySchemaVersion(ctx context.Context) bool {
	files, _ := filepath.Glob(filepath.Join(p.cfg.RepoRoot, "migrations", "*.up.sql"))
	if len(files) == 0 {
		p.log.Linef("[WARN]  未发现 migrations/*.up.sql，跳过 schema 版本核对")
		return true
	}
	expected := strconv.Itoa(len(files))
	p.log.Linef("验证数据库 schema 版本 …")
	out, _, err := p.cmds.Run(ctx, "docker", p.composeArgs("exec", "-T", "postgres", "psql", "-U", "lab",
		"-tAc", "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1")...)
	actual := strings.TrimSpace(out)
	if err != nil || actual == "" {
		p.log.Linef("[ERROR] schema 版本读取失败（postgres 未就绪？）")
		return false
	}
	if actual != expected {
		p.log.Linef("[ERROR] schema 版本不匹配: 期望 %s，实际 %s（迁移未跑完？）", expected, actual)
		return false
	}
	p.log.Linef("schema 版本 OK: %s", actual)
	return true
}

// verifyFrontendEmbed 核对 server 容器内 embed 的前端产物非空，并与工作区
// web-ui/dist 对比（R9，移植 update.sh 附加验证 2，防白屏回归——AGENTS.md PR 清单项）。
func (p *Pipeline) verifyFrontendEmbed(ctx context.Context) bool {
	p.log.Linef("验证前端产物（server 容器内 embed 的 static）…")
	out, _, err := p.cmds.Run(ctx, "docker", p.composeArgs("exec", "-T", "server", "sh", "-c",
		"ls /app/static/assets/*.js 2>/dev/null | head -1")...)
	if err != nil || strings.TrimSpace(out) == "" {
		p.log.Linef("[ERROR] server 容器内未发现前端静态产物（/app/static/assets/*.js 为空，疑似白屏风险）")
		return false
	}
	embedded := filepath.Base(strings.TrimSpace(strings.Fields(out)[0]))
	p.log.Linef("容器内产物: %s", embedded)
	local, gerr := filepath.Glob(filepath.Join(p.cfg.RepoRoot, "web-ui", "dist", "assets", "*.js"))
	if gerr != nil || len(local) == 0 {
		p.log.Linef("[WARN]  工作区无 web-ui/dist 产物，跳过对比（容器内产物已验证非空）")
		return true
	}
	if filepath.Base(local[0]) != embedded {
		p.log.Linef("[ERROR] 前端产物不一致: 容器 %s vs 工作区 %s（embed/构建过期？）", embedded, filepath.Base(local[0]))
		return false
	}
	p.log.Linef("前端产物与工作区 web-ui/dist 一致")
	return true
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
	if _, err := p.gitRun(ctx, "-C", p.cfg.RepoRoot, "checkout", p.oldSHA); err != nil {
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
		// 更新失败额外上报告警中心（保留上方 ntfy 直发；告警中心统一聚合去重）。
		p.reportAlert(ctx, "系统更新失败-迁移变更阻塞",
			fmt.Sprintf("Rollback to %s blocked: schema may have changed. Manual migrate down required.", oldShort))
		// 仓库停在 OLD_SHA（与当前运行的旧镜像一致）；若切回分支，
		// 工作区是新代码而运行的是旧镜像，下次更新的 diff 检测会空转。
		p.returnToBranchAt(ctx, p.oldSHA)
		return
	}

	if p.backupFile != "" {
		p.log.Linef("[WARN] 数据库备份保留: %s", p.backupFile)
	}

	p.log.Linef("[UPDATE] 用旧代码重建受影响服务 …")
	for _, svc := range p.rollbackServices() {
		p.log.Linef("[UPDATE] 回滚重建 %s …", svc)
		if err := p.streamLogged(ctx, "docker", p.composeArgs("build", svc)...); err != nil {
			p.log.Linef("[ERROR] 回滚构建 %s 失败: %v", svc, err)
			continue
		}
		if _, err := p.runLogged(ctx, "docker", p.composeArgs("up", "-d", "--no-deps", svc)...); err != nil {
			p.log.Linef("[ERROR] 回滚重启 %s 失败: %v", svc, err)
		}
	}

	p.notify(ctx, "系统更新失败-已回滚", "urgent", "warning",
		fmt.Sprintf("Rollback to %s after update %s failed", oldShort, shortSHA(p.newSHA)))
	// 更新失败额外上报告警中心（保留上方 ntfy 直发）。
	p.reportAlert(ctx, "系统更新失败-已回滚",
		fmt.Sprintf("Rollback to %s after update %s failed", oldShort, shortSHA(p.newSHA)))

	// 同上：仓库与运行的旧镜像保持一致（OLD_SHA），但必须回到分支，
	// 否则脱离 HEAD 状态下下次 `git pull --ff-only origin <branch>` 仍能走但状态非分支态，
	// 且与 update.sh（checkout 分支后 reset）行为不一致（§9.1）。
	p.returnToBranchAt(ctx, p.oldSHA)

	p.log.Linef("[WARN] ========== 回滚完成 ==========")
	p.log.Linef("[WARN] 请检查服务状态并排查失败原因。")
}

// returnToBranchAt 切回更新分支并把工作区/分支指针硬重置到指定 commit：
// 回滚后仓库停在该 commit（与运行中的旧镜像一致），同时保持在分支上，
// 下次更新 `git pull --ff-only origin <branch>` 直接快进，避免脱离 HEAD 的脆弱状态。
func (p *Pipeline) returnToBranchAt(ctx context.Context, sha string) {
	if _, err := p.gitRun(ctx, "-C", p.cfg.RepoRoot, "checkout", p.cfg.branch()); err != nil {
		p.log.Linef("[WARN]  git checkout %s 失败（可能不在该分支）: %v", p.cfg.branch(), err)
	}
	_, _ = p.gitRun(ctx, "-C", p.cfg.RepoRoot, "reset", "--hard", sha)
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

// gitTimeout 单条 git 命令上限（对齐 update.sh 的 GIT_TIMEOUT=120s）。R13：SSH
// 半开连接等场景下不能干等外层 30min 看门狗，git 命令逐条自带短超时。
const gitTimeout = 120 * time.Second

// healthRetryRounds 健康检查轮数（R14）。
const healthRetryRounds = 3

// gitRun 执行 git 命令：套单命令超时（R13）+ 失败时 stderr 尾部落日志（R3）。
func (p *Pipeline) gitRun(ctx context.Context, args ...string) (string, error) {
	gctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	return p.runLogged(gctx, "git", args...)
}

// runLogged 执行命令；失败时把 stderr 尾部（脱敏）写进会话日志（R3）。
func (p *Pipeline) runLogged(ctx context.Context, name string, args ...string) (string, error) {
	stdout, stderr, err := p.cmds.Run(ctx, name, args...)
	if err != nil {
		p.logCmdTail(name, args, stderr)
	}
	return stdout, err
}

// streamLogged 执行长命令（compose build/migrate/回滚重建），stdout+stderr 按
// 行实时转发进会话日志（R3）：分钟级构建期间流水线不再静默，镜像源 403、
// 编译错误等失败原因随行流直接可见。命令结束后 flush 残留的半行。
func (p *Pipeline) streamLogged(ctx context.Context, name string, args ...string) error {
	w := &logLineWriter{log: p.log}
	err := p.cmds.RunTee(ctx, w, name, args...)
	w.flush()
	if err != nil {
		return fmt.Errorf("%w（详细输出见上方行流日志）", err)
	}
	return nil
}

// logCmdTail 把失败命令的 stderr 尾部脱敏后写进会话日志（R3：失败原因不再蒸发）。
// 命令行回显同样脱敏：参数本身可能携带凭据（如带 token 的 URL 参数），
// 反例实测发现只脱敏输出尾部时命令回显行是泄漏点。
func (p *Pipeline) logCmdTail(name string, args []string, stderr string) {
	lines := tailLines(sanitizeOutput(stderr), 12)
	if len(lines) == 0 {
		return
	}
	p.log.Linef("[UPDATE] %s 失败，输出尾部:", sanitizeOutput(strings.TrimSpace(name+" "+strings.Join(args, " "))))
	for _, ln := range lines {
		p.log.Linef("  | %s", ln)
	}
}

var (
	urlCredRe = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://[^/@:\s]+:)[^@/\s]+@`)
	bearerRe  = regexp.MustCompile(`(?i)(authorization:\s*(?:bearer|token|basic)\s+)[^\s]+`)
)

// sanitizeOutput 脱敏命令输出：遮蔽 URL 内嵌凭据与 Authorization 头，
// 防止 token/密码随 stderr 尾部/行流落进会话日志（R3）。
func sanitizeOutput(s string) string {
	s = urlCredRe.ReplaceAllString(s, "$1***@")
	return bearerRe.ReplaceAllString(s, "$1***")
}

// tailLines 返回 s 的末尾至多 n 个非空行（保持原序）。
func tailLines(s string, n int) []string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	var out []string
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		if ln := strings.TrimSpace(lines[i]); ln != "" {
			out = append(out, ln)
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// logLineWriter 把子进程合并输出按行写入 Logger（跨 chunk 缓存半行，
// 长行不被拆断；并发安全以兼容 stdin/err 双流写）。
type logLineWriter struct {
	log *Logger
	mu  sync.Mutex
	buf []byte
}

func (w *logLineWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, b...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(string(w.buf[:i]), "\r")
		w.buf = w.buf[i+1:]
		if line != "" {
			w.log.Linef("  | %s", sanitizeOutput(line))
		}
	}
	return len(b), nil
}

// flush 输出残留的半行（进程末尾无换行的最后一段输出）。
func (w *logLineWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) == 0 {
		return
	}
	line := strings.TrimSpace(string(w.buf))
	w.buf = nil
	if line != "" {
		w.log.Linef("  | %s", sanitizeOutput(line))
	}
}

// ---------- deployed-sha 标记（R4 部署事实源） ----------

// deployedSHAFile 是已部署版本标记文件：位于仓库共享目录，Go 引擎与 update.sh
// 双引擎读写同一文件（R4）。部署成功/确认无变更时写入 HEAD。
func deployedSHAFile(repoRoot string) string {
	return filepath.Join(repoRoot, ".hermes", "updates", "deployed-sha")
}

func readDeployedSHA(repoRoot string) (string, bool) {
	data, err := os.ReadFile(deployedSHAFile(repoRoot))
	if err != nil {
		return "", false
	}
	sha := strings.TrimSpace(string(data))
	if sha == "" {
		return "", false
	}
	return sha, true
}

func writeDeployedSHA(repoRoot, sha string) error {
	if sha == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, ".hermes", "updates"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(deployedSHAFile(repoRoot), []byte(sha), 0o644)
}

// deployedAligned 判断 HEAD（newSHA）是否与已部署标记一致。
// 标记缺失视为不一致（R4 反例）：删掉 deployed-sha 后触发更新必须全量对齐重建
// 自愈，而不是「跳过 + 重写标记」把 HEAD 超前镜像的状态永久掩盖成空转。
// 代价是升级到本版本后的首次无变更触发会做一次全量重建（一次性，随后落标记）。
func (p *Pipeline) deployedAligned() bool {
	sha, ok := readDeployedSHA(p.cfg.RepoRoot)
	return ok && sha == p.newSHA
}

// ---------- SSH 凭据自检（R2） ----------

// checkGitSSH 在 origin 为 SSH 协议时用 git ls-remote 预检 deploy key；这样会复用
// runner 注入的 GIT_SSH_COMMAND，不再依赖 OpenSSH 忽略 HOME 的身份文件查找规则。
func (p *Pipeline) checkGitSSH(ctx context.Context) error {
	out, err := p.gitRun(ctx, "-C", p.cfg.RepoRoot, "remote", "get-url", "origin")
	if err != nil || strings.TrimSpace(out) == "" {
		return nil // 拿不到 origin URL 不在此拦截，留给 fetch 报具体错误
	}
	host := sshHostOf(strings.TrimSpace(out))
	if host == "" {
		return nil // https 等非 SSH 协议无需凭据自检
	}
	args := []string{"-C", p.cfg.RepoRoot, "ls-remote", "origin", "HEAD"}
	gctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	out, serr, runErr := p.cmds.Run(gctx, "git", args...)
	if runErr == nil && strings.TrimSpace(out) != "" {
		p.log.Linef("[UPDATE] SSH 凭据自检通过 (git@%s)", host)
		return nil
	}
	if runErr != nil {
		p.logCmdTail("git", args, serr)
	}
	msg := sanitizeOutput(serr)
	for _, kw := range []string{"timed out", "Could not resolve", "Connection refused", "Network is unreachable", "Connection closed"} {
		if strings.Contains(strings.ToLower(msg), strings.ToLower(kw)) {
			return fmt.Errorf("GitHub SSH 远程不可达（git@%s）：请检查网络、DNS 与防火墙", host)
		}
	}
	p.log.Linef("[ERROR] SSH 凭据自检失败 (git@%s)", host)
	return fmt.Errorf("GitHub SSH 凭据不可用（git@%s）：请按 .hermes/runner-home/README.md 配置 deploy key（UPDATE_GIT_HOME 默认 .hermes/runner-home，需含 .ssh/id_ed25519 与 known_hosts）", host)
}

// sshHostOf 从 git remote URL 提取 SSH 主机；非 SSH 形式返回空。
func sshHostOf(origin string) string {
	if after, ok := strings.CutPrefix(origin, "ssh://"); ok {
		// ssh://git@host[:port]/path
		if at := strings.Index(after, "@"); at >= 0 {
			after = after[at+1:]
		}
		if c := strings.IndexAny(after, ":/"); c >= 0 {
			return after[:c]
		}
		return after
	}
	// scp 语法 git@host:path
	if at := strings.Index(origin, "@"); at >= 0 {
		rest := origin[at+1:]
		if c := strings.Index(rest, ":"); c > 0 && !strings.Contains(rest[:c], "/") {
			return rest[:c]
		}
	}
	return ""
}

// ---------- base 镜像预检（R7） ----------

// checkBaseImages 构建前预检受影响服务所需 base 镜像：解析 compose 的 build/image
// 定义 + Dockerfile FROM 行，逐个 docker image inspect。本地缺失时先尝试 docker pull
// （输出行流转发）：在线环境直接补齐，不误伤「新 base 版本本地未缓存、但 registry
// 可达」的正常更新；拉取也失败（镜像源 403 等）才报「离线导入」指引中止——把
// 分钟级中段构建失败变成开头的清晰失败。
func (p *Pipeline) checkBaseImages(ctx context.Context) error {
	data, err := os.ReadFile(p.cfg.composeAbs())
	if err != nil {
		return nil // compose 文件不可读：不在此拦截，交给后续 compose 命令报错
	}
	specs := parseComposeServices(string(data))
	collect := func(svc string, wanted map[string]bool) {
		for _, s := range specs {
			if s.Service != svc {
				continue
			}
			for _, img := range s.baseImages(p.cfg.composeAbs()) {
				wanted[img] = true
			}
		}
	}
	wanted := map[string]bool{}
	if p.affected != nil && p.affected.All {
		for _, s := range specs {
			collect(s.Service, wanted)
		}
	} else {
		for _, svc := range fullServiceList {
			if p.affectedHas(svc) {
				collect(svc, wanted)
			}
		}
	}
	if len(wanted) == 0 {
		return nil
	}

	var missing []string
	for img := range wanted {
		if _, _, err := p.cmds.Run(ctx, "docker", "image", "inspect", img); err == nil {
			continue
		}
		p.log.Linef("[UPDATE] base 镜像 %s 本地缺失，尝试拉取 …", img)
		if perr := p.streamLogged(ctx, "docker", "pull", img); perr == nil {
			continue
		}
		missing = append(missing, img)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		p.log.Linef("[ERROR] 基础镜像缺失: %s", strings.Join(missing, ", "))
		p.log.Linef("[ERROR] 镜像源拉取可能被拒（如 daocloud mirror 403）。请按 deploy/scripts/README.md 离线导入（docker save / docker load）后再触发更新。")
		return fmt.Errorf("基础镜像缺失: %s", strings.Join(missing, ", "))
	}
	p.log.Linef("[UPDATE] base 镜像预检通过（%d 个）", len(wanted))
	return nil
}

// composeServiceSpec 是 parseComposeServices 解析出的单服务构建/镜像定义。
type composeServiceSpec struct {
	Service    string
	HasBuild   bool
	Context    string // 相对 compose 文件目录
	Dockerfile string // 相对 context；空 = 默认 Dockerfile
	Image      string // image: 直接引用的外部镜像（非构建）
}

// baseImages 返回该服务的 base 镜像集合：build 服务取 Dockerfile 全部 FROM；
// image 服务取 image 本身。Dockerfile 不可读/无 FROM 时返回空（预检放行）。
func (s composeServiceSpec) baseImages(composeAbsPath string) []string {
	if !s.HasBuild {
		if s.Image != "" {
			return []string{s.Image}
		}
		return nil
	}
	ctxDir := s.Context
	if !filepath.IsAbs(ctxDir) {
		ctxDir = filepath.Join(filepath.Dir(composeAbsPath), ctxDir)
	}
	df := s.Dockerfile
	if df == "" {
		df = "Dockerfile"
	}
	dfPath := df
	if !filepath.IsAbs(dfPath) {
		dfPath = filepath.Join(ctxDir, df)
	}
	data, err := os.ReadFile(dfPath)
	if err != nil {
		return nil
	}
	return dockerfileFromImages(string(data))
}

var fromRe = regexp.MustCompile(`(?mi)^FROM[ \t]+(?:--platform=\S+[ \t]+)?([^\s]+)`)

// dockerfileFromImages 提取 Dockerfile 全部 FROM 基础镜像（含多阶段），去重。
func dockerfileFromImages(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range fromRe.FindAllStringSubmatch(text, -1) {
		img := strings.Trim(m[1], `"'`)
		if img == "" || seen[img] {
			continue
		}
		seen[img] = true
		out = append(out, img)
	}
	return out
}

// parseComposeServices 手写解析 compose 顶层 services 的 image:/build: 定义
// （两级缩进约定，仅服务于 base 镜像预检；完整解析仍以 docker compose 为准）。
func parseComposeServices(text string) []composeServiceSpec {
	var out []composeServiceSpec
	inServices := false
	cur := -1
	inBuildBlock := false
	for _, raw := range strings.Split(text, "\n") {
		ln := strings.TrimRight(raw, " \t\r")
		if ln == "" || strings.HasPrefix(strings.TrimSpace(ln), "#") {
			continue
		}
		indent := len(ln) - len(strings.TrimLeft(ln, " "))
		trimmed := strings.TrimSpace(ln)
		if indent == 0 {
			inServices = trimmed == "services:"
			inBuildBlock = false
			continue
		}
		if !inServices || indent < 2 {
			inBuildBlock = false
			continue
		}
		if indent == 2 {
			inBuildBlock = false
			if name, ok := strings.CutSuffix(trimmed, ":"); ok && !strings.Contains(name, " ") {
				out = append(out, composeServiceSpec{Service: name})
				cur = len(out) - 1
			}
			continue
		}
		if cur < 0 {
			continue
		}
		key, val, _ := strings.Cut(trimmed, ":")
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		switch {
		case inBuildBlock && key == "context" && val != "":
			out[cur].Context = val
		case inBuildBlock && key == "dockerfile" && val != "":
			out[cur].Dockerfile = val
		case key == "image" && val != "":
			out[cur].Image = val
		case key == "build":
			out[cur].HasBuild = true
			if val != "" { // 简写：build: <context>
				out[cur].Context = val
				inBuildBlock = false
			} else {
				inBuildBlock = true
			}
		default:
			if indent <= 4 {
				inBuildBlock = false // 遇到同级其它键，退出 build 块
			}
		}
	}
	return out
}

// warnServicesDrift 把 compose 实际服务表与硬编码健康检查/全量构建列表比对差集
// 告警（R15：设计 §5 兜底校验，warn 不阻断，防止 compose 加新服务后硬编码表漏检）。
func (p *Pipeline) warnServicesDrift(ctx context.Context) {
	out, _, err := p.cmds.Run(ctx, "docker", p.composeArgs("config", "--services")...)
	if err != nil {
		return
	}
	known := map[string]bool{}
	for _, svc := range healthCheckServices {
		known[svc] = true
	}
	for _, svc := range fullServiceList {
		known[svc] = true
	}
	actual := map[string]bool{}
	for _, svc := range parseComposeLines([]byte(out)) {
		actual[svc] = true
		if !known[svc] {
			p.log.Linef("[WARN]  compose 服务 %q 不在更新系统硬编码列表内（健康检查/构建可能漏掉它，请同步 healthCheckServices/fullServiceList）", svc)
		}
	}
	for _, svc := range append(append([]string{}, healthCheckServices...), fullServiceList...) {
		if !actual[svc] {
			p.log.Linef("[WARN]  硬编码服务 %q 不在 compose 服务表中（列表可能过期）", svc)
		}
	}
}

func (p *Pipeline) gitRevParse(ctx context.Context, rev string) string {
	out, err := p.gitRun(ctx, "-C", p.cfg.RepoRoot, "rev-parse", rev)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func (p *Pipeline) gitDiffNameOnly(ctx context.Context, oldSHA, newSHA string) ([]string, error) {
	out, err := p.gitRun(ctx, "-C", p.cfg.RepoRoot, "diff", "--name-only", oldSHA, newSHA)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, ln := range strings.Split(out, "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			files = append(files, ln)
		}
	}
	return files, nil
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
	out, _, err := p.cmds.Run(ctx, "docker", "inspect", "-f", "{{.Config.Image}}", "lab-server")
	if err != nil {
		return "", err
	}
	images, perr := parseComposeImages([]byte(out))
	if perr != nil || len(images) == 0 {
		return "", errors.New("docker inspect lab-server 无镜像输出")
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

// notify 发送 ntfy 通知。镜像虽已含 curl，但保留 Go net/http 直发：无 shell 拼接
// 注入面、少一次子进程开销（历史注释「runner 镜像没有 curl」已过时，R15 修正）。
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

// reportAlert 将更新失败上报告警中心（runner 容器内 Go 直发，复用 SERVICE_TOKEN_FILE：
// 优先 env 指定路径，默认仓库 deploy/secrets/service_token.txt；server 地址取
// UPDATE_SERVER_URL env，默认 http://localhost:8000，与 UPDATE_NTFY_URL 同网栈）。
// 与 p.notify 互补：ntfy 直发保留（D6 双保险），此处额外收拢到告警中心统一去重。
// 配置缺失/失败仅记 WARN 日志，不影响更新流程与既有通知。
func (p *Pipeline) reportAlert(ctx context.Context, title, detail string) {
	serverURL := os.Getenv("UPDATE_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8000"
	}
	tokenFile := os.Getenv("SERVICE_TOKEN_FILE")
	if tokenFile == "" {
		tokenFile = filepath.Join(p.cfg.RepoRoot, "deploy", "secrets", "service_token.txt")
	}
	token, err := os.ReadFile(tokenFile)
	if err != nil {
		p.log.Linef("[WARN]  告警中心上报跳过（service token 不可读）: %v", err)
		return
	}
	tokenStr := strings.TrimSpace(string(token))
	if tokenStr == "" {
		p.log.Linef("[WARN]  告警中心上报跳过（service token 为空）")
		return
	}
	payload, err := json.Marshal(map[string]string{
		"level": "warning", "source": "updater", "title": title, "detail": detail,
	})
	if err != nil {
		p.log.Linef("[WARN]  告警中心上报跳过（序列化失败）: %v", err)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(serverURL, "/")+"/api/v1/alerts/report", bytes.NewReader(payload))
	if err != nil {
		p.log.Linef("[WARN]  告警中心上报请求构造失败: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		p.log.Linef("[WARN]  告警中心上报失败: %v", err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		p.log.Linef("[UPDATE] 告警中心上报成功")
	}
}
