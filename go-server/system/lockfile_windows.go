//go:build windows

package system

// AcquireUpdateLock Windows 本地开发实现：无 flock 概念（runner 为进程内 goroutine，
// 单进程内由 Service.active 互斥），直接返回空实现保证可编译可测。
func AcquireUpdateLock(repoRoot string) (func(), error) {
	return func() {}, nil
}
