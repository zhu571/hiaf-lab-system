package translations

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/lib/pq"
)

func TestRepositoryHashInvalidationAndClaimOwnership(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	const entityID = "f4100000-0000-4000-8000-000000000001"
	_, _ = db.Exec(`DELETE FROM content_translations WHERE entity_id=$1`, entityID)
	defer db.Exec(`DELETE FROM content_translations WHERE entity_id=$1`, entityID)
	r := NewRepository(db)
	if err := r.Ensure("log", entityID, "content", "old", "zh", "en", "", false); err != nil {
		t.Fatal(err)
	}
	if err := r.SaveManual("log", entityID, "content", "old", "zh", "en", "人工译文", ""); err != nil {
		t.Fatal(err)
	}
	if err := r.Ensure("log", entityID, "content", "old", "zh", "en", "", false); err != nil {
		t.Fatal(err)
	}
	var status, origin, text string
	if err := db.QueryRow(`SELECT status,origin,translated_text FROM content_translations WHERE entity_id=$1`, entityID).Scan(&status, &origin, &text); err != nil {
		t.Fatal(err)
	}
	if status != StatusReady || origin != "manual" || text != "人工译文" {
		t.Fatalf("same hash changed manual: %s %s %s", status, origin, text)
	}
	if err := r.Ensure("log", entityID, "content", "new", "zh", "en", "", false); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status,origin,COALESCE(translated_text,'') FROM content_translations WHERE entity_id=$1`, entityID).Scan(&status, &origin, &text); err != nil {
		t.Fatal(err)
	}
	if status != StatusPending || origin != "ai" || text != "" {
		t.Fatalf("new hash not invalidated: %s %s %s", status, origin, text)
	}
	if _, err := db.Exec(`UPDATE content_translations SET status='failed',attempts=3 WHERE entity_id=$1`, entityID); err != nil {
		t.Fatal(err)
	}
	if err := r.Ensure("log", entityID, "content", "new", "zh", "en", "", false); err != nil {
		t.Fatal(err)
	}
	var attempts int
	if err := db.QueryRow(`SELECT status,attempts FROM content_translations WHERE entity_id=$1`, entityID).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != StatusPending || attempts != 0 {
		t.Fatalf("failed retry not requeued: %s attempts=%d", status, attempts)
	}
	x, err := r.Claim(context.Background())
	if err != nil || x == nil {
		t.Fatalf("claim: %#v %v", x, err)
	}
	if err := r.Complete(x.ID, "wrong-token", x.SourceHash, "bad", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status FROM content_translations WHERE id=$1`, x.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "processing" {
		t.Fatalf("wrong token changed status: %s", status)
	}
	if err := r.Complete(x.ID, x.ClaimToken, x.SourceHash, "good", "m", "1.0"); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status FROM content_translations WHERE id=$1`, x.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != StatusReady {
		t.Fatalf("valid token did not complete: %s", status)
	}
}
