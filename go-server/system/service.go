package system

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	stepRe = regexp.MustCompile(`===== 步骤 (\d+)/(\d+)：(.+) =====`)
	ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
)

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

type Service struct {
	repoRoot   string
	scriptPath string
	mu         sync.Mutex
	active     *UpdateSession
	sessions   map[string]*UpdateSession // 全部 session（含已完成，TTL 后 sweep）
	logDir     string                    // 日志目录，默认 /tmp
	maxSubs    int                       // 单 session 订阅上限，默认 4
	timeout    time.Duration             // 脚本超时，默认 30min
}

func NewService(repoRoot string) *Service {
	s := &Service{
		repoRoot:   repoRoot,
		scriptPath: filepath.Join(repoRoot, ".hermes", "update.sh"),
		sessions:   make(map[string]*UpdateSession),
		logDir:     defaultLogDir,
		maxSubs:    defaultMaxSubs,
		timeout:    defaultTimeout,
	}
	// 后台 TTL sweep：只清"已完成且超期"的 session，运行中的 session 永不回收
	go s.sweepSessions()
	return s
}

// GetVersion 获取当前与远程版本信息。git 不可用/网络不可达时降级返回空值。
func (s *Service) GetVersion() (*VersionInfo, error) {
	current := s.gitRevParse("HEAD")
	latest := s.gitLsRemote()
	behind := 0
	if current != "" && latest != "" {
		behind = s.gitRevListCount(current, "origin/main")
	}
	return &VersionInfo{
		Current:      current,
		CurrentShort: shortSHA(current),
		Latest:       latest,
		LatestShort:  shortSHA(latest),
		Behind:       behind,
		CanUpdate:    latest != "" && latest != current,
	}, nil
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func (s *Service) gitRevParse(rev string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", s.repoRoot, "rev-parse", rev).Output()
	if err != nil {
		slog.Warn("git rev-parse failed", "rev", rev, "error", err)
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitLsRemote 查询远程 origin/HEAD，网络不可达时返回空字符串。
func (s *Service) gitLsRemote() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", s.repoRoot, "ls-remote", "origin", "HEAD")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	if err != nil {
		slog.Warn("git ls-remote failed", "error", err)
		return ""
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func (s *Service) gitRevListCount(a, b string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", s.repoRoot, "rev-list", "--count", a+".."+b).Output()
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

// Trigger 触发一次更新：单例互斥 + 生成 session 并脱离运行脚本。返回 sessionID。
func (s *Service) Trigger() (string, error) {
	s.mu.Lock()
	if s.active != nil && s.active.Status == "running" {
		sid := s.active.ID
		s.mu.Unlock()
		return "", fmt.Errorf("%w（当前 session: %s）", ErrUpdateInProgress, sid)
	}
	if _, err := os.Stat(s.scriptPath); err != nil {
		s.mu.Unlock()
		return "", ErrScriptMissing
	}
	id := "upd_" + nanoid(10)
	sess := &UpdateSession{
		ID:        id,
		Status:    "running",
		LogBuffer: NewRingBuffer(ringBufferCap),
		subs:      make(map[chan SSEEvent]struct{}),
		done:      make(chan struct{}),
		logFile:   filepath.Join(s.logDir, "lab-update-"+id+".log"),
		doneFile:  filepath.Join(s.logDir, "lab-update-"+id+".done"),
		maxSubs:   s.maxSubs,
	}
	s.sessions[id] = sess
	s.active = sess
	s.mu.Unlock()

	s.runScript(sess)
	return id, nil
}

func nanoid(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	seed := uint64(time.Now().UnixNano())<<32 ^ uint64(buf[0])<<24 ^ uint64(buf[1])<<16 ^ uint64(buf[2])<<8 ^ uint64(buf[3])
	b := make([]byte, n)
	for i := range b {
		seed ^= seed << 13
		seed ^= seed >> 7
		seed ^= seed << 17
		b[i] = chars[seed%uint64(len(chars))]
	}
	return string(b)
}

// Subscribe 订阅指定 session 的日志流，返回 channel 和断开函数。
// 内存无该 session 时从磁盘文件重建；先回放历史，再转发实时事件。
func (s *Service) Subscribe(sessionID string) (<-chan SSEEvent, func(), error) {
	sess, ok := s.session(sessionID)
	if !ok {
		sess = s.recoverFromDisk(sessionID)
		if sess == nil {
			return nil, nil, ErrSessionNotFound
		}
	}

	ch, err := sess.subscribe()
	if err != nil {
		return nil, nil, err
	}

	stop := make(chan struct{})
	go func() {
		defer sess.unsubscribe(ch)
		history := sess.replaySnapshot()
		last := 0
		for _, evt := range history {
			last = evt.Seq
			select {
			case ch <- evt:
			case <-stop:
				return
			}
		}
		// 回放完成后确认状态：若已 done，把 done 事件阻塞式补投（修复 #2）。
		sess.mu.Lock()
		doneEvt := sess.doneEvent
		isDone := sess.Status == "done"
		sess.mu.Unlock()
		if isDone {
			if doneEvt.Seq > last {
				select {
				case ch <- doneEvt:
				case <-stop:
					return
				}
			}
			return
		}
		// running：继续从 broadcast 收实时帧，直到 stop/done 关闭
		select {
		case <-stop:
		case <-sess.done:
		}
	}()

	var once sync.Once
	return ch, func() {
		once.Do(func() { close(stop) })
	}, nil
}

func (s *Service) session(sessionID string) (*UpdateSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sessionID]
	return sess, ok
}

// SessionStatus 获取 session 状态（用于重连时判断是否存活）。
func (s *Service) SessionStatus(sessionID string) (*UpdateSession, bool) {
	return s.session(sessionID)
}

// runScript 以 setsid 新会话脱离启动更新脚本（脱离 Go 父进程与 server 容器）。
// 脚本只通过日志文件输出，Go 侧 tail 该文件重建事件序列。
func (s *Service) runScript(session *UpdateSession) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("runScript panic", "session", session.ID, "err", r)
			s.finish(session, -1, false)
		}
	}()

	pid, err := s.spawnDetached(session)
	if err != nil {
		session.broadcast(SSEEvent{Type: "error", Message: err.Error()})
		s.finish(session, -1, false)
		return
	}
	session.runnerPID = pid

	timer := time.AfterFunc(s.timeout, func() {
		s.killRunner(session)
		session.broadcast(SSEEvent{Type: "error", Message: "更新超时已终止"})
		s.finish(session, -2, false)
	})
	session.timeoutTimer = timer

	// tail goroutine：增量读日志文件 + 轮询 done marker
	session.tailing = true
	go s.tailSessionLog(session, 0)
}

// spawnDetached 以 setsid（新会话 + 新进程组）启动脚本，使其脱离 Go 父进程。
// 返回脚本进程的 PID（进程组 PGID 相同，kill 时用负 PID）。
func (s *Service) spawnDetached(session *UpdateSession) (int, error) {
	if _, err := os.Stat(s.scriptPath); err != nil {
		return 0, ErrScriptMissing
	}
	cmd := exec.Command("/bin/bash", s.scriptPath)
	cmd.Dir = s.repoRoot
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Env = append(os.Environ(),
		"UPDATE_SESSION_ID="+session.ID,
		"UPDATE_LOG_FILE="+session.logFile,
		"UPDATE_DONE_FILE="+session.doneFile,
	)
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrScriptStartFailed, err)
	}
	defer devNull.Close()
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrScriptStartFailed, err)
	}
	return cmd.Process.Pid, nil
}

// killRunner 杀掉整个进程组（Setsid 后脚本不是 Go 直系子进程）。
func (s *Service) killRunner(session *UpdateSession) {
	if session.runnerPID > 0 {
		// 先杀进程组，再兜底杀单进程
		_ = syscall.Kill(-session.runnerPID, syscall.SIGKILL)
		_ = syscall.Kill(session.runnerPID, syscall.SIGKILL)
	}
}

// runnerAlive 判断脚本进程是否仍在运行（进程组存在即视为存活）。
func (s *Service) runnerAlive(sess *UpdateSession) bool {
	if sess.runnerPID <= 0 {
		return false
	}
	return syscall.Kill(-sess.runnerPID, 0) == nil || syscall.Kill(sess.runnerPID, 0) == nil
}

// tailSessionLog 每 200ms 增量读日志文件，把新行推入 RingBuffer + 广播；
// 同时轮询 done marker，出现即收尾。
func (s *Service) tailSessionLog(session *UpdateSession, startOffset int64) {
	f, err := os.Open(session.logFile)
	if err != nil {
		s.finish(session, -1, false)
		return
	}
	defer f.Close()
	offset := startOffset
	var lastLineAt time.Time

	ticker := time.NewTicker(tailPollInterval)
	defer ticker.Stop()

	for {
		buf := make([]byte, 64*1024)
		n, rerr := f.ReadAt(buf, offset)
		if n > 0 {
			offset += int64(n)
			for _, line := range strings.Split(string(buf[:n]), "\n") {
				line = stripANSI(line)
				if line == "" {
					continue
				}
				lastLineAt = time.Now()
				session.ingestLine(line)
			}
		}
		if rerr == io.EOF {
			// 检查 done marker：脚本 EXIT trap 写入
			if _, err := os.Stat(session.doneFile); err == nil {
				s.finishFromMarker(session)
				return
			}
			// 无 marker 但 runner 已消亡（被 kill，未走 EXIT trap）→ 判中断
			if time.Since(lastLineAt) > 30*time.Second && !s.runnerAlive(session) {
				session.broadcast(SSEEvent{Type: "error", Message: "更新进程异常终止"})
				s.finish(session, -1, false)
				return
			}
		}
		select {
		case <-session.done: // 已被其它路径 finish（如超时 kill）
			return
		case <-ticker.C:
		}
	}
}

// finishFromMarker 从 .done marker 解析结果并收尾。
func (s *Service) finishFromMarker(session *UpdateSession) {
	data, err := os.ReadFile(session.doneFile)
	if err != nil {
		s.finish(session, -1, false)
		return
	}
	var m struct {
		ExitCode int    `json:"exit_code"`
		OldSHA   string `json:"old_sha"`
		NewSHA   string `json:"new_sha"`
	}
	if json.Unmarshal(data, &m) != nil {
		s.finish(session, -1, false)
		return
	}
	session.mu.Lock()
	session.OldSHA = m.OldSHA
	session.NewSHA = m.NewSHA
	session.mu.Unlock()
	s.finish(session, m.ExitCode, m.ExitCode == 0)
}

// finish 广播 done 事件并标记 session 为 done；sync.Once 保证只执行一次。
func (s *Service) finish(session *UpdateSession, exitCode int, success bool) {
	session.once.Do(func() {
		if session.timeoutTimer != nil {
			session.timeoutTimer.Stop()
		}
		session.mu.Lock()
		session.Status = "done"
		session.ExitCode = exitCode
		session.DoneAt = time.Now()
		doneEvent := SSEEvent{
			Seq:       session.seq + 1,
			Timestamp: time.Now().Format(time.RFC3339Nano),
			Type:      "done",
			ExitCode:  exitCode,
			Success:   success,
			OldSHA:    session.OldSHA,
			NewSHA:    session.NewSHA,
		}
		session.doneEvent = doneEvent
		session.mu.Unlock()

		session.writeMarker(exitCode)
		session.broadcast(doneEvent)
		close(session.done)
	})
}

// broadcast 非阻塞向所有订阅者投递事件；缓冲满时丢帧，由历史回放兜底。
func (s *UpdateSession) broadcast(evt SSEEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- evt:
		default: // 缓冲满 → 丢弃该帧
		}
	}
}

// subscribe 为每个连接创建独立带缓冲 channel，超限返回错误。
func (s *UpdateSession) subscribe() (chan SSEEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.subsCount >= s.maxSubs {
		return nil, ErrTooManySubscribers
	}
	ch := make(chan SSEEvent, subBufferSize)
	s.subs[ch] = struct{}{}
	s.subsCount++
	return ch, nil
}

// unsubscribe 从 map 移除并 close(ch)，与 broadcast 同持 mu 互斥。
func (s *UpdateSession) unsubscribe(ch chan SSEEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.subs[ch]; ok {
		delete(s.subs, ch)
		s.subsCount--
		close(ch) // 通知 handler 退出
	}
}

// ingestLine 把一行日志写入内存 RingBuffer、分配 seq 并广播。
func (s *UpdateSession) ingestLine(line string) {
	evt := SSEEvent{
		Seq:       s.nextSeq(),
		Timestamp: time.Now().Format(time.RFC3339Nano),
		Type:      "line",
		Text:      line,
	}
	if m := stepRe.FindStringSubmatch(line); m != nil {
		evt.Type = "step"
		evt.Step, _ = strconv.Atoi(m[1])
		evt.StepTotal, _ = strconv.Atoi(m[2])
		evt.Title = m[3]
	}
	s.LogBuffer.Append(line)
	s.broadcast(evt)
}

// writeMarker 写 done marker 文件（幂等）。
func (s *UpdateSession) writeMarker(exitCode int) {
	marker, _ := json.Marshal(map[string]any{
		"exit_code": exitCode,
		"old_sha":   s.OldSHA,
		"new_sha":   s.NewSHA,
		"ended_at":  time.Now().Format(time.RFC3339),
	})
	_ = os.WriteFile(s.doneFile, marker, 0o644)
}

// recoverFromDisk 进程重启后根据日志文件 + marker 重建 session。
func (s *Service) recoverFromDisk(sessionID string) *UpdateSession {
	logPath := filepath.Join(s.logDir, "lab-update-"+sessionID+".log")
	donePath := logPath + ".done"
	if _, err := os.Stat(logPath); err != nil {
		return nil // 磁盘也没有 → 404
	}

	sess := &UpdateSession{
		ID:       sessionID,
		subs:     make(map[chan SSEEvent]struct{}),
		done:     make(chan struct{}),
		logFile:  logPath,
		doneFile: donePath,
		maxSubs:  s.maxSubs,
	}

	if data, err := os.ReadFile(logPath); err == nil {
		lines := strings.Split(string(data), "\n")
		if len(lines) > logFileMaxLines {
			lines = lines[len(lines)-logFileMaxLines:]
		}
		for _, ln := range lines {
			if ln == "" {
				continue
			}
			ln = stripANSI(ln)
			evt := SSEEvent{Seq: len(sess.history) + 1, Type: "line", Text: ln}
			if m := stepRe.FindStringSubmatch(ln); m != nil {
				evt.Type = "step"
				evt.Step, _ = strconv.Atoi(m[1])
				evt.StepTotal, _ = strconv.Atoi(m[2])
				evt.Title = m[3]
			}
			sess.history = append(sess.history, evt)
		}
	}

	sess.mu.Lock()
	if data, err := os.ReadFile(donePath); err == nil {
		var m struct {
			ExitCode int    `json:"exit_code"`
			OldSHA   string `json:"old_sha"`
			NewSHA   string `json:"new_sha"`
		}
		if json.Unmarshal(data, &m) == nil {
			sess.ExitCode = m.ExitCode
			sess.OldSHA = m.OldSHA
			sess.NewSHA = m.NewSHA
			sess.Status = "done"
			sess.DoneAt = time.Now()
			sess.doneEvent = SSEEvent{
				Seq:       len(sess.history) + 1,
				Type:      "done",
				ExitCode:  m.ExitCode,
				Success:   m.ExitCode == 0,
				OldSHA:    m.OldSHA,
				NewSHA:    m.NewSHA,
			}
			sess.history = append(sess.history, sess.doneEvent)
		}
	} else if s.runnerAlive(sess) {
		// 脚本因 setsid 脱离仍在运行 → 继续 tail
		sess.Status = "running"
	} else {
		// 进程被 kill、脚本中断
		sess.Status = "done"
		sess.DoneAt = time.Now()
		sess.history = append(sess.history, SSEEvent{
			Seq:     len(sess.history) + 1,
			Type:    "error",
			Message: "服务重启导致更新中断，请重新触发",
		})
	}
	sess.mu.Unlock()

	// 恢复的 running session：seq 接续历史
	if sess.Status == "running" {
		sess.mu.Lock()
		sess.seq = len(sess.history)
		sess.mu.Unlock()
	}

	s.mu.Lock()
	s.sessions[sessionID] = sess
	s.mu.Unlock()

	// runner 仍存活 → 恢复 tail（从文件末尾续读）
	sess.mu.Lock()
	shouldTail := sess.Status == "running" && !sess.tailing
	if shouldTail {
		sess.tailing = true
	}
	sess.mu.Unlock()
	if shouldTail {
		var size int64
		if st, err := os.Stat(logPath); err == nil {
			size = st.Size()
		}
		go s.tailSessionLog(sess, size)
	}
	return sess
}

// replaySnapshot 返回按 seq 排序的历史事件快照。
func (s *UpdateSession) replaySnapshot() []SSEEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.history != nil {
		return append([]SSEEvent{}, s.history...)
	}
	var out []SSEEvent
	for i, line := range s.LogBuffer.Snapshot() {
		evt := SSEEvent{Seq: i + 1, Timestamp: "", Type: "line", Text: line}
		if m := stepRe.FindStringSubmatch(line); m != nil {
			evt.Type = "step"
			evt.Step, _ = strconv.Atoi(m[1])
			evt.StepTotal, _ = strconv.Atoi(m[2])
			evt.Title = m[3]
		}
		out = append(out, evt)
	}
	if s.Status == "done" && s.doneEvent.Seq != 0 {
		out = append(out, s.doneEvent)
	}
	return out
}

// sweepSessions 每 sweepInterval 清理超期（historyTTL）的已完成 session。
// 运行中的 session 绝不能被 sweep 误删。
func (s *Service) sweepSessions() {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		s.mu.Lock()
		for id, sess := range s.sessions {
			sess.mu.Lock()
			expired := sess.Status == "done" && now.Sub(sess.DoneAt) > historyTTL
			sess.mu.Unlock()
			if expired {
				slog.Info("sweep expired update session", "session", id)
				if s.active == sess {
					s.active = nil
				}
				delete(s.sessions, id)
			}
		}
		s.mu.Unlock()
	}
}
