//go:build !windows

package system

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBuildRunnerCmdGoEngine(t *testing.T) {
	cfg := RunnerSpawnConfig{
		SessionID:     "upd_abc1234567",
		RepoRoot:      "/opt/hiaf-lab-system",
		ComposeFile:   "deploy/docker-compose.yml",
		ProjectName:   "deploy",
		LogFile:       "/updates/lab-update-upd_abc1234567.log",
		DoneFile:      "/updates/lab-update-upd_abc1234567.done",
		LogDir:        "/updates",
		RunnerImage:   "deploy-server",
		NtfyURL:       "http://localhost:8085/lab-system",
		BackupDir:     "/backups",
		Branch:        "main",
		UpdateTimeout: 30 * time.Minute,
		RunUID:        1000,
		RunGID:        1000,
		DockerGID:     993,
		Engine:        "go",
		Flags:         []string{"--force"},
	}
	args := buildRunnerCmd(cfg)

	for _, want := range []string{
		"run", "-d",
		"lab-updater-upd_abc1234567", // 容器名 = 白名单 session id
		"--network", "host",
		"/var/run/docker.sock:/var/run/docker.sock",
		"/opt/hiaf-lab-system:/opt/hiaf-lab-system", // 仓库挂载 = 宿主绝对路径
		"--user", "1000:1000",
		"--group-add", "993",
		"deploy-server",
		"lab-update", "--session", "upd_abc1234567", "--repo", "/opt/hiaf-lab-system",
		"--force",
	} {
		if !hasArg(args, want) {
			t.Errorf("go 引擎 docker run 缺参数 %q: %v", want, args)
		}
	}
	// R11：不使用 --rm——退出容器需保留供失败时 docker logs 回读，由 Kill/Reap 清理
	if hasArg(args, "--rm") {
		t.Errorf("docker run 不应带 --rm（R11）: %v", args)
	}
	if !hasArg(args, "-e") || !hasArg(args, "UPDATE_DONE_FILE=/updates/lab-update-upd_abc1234567.done") {
		t.Errorf("缺少 UPDATE_DONE_FILE env: %v", args)
	}
	if !hasArg(args, "UPDATE_SERVER_URL=http://localhost:8000") {
		t.Errorf("缺少 UPDATE_SERVER_URL env（告警中心上报闭环）: %v", args)
	}
	if !hasArg(args, "SERVICE_TOKEN_FILE=/opt/hiaf-lab-system/deploy/secrets/service_token.txt") {
		t.Errorf("缺少 SERVICE_TOKEN_FILE env（告警中心上报闭环）: %v", args)
	}
	// R12/R10：分支与看门狗预算透传 runner
	if !hasArg(args, "UPDATE_BRANCH=main") {
		t.Errorf("缺少 UPDATE_BRANCH env（R12）: %v", args)
	}
	if !hasArg(args, "UPDATE_UPDATE_TIMEOUT=30m0s") {
		t.Errorf("缺少 UPDATE_UPDATE_TIMEOUT env（R10）: %v", args)
	}
}

func TestBuildRunnerCmdShellEngine(t *testing.T) {
	cfg := RunnerSpawnConfig{
		SessionID:   "upd_abc1234567",
		RepoRoot:    "/opt/hiaf-lab-system",
		ComposeFile: "deploy/docker-compose.yml",
		RunnerImage: "deploy-server",
		Engine:      "shell",
	}
	args := buildRunnerCmd(cfg)
	if !hasArg(args, "bash") || !hasArg(args, "/opt/hiaf-lab-system/.hermes/update.sh") {
		t.Errorf("shell 引擎 entrypoint 应为 bash update.sh: %v", args)
	}
}

// TestDockerRunnerSpawnViaFake 用 fake CmdRunner 验证 Spawn 组装 `docker run -d`、
// 镜像内 lab-update 预检通过并返回容器名（R11）。
// TestDockerRunnerSpawnViaFake 镜像内 precheck + 主 run 不带 --rm 的黄金用例。
func TestDockerRunnerSpawnViaFake(t *testing.T) {
	fake := &fakeCmdRunner{fn: func(c Call) (string, string, error) {
		if hasArg(c.Args, "inspect") {
			return "running 0\n", "", nil // spawn 后容器进入运行态
		}
		return "", "", nil
	}}
	r := &dockerRunner{cmds: fake, cfg: RunnerSpawnConfig{
		RepoRoot:    "/r",
		ComposeFile: "deploy/docker-compose.yml",
		RunnerImage: "img",
		Engine:      "go",
	}}
	sess := &UpdateSession{ID: "upd_abc1234567", logFile: "/l.log", doneFile: "/l.done"}
	id, err := r.Spawn(context.Background(), sess)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if id != RunnerID("lab-updater-upd_abc1234567") {
		t.Errorf("Spawn id = %q", id)
	}
	calls := fake.callsSnapshot()
	// 第一步先探测镜像内 lab-update 存在（R11 自举死锁预防）；
	// 命令形如 `run --rm --entrypoint sh img -c "test -x /usr/local/bin/lab-update"`。
	if len(calls) == 0 || !strings.Contains(strings.Join(calls[0].Args, " "), "/usr/local/bin/lab-update") {
		t.Errorf("Spawn 应先预检镜像内 lab-update: %v", callNames(calls))
	}
	for _, c := range calls {
		if hasArg(c.Args, "run") && hasArg(c.Args, "--rm") {
			if hasArg(c.Args, "lab-updater-") { // 主 run 不带 --rm；预检探测容器可用 --rm
				t.Errorf("runner 主容器不应带 --rm: %v", c.Args)
			}
		}
	}
}

// TestDockerRunnerSpawnInjectsGitSSHCommand 配置 GitHome（R2/17）时，
// 主 run 必须注入 GIT_SSH_COMMAND，用绝对路径显式指 deploy key（2026-08-22 实测：
// ssh 的 ~ 不读 HOME 而读 passwd 家目录，容器内 HOME=GitHome 仍 Permission denied）。
func TestDockerRunnerSpawnInjectsGitSSHCommand(t *testing.T) {
	fake := &fakeCmdRunner{fn: func(c Call) (string, string, error) {
		if hasArg(c.Args, "inspect") {
			return "running 0\n", "", nil
		}
		return "", "", nil
	}}
	r := &dockerRunner{cmds: fake, cfg: RunnerSpawnConfig{
		RepoRoot:    "/opt/hiaf-lab-system",
		ComposeFile: "deploy/docker-compose.yml",
		RunnerImage: "img",
		Engine:      "go",
		GitHome:     "/opt/hiaf-lab-system/.hermes/runner-home",
	}}
	sess := &UpdateSession{ID: "upd_abcdef1234", logFile: "/l.log", doneFile: "/l.done"}
	if _, err := r.Spawn(context.Background(), sess); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	// 找主 run（LAB 容器名）的 env 断言 GIT_SSH_COMMAND 存在且指绝对路径 key。
	var found bool
	for _, c := range fake.callsSnapshot() {
		if hasArg(c.Args, "run") && hasArg(c.Args, "lab-updater-upd_abcdef1234") {
			for _, a := range c.Args {
				if strings.HasPrefix(a, "GIT_SSH_COMMAND=") {
					if !strings.Contains(a, "-i /opt/hiaf-lab-system/.hermes/runner-home/.ssh/id_ed25519") ||
						!strings.Contains(a, "UserKnownHostsFile=/opt/hiaf-lab-system/.hermes/runner-home/.ssh/known_hosts") {
						t.Errorf("GIT_SSH_COMMAND 应含绝对路径 key 与 known_hosts: %q", a)
					}
					found = true
				}
				if strings.HasPrefix(a, "HOME=") && a != "HOME=/opt/hiaf-lab-system/.hermes/runner-home" {
					t.Errorf("HOME 应为 runner-home: %q", a)
				}
			}
		}
	}
	if !found {
		t.Errorf("主 run 应注入 GIT_SSH_COMMAND: %v", callNames(fake.callsSnapshot()))
	}
}

// TestDockerRunnerSpawnBinaryMissing 镜像内缺 lab-update（旧镜像自举死锁）→ Spawn
// 直接失败并提示重建镜像，不进入 docker run 主流程（R11）。
func TestDockerRunnerSpawnBinaryMissing(t *testing.T) {
	fake := &fakeCmdRunner{fn: func(c Call) (string, string, error) {
		if strings.Contains(strings.Join(c.Args, " "), "/usr/local/bin/lab-update") && hasArg(c.Args, "--entrypoint") {
			return "", "sh: test: not found", context.DeadlineExceeded
		}
		return "", "", nil
	}}
	r := &dockerRunner{cmds: fake, cfg: RunnerSpawnConfig{
		RepoRoot:    "/r",
		ComposeFile: "deploy/docker-compose.yml",
		RunnerImage: "oldimg",
		Engine:      "go",
	}}
	sess := &UpdateSession{ID: "upd_abc1234567", logFile: "/l.log", doneFile: "/l.done"}
	_, err := r.Spawn(context.Background(), sess)
	if err == nil {
		t.Fatal("镜像缺 lab-update 应 Spawn 失败")
	}
	if !strings.Contains(err.Error(), "lab-update") || !strings.Contains(err.Error(), "重建") {
		t.Errorf("错误应提示重建镜像: %v", err)
	}
	for _, c := range fake.callsSnapshot() {
		if hasArg(c.Args, "lab-updater-upd_abc1234567") {
			t.Errorf("预检失败后不应派发主容器: %v", c.Args)
		}
	}
}

// TestDockerRunnerSpawnContainerExited docker run -d 成功但容器立即退出（旧镜像
// entrypoint 127）→ Spawn 失败并回读 docker logs 写明原因、rm 残留容器（R11）。
func TestDockerRunnerSpawnContainerExited(t *testing.T) {
	fake := &fakeCmdRunner{fn: func(c Call) (string, string, error) {
		switch {
		case hasArg(c.Args, "inspect"):
			return "exited 127\n", "", nil
		case hasArg(c.Args, "logs"):
			return "lab-update: not found\n", "", nil
		default:
			return "", "", nil
		}
	}}
	r := &dockerRunner{cmds: fake, cfg: RunnerSpawnConfig{
		RepoRoot:    "/r",
		ComposeFile: "deploy/docker-compose.yml",
		RunnerImage: "img",
		Engine:      "go",
	}}
	sess := &UpdateSession{ID: "upd_abc1234567", logFile: "/l.log", doneFile: "/l.done"}
	_, err := r.Spawn(context.Background(), sess)
	if err == nil {
		t.Fatal("容器启动即退出应 Spawn 失败")
	}
	if !strings.Contains(err.Error(), "127") || !strings.Contains(err.Error(), "lab-update: not found") {
		t.Errorf("错误应含退出码与容器日志: %v", err)
	}
	calls := fake.callsSnapshot()
	if !containsCall(calls, "docker", "logs", "lab-updater-upd_abc1234567") {
		t.Errorf("应 docker logs 回读失败原因: %v", callNames(calls))
	}
	if !containsCall(calls, "docker", "rm", "-f", "lab-updater-upd_abc1234567") {
		t.Errorf("应清理退出容器: %v", callNames(calls))
	}
}

// Spawn 在 Trigger 返回前就失败时，labctl 只能事后订阅；容器无日志文件
// 也必须回放 docker logs 原因，不能只返回 done exit_code=-1。
func TestSpawnExitedWithoutLogReplaysError(t *testing.T) {
	fake := &fakeCmdRunner{fn: func(c Call) (string, string, error) {
		switch {
		case hasArg(c.Args, "inspect"):
			return "exited 127\n", "", nil
		case hasArg(c.Args, "logs"):
			return "lab-update: not found\n", "", nil
		default:
			return "", "", nil
		}
	}}
	dir := t.TempDir()
	id := "upd_abc1234567"
	logPath, donePath, runnerPath := sessionPaths(dir, id)
	sess := &UpdateSession{
		ID: id, Status: "running", LogBuffer: NewRingBuffer(ringBufferCap),
		subs: make(map[chan SSEEvent]struct{}), done: make(chan struct{}),
		logFile: logPath, doneFile: donePath, runnerFile: runnerPath, maxSubs: 4,
	}
	svc := NewService(t.TempDir())
	svc.logDir = dir
	svc.runner = &dockerRunner{cmds: fake, cfg: RunnerSpawnConfig{
		RepoRoot: "/r", ComposeFile: "deploy/docker-compose.yml", RunnerImage: "img", Engine: "go",
	}}
	svc.mu.Lock()
	svc.sessions[id] = sess
	svc.mu.Unlock()
	svc.runScript(sess)

	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("失败 runner 不应创建日志文件: %v", err)
	}
	ch, stop, err := svc.Subscribe(id) // 模拟 POST 返回后 labctl 才连 SSE
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	first := <-ch
	if first.Type != "error" || !strings.Contains(first.Message, "127") || !strings.Contains(first.Message, "lab-update: not found") {
		t.Fatalf("迟到订阅应先回放容器失败原因: %+v", first)
	}
	if done := <-ch; done.Type != "done" || done.ExitCode != -1 {
		t.Fatalf("错误后应正常收尾: %+v", done)
	}
}

// TestDockerRunnerSpawnCreatedStatePolls R11 回归：docker run 返回与 start 完成
// 之间的 created 窗口（Status=created、ExitCode=0）不得被误判「启动即退出」rm 掉，
// 应继续轮询直到进入运行态。
func TestDockerRunnerSpawnCreatedStatePolls(t *testing.T) {
	inspects := 0
	fake := &fakeCmdRunner{fn: func(c Call) (string, string, error) {
		if hasArg(c.Args, "inspect") {
			inspects++
			if inspects == 1 {
				return "created 0\n", "", nil
			}
			return "running 0\n", "", nil
		}
		return "", "", nil
	}}
	r := &dockerRunner{cmds: fake, cfg: RunnerSpawnConfig{
		RepoRoot:    "/r",
		ComposeFile: "deploy/docker-compose.yml",
		RunnerImage: "img",
		Engine:      "go",
	}}
	sess := &UpdateSession{ID: "upd_abc1234567", logFile: "/l.log", doneFile: "/l.done"}
	id, err := r.Spawn(context.Background(), sess)
	if err != nil {
		t.Fatalf("created 态应继续轮询而非误判失败: %v", err)
	}
	if id != RunnerID("lab-updater-upd_abc1234567") {
		t.Errorf("Spawn id = %q", id)
	}
	if containsCall(fake.callsSnapshot(), "docker", "rm") {
		t.Error("created 态容器不应被 rm（那是误杀刚创建的容器）")
	}
}

// TestDockerRunnerResolveImageFails 镜像解析失败 → Spawn 失败，不落魔法 tag。
func TestDockerRunnerResolveImageFails(t *testing.T) {
	fake := &fakeCmdRunner{fn: func(c Call) (string, string, error) {
		return "", "no such service", context.DeadlineExceeded
	}}
	r := &dockerRunner{cmds: fake, cfg: RunnerSpawnConfig{
		RepoRoot:    "/r",
		ComposeFile: "deploy/docker-compose.yml",
		Engine:      "go",
	}}
	sess := &UpdateSession{ID: "upd_abc1234567", logFile: "/l.log", doneFile: "/l.done"}
	if _, err := r.Spawn(context.Background(), sess); err == nil {
		t.Error("镜像解析失败应中止 Spawn")
	}
	// 不应执行 docker run（解析先行失败）
	calls := fake.callsSnapshot()
	for _, c := range calls {
		if hasArg(c.Args, "run") {
			t.Errorf("解析失败后不应 docker run: %v", calls)
		}
	}
}

func TestDockerRunnerResolveImageSelectsServer(t *testing.T) {
	fake := &fakeCmdRunner{fn: func(c Call) (string, string, error) {
		if hasArg(c.Args, "inspect") && hasArg(c.Args, "lab-server") {
			return "deploy-server:latest\n", "", nil
		}
		return "", "", nil
	}}
	r := &dockerRunner{cmds: fake, cfg: RunnerSpawnConfig{RepoRoot: "/r", ComposeFile: "deploy/docker-compose.yml"}}
	if got, err := r.resolveImage(context.Background()); err != nil || got != "deploy-server:latest" {
		t.Fatalf("resolveImage = %q, %v", got, err)
	}
}

func TestBuildRunnerCmdContainerNameFromSession(t *testing.T) {
	cfg := RunnerSpawnConfig{
		SessionID:   "upd_abc1234567",
		RepoRoot:    "/r",
		ComposeFile: "deploy/docker-compose.yml",
		RunnerImage: "img",
		Engine:      "go",
	}
	args := buildRunnerCmd(cfg)
	found := false
	for _, a := range args {
		if strings.HasPrefix(a, "lab-updater-") {
			found = true
			if a != "lab-updater-upd_abc1234567" {
				t.Errorf("容器名应为白名单 session id，got %q", a)
			}
		}
	}
	if !found {
		t.Error("docker run 缺少容器名")
	}
}

// TestDockerRunnerReap 孤儿回收：超阈值的 lab-updater-* 容器（不在受保护名单）被 kill+rm；
// 受保护（仍存活 session）与未超阈值的容器不受影响；已退出的容器直接 rm 兜底清理
// （R11：spawn 不再 --rm，正常退出的容器由此回收）。
func TestDockerRunnerReap(t *testing.T) {
	fake := &fakeCmdRunner{fn: func(c Call) (string, string, error) {
		switch {
		case hasArg(c.Args, "ps"):
			return "cid-orphan\ncid-young\ncid-protected\ncid-exited\n", "", nil
		case hasArg(c.Args, "inspect"):
			name := "lab-updater-upd_xxxx"
			if hasArg(c.Args, "cid-young") {
				name = "lab-updater-upd_young0"
			}
			if hasArg(c.Args, "cid-protected") {
				name = "lab-updater-upd_protect"
			}
			if hasArg(c.Args, "cid-exited") {
				name = "lab-updater-upd_exited0"
			}
			// orphan/protected 启动于 1 小时前（超阈值），young 为 1 分钟前（不超阈值）
			started := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
			if hasArg(c.Args, "cid-young") {
				started = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
			}
			running := "true"
			if hasArg(c.Args, "cid-exited") {
				running = "false"
			}
			return name + " " + running + " " + started, "", nil
		default:
			return "", "", nil
		}
	}}
	r := &dockerRunner{cmds: fake}
	protect := map[RunnerID]bool{"lab-updater-upd_protect": true}
	if err := r.Reap(context.Background(), protect); err != nil {
		t.Fatalf("Reap: %v", err)
	}
	calls := fake.callsSnapshot()
	killNames := map[string]bool{}
	rmNames := map[string]bool{}
	for _, c := range calls {
		if hasArg(c.Args, "kill") && len(c.Args) > 0 {
			killNames[c.Args[len(c.Args)-1]] = true
		}
		if hasArg(c.Args, "rm") && len(c.Args) > 0 {
			rmNames[c.Args[len(c.Args)-1]] = true
		}
	}
	if !killNames["lab-updater-upd_xxxx"] {
		t.Errorf("超阈值孤儿应被 kill: %v", callNames(calls))
	}
	if killNames["lab-updater-upd_protect"] {
		t.Errorf("受保护容器不应被 kill: %v", callNames(calls))
	}
	if killNames["lab-updater-upd_young0"] {
		t.Errorf("未超阈值容器不应被 kill: %v", callNames(calls))
	}
	if !rmNames["lab-updater-upd_exited0"] {
		t.Errorf("已退出容器应被 rm 兜底清理（R11）: %v", callNames(calls))
	}
	if killNames["lab-updater-upd_exited0"] {
		t.Errorf("已退出容器无需 kill: %v", callNames(calls))
	}
}

// TestDockerRunnerKillGracefulThenHardKill Kill 先 SIGTERM 等待优雅退出，预算耗尽仍存活才 SIGKILL + rm。
func TestDockerRunnerKillGracefulThenHardKill(t *testing.T) {
	fake := &fakeCmdRunner{fn: func(c Call) (string, string, error) {
		if hasArg(c.Args, "inspect") {
			return "true", "", nil // 一直存活直到优雅预算耗尽
		}
		return "", "", nil
	}}
	r := &dockerRunner{cmds: fake, killGrace: 150 * time.Millisecond, killPoll: 20 * time.Millisecond}
	if err := r.Kill("lab-updater-upd_abc1234567"); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	calls := fake.callsSnapshot()
	// 第一次 kill 必须带 --signal SIGTERM（先优雅、后硬杀）
	if len(calls) == 0 || !hasArg(calls[0].Args, "kill") || !hasArg(calls[0].Args, "--signal") || !hasArg(calls[0].Args, "SIGTERM") {
		t.Errorf("应先发送 SIGTERM: %v", callNames(calls))
	}
	var hardKill, rm bool
	for _, c := range calls {
		if hasArg(c.Args, "kill") && !hasArg(c.Args, "--signal") {
			hardKill = true
		}
		if hasArg(c.Args, "rm") {
			rm = true
		}
	}
	if !hardKill || !rm {
		t.Errorf("优雅预算耗尽后应 SIGKILL + rm: %v", callNames(calls))
	}
}

// TestDockerRunnerKillGracefulExit 容器收到 SIGTERM 后自行退出 → 不再硬杀。
func TestDockerRunnerKillGracefulExit(t *testing.T) {
	fake := &fakeCmdRunner{fn: func(c Call) (string, string, error) {
		if hasArg(c.Args, "inspect") {
			return "false", "", nil // SIGTERM 后已退出
		}
		return "", "", nil
	}}
	r := &dockerRunner{cmds: fake, killGrace: time.Minute, killPoll: 20 * time.Millisecond}
	if err := r.Kill("lab-updater-upd_abc1234567"); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	calls := fake.callsSnapshot()
	var sigterm, plainKill bool
	for _, c := range calls {
		if hasArg(c.Args, "kill") && hasArg(c.Args, "--signal") {
			sigterm = true
		}
		if hasArg(c.Args, "kill") && !hasArg(c.Args, "--signal") {
			plainKill = true
		}
	}
	if !sigterm {
		t.Errorf("应先发送 SIGTERM: %v", callNames(calls))
	}
	if plainKill {
		t.Errorf("优雅退出后不应硬杀: %v", callNames(calls))
	}
}

// TestDockerRunnerReapNoContainers ps 无输出时不报错也不误杀。
func TestDockerRunnerReapNoContainers(t *testing.T) {
	fake := &fakeCmdRunner{fn: func(c Call) (string, string, error) {
		if hasArg(c.Args, "ps") {
			return "", "", nil
		}
		return "", "", nil
	}}
	r := &dockerRunner{cmds: fake}
	if err := r.Reap(context.Background(), nil); err != nil {
		t.Fatalf("Reap: %v", err)
	}
	for _, c := range fake.callsSnapshot() {
		if hasArg(c.Args, "kill") {
			t.Errorf("不应有任何 kill: %v", callNames(fake.callsSnapshot()))
		}
	}
}
