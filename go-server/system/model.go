package system

import (
	"errors"
	"os"
	"sync"
	"time"
)

type VersionInfo struct {
	Current      string `json:"current"`
	CurrentShort string `json:"current_short"`
	Latest       string `json:"latest"`
	LatestShort  string `json:"latest_short"`
	Behind       int    `json:"behind"`
	CanUpdate    bool   `json:"can_update"`
}

type TriggerResponse struct {
	SessionID string `json:"session_id"`
	Current   string `json:"current"`
}

// SSEEvent 是推送给前端的一条更新日志事件。
type SSEEvent struct {
	Seq       int    `json:"seq"`
	Timestamp string `json:"ts"`
	Type      string `json:"type"` // "line" | "step" | "done" | "error"
	// line
	Text string `json:"text,omitempty"`
	// step
	Step      int    `json:"step,omitempty"`
	StepTotal int    `json:"step_total,omitempty"`
	Title     string `json:"title,omitempty"`
	// done
	ExitCode int    `json:"exit_code,omitempty"`
	Success  bool   `json:"success,omitempty"`
	OldSHA   string `json:"old_sha,omitempty"`
	NewSHA   string `json:"new_sha,omitempty"`
	// error
	Message string `json:"message,omitempty"`
}

var (
	ErrUpdateInProgress   = errors.New("已有更新任务正在执行")
	ErrSessionNotFound    = errors.New("session 不存在")
	ErrTooManySubscribers = errors.New("订阅者过多")
	ErrScriptMissing      = errors.New("更新脚本不存在")
	ErrScriptStartFailed  = errors.New("更新脚本启动失败")
)

const (
	defaultMaxSubs   = 4
	subBufferSize    = 512
	defaultTimeout   = 30 * time.Minute
	historyTTL       = time.Hour       // 内存 session 保留时长
	sweepInterval    = 5 * time.Minute // TTL sweep 轮询周期
	ringBufferCap    = 5000            // 内存环形缓冲最大行数
	logFileMaxLines  = 5000            // 磁盘回放时最多重建行数
	tailPollInterval = 200 * time.Millisecond
	sendTimeout      = 5 * time.Second // sendLocked 持锁等待慢客户端的上限，超时判死客户端
	versionCacheTTL  = time.Minute       // GetVersion 结果缓存时长（避免每次跑 3 个 git 子进程）
	tailOpenRetries  = 50 // 等待 runner 创建日志文件的最多重试次数（50×200ms = 10s）
)

// defaultLogDir 返回默认日志目录（跨平台临时目录；UPDATE_LOG_DIR 可覆盖，
// 生产部署要求 server 与 runner 容器以相同宿主机绝对路径挂载该目录）。
func defaultLogDir() string {
	return os.TempDir()
}

// UpdateSession 记录一次更新任务的内存状态，日志同时落盘到 logFile 供进程重启后回放。
type UpdateSession struct {
	ID         string
	Status     string // "running" | "done"
	ExitCode   int
	OldSHA     string
	NewSHA     string
	LogBuffer  *RingBuffer // 内存环形日志缓冲
	history    []SSEEvent  // recoverFromDisk 重建的历史事件序列
	doneEvent  SSEEvent    // finish 时记录的最终 done 事件
	logFile    string      // {logDir}/lab-update-{id}.log（runner 写入）
	doneFile   string      // {logDir}/lab-update-{id}.done
	runnerFile string      // {logDir}/lab-update-{id}.runner（持久化 runner ID，重启恢复用）
	subs       map[chan SSEEvent]struct{}
	subsCount  int
	maxSubs    int
	seq        int
	done       chan struct{}
	once       sync.Once
	DoneAt     time.Time
	mu         sync.Mutex

	runnerID     RunnerID    // runner 标识（lab-updater-<id> / pid:N / local:<id>）
	timeoutTimer *time.Timer // 超时看门狗，finish 时 Stop
	tailing      bool        // tail goroutine 是否已在跑
}

// setRunnerID 记录 runner 标识（线程安全，供 killRunner/runnerAlive 读取）。
func (s *UpdateSession) setRunnerID(id RunnerID) {
	s.mu.Lock()
	s.runnerID = id
	s.mu.Unlock()
}

// getRunnerID 读取 runner 标识（线程安全）。
func (s *UpdateSession) getRunnerID() RunnerID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runnerID
}

// setTimeoutTimer 设置超时看门狗（线程安全，finish 时读取并 Stop）。
func (s *UpdateSession) setTimeoutTimer(t *time.Timer) {
	s.mu.Lock()
	s.timeoutTimer = t
	s.mu.Unlock()
}

// setTailing 标记 tail goroutine 是否已在跑（线程安全，防重复启动）。
func (s *UpdateSession) setTailing(on bool) {
	s.mu.Lock()
	s.tailing = on
	s.mu.Unlock()
}

// RingBuffer 固定大小的环形日志缓冲（内存回放用）。
// 保存真实 seq/timestamp，重连回放时按原序号投递，前端可按 seq 精确去重。
type ringLine struct {
	seq  int
	ts   string
	text string
}

type RingBuffer struct {
	lines []ringLine
	head  int
	size  int
	cap   int
	mu    sync.RWMutex
}

func NewRingBuffer(capacity int) *RingBuffer {
	return &RingBuffer{lines: make([]ringLine, capacity), cap: capacity}
}

// Append 追加一行（携带真实 seq/timestamp），覆盖最旧行。
func (rb *RingBuffer) Append(seq int, ts, line string) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.lines[rb.head] = ringLine{seq: seq, ts: ts, text: line}
	rb.head = (rb.head + 1) % rb.cap
	if rb.size < rb.cap {
		rb.size++
	}
}

// Snapshot 返回按写入顺序的行切片（重连回放用），包含真实 seq/timestamp。
func (rb *RingBuffer) Snapshot() []ringLine {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	out := make([]ringLine, 0, rb.size)
	for i := 0; i < rb.size; i++ {
		idx := (rb.head - rb.size + i + rb.cap) % rb.cap
		out = append(out, rb.lines[idx])
	}
	return out
}
