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
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	stepRe      = regexp.MustCompile(`===== 步骤 (\d+)/(\d+)：(.+) =====`)
	ansiRe      = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	sessionIDRe = regexp.MustCompile(`^upd_[a-z0-9]{10}$`)
)

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

// validSessionID 校验 sessionID 白名单格式，防止把任意 URL 参数拼进文件路径。
func validSessionID(id string) bool {
	return sessionIDRe.MatchString(id)
}

type Service struct {
	repoRoot   string
	scriptPath string
	mu         sync.Mutex
	active     *UpdateSession
	sessions   map[string]*UpdateSession // 全部 session（含已完成，TTL 后 sweep）
	logDir     string                    // 日志目录，默认 /tmp（UPDATE_LOG_DIR 可覆盖）
	maxSubs    int                       // 单 session 订阅上限，默认 4
	timeout    time.Duration             // 脚本超时，默认 30min

	// UPDATE_ENGINE 开关：go → 新流水线（Step 表驱动）；shell → 原 update.sh 兜底（默认）。
	engine string
	runner SessionRunner

	// Go 引擎 runner 派发参数（从 UPDATE_* env 读取，为空/0 时使用合理默认）。
	composeFile string
	projectName string
	runnerImage string
	ntfyURL     string
	backupDir   string
	gitHome     string
	runUID      int
	runGID      int
	dockerGID   int
	force       bool
	dryRun      bool
	noRollback  bool
}

func NewService(repoRoot string) *Service {
	s := &Service{
		repoRoot:    repoRoot,
		scriptPath:  filepath.Join(repoRoot, ".hermes", "update.sh"),
		sessions:    make(map[string]*UpdateSession),
		logDir:      envOrStr("UPDATE_LOG_DIR", defaultLogDir),
		maxSubs:     defaultMaxSubs,
		timeout:     defaultTimeout,
		composeFile: "deploy/docker-compose.yml",
		projectName: envOrStr("UPDATE_PROJECT", ""),
		runnerImage: envOrStr("UPDATE_RUNNER_IMAGE", ""),
		ntfyURL:     envOrStr("UPDATE_NTFY_URL", "http://localhost:8085/lab-system"),
		backupDir:   envOrStr("UPDATE_BACKUP_DIR", ""),
		gitHome:     envOrStr("UPDATE_GIT_HOME", ""),
		runUID:      envOrInt("UPDATE_RUN_UID", 0),
		runGID:      envOrInt("UPDATE_RUN_GID", 0),
		dockerGID:   envOrInt("UPDATE_DOCKER_GID", 0),
	}
	s.applyUpdateFlags(envOrStr("UPDATE_FLAGS", ""))
	s.engine = envOrStr("UPDATE_ENGINE", "")
	if s.engine == "" {
		if runtime.GOOS == "windows" {
			s.engine = "go" // Windows 无 bash setsid，默认走进程内流水线
		} else {
			s.engine = "shell" // 灰度开关：默认保持原 update.sh，P3 起切 go
		}
	}
	s.runner = newRunner(s, s.engine)
	// 后台 TTL sweep：只清"已完成且超期"的 session，运行中的 session 永不回收
	go s.sweepSessions()
	return s
}

// applyUpdateFlags 解析 UPDATE_FLAGS（空格分隔的 --force/--dry-run/--no-rollback）。
func (s *Service) applyUpdateFlags(raw string) {
	for _, f := range strings.Fields(raw) {
		switch f {
		case "--force":
			s.force = true
		case "--dry-run":
			s.dryRun = true
		case "--no-rollback":
			s.noRollback = true
		}
	}
}

func envOrStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envOrInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
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
	if s.active != nil {
		// Status 由 finish 在 sess.mu 下写入，读状态必须持 sess.mu，避免数据竞争
		s.active.mu.Lock()
		running := s.active.Status == "running"
		sid := s.active.ID
		s.active.mu.Unlock()
		if running {
			s.mu.Unlock()
			return "", fmt.Errorf("%w（当前 session: %s）", ErrUpdateInProgress, sid)
		}
	}
	if s.engine == "shell" {
		if _, err := os.Stat(s.scriptPath); err != nil {
			s.mu.Unlock()
			return "", ErrScriptMissing
		}
	}
	rawID, err := nanoid(10)
	if err != nil {
		s.mu.Unlock()
		return "", fmt.Errorf("生成 session ID 失败: %w", err)
	}
	id := "upd_" + rawID
	sess := &UpdateSession{
		ID:        id,
		Status:    "running",
		LogBuffer: NewRingBuffer(ringBufferCap),
		subs:      make(map[chan SSEEvent]struct{}),
		done:      make(chan struct{}),
		logFile:   filepath.Join(s.logDir, "lab-update-"+id+".log"),
		doneFile:  filepath.Join(s.logDir, "lab-update-"+id+".done"),
		runnerFile: filepath.Join(s.logDir, "lab-update-"+id+".runner"),
		maxSubs:   s.maxSubs,
	}
	s.sessions[id] = sess
	s.active = sess
	s.mu.Unlock()

	s.runScript(sess)
	return id, nil
}

// nanoid 生成 n 位随机 ID（crypto/rand + 拒绝采样，避免取模偏差；错误向上传播）。
func nanoid(n int) (string, error) {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	// 拒绝采样：跳过 >= maxByte 的字节，保证各字符均匀（256 非 36 的整数倍）
	const maxByte = 256 - (256 % 36) // 36 = len(chars)，拒绝采样边界
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	b := make([]byte, n)
	for i := range b {
		v := raw[i]
		for v >= maxByte {
			if _, err := rand.Read(raw[i : i+1]); err != nil {
				return "", err
			}
			v = raw[i]
		}
		b[i] = chars[int(v)%len(chars)]
	}
	return string(b), nil
}

// Subscribe 订阅指定 session 的日志流，返回 channel 和断开函数。
// 内存无该 session 时从磁盘文件重建；先回放历史，再转发实时事件。
func (s *Service) Subscribe(sessionID string) (<-chan SSEEvent, func(), error) {
	if !validSessionID(sessionID) {
		return nil, nil, ErrSessionNotFound
	}
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
			if !sess.sendLocked(ch, evt, stop) {
				return
			}
		}
		// 回放完成后确认状态：若已 done，把 done 事件阻塞式补投。
		// 循环处理"finish 恰在回放与检查之间发生"的竞态，广播丢帧由这里兜底。
		for {
			sess.mu.Lock()
			doneEvt := sess.doneEvent
			isDone := sess.Status == "done"
			sess.mu.Unlock()
			if !isDone {
				break
			}
			if doneEvt.Seq <= last {
				return // done 已在回放历史中投递
			}
			if !sess.sendLocked(ch, doneEvt, stop) {
				return
			}
			last = doneEvt.Seq
		}
		// running：实时帧由 broadcast 投递；等待 stop 或 done。
		// done 后再次补投一次，覆盖"finish 在回放检查之后发生"的窗口。
		select {
		case <-stop:
			return
		case <-sess.done:
		}
		sess.mu.Lock()
		doneEvt := sess.doneEvent
		sess.mu.Unlock()
		if doneEvt.Seq > last {
			sess.sendLocked(ch, doneEvt, stop)
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

// runScript 通过 SessionRunner 派发更新执行者（I1：独立于 server 容器的进程/容器），
// 并挂载超时看门狗 + tail goroutine。runner 只通过日志文件输出，Go 侧 tail 该文件重建事件序列。
func (s *Service) runScript(session *UpdateSession) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("runScript panic", "session", session.ID, "err", r)
			s.finish(session, -1, false)
		}
	}()

	id, err := s.runner.Spawn(context.Background(), session)
	if err != nil {
		session.broadcast(SSEEvent{Type: "error", Message: err.Error()})
		s.finish(session, -1, false)
		return
	}
	session.setRunnerID(id)
	s.writeRunnerFile(session, id)

	timer := time.AfterFunc(s.timeout, func() {
		s.killRunner(session)
		session.broadcast(SSEEvent{Type: "error", Message: "更新超时已终止"})
		s.finish(session, -2, false)
	})
	session.setTimeoutTimer(timer)

	// tail goroutine：增量读日志文件 + 轮询 done marker
	session.setTailing(true)
	go s.tailSessionLog(session, 0)
}

// killRunner 停止 runner（docker kill / 进程组 kill / context cancel，由平台实现决定）。
func (s *Service) killRunner(session *UpdateSession) {
	if id := session.getRunnerID(); id != "" {
		_ = s.runner.Kill(id)
	}
}

// runnerAlive 判断 runner 是否仍在运行（docker inspect / 进程组 / goroutine 状态）。
func (s *Service) runnerAlive(sess *UpdateSession) bool {
	if id := sess.getRunnerID(); id != "" {
		return s.runner.Alive(id)
	}
	return false
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
	var partial []byte // 跨 chunk 的未完成行，避免长行被拆成两条事件

	ticker := time.NewTicker(tailPollInterval)
	defer ticker.Stop()

	for {
		buf := make([]byte, 64*1024)
		n, rerr := f.ReadAt(buf, offset)
		if n > 0 {
			offset += int64(n)
			chunk := append(partial, buf[:n]...)
			parts := strings.Split(string(chunk), "\n")
			partial = []byte(parts[len(parts)-1])
			for _, line := range parts[:len(parts)-1] {
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
				// 末尾无换行的最后一行在 marker 出现时补投
				if len(partial) > 0 {
					line := stripANSI(string(partial))
					if line != "" {
						session.ingestLine(line)
					}
					partial = nil
				}
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
		session.mu.Lock()
		if session.timeoutTimer != nil {
			session.timeoutTimer.Stop()
			session.timeoutTimer = nil
		}
		session.Status = "done"
		session.ExitCode = exitCode
		session.DoneAt = time.Now()
		// 与 ingestLine 同锁内联递增 seq（等价 nextSeq()），真正消费序号计数器：
		// 原 seq+1 不递增，会与并发日志行拿到相同 seq，回放时前端按 seq 去重会丢 done。
		session.seq++
		doneEvent := SSEEvent{
			Seq:       session.seq,
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
		s.removeRunnerFile(session)
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

// sendLocked 与 broadcast 共用 sess.mu，串行化"回放补投"与"实时广播"对同一 channel 的写入，
// 保证事件按 seq 有序送达（前端按 seq 去重，回放与实时交错会导致丢帧）。
// 持锁期间 select 只会在 send 可立即完成时选中（缓冲有余位或有接收者），
// 不会因慢客户端无限持锁；stop 关闭后立即让出并由 unsubscribe 收尾。
func (s *UpdateSession) sendLocked(ch chan SSEEvent, evt SSEEvent, stop <-chan struct{}) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case ch <- evt:
		return true
	case <-stop:
		return false
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
// 与 finish 同锁内联递增 seq（等价 nextSeq()）；已 done 时丢弃迟到行，
// 防止行事件 seq 反超 done 事件导致回放时前端按 seq 去重丢掉 done。
func (s *UpdateSession) ingestLine(line string) {
	s.mu.Lock()
	if s.Status == "done" {
		s.mu.Unlock()
		return
	}
	s.seq++
	seq := s.seq
	s.mu.Unlock()
	ts := time.Now().Format(time.RFC3339Nano)
	evt := SSEEvent{
		Seq:       seq,
		Timestamp: ts,
		Type:      "line",
		Text:      line,
	}
	if m := stepRe.FindStringSubmatch(line); m != nil {
		evt.Type = "step"
		evt.Step, _ = strconv.Atoi(m[1])
		evt.StepTotal, _ = strconv.Atoi(m[2])
		evt.Title = m[3]
	}
	s.LogBuffer.Append(seq, ts, line)
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

// writeRunnerFile 持久化 runner 标识（容器名/pid/local），供进程重启后 recoverFromDisk 判断存活。
func (s *Service) writeRunnerFile(session *UpdateSession, id RunnerID) {
	if session.runnerFile == "" {
		return
	}
	_ = os.WriteFile(session.runnerFile, []byte(string(id)+"\n"), 0o644)
}

// readRunnerFile 读取持久化的 runner 标识；不存在或非法时返回空。
func (s *Service) readRunnerFile(session *UpdateSession) RunnerID {
	data, err := os.ReadFile(session.runnerFile)
	if err != nil {
		return ""
	}
	id := strings.TrimSpace(string(data))
	if id == "" {
		return ""
	}
	return RunnerID(id)
}

// removeRunnerFile 移除 runner 标识文件（finish 时调用，防陈旧 ID 误杀/误判）。
func (s *Service) removeRunnerFile(session *UpdateSession) {
	if session.runnerFile != "" {
		_ = os.Remove(session.runnerFile)
	}
}

// removeSessionFiles 清理该 session 的全部临时文件（TTL sweep 时调用）。
func (s *Service) removeSessionFiles(session *UpdateSession) {
	for _, p := range []string{session.logFile, session.doneFile, session.runnerFile} {
		if p != "" {
			_ = os.Remove(p)
		}
	}
}

// recoverFromDisk 进程重启后根据日志文件 + marker 重建 session。
func (s *Service) recoverFromDisk(sessionID string) *UpdateSession {
	if !validSessionID(sessionID) {
		return nil
	}
	logPath := filepath.Join(s.logDir, "lab-update-"+sessionID+".log")
	donePath := logPath + ".done"
	runnerPath := logPath + ".runner"

	// 并发恢复双重检查：已有内存 session 直接复用，避免重复 tail
	s.mu.Lock()
	if existing, ok := s.sessions[sessionID]; ok {
		s.mu.Unlock()
		return existing
	}
	s.mu.Unlock()

	if _, err := os.Stat(logPath); err != nil {
		return nil // 磁盘也没有 → 404
	}

	sess := &UpdateSession{
		ID:        sessionID,
		LogBuffer: NewRingBuffer(ringBufferCap),
		subs:      make(map[chan SSEEvent]struct{}),
		done:      make(chan struct{}),
		logFile:   logPath,
		doneFile:  donePath,
		runnerFile: runnerPath,
		maxSubs:   s.maxSubs,
	}

	data, _ := os.ReadFile(logPath)
	raw := string(data)
	lines := strings.Split(raw, "\n")
	// 文件末尾无换行的最后一段是中断写入的 partial 行（未写完），恢复时丢弃，
	// 避免半个残缺行被当作完整日志事件回放。
	if !strings.HasSuffix(raw, "\n") && len(lines) > 0 {
		lines = lines[:len(lines)-1]
	}
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

	// 无锁阶段读取 runner 标识/存活状态：sess 尚未发布到 map，无并发读者，
	// 而 runnerAlive 内部会再持 sess.mu，不能在持有锁时调用（防自死锁）。
	runnerID := s.readRunnerFile(sess)
	alive := false
	if runnerID != "" {
		sess.runnerID = runnerID
		alive = s.runnerAlive(sess)
	}

	sess.mu.Lock()
	if marker, err := os.ReadFile(donePath); err == nil {
		var m struct {
			ExitCode int    `json:"exit_code"`
			OldSHA   string `json:"old_sha"`
			NewSHA   string `json:"new_sha"`
			EndedAt  string `json:"ended_at"`
		}
		if json.Unmarshal(marker, &m) == nil {
			sess.ExitCode = m.ExitCode
			sess.OldSHA = m.OldSHA
			sess.NewSHA = m.NewSHA
			sess.Status = "done"
			if ended, err := time.Parse(time.RFC3339, m.EndedAt); err == nil {
				sess.DoneAt = ended
			} else {
				sess.DoneAt = time.Now()
			}
			sess.doneEvent = SSEEvent{
				Seq:       len(sess.history) + 1,
				Timestamp: sess.DoneAt.Format(time.RFC3339Nano),
				Type:      "done",
				ExitCode:  m.ExitCode,
				Success:   m.ExitCode == 0,
				OldSHA:    m.OldSHA,
				NewSHA:    m.NewSHA,
			}
			sess.history = append(sess.history, sess.doneEvent)
		} else {
			// marker 损坏 → 判中断
			sess.Status = "done"
			sess.DoneAt = time.Now()
			sess.history = append(sess.history, SSEEvent{
				Seq:       len(sess.history) + 1,
				Timestamp: sess.DoneAt.Format(time.RFC3339Nano),
				Type:      "error",
				Message:   "更新状态文件损坏，请重新触发",
			})
		}
	} else {
		// 无 marker：从 runner 文件恢复 runner 标识，判断是否仍在运行
		sess.runnerID = runnerID
		if alive {
			// runner 因脱离仍在运行 → 继续 tail
			sess.Status = "running"
			sess.seq = len(sess.history)
		} else {
			// runner 被 kill、更新中断
			sess.Status = "done"
			sess.DoneAt = time.Now()
			sess.history = append(sess.history, SSEEvent{
				Seq:       len(sess.history) + 1,
				Timestamp: sess.DoneAt.Format(time.RFC3339Nano),
				Type:      "error",
				Message:   "服务重启导致更新中断，请重新触发",
			})
		}
	}
	sess.mu.Unlock()

	// 注册进内存 map；并发恢复时后到者复用先到者的 session
	var shouldTail bool
	s.mu.Lock()
	if existing, ok := s.sessions[sessionID]; ok && existing != sess {
		s.mu.Unlock()
		return existing
	}
	if sess.Status == "running" && (s.active == nil || s.active == sess) {
		s.active = sess
	}
	s.sessions[sessionID] = sess
	s.mu.Unlock()

	sess.mu.Lock()
	if sess.Status == "running" && !sess.tailing {
		sess.tailing = true
		shouldTail = true
	}
	sess.mu.Unlock()

	if shouldTail {
		// 从"已读字节数"处续读，避免 ReadFile 与重新 Stat 之间新增的行丢失
		startOffset := int64(len(data))
		// 重启后原超时看门狗已丢，重新挂载；期间若已 finish 则停止
		timer := time.AfterFunc(s.timeout, func() {
			s.killRunner(sess)
			sess.broadcast(SSEEvent{Type: "error", Message: "更新超时已终止"})
			s.finish(sess, -2, false)
		})
		sess.mu.Lock()
		if sess.Status == "done" {
			timer.Stop()
		} else {
			sess.timeoutTimer = timer
		}
		sess.mu.Unlock()
		go s.tailSessionLog(sess, startOffset)
	}
	return sess
}

// replaySnapshot 返回按真实 seq 排序的历史事件快照。
func (s *UpdateSession) replaySnapshot() []SSEEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.history != nil {
		return append([]SSEEvent{}, s.history...)
	}
	var out []SSEEvent
	for _, ln := range s.LogBuffer.Snapshot() {
		evt := SSEEvent{Seq: ln.seq, Timestamp: ln.ts, Type: "line", Text: ln.text}
		if m := stepRe.FindStringSubmatch(ln.text); m != nil {
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
		s.sweepOnce()
	}
}

// sweepOnce 清理超期的已完成 session，并删除其日志/marker/pid 临时文件。
func (s *Service) sweepOnce() {
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
			s.removeSessionFiles(sess)
		}
	}
	s.mu.Unlock()
}
