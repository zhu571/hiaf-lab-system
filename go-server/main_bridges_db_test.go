package main

import (
	"bytes"
	"database/sql"
	"errors"
	"mime/multipart"
	"os"
	"testing"

	"github.com/zhu571/hiaf-lab-system/go-server/assembly"
	"github.com/zhu571/hiaf-lab-system/go-server/attachments"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/issues"
	"github.com/zhu571/hiaf-lab-system/go-server/logs"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
	"github.com/zhu571/hiaf-lab-system/go-server/rfmatch"
	"github.com/zhu571/hiaf-lab-system/go-server/runs"
	"github.com/zhu571/hiaf-lab-system/go-server/testdata"

	_ "github.com/lib/pq"
)

// R3 集成测试：attachmentPermissionBridge 用各模块真实读路径 + 项目 ACL 判定
// 实体权限（替代已删除的回环 HTTP permission-check）。需要 TEST_DATABASE_URL，
// 为空跳过（与 attachments/auth db 测试同模式）。固定 UUID 段 e2xx。

const (
	bridgeMemberID  = "00000000-0000-0000-0000-00000000e201"
	bridgeViewerID  = "00000000-0000-0000-0000-00000000e202"
	bridgeOutsiderD = "00000000-0000-0000-0000-00000000e203"
	bridgeProjectID = "e2000000-0000-4000-8000-000000000001"
	bridgeTestDataD = "e2000000-0000-4000-8000-000000000002"
	bridgeIssueID   = "e2000000-0000-4000-8000-000000000003"
	bridgeReportID  = "e2000000-0000-4000-8000-000000000004"
)

func openBridgeTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	for _, u := range []struct{ id, username, role string }{
		{bridgeMemberID, "bridge_member", "member"},
		{bridgeViewerID, "bridge_viewer", "viewer"},
		{bridgeOutsiderD, "bridge_outsider", "member"},
	} {
		if _, err := db.Exec(
			`INSERT INTO users (id, username, password_hash, display_name, role, must_change_pw)
			 VALUES ($1, $2, 'x', 'Bridge Test', $3, false) ON CONFLICT (id) DO NOTHING`,
			u.id, u.username, u.role); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(
		`INSERT INTO projects (id, code, name, status, visibility, owner_user_id, created_by)
		 VALUES ($1, 'BRIDGE-TEST', '桥接测试项目', 'active', 'restricted', $2, $2)
		 ON CONFLICT (id) DO NOTHING`, bridgeProjectID, bridgeMemberID); err != nil {
		t.Fatal(err)
	}
	// member（≥member 可写 test_data/rf_matching）+ viewer（只读）。
	if _, err := db.Exec(
		`INSERT INTO project_members (project_id, user_id, role, status, added_by)
		 VALUES ($1, $2, 'member', 'active', $2), ($1, $3, 'viewer', 'active', $2)
		 ON CONFLICT (project_id, user_id) DO NOTHING`,
		bridgeProjectID, bridgeMemberID, bridgeViewerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO test_data (id, project_id, recorded_by, data_type, measurement, value, unit, quality)
		 VALUES ($1, $2, $3, 'pressure', 'bridge-test', 1.0, 'V', 'normal') ON CONFLICT (id) DO NOTHING`,
		bridgeTestDataD, bridgeProjectID, bridgeMemberID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO issues (id, project_id, author_id, title, description, status, severity)
		 VALUES ($1, $2, $3, 'bridge 测试 issue', '', 'open', 'medium') ON CONFLICT (id) DO NOTHING`,
		bridgeIssueID, bridgeProjectID, bridgeMemberID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO daily_reports (id, report_date, author_id, raw_text, summary, content_status)
		 VALUES ($1, CURRENT_DATE, $2, 'bridge 作者日报', '', 'confirmed') ON CONFLICT (id) DO NOTHING`,
		bridgeReportID, bridgeMemberID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM attachment_links WHERE entity_id IN ($1,$2,$3)`,
			bridgeTestDataD, bridgeIssueID, bridgeReportID)
		db.Exec(`DELETE FROM attachments WHERE uploaded_by IN ($1,$2,$3)`,
			bridgeMemberID, bridgeViewerID, bridgeOutsiderD)
		db.Exec(`DELETE FROM test_data WHERE id = $1`, bridgeTestDataD)
		db.Exec(`DELETE FROM issues WHERE id = $1`, bridgeIssueID)
		db.Exec(`DELETE FROM daily_reports WHERE id = $1`, bridgeReportID)
		db.Exec(`DELETE FROM project_members WHERE project_id = $1`, bridgeProjectID)
		db.Exec(`DELETE FROM projects WHERE id = $1`, bridgeProjectID)
		db.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3)`, bridgeMemberID, bridgeViewerID, bridgeOutsiderD)
	})
	return db
}

func newBridgeChecker(t *testing.T, db *sql.DB) (attachmentPermissionBridge, *attachments.Service) {
	projectsRepo := projects.NewRepository(db)
	logsSvc := logs.NewService(logs.NewRepository(db), "Asia/Shanghai",
		logs.ProjectAccessAdapter{DB: db, Repo: projectsRepo})
	// issues.NewService 的 validators 是可变参（agent 任务校验器，Create 路径才用），只读场景可省。
	issuesSvc := issues.NewService(issues.NewRepository(db),
		issues.ProjectAccessAdapter{DB: db, Repo: projectsRepo})
	assemblySvc := assembly.NewService(assembly.NewRepository(db),
		assembly.ProjectAccessAdapter{Repo: projectsRepo})
	runsSvc := runs.NewService(runs.NewRepository(db),
		runs.ProjectAccessAdapter{Repo: projectsRepo})
	testDataSvc := testdata.NewService(testdata.NewRepository(db),
		testdata.ProjectAccessAdapter{Repo: projectsRepo}, nil)
	rfMatchingSvc := rfmatch.NewService(rfmatch.NewRepository(db),
		rfmatch.ProjectAccessAdapter{Repo: projectsRepo})
	bridge := attachmentPermissionBridge{
		db: db, logs: logsSvc, issues: issuesSvc, assembly: assemblySvc,
		runs: runsSvc, testdata: testDataSvc, rfmatch: rfMatchingSvc, projects: projectsRepo,
	}
	return bridge, attachments.NewService(attachments.NewRepository(db), bridge, t.TempDir())
}

// 桥接层直测：真实模块权限语义（成员读/写、viewer 只读、局外人拒绝、日报作者制）。
func TestAttachmentPermissionBridge_Check(t *testing.T) {
	db := openBridgeTestDB(t)
	bridge, _ := newBridgeChecker(t, db)

	cases := []struct {
		name                         string
		entityType, entityID         string
		userID, userRole, action     string
		wantAllowed                  bool
	}{
		{"member reads test_data", attachments.EntityTestData, bridgeTestDataD, bridgeMemberID, auth.RoleMember, "read", true},
		{"member writes test_data", attachments.EntityTestData, bridgeTestDataD, bridgeMemberID, auth.RoleMember, "write", true},
		{"viewer reads test_data", attachments.EntityTestData, bridgeTestDataD, bridgeViewerID, auth.RoleViewer, "read", true},
		{"viewer cannot write test_data", attachments.EntityTestData, bridgeTestDataD, bridgeViewerID, auth.RoleViewer, "write", false},
		{"outsider cannot read test_data", attachments.EntityTestData, bridgeTestDataD, bridgeOutsiderD, auth.RoleMember, "read", false},
		{"outsider cannot write issue", attachments.EntityIssue, bridgeIssueID, bridgeOutsiderD, auth.RoleMember, "write", false},
		{"member reads issue", attachments.EntityIssue, bridgeIssueID, bridgeMemberID, auth.RoleMember, "read", true},
		{"member writes issue (member has PermUpdateIssue)", attachments.EntityIssue, bridgeIssueID, bridgeMemberID, auth.RoleMember, "write", true},
		{"viewer cannot write issue", attachments.EntityIssue, bridgeIssueID, bridgeViewerID, auth.RoleViewer, "write", false},
		{"report author reads own report", attachments.EntityDailyReport, bridgeReportID, bridgeMemberID, auth.RoleMember, "read", true},
		{"report author writes own report", attachments.EntityDailyReport, bridgeReportID, bridgeMemberID, auth.RoleMember, "write", true},
		{"non-author cannot read report", attachments.EntityDailyReport, bridgeReportID, bridgeOutsiderD, auth.RoleMember, "read", false},
		{"non-author cannot write report", attachments.EntityDailyReport, bridgeReportID, bridgeViewerID, auth.RoleViewer, "write", false},
		{"missing entity denies", attachments.EntityIssue, "00000000-0000-0000-0000-000000009999", bridgeMemberID, auth.RoleMember, "read", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			allowed, err := bridge.Check(tc.entityType, tc.entityID, tc.userID, tc.userRole, tc.action)
			if err != nil {
				t.Fatalf("Check error: %v", err)
			}
			if allowed != tc.wantAllowed {
				t.Fatalf("allowed = %v, want %v", allowed, tc.wantAllowed)
			}
		})
	}

	// 未知实体类型：显式报错（不再有 404 fail-open）。
	if _, err := bridge.Check("bogus", bridgeTestDataD, bridgeMemberID, auth.RoleMember, "read"); err == nil {
		t.Fatal("unknown entity type must return an error, not fail-open")
	}
}

// bridgeMemFile / bridgeFileHeader 构造最小可用的上传入参（multipart.File 接口）。
type bridgeMemFile struct{ *bytes.Reader }

func (bridgeMemFile) Close() error { return nil }

// 端到端：attachments.Service 挂真实桥接——成员挂实体/读链接放行，局外人拒绝。
func TestAttachmentServiceWithBridge_EndToEnd(t *testing.T) {
	db := openBridgeTestDB(t)
	_, svc := newBridgeChecker(t, db)

	content := []byte("bridge-attachment-bytes")
	upload := func(userID, role string) error {
		_, err := svc.Upload(bridgeMemFile{bytes.NewReader(content)},
			&multipart.FileHeader{Filename: "bridge.txt"}, userID, role,
			attachments.EntityTestData, bridgeTestDataD, "成员上传")
		return err
	}

	// 局外人向 test_data 上传挂实体：write 拒绝 → ErrForbidden。
	if err := upload(bridgeOutsiderD, auth.RoleMember); !errors.Is(err, attachments.ErrForbidden) {
		t.Fatalf("outsider upload-to-entity must be forbidden, got %v", err)
	}

	// 成员上传并挂到 test_data：放行。
	if err := upload(bridgeMemberID, auth.RoleMember); err != nil {
		t.Fatalf("member upload-to-entity: %v", err)
	}
	var attachmentID string
	if err := db.QueryRow(
		`SELECT id FROM attachments WHERE uploaded_by = $1 ORDER BY created_at DESC LIMIT 1`,
		bridgeMemberID).Scan(&attachmentID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM attachments WHERE id = $1`, attachmentID) })

	// 局外人读该附件：链接指向不可读实体 → ErrForbidden（修复前 fail-open 会放行）。
	if _, err := svc.GetByID(attachmentID, bridgeOutsiderD, auth.RoleMember); !errors.Is(err, attachments.ErrForbidden) {
		t.Fatalf("outsider read must be forbidden, got %v", err)
	}
	// 成员读：放行。
	if _, err := svc.GetByID(attachmentID, bridgeMemberID, auth.RoleMember); err != nil {
		t.Fatalf("member read: %v", err)
	}
	// viewer 读：放行（viewer 有 PermRead）。
	if _, err := svc.GetByID(attachmentID, bridgeViewerID, auth.RoleViewer); err != nil {
		t.Fatalf("viewer read: %v", err)
	}

	// 局外人把已有附件挂到 issue：write 拒绝。
	_, err := svc.AddLink(attachmentID, bridgeOutsiderD, auth.RoleMember,
		attachments.CreateLinkRequest{EntityType: attachments.EntityIssue, EntityID: bridgeIssueID})
	if !errors.Is(err, attachments.ErrForbidden) {
		t.Fatalf("outsider add-link must be forbidden, got %v", err)
	}
	// 成员挂 issue：放行（member 有 PermUpdateIssue）。
	if _, err := svc.AddLink(attachmentID, bridgeMemberID, auth.RoleMember,
		attachments.CreateLinkRequest{EntityType: attachments.EntityIssue, EntityID: bridgeIssueID}); err != nil {
		t.Fatalf("member add-link: %v", err)
	}
}
