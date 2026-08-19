package translations

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestDetectLocaleAndHash(t *testing.T) {
	if DetectLocale("真空达到 5e-6 Pa") != "zh" || DetectLocale("vacuum reached 5e-6 Pa") != "en" || DetectLocale("RF 匹配 passed") != "mixed" {
		t.Fatal("locale detection")
	}
	if Hash("x") == Hash("y") || len(Hash("x")) != 64 {
		t.Fatal("hash")
	}
}

func TestProtectedTerms(t *testing.T) {
	terms := ProtectedTerms("E5063A at 5e-6 Pa, PV:RF:POWER")
	for _, want := range []string{"E5063A", "5e-6 Pa", "PV:RF:POWER"} {
		found := false
		for _, got := range terms {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %q in %#v", want, terms)
		}
	}
}

func TestResponseJSONTags(t *testing.T) {
	b, err := json.Marshal(Response{TranslatedText: "译文", PromptVersion: "1.0"})
	if err != nil || string(b) != `{"status":"","translated_text":"译文","model":"","prompt_version":"1.0"}` {
		t.Fatalf("response json: %s (%v)", b, err)
	}
}

func TestClaimCompletionRequiresToken(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r := NewRepository(db)
	mock.ExpectQuery(regexp.QuoteMeta("WITH picked AS")).WillReturnRows(sqlmock.NewRows([]string{"id", "entity_type", "entity_id", "field_name", "target_locale", "source_locale", "source_hash", "text", "status", "origin", "model", "prompt_version", "error_code", "attempts", "updated_at", "claim_token"}).AddRow("id", "log", "id", "content", "en", "zh", Hash("原文"), "", "processing", "ai", "", "", "", 1, time.Now(), "claim-a"))
	x, err := r.Claim(context.Background())
	if err != nil || x == nil || x.ClaimToken != "claim-a" {
		t.Fatalf("claim: %#v %v", x, err)
	}
	mock.ExpectExec(regexp.QuoteMeta("UPDATE content_translations SET translated_text=$4")).WithArgs("id", "claim-a", Hash("原文"), "translated", "model", "1.0").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := r.Complete("id", "claim-a", Hash("原文"), "translated", "model", "1.0"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
