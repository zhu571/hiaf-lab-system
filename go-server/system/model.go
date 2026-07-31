package system

import (
	"errors"
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
	defaultLogDir    = "/tmp"
	defaultMaxSubs   = 4
	subBufferSize    = 512
	defaultTimeout   = 30 * time.Minute
	historyTTL       = time.Hour       // 内存 session 保留时长
	sweepInterval    = 5 * time.Minute // TTL sweep 轮询周期
	ringBufferCap    = 5000            // 内存环形缓冲最大行数
	logFileMaxLines  = 5000            // 磁盘回放时最多重建行数
	tailPollInterval = 200 * time.Millisecond
)

// UpdateSession 记录一次更新任务的内存状态，日志同时落盘到 logFile 供进程重启后回放。
type UpdateSession struct {
	ID        string
	Status    string // "running" | "done"
	ExitCode  int
	OldSHA    string
	NewSHA    string
	LogBuffer *RingBuffer // 内存环形日志缓冲
	history   []SSEEvent  // recoverFromDisk 重建的历史事件序列
	doneEvent SSEEvent    // finish 时记录的最终 done 事件
	logFile   string      // /tmp/lab-update-{id}.log（脚本 tee 写入）
	doneFile  string      // /tmp/lab-update-{id}.done
	subs      map[chan SSEEvent]struct{}
	subsCount int
	maxSubs   int
	seq       int
	done      chan struct{}
	once      sync.Once
	DoneAt    time.Time
	mu        sync.Mutex

	runnerPID    int         // 宿主命名空间执行时的进程组 PID（setsid 后）
	timeoutTimer *time.Timer // 超时看门狗，finish 时 Stop
	tailing      bool        // tail goroutine 是否已在跑
}

// nextSeq 生成递增事件序号（线程安全）。
func (s *UpdateSession) nextSeq() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	return s.seq
}

// RingBuffer 固定大小的环形日志缓冲（内存回放用）。
type RingBuffer struct {
	lines []string
	head  int
	size  int
	cap   int
	mu    sync.RWMutex
}

func NewRingBuffer(capacity int) *RingBuffer {
	return &RingBuffer{lines: make([]string, capacity), cap: capacity}
}

// Append 追加一行，覆盖最旧行。
func (rb *RingBuffer) Append(line string) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.lines[rb.head] = line
	rb.head = (rb.head + 1) % rb.cap
	if rb.size < rb.cap {
		rb.size++
	}
}

// Snapshot 返回按写入顺序的原始行切片（重连回放用）。
func (rb *RingBuffer) Snapshot() []string {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	out := make([]string, 0, rb.size)
	for i := 0; i < rb.size; i++ {
		idx := (rb.head - rb.size + i + rb.cap) % rb.cap
		out = append(out, rb.lines[idx])
	}
	return out
}
