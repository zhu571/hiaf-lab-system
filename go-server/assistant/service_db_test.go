package assistant

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/lib/pq"
)

const (
	dbTestUserID    = "00000000-0000-0000-0000-00000000a501"
	dbTestOtherID   = "00000000-0000-0000-0000-00000000a502"
	dbTestSessionID = "00000000-0000-0000-0000-00000000a510"
)

func openAssistantTestDB(t *testing.T) *sql.DB {
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
	for _, u := range []struct{ id, name string }{
		{dbTestUserID, "assistant_dbtest_owner"},
		{dbTestOtherID, "assistant_dbtest_other"},
	} {
		if _, err := db.Exec(
			`INSERT INTO users (id, username, password_hash, display_name, role, must_change_pw, disabled)
			 VALUES ($1, $2, 'x', 'Assistant DB Test', 'member', false, false)
			 ON CONFLICT (id) DO NOTHING`, u.id, u.name); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM assistant_sessions WHERE user_id IN ($1, $2)`, dbTestUserID, dbTestOtherID)
		_, _ = db.Exec(`DELETE FROM assistant_usage WHERE user_id IN ($1, $2)`, dbTestUserID, dbTestOtherID)
		_, _ = db.Exec(`DELETE FROM users WHERE id IN ($1, $2)`, dbTestUserID, dbTestOtherID)
	})
	return db
}

type fakeReplier struct{}

func (fakeReplier) Reply(context.Context, []Message) (string, error) { return "fake answer", nil }

func TestDBServiceSessionLifecycle(t *testing.T) {
	db := openAssistantTestDB(t)
	svc := NewService(NewRepository(db))
	ctx := context.Background()

	sess, err := svc.Create(ctx, dbTestUserID, CreateSessionRequest{Title: "测试会话"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sess.ID == "" || sess.UserID != dbTestUserID || sess.Title != "测试会话" {
		t.Fatalf("unexpected session: %+v", sess)
	}

	// 其他用户不可见、不可写。
	if _, err := svc.Get(ctx, dbTestOtherID, sess.ID); err != ErrNotFound {
		t.Fatalf("other Get err = %v", err)
	}
	if _, err := svc.AddMessage(ctx, dbTestOtherID, sess.ID, "hijack"); err != ErrNotFound {
		t.Fatalf("other AddMessage err = %v", err)
	}

	msg, err := svc.AddMessage(ctx, dbTestUserID, sess.ID, "你好，帮我查一下上周的测试数据")
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	if msg.Role != "user" || msg.Content == "" {
		t.Fatalf("unexpected message: %+v", msg)
	}
	msgs, err := svc.Messages(ctx, dbTestUserID, sess.ID)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("Messages = %v, %v", msgs, err)
	}
	title := "改名后的会话"
	if _, err := svc.Update(ctx, dbTestUserID, sess.ID, UpdateSessionRequest{Title: &title}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	// run 骨架：用户消息 + assistant 消息，run 完成。
	svc.SetReplier(fakeReplier{})
	runResult, err := svc.StartRun(ctx, dbTestUserID, sess.ID, "再问一次")
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if runResult.Run.Status != "done" || len(runResult.Messages) != 2 {
		t.Fatalf("unexpected run result: %+v", runResult)
	}
	if runResult.Messages[1].Role != "assistant" || runResult.Messages[1].Content != "fake answer" {
		t.Fatalf("unexpected assistant message: %+v", runResult.Messages[1])
	}

	if err := svc.Archive(ctx, dbTestUserID, sess.ID); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	list, err := svc.List(ctx, dbTestUserID, statusArchived, 10)
	if err != nil || len(list) == 0 {
		t.Fatalf("archived list = %v, %v", list, err)
	}
}
