//go:build !windows

package system

import (
	"context"
	"strings"
	"testing"
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
