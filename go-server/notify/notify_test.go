package notify

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSend(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || r.URL.Path != "/lab-alerts" || string(body) != "details" {
			t.Fatalf("unexpected request: %s %s %q", r.Method, r.URL.Path, body)
		}
		if r.Header.Get("Title") != "alert" || r.Header.Get("Click") != "http://example.test" || r.Header.Get("Priority") != "urgent" || r.Header.Get("Tags") != "warning,skull" {
			t.Fatalf("unexpected headers: %v", r.Header)
		}
		if user, pass, ok := r.BasicAuth(); !ok || user != "alice" || pass != "secret" {
			t.Fatalf("unexpected basic auth: %q %q %v", user, pass, ok)
		}
	}))
	defer server.Close()
	t.Setenv("NTFY_ADDR", server.URL)
	t.Setenv("NTFY_USER", "alice")
	t.Setenv("NTFY_PASS", "secret")

	if err := Send("lab-alerts", "alert", "details", "http://example.test", "urgent", []string{"warning", "skull"}); err != nil {
		t.Fatal(err)
	}
}

func TestMeowSend(t *testing.T) {
	originalClient := client
	t.Cleanup(func() { client = originalClient })
	client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || r.URL.EscapedPath() != "/f064e4e8/%E5%91%8A%E8%AD%A6%2FA" || r.URL.Query().Get("msgType") != "markdown" || string(body) != "details" {
			t.Fatalf("unexpected request: %s %s %q", r.Method, r.URL.String(), body)
		}
		if r.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
			t.Fatalf("unexpected content type: %s", r.Header.Get("Content-Type"))
		}
		return &http.Response{StatusCode: http.StatusNoContent, Status: "204 No Content", Body: io.NopCloser(strings.NewReader(""))}, nil
	})}

	if err := meowSend("告警/A", "details"); err != nil {
		t.Fatal(err)
	}
}
