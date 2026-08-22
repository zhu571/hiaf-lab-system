//go:build !windows

package system

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// updateLockFile 返回更新互斥锁文件路径：放在仓库共享目录 .hermes/updates（R8），
// 宿主机手工 update.sh（flock）与 runner 内 lab-update（syscall.Flock）争用同一把锁，
// 防止 Web 触发更新与宿主机脚本并发操作同一仓库/compose 项目。
func updateLockFile(repoRoot string) string {
	return filepath.Join(repoRoot, ".hermes", "updates", "lab-update.lock")
}

// AcquireUpdateLock 非阻塞获取更新互斥锁（LOCK_EX|LOCK_NB），成功返回释放函数。
// 持锁失败说明另一更新进程（Web 触发的 runner 或宿主机 update.sh）正在进行。
func AcquireUpdateLock(repoRoot string) (func(), error) {
	path := updateLockFile(repoRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("创建更新锁目录失败: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("打开更新锁失败: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("另一更新进程正在执行（共享锁 %s 被占用），请稍后重试", path)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
