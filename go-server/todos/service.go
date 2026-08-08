package todos

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

var (
	ErrNotFound        = errors.New("待办不存在")
	ErrForbidden       = errors.New("当前用户无权操作该待办")
	ErrAgentRejected   = errors.New("agent 角色不允许操作待办")
	ErrInvalidInput    = errors.New("请求参数无效")
	ErrRateLimited     = errors.New("操作过于频繁，请稍后再试")
	ErrStateConflict   = errors.New("待办状态已变更")
	ErrVersionConflict = errors.New("待办已被修改，请刷新后重试")
	// ErrInvalidProvisionToken provision_token 无效/过期/已兑换/不属于当前用户。
	ErrInvalidProvisionToken = errors.New("provision token 无效或已过期")
)

// todoRepository 只读写 todos 表。
type todoRepository interface {
	Create(*Todo) (*Todo, error)
	InsertGenerated(*Todo) (bool, error)
	GetByID(id string) (*Todo, error)
	List(userID string, projectIDs []string, p ListParams) ([]Todo, error)
	UpdateDone(id, completedBy string, now time.Time) (bool, error)
	UpdateDefer(id, tomorrow string, now time.Time) (bool, error)
	UpdateEdit(id string, oldUpdatedAt time.Time, req UpdateRequest, now time.Time) (bool, error)
	Delete(id string) (bool, error)
	Rollover(today string, now time.Time) (int64, error)
	IssueSync() (int64, error)
	Cleanup(createdForCutoff string, createdAtCutoff time.Time) (int64, int64, error)
	OpenVisibleForUser(userID string, projectIDs []string, date string) ([]Todo, error)
	InflightIssueIDs(userID string) (map[string]bool, error)
	CleanupHintCandidates(userID, cutoffDate string) ([]Todo, error)
}

type snapshotReader interface {
	ActiveUsers() ([]UserSnapshot, error)
	UserByID(userID string) (*UserSnapshot, error)
	OpenIssuesForUser(userID string) ([]IssueSnapshot, error)
	MyProjectIDs(userID string) ([]string, error)
	ProjectRole(userID, projectID string) (string, bool, error)
}

type permChecker interface {
	HasPermission(projectID, userID string, perm middleware.Permission) (bool, error)
}

type auditWriter interface {
	WriteSystemAudit(ctx context.Context, action string, detail map[string]any) error
}

// llmPlanner 封装 py-agent 的 todo-add / todo-daily 两个端点。
type llmPlanner interface {
	ParseAdd(ctx context.Context, userID, rawText string) (*LLMParseResponse, error)
	GenerateDaily(ctx context.Context, userID, yesterdayReport string, issues []IssueSnapshot, existingTitles []string) ([]LLMItem, error)
}

// reportFetcher 取用户最近一份非空日报（scheduler 生成时用，经 by-date service token 调用）。
type reportFetcher interface {
	FetchLatestReport(ctx context.Context, userID string) (string, error)
}

type ntfyClient interface {
	EnsureUser(name, password string) error
	ResetPassword(name, password string) error
	EnsureAccess(topic, user, perm string) error
}

// publishClient 封装 ntfy HTTP 发布（todo-publisher Bearer token）。
type publishClient interface {
	Publish(topic, title, message string) error
}

// dbPermChecker 生产权限检查：复用 middleware.HasPermission。
type dbPermChecker struct {
	db *sql.DB
}

func (c dbPermChecker) HasPermission(projectID, userID string, perm middleware.Permission) (bool, error) {
	return middleware.HasPermission(c.db, projectID, userID, perm)
}

// auditWriterImpl 生产审计写入：复用抽取出的 middleware.WriteSystemAudit。
type auditWriterImpl struct {
	db *sql.DB
}

func (w auditWriterImpl) WriteSystemAudit(ctx context.Context, action string, detail map[string]any) error {
	return middleware.WriteSystemAudit(ctx, w.db, action, detail)
}

// NewDBPermChecker 构造生产权限检查器（复用 middleware.HasPermission）。
func NewDBPermChecker(db *sql.DB) permChecker {
	return dbPermChecker{db: db}
}

// NewAuditWriter 构造系统审计写入器。
func NewAuditWriter(db *sql.DB) auditWriter {
	return auditWriterImpl{db: db}
}

type Service struct {
	repo       todoRepository
	snap       snapshotReader
	perm       permChecker
	audit      auditWriter
	llm        llmPlanner
	reports    reportFetcher
	ntfy       ntfyClient
	publisher  publishClient
	loc        *time.Location
	now        func() time.Time
	provisions *provisionStore

	// publishRetryDelay 是 publish 401/403 后的重试间隔（测试注入为 0）。
	publishRetryDelay time.Duration
	ensureMu          sync.Mutex
	ensured           map[string]bool

	rlMu    sync.Mutex
	rlCalls map[string][]time.Time
}

// NewService 组装 todos 服务。loc/now 显式注入（Asia/Shanghai 与时钟可测）。
func NewService(repo todoRepository, snap snapshotReader, perm permChecker, audit auditWriter,
	llm llmPlanner, reports reportFetcher, ntfy ntfyClient, publisher publishClient,
	loc *time.Location, now func() time.Time) *Service {
	return &Service{
		repo: repo, snap: snap, perm: perm, audit: audit, llm: llm, reports: reports,
		ntfy: ntfy, publisher: publisher, loc: loc, now: now,
		provisions:        newProvisionStore(),
		publishRetryDelay: 2 * time.Second,
		ensured:           map[string]bool{},
		rlCalls:           map[string][]time.Time{},
	}
}

// ---------- CRUD ----------

func (s *Service) Create(userID, userRole string, req CreateRequest) (*Todo, error) {
	if userRole == auth.RoleAgent {
		return nil, ErrAgentRejected
	}
	title := cleanTitle(req.Title)
	if title == "" {
		return nil, ErrInvalidInput
	}
	priority := clampPriority(req.Priority)
	if req.ProjectID != nil && strings.TrimSpace(*req.ProjectID) != "" {
		pid := strings.TrimSpace(*req.ProjectID)
		ok, err := s.perm.HasPermission(pid, userID, middleware.PermRead)
		if err != nil {
			return nil, err
		}
		if !ok {
			// 非成员添加共享 → 403（§5）
			return nil, ErrForbidden
		}
		req.ProjectID = &pid
	} else {
		req.ProjectID = nil
	}
	t := &Todo{
		Title: title, Priority: priority, Status: StatusPending, Source: SourceManual,
		CreatedBy: userID, CreatedFor: s.todayStr(), ProjectID: req.ProjectID,
	}
	return s.repo.Create(t)
}

// ParseLLM 调 py-agent 解析自然语言为草稿（不落库）；ParseError/上游失败降级为按标题保存。
func (s *Service) ParseLLM(ctx context.Context, userID, userRole, rawText string) (*LLMParseResponse, error) {
	if userRole == auth.RoleAgent {
		return nil, ErrAgentRejected
	}
	if !s.allowOne(userID) {
		return nil, ErrRateLimited
	}
	rawText = strings.TrimSpace(rawText)
	if rawText == "" || len(rawText) > 2000 {
		return nil, ErrInvalidInput
	}
	resp, err := s.llm.ParseAdd(ctx, userID, rawText)
	if err != nil || resp == nil {
		// 解析失败（注入/非法输出/上游不可用）→ ensure_safe 清洗后降级存为标题（§6/§8）。
		reason := "无法解析，已按标题保存"
		return &LLMParseResponse{Status: "ok", Title: cleanTitle(rawText), Priority: PriorityMedium, Reason: &reason}, nil
	}
	if resp.Status == "rejected" {
		return resp, nil
	}
	resp.Title = cleanTitle(resp.Title)
	if resp.Title == "" {
		resp.Title = "（未命名待办）"
	}
	resp.Priority = clampPriority(resp.Priority)
	return resp, nil
}

// LLMAdd 用户确认草稿后落库（draft_id 仅追踪用，草稿内容由前端原样回传，不二次解析）。
func (s *Service) LLMAdd(userID, userRole string, req LLMAddRequest) (*Todo, error) {
	if userRole == auth.RoleAgent {
		return nil, ErrAgentRejected
	}
	title := cleanTitle(req.Title)
	if title == "" {
		return nil, ErrInvalidInput
	}
	t := &Todo{
		Title: title, Priority: clampPriority(req.Priority), Status: StatusPending,
		Source: SourceLLM, CreatedBy: userID, CreatedFor: s.todayStr(),
	}
	return s.repo.Create(t)
}

func (s *Service) List(userID string, p ListParams) ([]Todo, error) {
	if p.Date == "" {
		p.Date = s.todayStr()
	}
	if _, err := time.Parse(time.DateOnly, p.Date); err != nil {
		return nil, ErrInvalidInput
	}
	if p.Scope == "" {
		p.Scope = ScopeAll
	}
	switch p.Scope {
	case ScopeAll, ScopeMine, ScopeShared:
	default:
		return nil, ErrInvalidInput
	}
	if p.Status == "" {
		p.Status = "open"
	}
	switch p.Status {
	case "open", StatusDone, StatusCancelled, "all":
	default:
		return nil, ErrInvalidInput
	}
	if p.Limit <= 0 {
		p.Limit = 100
	}
	var projectIDs []string
	if p.Scope != ScopeMine {
		ids, err := s.snap.MyProjectIDs(userID)
		if err != nil {
			return nil, err
		}
		projectIDs = ids
	}
	items, err := s.repo.List(userID, projectIDs, p)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []Todo{}
	}
	return items, nil
}

// Done 勾选完成：owner 或共享项 active 成员（viewer 只读）；状态守卫 0 rows → 409。
func (s *Service) Done(id, userID string, userRole string) (*Todo, error) {
	if userRole == auth.RoleAgent {
		return nil, ErrAgentRejected
	}
	if _, err := s.authorizeWrite(id, userID, "done"); err != nil {
		return nil, err
	}
	ok, err := s.repo.UpdateDone(id, userID, s.now())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrStateConflict
	}
	return s.repo.GetByID(id)
}

// Defer 推迟到明天（Asia/Shanghai）：仅 owner；状态守卫 0 rows → 409。
func (s *Service) Defer(id, userID, userRole string) (*Todo, error) {
	if userRole == auth.RoleAgent {
		return nil, ErrAgentRejected
	}
	todo, err := s.authorizeWrite(id, userID, "defer")
	if err != nil {
		return nil, err
	}
	if todo.CreatedBy != userID {
		return nil, ErrForbidden
	}
	tomorrow := s.now().In(s.loc).AddDate(0, 0, 1).Format(time.DateOnly)
	ok, err := s.repo.UpdateDefer(id, tomorrow, s.now())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrStateConflict
	}
	return s.repo.GetByID(id)
}

// Edit 编辑（仅 owner）：updated_at 乐观锁，0 rows → 409 version_conflict。
func (s *Service) Edit(id, userID, userRole string, req UpdateRequest) (*Todo, error) {
	if userRole == auth.RoleAgent {
		return nil, ErrAgentRejected
	}
	if req.UpdatedAt == nil {
		return nil, ErrInvalidInput
	}
	todo, err := s.authorizeWrite(id, userID, "edit")
	if err != nil {
		return nil, err
	}
	if todo.CreatedBy != userID {
		return nil, ErrForbidden
	}
	if req.Title != nil {
		title := cleanTitle(*req.Title)
		if title == "" {
			return nil, ErrInvalidInput
		}
		req.Title = &title
	}
	if req.Priority != nil {
		priority := clampPriority(*req.Priority)
		req.Priority = &priority
	}
	if req.ProjectID != nil {
		pid := strings.TrimSpace(*req.ProjectID)
		if pid != "" {
			// 重新共享：校验是项目成员；空串 = 取消共享（置 NULL，见 api-contract §3.9）。
			ok, err := s.perm.HasPermission(pid, userID, middleware.PermRead)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, ErrForbidden
			}
		}
		req.ProjectID = &pid
	}
	ok, err := s.repo.UpdateEdit(id, *req.UpdatedAt, req, s.now())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrVersionConflict
	}
	return s.repo.GetByID(id)
}

func (s *Service) Delete(id, userID, userRole string) error {
	if userRole == auth.RoleAgent {
		return ErrAgentRejected
	}
	todo, err := s.authorizeWrite(id, userID, "delete")
	if err != nil {
		return err
	}
	if todo.CreatedBy != userID {
		return ErrForbidden
	}
	ok, err := s.repo.Delete(id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// NotificationTopic 幂等无副作用：只返回当前用户的 topic 与订阅地址。
func (s *Service) NotificationTopic(userID string) (*NotificationTopic, error) {
	topic := topicForUser(userID)
	return &NotificationTopic{Topic: topic, SubscribeURL: subscribeURL(topic)}, nil
}

// Provision 生成订阅凭据：重置 ntfy 密码 + 签发一次性 provision_token（24h TTL，再次 provision 作废旧 token）。
func (s *Service) Provision(userID, userRole string) (*ProvisionResponse, error) {
	if userRole == auth.RoleAgent {
		return nil, ErrAgentRejected
	}
	if !s.allowOne(userID) {
		return nil, ErrRateLimited
	}
	user, err := s.snap.UserByID(userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrNotFound
	}
	password, err := randomPassword()
	if err != nil {
		return nil, err
	}
	if err := s.ntfy.ResetPassword("todo-"+user.Username, password); err != nil {
		return nil, fmt.Errorf("重置 ntfy 密码失败: %w", err)
	}
	entry := s.provisions.put(userID, password, s.now().Add(24*time.Hour))
	return &ProvisionResponse{ProvisionToken: entry.token, ExpiresAt: entry.expiresAt}, nil
}

// Redeem 兑换 provision_token：一次性（兑换即删除），返回 ntfy 账号与一次性密码。
func (s *Service) Redeem(userID, userRole, token string) (*RedeemResponse, error) {
	if userRole == auth.RoleAgent {
		return nil, ErrAgentRejected
	}
	if !s.allowOne(userID) {
		return nil, ErrRateLimited
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrInvalidInput
	}
	entry, ok := s.provisions.take(token, s.now())
	if !ok || entry.userID != userID {
		// token 归属校验：provision_token 只对签发对象有效（防泄漏后被他人兑换）。
		return nil, ErrInvalidProvisionToken
	}
	user, err := s.snap.UserByID(entry.userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrNotFound
	}
	return &RedeemResponse{
		Username: "todo-" + user.Username,
		Password: entry.password,
		Topic:    topicForUser(entry.userID),
	}, nil
}

// ---------- 权限与工具 ----------

// authorizeWrite 读取待办并判定可见性：owner 全权；共享项 active 成员可见
// （viewer 只读 → 写操作 403）；无可见性 → 404（不区分不存在与无权限，防猜 id）。
func (s *Service) authorizeWrite(id, userID, action string) (*Todo, error) {
	todo, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if todo == nil {
		return nil, ErrNotFound
	}
	if todo.CreatedBy == userID {
		return todo, nil
	}
	if todo.ProjectID == nil {
		return nil, ErrNotFound
	}
	role, member, err := s.snap.ProjectRole(userID, *todo.ProjectID)
	if err != nil {
		return nil, err
	}
	if !member {
		return nil, ErrNotFound
	}
	if role == auth.RoleViewer {
		// viewer 对共享项只读：不可完成/推迟/编辑/删除（§5 权限矩阵）。
		return nil, ErrForbidden
	}
	if action == "done" {
		return todo, nil
	}
	// defer/edit/delete 仅添加者（owner）可做。
	return nil, ErrForbidden
}

// allowOne 进程内限流：10 次/分钟/用户（复制自 steptemplates/service.go:319，
// 若后续抽 common/ 共享则迁移）。llm-parse 与 provision/redeem 共用（防滥用/爆破）。
func (s *Service) allowOne(userID string) bool {
	now, cutoff := s.now(), s.now().Add(-time.Minute)
	s.rlMu.Lock()
	defer s.rlMu.Unlock()
	calls := s.rlCalls[userID][:0]
	for _, call := range s.rlCalls[userID] {
		if call.After(cutoff) {
			calls = append(calls, call)
		}
	}
	if len(calls) >= 10 {
		s.rlCalls[userID] = calls
		return false
	}
	s.rlCalls[userID] = append(calls, now)
	return true
}

func (s *Service) todayStr() string {
	return s.now().In(s.loc).Format(time.DateOnly)
}

// cleanTitle 清洗标题：trim → 控制字符剔除（含 \r\n，防 Web 渲染/推送格式伪造）→ 按字符截断 256
// （VARCHAR(256) 按字符计；按 rune 截断避免切断 UTF-8 序列产生非法字节）。
func cleanTitle(title string) string {
	title = strings.TrimSpace(title)
	title = controlChars.ReplaceAllString(title, "")
	title = strings.TrimSpace(title)
	if utf8.RuneCountInString(title) > 256 {
		title = string([]rune(title)[:256])
	}
	return title
}

var controlChars = regexp.MustCompile(`[\x00-\x1f\x7f]`)

func clampPriority(p string) string {
	switch strings.TrimSpace(p) {
	case PriorityHigh, PriorityMedium, PriorityLow:
		return strings.TrimSpace(p)
	default:
		return PriorityMedium
	}
}

// normalizeTitle 归一化用于去重：trim + ToLower + 空白折叠（方案 §2）。
func normalizeTitle(title string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(title))), " ")
}

// severityToPriority 映射 issues severity → todos priority；未覆盖值默认 medium。
func severityToPriority(severity string) string {
	switch severity {
	case "critical", "high":
		return PriorityHigh
	case "low":
		return PriorityLow
	default:
		return PriorityMedium
	}
}

// topicForUser 由 user_id 确定性推导 per-user topic：lab-todos-{sha256(user_id)[:16]}。
func topicForUser(userID string) string {
	sum := sha256.Sum256([]byte(userID))
	return "lab-todos-" + hex.EncodeToString(sum[:])[:16]
}

func subscribeURL(topic string) string {
	base := strings.TrimRight(ntfyPublicURL(), "/")
	return base + "/" + topic
}

func randomPassword() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func logError(action string, err error) {
	slog.Error("todos "+action+" failed", "error", err)
}
