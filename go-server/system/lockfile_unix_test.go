//go:build !windows

package system

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAcquireUpdateLockExclusive R8：同一仓库的更新锁互斥——持锁期间第二次获取
// 必须失败（对应宿主机 update.sh 的 flock -n 争用同一文件），释放后可重新获取。
func TestAcquireUpdateLockExclusive(t *testing.T) {
	dir := t.TempDir()
	release, err := AcquireUpdateLock(dir)
	if err != nil {
		t.Fatalf("首次获取锁: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".hermes", "updates", "lab-update.lock")); err != nil {
		t.Fatalf("锁文件应落在仓库共享目录: %v", err)
	}

	if _, err2 := AcquireUpdateLock(dir); err2 == nil {
		t.Fatal("持锁期间第二次获取应失败")
	} else if !strings.Contains(err2.Error(), "另一更新进程") {
		t.Errorf("错误应可操作: %v", err2)
	}

	release()

	release2, err := AcquireUpdateLock(dir)
	if err != nil {
		t.Fatalf("释放后应可重新获取: %v", err)
	}
	release2()
}
