//go:build !windows

package system

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBuildRunnerCmdGoEngine(t *testing.T) {
	cfg := RunnerSpawnConfig{
		SessionID:   "upd_abc1234567",
		RepoRoot:    "/opt/hiaf-lab-system",
		ComposeFile: "deploy/docker-compose.yml",
		ProjectName: "deploy",
		LogFile:     "/updates/lab-update-upd_abc1234567.log",
		DoneFile:    "/updates/lab-update-upd_abc1234567.done",
		LogDir:      "/updates",
		RunnerImage: "deploy-server",
		NtfyURL:     "http://localhost:8085/lab-system",
		BackupDir:   "/backups",
		RunUID:      1000,
		RunGID:      1000,
		DockerGID:   993,
		Engine:      "go",
		Flags:       []string{"--force"},
	}
	args := buildRunnerCmd(cfg)

	for _, want := range []string{
		"run", "-d", "--rm",
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
	if !hasArg(args, "-e") || !hasArg(args, "UPDATE_DONE_FILE=/updates/lab-update-upd_abc1234567.done") {
		t.Errorf("缺少 UPDATE_DONE_FILE env: %v", args)
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

// TestDockerRunnerSpawnViaFake 用 fake CmdRunner 验证 Spawn 组装 `docker run -d` 并返回容器名。
func TestDockerRunnerSpawnViaFake(t *testing.T) {
	fake := &fakeCmdRunner{fn: func(c Call) (string, string, error) { return "", "", nil }}
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
	if len(calls) != 1 || calls[0].Name != "docker" || !hasArg(calls[0].Args, "run") {
		t.Errorf("Spawn 应执行 docker run: %v", calls)
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
// 受保护（仍存活 session）与未超阈值的容器不受影响。
func TestDockerRunnerReap(t *testing.T) {
	fake := &fakeCmdRunner{fn: func(c Call) (string, string, error) {
		switch {
		case hasArg(c.Args, "ps"):
			return "cid-orphan\ncid-young\ncid-protected\n", "", nil
		case hasArg(c.Args, "inspect"):
			name := "lab-updater-upd_xxxx"
			if hasArg(c.Args, "cid-young") {
				name = "lab-updater-upd_young0"
			}
			if hasArg(c.Args, "cid-protected") {
				name = "lab-updater-upd_protect"
			}
			// orphan/protected 启动于 1 小时前（超阈值），young 为 1 分钟前（不超阈值）
			started := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
			if hasArg(c.Args, "cid-young") {
				started = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
			}
			return name + " true " + started, "", nil
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
	for _, c := range calls {
		if hasArg(c.Args, "kill") && len(c.Args) > 0 {
			killNames[c.Args[len(c.Args)-1]] = true
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
