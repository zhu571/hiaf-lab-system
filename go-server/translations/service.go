package translations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

type Service struct {
	repo       *Repository
	client     *http.Client
	url, token string
	stop       chan struct{}
	mu         sync.Mutex
	sources    map[string]string
	reader     SourceReader
	audit      func(context.Context, string, map[string]any) error
}

type SourceReader interface {
	Source(context.Context, string, string, string) (string, error)
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo, client: &http.Client{Timeout: 180 * time.Second}, stop: make(chan struct{}), sources: map[string]string{}}
}
func (s *Service) SetSourceReader(reader SourceReader) { s.reader = reader }
func (s *Service) SetAuditWriter(writer func(context.Context, string, map[string]any) error) {
	s.audit = writer
}
func (s *Service) AutoConfigure() {
	s.url = strings.TrimRight(os.Getenv("PY_AGENT_INTERPRET_URL"), "/")
	if p := os.Getenv("PY_AGENT_INTERNAL_TOKEN_FILE"); p != "" {
		if b, e := os.ReadFile(p); e == nil {
			s.token = strings.TrimSpace(string(b))
		}
	}
}
func (s *Service) Start(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stop:
				return
			default:
			}
			s.run(ctx)
			time.Sleep(time.Second)
		}
	}()
}
func (s *Service) Stop()      { close(s.stop) }
func Hash(text string) string { h := sha256.Sum256([]byte(text)); return hex.EncodeToString(h[:]) }
func DetectLocale(text string) string {
	for _, term := range ProtectedTerms(text) {
		text = strings.ReplaceAll(text, term, " ")
	}
	var zh, lat int
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			zh++
		} else if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			lat++
		}
	}
	if zh == 0 && lat == 0 {
		return "und"
	}
	if zh > 0 && lat > 0 && float64(zh)/float64(zh+lat) >= .2 && float64(lat)/float64(zh+lat) >= .2 {
		return "mixed"
	}
	if zh >= lat {
		return "zh"
	}
	return "en"
}

var protected = regexp.MustCompile(`(?:https?://[^\s]+|` + "`[^`]+`" + `|\b(?:[A-Z]{2,}[A-Z0-9_:-]*|[A-Za-z]+\d+[A-Za-z0-9-]*)\b|\b\d+(?:\.\d+)?(?:e[+-]?\d+|×10[⁻⁰¹²³⁴⁵⁶⁷⁸⁹]+)?\s*(?:Pa|K|dBm|V|A|Hz|mbar)?)`)

func ProtectedTerms(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range protected.FindAllString(text, -1) {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}
func (s *Service) Ensure(entity, id, field, source, target, user string, force bool) error {
	if source == "" {
		return nil
	}
	if target != "zh" && target != "en" {
		return errors.New("invalid target locale")
	}
	s.mu.Lock()
	s.sources[id+":"+field+":"+target] = source
	s.mu.Unlock()
	return s.repo.Ensure(entity, id, field, source, DetectLocale(source), target, user, force)
}

func (s *Service) EnsureAuto(entity, id, field, source, user string) error {
	if source == "" {
		return nil
	}
	locale := DetectLocale(source)
	targets := []string{"zh", "en"}
	if locale == "zh" {
		targets = []string{"en"}
	}
	if locale == "en" {
		targets = []string{"zh"}
	}
	for _, target := range targets {
		if err := s.Ensure(entity, id, field, source, target, user, false); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) SaveManual(entity, id, field, source, target, text, user string) error {
	if strings.TrimSpace(text) == "" || len([]rune(text)) > 4000 {
		return errors.New("invalid translation")
	}
	return s.repo.SaveManual(entity, id, field, source, DetectLocale(source), target, text, user)
}
func (s *Service) Sidecar(ctx context.Context, entity string, ids []string, fields []string, sources map[string]string) (map[string]FieldTranslations, error) {
	rows, err := s.repo.List(ctx, entity, ids, fields, sources)
	if err != nil {
		return nil, err
	}
	out := map[string]FieldTranslations{}
	for _, id := range ids {
		for _, f := range fields {
			source := sources[id+":"+f]
			v := FieldTranslations{SourceLocale: DetectLocale(source), SourceHash: Hash(source), Zh: Variant{Status: StatusMissing}, En: Variant{Status: StatusMissing}}
			for _, x := range rows {
				if x.EntityID != id || x.FieldName != f {
					continue
				}
				variant := Variant{Status: x.Status, Origin: x.Origin, Editable: x.Origin != "source"}
				if x.Status == "ready" {
					variant.Text = x.Text
					variant.Editable = x.Origin != "source"
				}
				if x.TargetLocale == "zh" {
					v.Zh = variant
				} else {
					v.En = variant
				}
			}
			if v.SourceLocale == "zh" {
				v.Zh = Variant{Status: "ready", Origin: "source"}
			}
			if v.SourceLocale == "en" {
				v.En = Variant{Status: "ready", Origin: "source"}
			}
			out[id+":"+f] = v
		}
	}
	return out, nil
}
func (s *Service) run(ctx context.Context) {
	if s.url == "" || s.token == "" {
		return
	}
	x, e := s.repo.Claim(ctx)
	if e != nil || x == nil {
		return
	}
	s.mu.Lock()
	source := s.sources[x.EntityID+":"+x.FieldName+":"+x.TargetLocale]
	s.mu.Unlock()
	if source == "" && s.reader != nil {
		source, _ = s.reader.Source(ctx, x.EntityType, x.EntityID, x.FieldName)
	}
	if source == "" {
		_ = s.repo.Fail(x.ID, x.ClaimToken, "source_unavailable", false)
		return
	}
	text, model, prompt, e := s.Translate(ctx, source, x)
	if e != nil {
		_ = s.repo.Fail(x.ID, x.ClaimToken, "provider_unavailable", x.Attempts < 3)
		return
	}
	_ = s.repo.Complete(x.ID, x.ClaimToken, x.SourceHash, text, model, prompt)
	if s.audit != nil {
		_ = s.audit(ctx, "content_translation.generated", map[string]any{"translation_id": x.ID, "status": StatusReady, "model": model, "prompt_version": prompt})
	}
}
func (s *Service) Translate(ctx context.Context, source string, x *row) (string, string, string, error) {
	payload, _ := json.Marshal(map[string]any{"source_text": source, "source_locale": x.SourceLocale, "target_locale": x.TargetLocale, "field": x.EntityType + "." + x.FieldName, "protected_terms": ProtectedTerms(source)})
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, s.url+"/v1/translate", strings.NewReader(string(payload)))
	if e != nil {
		return "", "", "", e
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")
	resp, e := s.client.Do(req)
	if e != nil {
		return "", "", "", e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", "", errors.New("translation upstream")
	}
	var v Response
	if e = json.NewDecoder(resp.Body).Decode(&v); e != nil || v.Status != "ok" || strings.TrimSpace(v.TranslatedText) == "" {
		return "", "", "", errors.New("invalid translation")
	}
	for _, term := range ProtectedTerms(source) {
		if !strings.Contains(v.TranslatedText, term) {
			return "", "", "", errors.New("protected term lost")
		}
	}
	return v.TranslatedText, v.Model, v.PromptVersion, nil
}
