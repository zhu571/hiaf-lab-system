package attachments

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// 集成测试：TEST_DATABASE_URL 为空则跳过（与 projects/auth db 测试同模式）。
// 覆盖上传（保存文件 + 元数据 + sha256 去重）、下载、软删、关联多实体、
// 权限（项目成员才能看）、大小/类型校验、handler 层 multipart 全链路。
// 固定 UUID 段 d1xx 避开其他包已用段（a/b/c 系、b5xx-bcxx、c7xx-ccxx、e0xx、f1xx）。

const (
	attAdminID    = "00000000-0000-0000-0000-00000000d101"
	attOwnerID    = "00000000-0000-0000-0000-00000000d102"
	attMemberID   = "00000000-0000-0000-0000-00000000d103"
	attOutsiderID = "00000000-0000-0000-0000-00000000d104"

	attProjectID = "d0000000-0000-4000-8000-00000000d101"
	attEntityID  = "d0000000-0000-4000-8000-00000000d102"
)

func openAttachmentsTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db := openDB(t)
	for _, u := range []struct {
		id       string
		username string
		role     string
	}{
		{attAdminID, "att_dbtest_admin", "admin"},
		{attOwnerID, "att_dbtest_owner", "member"},
		{attMemberID, "att_dbtest_member", "member"},
		{attOutsiderID, "att_dbtest_outsider", "member"},
	} {
		if _, err := db.Exec(
			`INSERT INTO users (id, username, password_hash, display_name, role, must_change_pw, disabled)
			 VALUES ($1, $2, 'x', 'Attachments DB Test', $3, false, false)
			 ON CONFLICT (id) DO NOTHING`, u.id, u.username, u.role); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM attachments WHERE uploaded_by IN ($1,$2,$3,$4)`, attAdminID, attOwnerID, attMemberID, attOutsiderID)
		db.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3,$4)`, attAdminID, attOwnerID, attMemberID, attOutsiderID)
	})
	return db
}

// fakePermissionChecker：按实体权限返回固定结果；attOutsiderID 模拟无成员关系者。
type fakePermissionChecker struct {
	read  bool
	write bool
}

func (f *fakePermissionChecker) Check(entityType, entityID, userID, userRole, action string) (bool, error) {
	if userID == attOutsiderID {
		return false, nil
	}
	if action == "write" {
		return f.write, nil
	}
	return f.read, nil
}

func testAttachmentService(t *testing.T, perms PermissionChecker) (*Service, *sql.DB) {
	t.Helper()
	db := openAttachmentsTestDB(t)
	dir := t.TempDir()
	return NewService(NewRepository(db), perms, dir), db
}

// buildUploadRequest 构造 multipart 上传请求（file 字段 + 可选表单字段）。
func buildUploadRequest(t *testing.T, token, idempotencyKey, entityType, entityID, description, fileName string, content []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if entityType != "" {
		_ = writer.WriteField("entity_type", entityType)
		_ = writer.WriteField("entity_id", entityID)
	}
	if description != "" {
		_ = writer.WriteField("description", description)
	}
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/attachments", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	return req
}

const attachmentsTestSecret = "attachments-handler-test-secret"

func newAttachmentsTestRouter(t *testing.T, db *sql.DB, svc *Service) http.Handler {
	t.Helper()
	middleware.SetJWTSecret([]byte(attachmentsTestSecret))
	h := NewHandler(svc)
	router := chi.NewRouter()
	router.Route("/api/v1/attachments", func(r chi.Router) {
		r.Use(middleware.RequestID)
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Get("/", h.List)
		r.Post("/", h.Upload)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.GetByID)
			r.Get("/content", h.Download)
			r.Post("/links", h.AddLink)
			r.Delete("/links/{link_id}", h.RemoveLink)
			r.Delete("/", h.SoftDelete)
		})
	})
	return router
}

func attachmentsToken(t *testing.T, userID, username, role string) string {
	t.Helper()
	token, err := middleware.GenerateToken(userID, username, role, 1, []byte(attachmentsTestSecret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func attRequest(t *testing.T, router http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func attUniqueKey(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("att-h-%d", time.Now().UnixNano())
}

// ---------- service 层 ----------

func TestDBUploadDownloadAndDedupe(t *testing.T) {
	perms := &fakePermissionChecker{read: true, write: true}
	svc, db := testAttachmentService(t, perms)
	content := []byte("attachment-bytes-1")

	uploaded, err := svc.Upload(memFileOf(content), headerFor(content), attMemberID, auth.RoleMember,
		EntityIssue, attEntityID, " 测量截图 ")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM attachments WHERE id = $1`, uploaded.Attachment.ID) })
	if uploaded.Attachment.Sha256 == "" || len(uploaded.Attachment.Sha256) != 64 {
		t.Fatalf("sha256 = %q", uploaded.Attachment.Sha256)
	}
	if uploaded.Attachment.OriginalName != "photo.png" || uploaded.Attachment.FileSize != int64(len(content)) {
		t.Fatalf("metadata: %+v", uploaded.Attachment)
	}
	if len(uploaded.Links) != 1 || uploaded.Links[0].EntityType != EntityIssue || uploaded.Links[0].EntityID != attEntityID {
		t.Fatalf("links: %+v", uploaded.Links)
	}
	// 文件真实落盘
	path := filepath.Join(svc.storageDir, uploaded.Attachment.StorageKey)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stored file missing: %v", err)
	}

	// sha256 去重：同内容再传（不同实体）→ 复用附件 ID，不新增文件，仅加新 link
	second, err := svc.Upload(memFileOf(content), headerFor(content), attOwnerID, auth.RoleMember,
		EntityIssue, attProjectID, "")
	if err != nil {
		t.Fatal(err)
	}
	if second.Attachment.ID != uploaded.Attachment.ID {
		t.Fatalf("dedupe failed: %s != %s", second.Attachment.ID, uploaded.Attachment.ID)
	}
	links, err := NewRepository(db).GetLinks(uploaded.Attachment.ID)
	if err != nil || len(links) != 2 {
		t.Fatalf("links after dedupe: %v err=%v", links, err)
	}

	// 下载：内容一致
	attachment, file, err := svc.Download(uploaded.Attachment.ID, attMemberID, auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	got := make([]byte, len(content))
	if _, err := file.Read(got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) || attachment.ID != uploaded.Attachment.ID {
		t.Fatalf("download mismatch: %q", got)
	}

	// 下载已删除文件 → ErrFileNotFound（成员可读元数据，但文件缺失）
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Download(uploaded.Attachment.ID, attMemberID, auth.RoleMember); err != ErrFileNotFound {
		t.Fatalf("download missing file: got %v, want ErrFileNotFound", err)
	}
}

func TestDBUploadValidation(t *testing.T) {
	svc, db := testAttachmentService(t, &fakePermissionChecker{write: true})

	// nil 文件 / 缺失 header
	if _, err := svc.Upload(nil, headerFor([]byte("x")), attMemberID, auth.RoleMember, "", "", ""); err != ErrInvalidInput {
		t.Fatalf("nil file: got %v", err)
	}
	if _, err := svc.Upload(memFileOf([]byte("x")), nil, attMemberID, auth.RoleMember, "", "", ""); err != ErrInvalidInput {
		t.Fatalf("nil header: got %v", err)
	}
	// entity_type/entity_id 只填一半 → 无效
	if _, err := svc.Upload(memFileOf([]byte("x")), headerFor([]byte("x")), attMemberID, auth.RoleMember,
		EntityIssue, "", ""); err != ErrInvalidInput {
		t.Fatalf("half entity: got %v", err)
	}
	// 非法 entity_type
	if _, err := svc.Upload(memFileOf([]byte("x")), headerFor([]byte("x")), attMemberID, auth.RoleMember,
		"bogus", attEntityID, ""); err != ErrInvalidInput {
		t.Fatalf("bad entity type: got %v", err)
	}
	// entity_id 非法 UUID
	if _, err := svc.Upload(memFileOf([]byte("x")), headerFor([]byte("x")), attMemberID, auth.RoleMember,
		EntityIssue, "not-a-uuid", ""); err != ErrInvalidInput {
		t.Fatalf("bad entity id: got %v", err)
	}
	// 超长文件名（>256）
	longName := strings.Repeat("n", 300) + ".png"
	if _, err := svc.Upload(memFileOf([]byte("x")), &multipart.FileHeader{Filename: longName}, attMemberID,
		auth.RoleMember, "", "", ""); err != ErrInvalidInput {
		t.Fatalf("long name: got %v", err)
	}
	// 写权限不足 → ErrForbidden
	svcDenied := NewService(NewRepository(db), &fakePermissionChecker{write: false}, svc.storageDir)
	if _, err := svcDenied.Upload(memFileOf([]byte("x")), headerFor([]byte("x")), attMemberID,
		auth.RoleMember, EntityIssue, attEntityID, ""); err != ErrForbidden {
		t.Fatalf("write denied: got %v", err)
	}
}

func TestDBGetReadablePermission(t *testing.T) {
	svc, db := testAttachmentService(t, &fakePermissionChecker{read: true, write: true})
	content := []byte("perm-test")
	uploaded, err := svc.Upload(memFileOf(content), headerFor(content), attMemberID, auth.RoleMember,
		EntityIssue, attEntityID, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM attachments WHERE id = $1`, uploaded.Attachment.ID) })

	// 成员（实体可读）→ 200 语义
	if _, err := svc.GetByID(uploaded.Attachment.ID, attMemberID, auth.RoleMember); err != nil {
		t.Fatalf("member read: %v", err)
	}
	// 实体不可读 → ErrForbidden（同一成员，read=false 的权限）
	svcDenied := NewService(NewRepository(db), &fakePermissionChecker{read: false}, svc.storageDir)
	if _, err := svcDenied.GetByID(uploaded.Attachment.ID, attMemberID, auth.RoleMember); err != ErrForbidden {
		t.Fatalf("entity not readable: got %v", err)
	}
	// 非上传者、非实体成员 → ErrForbidden
	if _, err := svcDenied.GetByID(uploaded.Attachment.ID, attOutsiderID, auth.RoleMember); err != ErrForbidden {
		t.Fatalf("outsider: got %v", err)
	}
	// admin 直通
	if _, err := svc.GetByID(uploaded.Attachment.ID, attAdminID, auth.RoleAdmin); err != nil {
		t.Fatalf("admin read: %v", err)
	}
	// 不存在 / 非法 id
	if _, err := svc.GetByID("d0000000-0000-4000-8000-000000009999", attAdminID, auth.RoleAdmin); err != ErrAttachmentNotFound {
		t.Fatalf("missing: got %v", err)
	}
	if _, err := svc.GetByID("bad-id", attAdminID, auth.RoleAdmin); err != ErrInvalidInput {
		t.Fatalf("bad id: got %v", err)
	}
}

func TestDBUnlinkedOwnershipVisibility(t *testing.T) {
	// 无 link 附件：上传者本人可见，他人不可见（除非 admin）
	svc, db := testAttachmentService(t, &fakePermissionChecker{read: true})
	content := []byte("unlinked")
	uploaded, err := svc.Upload(memFileOf(content), headerFor(content), attMemberID, auth.RoleMember, "", "", "个人文件")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM attachments WHERE id = $1`, uploaded.Attachment.ID) })

	if _, err := svc.GetByID(uploaded.Attachment.ID, attMemberID, auth.RoleMember); err != nil {
		t.Fatalf("owner read own unlinked: %v", err)
	}
	if _, err := svc.GetByID(uploaded.Attachment.ID, attOutsiderID, auth.RoleMember); err != ErrForbidden {
		t.Fatalf("outsider read unlinked: got %v", err)
	}

	// List 未关联：owner 只看自己的；admin 全量
	own, err := svc.List(attMemberID, auth.RoleMember, ListParams{Page: 1, PerPage: 20})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range own.Items {
		if item.ID == uploaded.Attachment.ID {
			found = true
		}
	}
	if !found || own.Total < 1 {
		t.Fatalf("owner unlinked list: %+v", own)
	}
	all, err := svc.List(attAdminID, auth.RoleAdmin, ListParams{Page: 1, PerPage: 100})
	if err != nil {
		t.Fatal(err)
	}
	if all.Total < 1 {
		t.Fatalf("admin unlinked list total = %d", all.Total)
	}
}

func TestDBSoftDelete(t *testing.T) {
	svc, _ := testAttachmentService(t, &fakePermissionChecker{read: true})
	content := []byte("soft-delete")
	uploaded, err := svc.Upload(memFileOf(content), headerFor(content), attMemberID, auth.RoleMember, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// 他人删除 → ErrForbidden
	if err := svc.SoftDelete(uploaded.Attachment.ID, attOutsiderID, auth.RoleMember); err != ErrForbidden {
		t.Fatalf("outsider delete: got %v", err)
	}
	// 上传者删除 → OK
	if err := svc.SoftDelete(uploaded.Attachment.ID, attMemberID, auth.RoleMember); err != nil {
		t.Fatalf("owner delete: %v", err)
	}
	// 删除后不可见（soft delete 语义：行还在但 get 过滤 deleted_at）
	if _, err := svc.GetByID(uploaded.Attachment.ID, attAdminID, auth.RoleAdmin); err != ErrAttachmentNotFound {
		t.Fatalf("get deleted: got %v", err)
	}
	// 重复删除 → ErrAttachmentNotFound
	if err := svc.SoftDelete(uploaded.Attachment.ID, attAdminID, auth.RoleAdmin); err != ErrAttachmentNotFound {
		t.Fatalf("double delete: got %v", err)
	}
}

func TestDBAddAndRemoveLink(t *testing.T) {
	svc, db := testAttachmentService(t, &fakePermissionChecker{read: true, write: true})
	content := []byte("link-test")
	uploaded, err := svc.Upload(memFileOf(content), headerFor(content), attMemberID, auth.RoleMember, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM attachments WHERE id = $1`, uploaded.Attachment.ID) })

	// AddLink：绑定 issue 实体
	link, err := svc.AddLink(uploaded.Attachment.ID, attMemberID, auth.RoleMember, CreateLinkRequest{
		EntityType: EntityIssue, EntityID: attEntityID, Description: " 补充说明 ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if link.EntityType != EntityIssue || link.Description != "补充说明" {
		t.Fatalf("link: %+v", link)
	}
	// 重复绑定 → ErrLinkExists
	if _, err := svc.AddLink(uploaded.Attachment.ID, attMemberID, auth.RoleMember, CreateLinkRequest{
		EntityType: EntityIssue, EntityID: attEntityID,
	}); err != ErrLinkExists {
		t.Fatalf("dup link: got %v", err)
	}
	// 非法实体 → ErrInvalidInput
	if _, err := svc.AddLink(uploaded.Attachment.ID, attMemberID, auth.RoleMember, CreateLinkRequest{
		EntityType: "nope", EntityID: attEntityID,
	}); err != ErrInvalidInput {
		t.Fatalf("bad entity: got %v", err)
	}
	// 目标实体无写权限 → ErrForbidden
	svcNoWrite := NewService(NewRepository(db), &fakePermissionChecker{read: true, write: false}, svc.storageDir)
	if _, err := svcNoWrite.AddLink(uploaded.Attachment.ID, attMemberID, auth.RoleMember, CreateLinkRequest{
		EntityType: EntityLog, EntityID: attEntityID,
	}); err != ErrForbidden {
		t.Fatalf("no write: got %v", err)
	}

	// RemoveLink：非法 link_id → ErrInvalidInput；不存在的 link → ErrLinkNotFound
	if err := svc.RemoveLink(uploaded.Attachment.ID, "bad", attMemberID, auth.RoleMember); err != ErrInvalidInput {
		t.Fatalf("bad link id: got %v", err)
	}
	if err := svc.RemoveLink(uploaded.Attachment.ID, "d0000000-0000-4000-8000-000000009999", attMemberID, auth.RoleMember); err != ErrLinkNotFound {
		t.Fatalf("missing link: got %v", err)
	}
	// 实体写权限成员（非上传者）可移除 link（canOperate 兜底 entityAllowed）
	otherLink, err := svc.AddLink(uploaded.Attachment.ID, attOwnerID, auth.RoleMember, CreateLinkRequest{
		EntityType: EntityLog, EntityID: attEntityID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RemoveLink(uploaded.Attachment.ID, otherLink.ID, attOwnerID, auth.RoleMember); err != nil {
		t.Fatalf("owner remove: %v", err)
	}
	// 上传者移除自己加的 link
	if err := svc.RemoveLink(uploaded.Attachment.ID, link.ID, attMemberID, auth.RoleMember); err != nil {
		t.Fatalf("uploader remove: %v", err)
	}
	// 移除后 link 消失
	remaining, err := NewRepository(db).GetLinks(uploaded.Attachment.ID)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("links after remove: %v err=%v", remaining, err)
	}
}

// ---------- handler 层 ----------

func TestHandlerUpload(t *testing.T) {
	db := openAttachmentsTestDB(t)
	svc := NewService(NewRepository(db), &fakePermissionChecker{write: true}, t.TempDir())
	router := newAttachmentsTestRouter(t, db, svc)
	member := attachmentsToken(t, attMemberID, "member", auth.RoleMember)

	// 401：无 token
	rec := attRequest(t, router, buildUploadRequest(t, "", attUniqueKey(t), "", "", "", "a.txt", []byte("x")))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token = %d", rec.Code)
	}
	// 400：缺 Idempotency-Key
	rec = attRequest(t, router, buildUploadRequest(t, member, "", "", "", "", "a.txt", []byte("x")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no idempotency key = %d", rec.Code)
	}
	// 400：缺 file 字段
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("entity_type", EntityIssue)
	_ = writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/attachments", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+member)
	req.Header.Set("Idempotency-Key", attUniqueKey(t))
	rec = attRequest(t, router, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no file field = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 201：上传成功，响应含 sha256/link
	rec = attRequest(t, router, buildUploadRequest(t, member, attUniqueKey(t), EntityIssue, attEntityID, "截图",
		"photo.png", []byte("hello-attachment")))
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload = %d, body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Attachment struct {
				ID     string `json:"id"`
				Sha256 string `json:"sha256"`
			} `json:"attachment"`
			Links []struct {
				EntityType string `json:"entity_type"`
			} `json:"links"`
		} `json:"data"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Attachment.ID == "" || len(envelope.Data.Attachment.Sha256) != 64 ||
		len(envelope.Data.Links) != 1 || envelope.Data.Links[0].EntityType != EntityIssue {
		t.Fatalf("upload response: %+v", envelope.Data)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM attachments WHERE id = $1`, envelope.Data.Attachment.ID) })
}

func TestHandlerListGetDownload(t *testing.T) {
	db := openAttachmentsTestDB(t)
	svc := NewService(NewRepository(db), &fakePermissionChecker{read: true, write: true}, t.TempDir())
	router := newAttachmentsTestRouter(t, db, svc)
	member := attachmentsToken(t, attMemberID, "member", auth.RoleMember)
	outsider := attachmentsToken(t, attOutsiderID, "outsider", auth.RoleMember)

	// 先直接 service 上传一个实体附件
	uploaded, err := svc.Upload(memFileOf([]byte("handler-download")), headerFor([]byte("handler-download")),
		attMemberID, auth.RoleMember, EntityIssue, attEntityID, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM attachments WHERE id = $1`, uploaded.Attachment.ID) })

	// GET list：实体过滤 + 空结果 + 非法 entity_type
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/attachments?entity_type="+EntityIssue+"&entity_id="+attEntityID, nil)
	req.Header.Set("Authorization", "Bearer "+member)
	rec := attRequest(t, router, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d, body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data.Items) != 1 || envelope.Data.Items[0].ID != uploaded.Attachment.ID {
		t.Fatalf("entity list: %+v", envelope.Data.Items)
	}
	// 非法 entity_type → 400
	req = httptest.NewRequest(http.MethodGet, "/api/v1/attachments?entity_type=bogus&entity_id="+attEntityID, nil)
	req.Header.Set("Authorization", "Bearer "+member)
	rec = attRequest(t, router, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bogus entity filter = %d", rec.Code)
	}

	// GET by id：200；outsider 403
	req = httptest.NewRequest(http.MethodGet, "/api/v1/attachments/"+uploaded.Attachment.ID, nil)
	req.Header.Set("Authorization", "Bearer "+member)
	rec = attRequest(t, router, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d, body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/attachments/"+uploaded.Attachment.ID, nil)
	req.Header.Set("Authorization", "Bearer "+outsider)
	rec = attRequest(t, router, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("outsider get = %d", rec.Code)
	}
	// 404：不存在
	req = httptest.NewRequest(http.MethodGet, "/api/v1/attachments/d0000000-0000-4000-8000-000000009999", nil)
	req.Header.Set("Authorization", "Bearer "+member)
	rec = attRequest(t, router, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing get = %d", rec.Code)
	}

	// GET content：200 + Content-Disposition（audit action=attachments.download）
	key := attUniqueKey(t)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/attachments/"+uploaded.Attachment.ID+"/content", nil)
	req.Header.Set("Authorization", "Bearer "+member)
	req.Header.Set("Idempotency-Key", key)
	rec = attRequest(t, router, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("download = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("content-disposition = %q", rec.Header().Get("Content-Disposition"))
	}
	if rec.Body.String() != "handler-download" {
		t.Fatalf("download body = %q", rec.Body.String())
	}
	assertAttachmentAudit(t, db, rec.Header().Get("X-Request-Id"), "attachments.download")
}

func TestHandlerLinksAndSoftDelete(t *testing.T) {
	db := openAttachmentsTestDB(t)
	svc := NewService(NewRepository(db), &fakePermissionChecker{read: true, write: true}, t.TempDir())
	router := newAttachmentsTestRouter(t, db, svc)
	member := attachmentsToken(t, attMemberID, "member", auth.RoleMember)

	uploaded, err := svc.Upload(memFileOf([]byte("links")), headerFor([]byte("links")),
		attMemberID, auth.RoleMember, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM attachments WHERE id = $1`, uploaded.Attachment.ID) })

	// POST links：201
	body := `{"entity_type":"issue","entity_id":"` + attEntityID + `","description":"d"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/attachments/"+uploaded.Attachment.ID+"/links", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+member)
	req.Header.Set("Idempotency-Key", attUniqueKey(t))
	rec := attRequest(t, router, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add link = %d, body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			ID         string `json:"id"`
			EntityType string `json:"entity_type"`
		} `json:"data"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.ID == "" || envelope.Data.EntityType != EntityIssue {
		t.Fatalf("add link response: %+v", envelope.Data)
	}
	assertAttachmentAudit(t, db, envelope.RequestID, "attachments."+uploaded.Attachment.ID+".links")

	// 重复绑定 → 409
	req = httptest.NewRequest(http.MethodPost, "/api/v1/attachments/"+uploaded.Attachment.ID+"/links", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+member)
	req.Header.Set("Idempotency-Key", attUniqueKey(t))
	rec = attRequest(t, router, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("dup link = %d, body=%s", rec.Code, rec.Body.String())
	}

	// DELETE links/{link_id}：200 + 审计落库（SetAuditAction → attachments.remove_link）
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/attachments/"+uploaded.Attachment.ID+"/links/"+envelope.Data.ID, nil)
	req.Header.Set("Authorization", "Bearer "+member)
	req.Header.Set("Idempotency-Key", attUniqueKey(t))
	rec = attRequest(t, router, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("remove link = %d, body=%s", rec.Code, rec.Body.String())
	}
	var okEnvelope struct {
		Data      struct{ Success bool } `json:"data"`
		RequestID string                 `json:"request_id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &okEnvelope)
	assertAttachmentAudit(t, db, okEnvelope.RequestID, "attachments.remove_link")

	// DELETE /{id}：软删 + 审计；再删 404
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/attachments/"+uploaded.Attachment.ID, nil)
	req.Header.Set("Authorization", "Bearer "+member)
	req.Header.Set("Idempotency-Key", attUniqueKey(t))
	rec = attRequest(t, router, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("soft delete = %d, body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &okEnvelope)
	assertAttachmentAudit(t, db, okEnvelope.RequestID, "attachments."+uploaded.Attachment.ID)

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/attachments/"+uploaded.Attachment.ID, nil)
	req.Header.Set("Authorization", "Bearer "+member)
	req.Header.Set("Idempotency-Key", attUniqueKey(t))
	rec = attRequest(t, router, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("double delete = %d, body=%s", rec.Code, rec.Body.String())
	}
}

// ---------- 工具 ----------

func openDB(t *testing.T) *sql.DB {
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
	return db
}

// memFile 包装内存内容为 multipart.File。
type memFile struct{ *bytes.Reader }

func (memFile) Close() error { return nil }

func memFileOf(content []byte) multipart.File { return &memFile{Reader: bytes.NewReader(content)} }

func headerFor(content []byte) *multipart.FileHeader {
	return &multipart.FileHeader{Filename: "photo.png", Size: int64(len(content))}
}

func assertAttachmentAudit(t *testing.T, db *sql.DB, requestID, action string) {
	t.Helper()
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM audit_log WHERE request_id = $1 AND action = $2`, requestID, action,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("audit_log rows = %d, want 1 (request_id=%s action=%s)", count, requestID, action)
	}
}
