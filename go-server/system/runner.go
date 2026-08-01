package system

import "context"

// RunnerID 标识一个 runner：容器名 lab-updater-<session>（unix，go/shell 双引擎共用）、
// 或本地占位 local:<session>（windows 进程内开发实现）。
type RunnerID string

// SessionRunner 抽象 runner 的启动/停止/存活判断。
// I1 核心约束：更新执行者必须独立于 server 容器，server 重建不影响它。
// unix 实现为 detached runner 容器（docker run），windows 实现为进程内 goroutine。
type SessionRunner interface {
	// Spawn 启动 runner 并返回其 ID。
	Spawn(ctx context.Context, sess *UpdateSession) (RunnerID, error)
	// Kill 停止 runner（docker kill / 进程组 kill / context cancel）。
	Kill(id RunnerID) error
	// Alive 判断 runner 是否仍在运行（docker inspect / 进程组 / goroutine 状态）。
	Alive(id RunnerID) bool
}

// RunnerSpawnConfig 是 dockerRunner 派发 runner 容器的全部参数。
type RunnerSpawnConfig struct {
	SessionID   string
	RepoRoot    string
	ComposeFile string // 相对仓库，如 deploy/docker-compose.yml
	ProjectName string
	LogFile     string
	DoneFile    string
	LogDir      string // 会话日志共享目录（挂载用）
	RunnerImage string // 为空由 Spawn 用 compose config --images server 解析
	NtfyURL     string
	BackupDir   string
	GitHome     string // runner 内 git HOME（只读挂载 .gitconfig/.ssh）
	RunUID      int
	RunGID      int
	DockerGID   int // --group-add，访问宿主机 docker.sock
	Engine      string
	Flags       []string
}

// updateFlags 把 Service 的布尔 flag 转成 lab-update 入参（--force/--dry-run/--no-rollback）。
func (s *Service) updateFlags() []string {
	var out []string
	if s.force {
		out = append(out, "--force")
	}
	if s.dryRun {
		out = append(out, "--dry-run")
	}
	if s.noRollback {
		out = append(out, "--no-rollback")
	}
	return out
}

// spawnConfig 组装 dockerRunner 派发参数（session 相关字段由 Spawn 填充）。
func (s *Service) spawnConfig() RunnerSpawnConfig {
	return RunnerSpawnConfig{
		RepoRoot:    s.repoRoot,
		ComposeFile: s.composeFile,
		ProjectName: s.projectName,
		LogDir:      s.logDir,
		RunnerImage: s.runnerImage,
		NtfyURL:     s.ntfyURL,
		BackupDir:   s.backupDir,
		GitHome:     s.gitHome,
		RunUID:      s.runUID,
		RunGID:      s.runGID,
		DockerGID:   s.dockerGID,
		Engine:      s.engine,
		Flags:       s.updateFlags(),
	}
}
