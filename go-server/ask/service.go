package ask

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/lib/pq"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

var (
	ErrNotFound     = errors.New("问答记录不存在")
	ErrInvalidInput = errors.New("请求参数无效")
	ErrRateLimited  = errors.New("请求过于频繁，请稍后再试")
	ErrUpstream     = errors.New("py-agent 上游服务错误")
	ErrSQLRejected  = errors.New("SQL 校验未通过")
	ErrSQLExec      = errors.New("SQL 执行失败")
)

// mainTables 是 ask 通道可 SELECT 的主表（R2 行级隔离收缩为 10 张：7 张项目
// 业务表 + projects + 2 张全局模板表）。daily_reports（个人表）、issue_comments /
// run_steps / attachments / instrument_results（无 project_id 的内容表）、todos
// （个人表）、project_members / automation_rules（跨项目敏感表）已移出白名单。
// DB 层 ask_reader GRANT（迁移 033，18 表）保持不变——Go 解析层更严，纵深不降级。
var mainTables = []string{
	"logs", "issues", "experiences", "test_data", "rf_matching_records",
	"assembly_steps", "experiment_runs", "step_templates", "step_template_items", "projects",
}

// projectFilterColumns 是需要行级隔离的表 → 过滤列：项目业务表按 project_id、
// projects 表按 id 注入 `IN (调用方可访问项目集合)`（对齐路由层
// RequireProjectPermission 的 PermRead 语义，admin 为全部 active 项目）。
// 不在表中的白名单表（step_templates/step_template_items）为全员可读的全局表。
var projectFilterColumns = map[string]string{
	"logs":                "project_id",
	"issues":              "project_id",
	"experiences":         "project_id",
	"test_data":           "project_id",
	"rf_matching_records": "project_id",
	"assembly_steps":      "project_id",
	"experiment_runs":     "project_id",
	"projects":            "id",
}

const (
	maxRows          = 200       // 行数封顶（方案 §5 防线 4）
	cellMaxRunes     = 500       // 单元格截断（长文本）
	snapshotBudget   = 256 << 10 // rows 快照总字节 ≤256KB
	schemaTTL        = 10 * time.Minute
	statementTimeout = 5000 // ms
	retentionDays    = 90   // 快照保留天数默认值（ASK_HISTORY_RETENTION_DAYS 可调）
	retentionTick    = 24 * time.Hour
	// AI-3 上下文注入：最近 7 天日报（≤5 条，每条 ≤200 字）+ 最近项目（≤3 个），
	// 总量 <4K 字符防 token 浪费；获取失败静默降级（不影响问答）。
	contextReportDays   = 7
	contextReportLimit  = 5
	contextProjectLimit = 3
	contextReportRunes  = 200
	contextBudgetRunes  = 4096
)

// 防线 2+3 正则（见方案 §5）。
var (
	fromKwRe   = regexp.MustCompile(`(?i)\bfrom\b`)
	whereKwRe  = regexp.MustCompile(`(?i)\bwhere\b`)
	orderByRe  = regexp.MustCompile(`(?i)\border\s+by\b`)
	groupByRe  = regexp.MustCompile(`(?i)\bgroup\s+by\b`)
	offsetRe   = regexp.MustCompile(`(?i)\boffset\b`)
	identRe    = regexp.MustCompile(`(?i)^[a-z_][a-z0-9_]*`)
	quotedRe   = regexp.MustCompile(`(?i)^"([^"]*)"`)
	joinRe     = regexp.MustCompile(`(?i)\bjoin\b`)
	writeRe    = regexp.MustCompile(`(?i)\b(insert|update|delete|merge|copy|into)\b`)
	forShareRe = regexp.MustCompile(`(?i)\bfor\s+share\b`)
	unionRe    = regexp.MustCompile(`(?i)\b(union|intersect|except)\b`)
	overRe     = regexp.MustCompile(`(?i)\bover\b`)
	subqueryRe = regexp.MustCompile(`(?i)\(\s*(select|values|table)\b`)
	versionRe  = regexp.MustCompile(`(?i)\bversion\s*\(`)
	limitRe    = regexp.MustCompile(`(?i)\blimit\b`)
	limitNumRe = regexp.MustCompile(`(?i)\blimit\s+(\d+)`)
)

// schemaRelations 是 BuildSchema 末尾附加的表间关系提示，帮助 py-agent 生成带
// project_id 过滤的查询。
const schemaRelations = "-- relations: logs.project_id → projects.id; test_data.project_id → projects.id; issues.project_id → projects.id"

// fromClauseEnd 是 FROM 表清单的终止关键字（遇之停止解析表引用）。
var fromClauseEnd = map[string]bool{
	"where": true, "group": true, "order": true, "limit": true, "offset": true,
	"having": true, "union": true, "intersect": true, "except": true, "fetch": true,
	"for": true, "into": true, "window": true, "returning": true, "natural": true,
	"inner": true, "left": true, "right": true, "full": true, "cross": true,
	"join": true, "on": true, "using": true,
}

var mainTableSet = func() map[string]bool {
	m := make(map[string]bool, len(mainTables))
	for _, t := range mainTables {
		m[t] = true
	}
	return m
}()

type Service struct {
	repo       *Repository
	db         *sql.DB
	client     *http.Client
	agentURL   string
	agentToken string
	rlMu       sync.Mutex
	rlCalls    map[string][]time.Time
	schemaMu   sync.Mutex
	schemaText string
	schemaAt   time.Time
	retention  sync.Mutex // 防保留任务重入（与 todos scheduler 互斥模式一致）
}

func NewService(repo *Repository, db *sql.DB) *Service {
	return &Service{
		repo:    repo,
		db:      db,
		client:  &http.Client{Timeout: 60 * time.Second},
		rlCalls: map[string][]time.Time{},
	}
}

// AutoConfigure 读取 py-agent 地址与内部 token（与 steptemplates 同机制）。
func (s *Service) AutoConfigure() {
	url := strings.TrimRight(os.Getenv("PY_AGENT_INTERPRET_URL"), "/")
	tokenPath := os.Getenv("PY_AGENT_INTERNAL_TOKEN_FILE")
	var token string
	if tokenPath != "" {
		if data, err := os.ReadFile(filepath.Clean(tokenPath)); err == nil {
			token = strings.TrimSpace(string(data))
		}
	}
	if url != "" && token != "" {
		s.agentURL = url
		s.agentToken = token
	}
}

// StartupCheck 启动差集校验：白名单表 vs information_schema.tables，差集非空打警告日志。
func (s *Service) StartupCheck(ctx context.Context) {
	missing, err := s.whitelistDiff(ctx)
	if err != nil {
		slog.Warn("ask whitelist diff check failed", "error", err)
		return
	}
	if len(missing) > 0 {
		slog.Warn("ask whitelist tables missing from database",
			"missing", missing, "hint", "须同步 GRANT SELECT ON <新表> TO ask_reader")
	}
}

func (s *Service) whitelistDiff(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT table_name FROM information_schema.tables WHERE table_schema = 'public'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	present := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		present[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var missing []string
	for _, t := range mainTables {
		if !present[t] {
			missing = append(missing, t)
		}
	}
	return missing, nil
}

// BuildSchema 组装紧凑 schema 文本（表名: 列名(类型) 说明，说明为空省略），
// 内存缓存 + 10min 刷新（方案 §3）。
func (s *Service) BuildSchema(ctx context.Context) (string, error) {
	s.schemaMu.Lock()
	defer s.schemaMu.Unlock()
	if s.schemaText != "" && time.Since(s.schemaAt) < schemaTTL {
		return s.schemaText, nil
	}
	if missing, err := s.whitelistDiff(ctx); err == nil && len(missing) > 0 {
		slog.Warn("ask whitelist tables missing from database",
			"missing", missing, "hint", "须同步 GRANT SELECT ON <新表> TO ask_reader")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.table_name, c.column_name, c.data_type,
		        col_description(format('%I.%I', c.table_schema, c.table_name)::regclass, c.ordinal_position)
		 FROM information_schema.columns c
		 WHERE c.table_schema = 'public' AND c.table_name = ANY($1)
		 ORDER BY c.table_name, c.ordinal_position`, pq.Array(mainTables))
	if err != nil {
		return "", fmt.Errorf("query information_schema: %w", err)
	}
	defer rows.Close()

	var b strings.Builder
	cur := ""
	for rows.Next() {
		var table, col, dtype string
		var comment sql.NullString
		if err := rows.Scan(&table, &col, &dtype, &comment); err != nil {
			return "", fmt.Errorf("scan schema row: %w", err)
		}
		if table != cur {
			if cur != "" {
				b.WriteString("\n")
			}
			b.WriteString(table + ":")
			cur = table
		}
		b.WriteString(" " + col + "(" + dtype + ")")
		if comment.Valid && comment.String != "" {
			b.WriteString(" " + comment.String)
		}
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate schema rows: %w", err)
	}
	if b.Len() > 0 {
		b.WriteString("\n")
	}
	b.WriteString(schemaRelations)
	s.schemaText, s.schemaAt = b.String(), time.Now()
	return s.schemaText, nil
}

// buildContext 组装 AI-3 上下文（最近 7 天【当前用户自己】的日报摘要 + 自己
// 可见的项目）：日报取 summary（为空回退 raw_text），每条截断到 200 字；总量
// 截断到 <4K 字符。R2：只取当前用户自己的数据，不再把全员日报摘要发给外部 LLM。
// 数据为空返回空串；错误上抛由调用方静默降级。
func (s *Service) buildContext(ctx context.Context, userID string) (string, error) {
	reports, err := s.repo.RecentDailyReports(ctx, userID, contextReportDays, contextReportLimit)
	if err != nil {
		return "", err
	}
	projects, err := s.repo.RecentProjects(ctx, userID, contextProjectLimit)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if len(reports) > 0 {
		b.WriteString("最近 7 天日报摘要：\n")
		for _, r := range reports {
			text := strings.TrimSpace(r.Summary)
			if text == "" {
				text = strings.TrimSpace(r.RawText)
			}
			if text == "" {
				continue
			}
			if n := utf8.RuneCountInString(text); n > contextReportRunes {
				text = string([]rune(text)[:contextReportRunes])
			}
			b.WriteString("- ")
			b.WriteString(r.ReportDate)
			b.WriteString(": ")
			b.WriteString(text)
			b.WriteString("\n")
		}
	}
	if len(projects) > 0 {
		b.WriteString("实验室项目：\n")
		for _, p := range projects {
			b.WriteString("- ")
			b.WriteString(p.Code)
			b.WriteString(" ")
			b.WriteString(p.Name)
			b.WriteString(" (")
			b.WriteString(p.Status)
			b.WriteString(")\n")
		}
	}
	out := strings.TrimRight(b.String(), "\n")
	if n := utf8.RuneCountInString(out); n > contextBudgetRunes {
		out = string([]rune(out)[:contextBudgetRunes])
	}
	return out, nil
}

// chatPayload 组装发给 py-agent /v1/ask 的载荷；context 为空时省略该键（零行为变化）。
// user_id（R2）随载荷下发，py-agent 在 /ask/execute 回调时回传，Go 侧按该用户
// 可访问项目集合做行级隔离。
func chatPayload(question, schema, contextText, userID string) map[string]any {
	payload := map[string]any{"question": question, "schema": schema, "user_id": userID}
	if contextText != "" {
		payload["context"] = contextText
	}
	return payload
}

// Chat 编排：组装 schema → 调 py-agent /v1/ask → 存 ask_history（含 rows 快照）→ 返回。
func (s *Service) Chat(ctx context.Context, userID, question string) (*ChatResponse, error) {
	question = strings.TrimSpace(question)
	if question == "" || utf8.RuneCountInString(question) > 1000 {
		return nil, fmt.Errorf("%w: 问题需 1-1000 字符", ErrInvalidInput)
	}
	if !s.allowOne(userID) {
		return nil, ErrRateLimited
	}
	if s.agentURL == "" || s.agentToken == "" {
		return nil, fmt.Errorf("AI 查询服务未配置")
	}
	schema, err := s.BuildSchema(ctx)
	if err != nil {
		return nil, err
	}
	// AI-3 上下文：当前用户最近 7 天日报摘要 + 自己可见的项目。失败静默降级（只警告，不影响问答）。
	ctxText := ""
	if c, err := s.buildContext(ctx, userID); err != nil {
		slog.Warn("ask context build failed, degraded to no-context", "error", err)
	} else {
		ctxText = c
	}
	started := time.Now()
	payload, err := json.Marshal(chatPayload(question, schema, ctxText, userID))
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.agentURL+"/v1/ask", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+s.agentToken)

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: py-agent 请求失败: %w", ErrUpstream, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("%w: py-agent 返回 %d: %s", ErrUpstream, resp.StatusCode, string(body))
	}
	var ar agentAskResponse
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if err := decoder.Decode(&ar); err != nil {
		return nil, fmt.Errorf("%w: 解码 AI 响应失败: %w", ErrUpstream, err)
	}
	if ar.Rows == nil {
		ar.Rows = []map[string]any{}
	}
	// 同一封顶：即使 py-agent 返回超限快照，落库前也按 execute 同规则收缩。
	ar.Columns, ar.Rows, ar.Truncated = capSnapshot(ar.Columns, ar.Rows)
	rowCount := ar.RowCount
	if rowCount == 0 {
		rowCount = len(ar.Rows)
	}
	reqID := common.GetRequestID(ctx)
	if utf8.RuneCountInString(reqID) > 64 {
		reqID = string([]rune(reqID)[:64]) // ask_history.request_id 为 VARCHAR(64)，超长截断防 500
	}
	history := &AskHistory{
		UserID:     userID,
		RequestID:  reqID,
		Question:   question,
		Answer:     ar.Answer,
		SQLText:    ar.SQL,
		TableName:  ar.Table,
		Columns:    ar.Columns,
		Rows:       ar.Rows,
		RowCount:   rowCount,
		DurationMS: int(time.Since(started).Milliseconds()),
		Model:      ar.Model,
	}
	if err := s.repo.SaveAsk(history); err != nil {
		return nil, err
	}
	return &ChatResponse{
		ID:         history.ID,
		Question:   question,
		Answer:     ar.Answer,
		SQL:        ar.SQL,
		TableName:  ar.Table,
		Columns:    ar.Columns,
		Rows:       ar.Rows,
		RowCount:   rowCount,
		Truncated:  ar.Truncated,
		DurationMS: history.DurationMS,
		CreatedAt:  history.CreatedAt,
	}, nil
}

// Execute 只读执行：SQL 校验（防线 2+3）→ 行级隔离（R2：按调用方可访问项目
// 集合强制注入 project_id IN (...)，admin 为全部 active 项目）→ 只读事务 +
// SET LOCAL ROLE ask_reader（防线 1，DB 权限级白名单）→ statement_timeout →
// 行集序列化 + 限额（防线 4）。
// userID 是发起问答的用户，经 Chat → py-agent → execute 链路回传（SERVICE_TOKEN
// 内部通道，非用户可控）；空则整体拒绝——fail-closed，不退化为全库可见。
func (s *Service) Execute(ctx context.Context, userID, rawSQL string) (*ExecuteResponse, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, fmt.Errorf("%w: 缺少调用方用户上下文（user_id）", ErrSQLRejected)
	}
	accessible, err := middleware.ListProjectsWithPermission(s.db, userID, middleware.PermRead)
	if err != nil {
		return nil, fmt.Errorf("resolve accessible projects: %w", err)
	}
	projectIDs := make([]string, len(accessible))
	for i, p := range accessible {
		projectIDs[i] = p.ID
	}
	sqlText, table, truncated, err := prepareSQL(rawSQL, projectIDs)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("%w: 开启只读事务失败: %v", ErrSQLExec, err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SET LOCAL ROLE ask_reader"); err != nil {
		return nil, fmt.Errorf("%w: SET LOCAL ROLE ask_reader 失败: %v", ErrSQLExec, err)
	}
	if _, err := tx.ExecContext(ctx, "SET LOCAL statement_timeout = "+strconv.Itoa(statementTimeout)); err != nil {
		return nil, fmt.Errorf("%w: SET LOCAL statement_timeout 失败: %v", ErrSQLExec, err)
	}

	dbRows, err := tx.QueryContext(ctx, sqlText)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSQLExec, err)
	}
	cols, err := dbRows.Columns()
	if err != nil {
		dbRows.Close()
		return nil, fmt.Errorf("%w: 读取列名失败: %v", ErrSQLExec, err)
	}
	cols = dedupColumns(cols, table)
	colTypes, err := dbRows.ColumnTypes()
	if err != nil {
		dbRows.Close()
		return nil, fmt.Errorf("%w: 读取列类型失败: %v", ErrSQLExec, err)
	}
	colDBTypes := make([]string, len(cols))
	for i := range colTypes {
		colDBTypes[i] = colTypes[i].DatabaseTypeName()
	}

	rows := make([]map[string]any, 0, maxRows)
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for dbRows.Next() {
		if len(rows) >= maxRows {
			truncated = true
			break
		}
		if err := dbRows.Scan(ptrs...); err != nil {
			dbRows.Close()
			return nil, fmt.Errorf("%w: 扫描行失败: %v", ErrSQLExec, err)
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			v := normalizeValue(vals[i], colDBTypes[i])
			if s, ok := v.(string); ok && utf8.RuneCountInString(s) > cellMaxRunes {
				v = string([]rune(s)[:cellMaxRunes]) // 扫描即截断，避免大 TEXT 全量物化进内存
				truncated = true
			}
			row[c] = v
		}
		rows = append(rows, row)
	}
	if err := dbRows.Close(); err != nil {
		return nil, fmt.Errorf("%w: 关闭结果集失败: %v", ErrSQLExec, err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("%w: 提交只读事务失败: %v", ErrSQLExec, err)
	}

	cols, rows, moreCut := capSnapshot(cols, rows)
	truncated = truncated || moreCut
	return &ExecuteResponse{
		SQL:       sqlText,
		TableName: table,
		Columns:   cols,
		Rows:      rows,
		RowCount:  len(rows),
		Truncated: truncated,
	}, nil
}

// prepareSQL 防线 2+3 校验 + 行级隔离注入（R2）+ LIMIT 封顶（防线 4）：
// 首字符 S（禁 WITH/CTE）、禁多语句/注释/写关键字/系统访问、FROM 仅白名单单表、
// JOIN 一律拒绝；项目表强制注入 project_id/id IN (projectIDs)；无 LIMIT 补 200，
// 已有 LIMIT n>200 改写 200。projectIDs 为调用方可访问项目集合（空集 → IN (NULL)
// 恒假，0 行）。
func prepareSQL(raw string, projectIDs []string) (sqlText, table string, truncated bool, err error) {
	sqlText = strings.TrimSpace(raw)
	if sqlText == "" {
		return "", "", false, fmt.Errorf("%w: SQL 为空", ErrSQLRejected)
	}
	first := sqlText[0]
	if first != 'S' && first != 's' {
		return "", "", false, fmt.Errorf("%w: 仅允许 SELECT 查询（禁 WITH/CTE 多语句）", ErrSQLRejected)
	}
	if strings.Contains(sqlText, ";") {
		return "", "", false, fmt.Errorf("%w: 禁止多语句（;）", ErrSQLRejected)
	}
	if strings.Contains(sqlText, "--") || strings.Contains(sqlText, "/*") {
		return "", "", false, fmt.Errorf("%w: 禁止注释", ErrSQLRejected)
	}
	// 关键字/字面量检查前先掩掉字符串与双引号标识符内容：既防字面量被误改写，也防误拒。
	stripped := stripStrings(sqlText)
	if writeRe.MatchString(stripped) {
		return "", "", false, fmt.Errorf("%w: 禁止写语句（INSERT/UPDATE/DELETE/MERGE/COPY/INTO）", ErrSQLRejected)
	}
	if forShareRe.MatchString(stripped) {
		return "", "", false, fmt.Errorf("%w: 禁止 FOR SHARE", ErrSQLRejected)
	}
	lower := strings.ToLower(stripped)
	for _, banned := range []string{"pg_", "information_schema", "current_setting", "current_user", "set_config"} {
		if strings.Contains(lower, banned) {
			return "", "", false, fmt.Errorf("%w: 禁止访问系统对象 %q", ErrSQLRejected, banned)
		}
	}
	if versionRe.MatchString(stripped) {
		return "", "", false, fmt.Errorf("%w: 禁止访问 version()", ErrSQLRejected)
	}
	if unionRe.MatchString(stripped) {
		return "", "", false, fmt.Errorf("%w: 禁止 UNION/INTERSECT/EXCEPT 集合操作", ErrSQLRejected)
	}
	if overRe.MatchString(stripped) {
		return "", "", false, fmt.Errorf("%w: 禁止窗口函数（OVER）", ErrSQLRejected)
	}
	if subqueryRe.MatchString(stripped) {
		return "", "", false, fmt.Errorf("%w: 禁止子查询", ErrSQLRejected)
	}
	if joinRe.MatchString(stripped) {
		return "", "", false, fmt.Errorf("%w: v1 禁止 JOIN", ErrSQLRejected)
	}

	fromTables, err := extractFromTables(sqlText)
	if err != nil {
		return "", "", false, err
	}
	if len(fromTables) == 0 {
		return "", "", false, fmt.Errorf("%w: 缺少 FROM 子句", ErrSQLRejected)
	}
	seen := map[string]bool{}
	for _, name := range fromTables {
		name = strings.ToLower(name)
		if !mainTableSet[name] {
			return "", "", false, fmt.Errorf("%w: 表 %q 不在只读白名单", ErrSQLRejected, name)
		}
		seen[name] = true
	}
	// 按 FROM 子句表引用 token 数判定（非去重后表数）：堵住同表逗号自连接
	// （FROM projects p1, projects p2 去重后只剩 1 张表，但笛卡尔积放大照常发生）。
	if len(fromTables) > 1 {
		return "", "", false, fmt.Errorf("%w: v1 仅允许查询单张表（禁止逗号自连接）", ErrSQLRejected)
	}
	for t := range seen {
		table = t
	}

	// R2 行级隔离：项目表强制注入过滤条件。定位基于 stripStrings 后的等长文本，
	// 字面量内的 WHERE/ORDER BY/LIMIT 不会被误定位；注入后重算 stripped 供
	// 下文 LIMIT 封顶使用（注入片段为引号包裹的 DB 侧 UUID，无关键字）。
	if col, ok := projectFilterColumns[table]; ok {
		sqlText = injectRowFilter(sqlText, stripped, col, projectIDs)
		stripped = stripStrings(sqlText)
	}

	// LIMIT 封顶：已有 LIMIT n>200 → 改写；LIMIT 非数字（ALL/NULL）→ 整体替换；无 LIMIT → 补 200。
	// 改写/补全只是查询层封顶，不置 truncated——只有实际截断（扫描循环/capSnapshot）才置位。
	if m := limitNumRe.FindStringSubmatchIndex(stripped); m != nil {
		n, _ := strconv.Atoi(stripped[m[2]:m[3]])
		if n > maxRows {
			sqlText = sqlText[:m[0]] + "LIMIT 200" + sqlText[m[1]:]
		}
	} else if loc := limitRe.FindStringIndex(stripped); loc != nil {
		sqlText = sqlText[:loc[0]] + "LIMIT 200"
	} else {
		sqlText = strings.TrimRight(sqlText, " \t\r\n") + " LIMIT 200"
	}
	return sqlText, table, truncated, nil
}

// stripStrings 把单引号字符串字面量与双引号标识符内容逐字节替换为空格：
// 字节长度不变，正则匹配偏移与原文一一对应；SQL 内两个连续引号视为转义。
// 未闭合的引号段掩到结尾（SQL 本身非法，交由 PG 拒绝，此处只做纵深）。
// 先掩蔽 dollar-quote（R1）：否则 $a$ ' $a$ 中未配对单引号会让下方循环把
// 后半句整体当"未闭合字符串"掩掉，UNION/pg_/子查询黑名单全部失效。
func stripStrings(s string) string {
	b := []byte(maskDollarQuotes(s))
	for i := 0; i < len(b); {
		q := b[i]
		if q != '\'' && q != '"' {
			i++
			continue
		}
		j := i + 1
		for j < len(b) {
			if b[j] == q {
				if j+1 < len(b) && b[j+1] == q {
					j += 2
					continue
				}
				break
			}
			j++
		}
		if j >= len(b) {
			j = len(b)
		} else {
			j++ // 含闭引号
		}
		for k := i; k < j; k++ {
			b[k] = ' '
		}
		i = j
	}
	return string(b)
}

// dollarQuoteRe 匹配 PG dollar-quote 起始定界符 $tag$（tag 可空，[A-Za-z_0-9]*）。
var dollarQuoteRe = regexp.MustCompile(`\$[A-Za-z_0-9]*\$`)

// maskDollarQuotes 把 $tag$...$tag$（含未闭合形态：掩到结尾）逐字节替换为空格，
// 字节长度不变，保持与原文的偏移对齐。未闭合 dollar-quote 在 PG 侧同为语法错误，
// 此处掩到结尾是纵深（防止尾部关键字检查被"吞进"未闭合段）。含单引号/双引号的
// 定界符内内容一并掩掉，stripStrings 的引号扫描不会再进入这些区域。
func maskDollarQuotes(s string) string {
	b := []byte(s)
	for i := 0; i < len(b); {
		loc := dollarQuoteRe.FindIndex(b[i:])
		if loc == nil {
			break
		}
		start := i + loc[0]
		tag := b[start : i+loc[1]]
		rest := b[i+loc[1]:]
		end := bytes.Index(rest, tag)
		if end < 0 {
			for k := start; k < len(b); k++ {
				b[k] = ' '
			}
			break
		}
		stop := i + loc[1] + end + len(tag)
		for k := start; k < stop; k++ {
			b[k] = ' '
		}
		i = stop
	}
	return string(b)
}

// injectRowFilter 把 `col IN (...)` 注入已通过防线 2+3 的单表 SELECT：
// 有 WHERE → 紧随其后以 AND 连接；无 WHERE → 插在「首个出现的
// GROUP BY / ORDER BY / LIMIT / OFFSET」之前（合法 SQL 中该位置必然在 WHERE 之后、
// 这些子句之前），无任何子句 → 句尾。定位用 stripStrings 后的等长文本
// （第二个参数），字面量内的关键字不参与定位。
// 扩展四个注入点的理由：仅认 ORDER BY / LIMIT 时，`... GROUP BY x` 与 `... OFFSET 10`
// 形态会落到句尾追加，产出 `GROUP BY x WHERE ...` / `OFFSET 10 WHERE ...` 非法 SQL，
// 被 PG 以 422 拒绝（fail-closed 但属可避免的回归）。
// projectIDs 为空时注入 IN (NULL)（恒假，0 行）——可访问集合为空即无行可见。
func injectRowFilter(sqlText, stripped, col string, ids []string) string {
	list := "NULL"
	if len(ids) > 0 {
		quoted := make([]string, len(ids))
		for i, id := range ids {
			quoted[i] = "'" + id + "'"
		}
		list = strings.Join(quoted, ",")
	}
	cond := col + " IN (" + list + ")"
	if loc := whereKwRe.FindStringIndex(stripped); loc != nil {
		return sqlText[:loc[1]] + " " + cond + " AND" + sqlText[loc[1]:]
	}
	best := -1
	for _, re := range []*regexp.Regexp{groupByRe, orderByRe, limitRe, offsetRe} {
		if loc := re.FindStringIndex(stripped); loc != nil && (best < 0 || loc[0] < best) {
			best = loc[0]
		}
	}
	if best >= 0 {
		return sqlText[:best] + " WHERE " + cond + " " + sqlText[best:]
	}
	return strings.TrimRight(sqlText, " \t\r\n") + " WHERE " + cond + " "
}

// extractFromTables 解析 SQL 中每个 FROM 子句的表清单（防线 2）。
// 支持 [schema.]table 与 "quoted" 标识符与 AS/裸别名；逗号分隔多表、子查询内
// 跨表引用都会产生多个表名（随后按"仅单张白名单主表"拒绝）。匹配前先掩掉
// 字符串字面量与双引号标识符内容，避免 'from xxx' 等字面量被误解析。
func extractFromTables(sqlText string) ([]string, error) {
	var tables []string
	for _, loc := range fromKwRe.FindAllStringIndex(stripStrings(sqlText), -1) {
		rest := sqlText[loc[1]:]
		for {
			rest = strings.TrimLeft(rest, " \t\r\n")
			if rest == "" {
				break
			}
			name, after, ok := readTableRef(rest)
			if !ok {
				break // 不是表引用（WHERE/子查询 ( / 其他）→ 结束该 FROM 子句
			}
			tables = append(tables, name)
			rest = skipAlias(after)
			rest = strings.TrimLeft(rest, " \t\r\n")
			if strings.HasPrefix(rest, ",") {
				rest = rest[1:]
				continue
			}
			break
		}
	}
	return tables, nil
}

// readTableRef 读取一个表引用：返回表名与剩余文本。支持 "quoted"、[schema.]table。
func readTableRef(rest string) (name, after string, ok bool) {
	if strings.HasPrefix(rest, `"`) {
		m := quotedRe.FindStringSubmatch(rest)
		if m == nil {
			return "", rest, false
		}
		return m[1], rest[len(m[0]):], true
	}
	first := identRe.FindString(rest)
	if first == "" {
		return "", rest, false
	}
	after = rest[len(first):]
	trimmed := strings.TrimLeft(after, " \t\r\n")
	if strings.HasPrefix(trimmed, ".") {
		trimmed = strings.TrimLeft(trimmed[1:], " \t\r\n")
		if strings.HasPrefix(trimmed, `"`) {
			m := quotedRe.FindStringSubmatch(trimmed)
			if m == nil {
				return first, rest, false
			}
			return m[1], trimmed[len(m[0]):], true
		}
		second := identRe.FindString(trimmed)
		if second == "" {
			return first, rest, false
		}
		return second, trimmed[len(second):], true
	}
	return first, after, true
}

// skipAlias 跳过可选的 AS 别名 / 裸别名（不越过 FROM 子句终止关键字）。
func skipAlias(rest string) string {
	rest = strings.TrimLeft(rest, " \t\r\n")
	if rest == "" {
		return rest
	}
	if strings.HasPrefix(rest, ",") || strings.HasPrefix(rest, "(") {
		return rest
	}
	if strings.HasPrefix(rest, `"`) {
		if m := quotedRe.FindStringSubmatch(rest); m != nil {
			return rest[len(m[0]):]
		}
		return rest
	}
	word := identRe.FindString(rest)
	if word == "" {
		return rest
	}
	if fromClauseEnd[strings.ToLower(word)] {
		return rest
	}
	after := rest[len(word):]
	if strings.HasPrefix(after, "(") {
		return after // 函数/子查询起点，交给后续解析
	}
	if strings.EqualFold(word, "as") {
		// AS 别名：继续消费别名标识符，避免遗漏其后的逗号（FROM a AS x, b）
		after = strings.TrimLeft(after, " \t\r\n")
		if alias := identRe.FindString(after); alias != "" {
			return after[len(alias):]
		}
	}
	return after
}

// dedupColumns 列名冲突自动加表名前缀（方案 §4/T4；同一列名再冲突追加序号）。
func dedupColumns(cols []string, table string) []string {
	counts := map[string]int{}
	for _, c := range cols {
		counts[c]++
	}
	out := make([]string, len(cols))
	used := map[string]int{}
	for i, c := range cols {
		name := c
		if counts[c] > 1 && used[name] > 0 {
			// 冲突列：后续出现自动加表名前缀（首列保留原名）
			name = table + "." + c
		}
		if n := used[name]; n > 0 {
			name = fmt.Sprintf("%s.%d", name, n+1)
		}
		used[name]++
		out[i] = name
	}
	return out
}

// normalizeValue 行集序列化规则：time→RFC3339、[]byte（uuid→标准 UUID 字符串、
// 合法 JSON→保留结构、其他→hex）、float/其他原样（方案 §5）。
// dbType 来自 rows.ColumnTypes() 的 DatabaseTypeName；为空（无列类型上下文）时
// 对 16 字节 []byte 按 UUID 兜底（lib/pq 的 uuid 列返回 16 字节二进制）。
func normalizeValue(v any, dbType string) any {
	switch x := v.(type) {
	case nil:
		return nil
	case time.Time:
		return x.Format(time.RFC3339)
	case []byte:
		// lib/pq 的 uuid 列返回 36 字节 ASCII 文本（"xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"）；
		// 个别驱动可能返回 16 字节二进制。两者都按 UUID 处理。
		if strings.EqualFold(dbType, "uuid") && len(x) == 36 {
			return string(x)
		}
		if len(x) == 16 && (strings.EqualFold(dbType, "uuid") || dbType == "") {
			return formatUUID(x)
		}
		if json.Valid(x) {
			var raw any
			if json.Unmarshal(x, &raw) == nil {
				return raw
			}
		}
		return fmt.Sprintf("%x", x) // bytea 等 → hex 字符串，不再置 null
	default:
		return v
	}
}

// formatUUID 将 16 字节二进制 UUID 格式化为标准字符串 xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx。
func formatUUID(b []byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// capSnapshot rows 快照总字节 ≤256KB（方案 §5 防线 4）：
// ① 单元格 500 字符截断 → ② 列裁剪（优先保留 id/project_id）→ ③ 行数截断。
func capSnapshot(cols []string, rows []map[string]any) ([]string, []map[string]any, bool) {
	truncated := false
	for _, row := range rows {
		for c, v := range row {
			if s, ok := v.(string); ok && utf8.RuneCountInString(s) > cellMaxRunes {
				row[c] = string([]rune(s)[:cellMaxRunes])
				truncated = true
			}
		}
	}
	if snapshotSize(rows) <= snapshotBudget {
		return cols, rows, truncated
	}
	orig := rows
	ordered := make([]string, 0, len(cols))
	for _, pri := range []string{"id", "project_id"} {
		for _, c := range cols {
			if c == pri {
				ordered = append(ordered, c)
			}
		}
	}
	for _, c := range cols {
		if c != "id" && c != "project_id" {
			ordered = append(ordered, c)
		}
	}
	kept := []string{}
	for _, c := range ordered {
		trial := pruneRows(orig, append(kept, c))
		if snapshotSize(trial) <= snapshotBudget {
			kept = append(kept, c)
		}
	}
	if len(kept) < len(cols) {
		truncated = true
	}
	rows = pruneRows(orig, kept)
	for len(rows) > 0 && snapshotSize(rows) > snapshotBudget {
		rows = rows[:len(rows)-1]
		truncated = true
	}
	return kept, rows, truncated
}

func pruneRows(rows []map[string]any, keep []string) []map[string]any {
	out := make([]map[string]any, len(rows))
	for i, row := range rows {
		nr := make(map[string]any, len(keep))
		for _, c := range keep {
			if v, ok := row[c]; ok {
				nr[c] = v
			}
		}
		out[i] = nr
	}
	return out
}

func snapshotSize(rows []map[string]any) int {
	data, err := json.Marshal(rows)
	if err != nil {
		return 1 << 30
	}
	return len(data)
}

// allowOne 进程内限流：10 次/分钟/用户（复制自 steptemplates/service.go:319 先例）。
func (s *Service) allowOne(userID string) bool {
	now, cutoff := time.Now(), time.Now().Add(-time.Minute)
	s.rlMu.Lock()
	defer s.rlMu.Unlock()
	for k, calls := range s.rlCalls {
		if k != userID && (len(calls) == 0 || calls[len(calls)-1].Before(cutoff)) {
			delete(s.rlCalls, k) // 顺手清理过期 key，防 map 只增不删
		}
	}
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

func (s *Service) List(userID string, page, perPage int) ([]AskHistory, int, error) {
	if perPage <= 0 {
		perPage = 20
	}
	if perPage > 50 {
		perPage = 50
	}
	if page < 1 {
		page = 1
	}
	return s.repo.ListHistory(userID, perPage, (page-1)*perPage)
}

func (s *Service) GetByUser(id, userID string) (*AskHistory, error) {
	return s.repo.GetHistoryByUser(id, userID)
}

// RunRetentionOnce 执行一轮快照保留：cutoff 之前且非空的行置 NULL（P2-3，定死方案：
// 不做 DELETE、不做 admin 清理端点——保留历史列表可用性）。返回置空行数。
func (s *Service) RunRetentionOnce(ctx context.Context, cutoff time.Time) (int64, error) {
	n, err := s.repo.NullifyOldSnapshots(cutoff)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		slog.Info("ask history snapshots nullified", "count", n, "cutoff", cutoff.Format(time.RFC3339))
	}
	return n, nil
}

// StartRetentionTask 启动每日快照保留任务：立即执行一次 + 每 24h tick；
// 互斥锁防重入；保留天数取 ASK_HISTORY_RETENTION_DAYS（默认 90），
// cutoff 用 Asia/Shanghai 本地日界对齐。ctx 取消即退出。
func (s *Service) StartRetentionTask(ctx context.Context) {
	days := retentionDays
	if v := os.Getenv("ASK_HISTORY_RETENTION_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			days = n
		}
	}
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		slog.Warn("ask retention: Asia/Shanghai unavailable, fallback to UTC", "error", err)
		loc = time.UTC
	}
	run := func() {
		s.retention.Lock()
		defer s.retention.Unlock()
		now := time.Now().In(loc)
		cutoff := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -days)
		if _, err := s.RunRetentionOnce(ctx, cutoff); err != nil && ctx.Err() == nil {
			slog.Warn("ask retention run failed", "error", err)
		}
	}
	run()
	ticker := time.NewTicker(retentionTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
