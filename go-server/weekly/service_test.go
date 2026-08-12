package weekly

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

var testLoc = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		panic(err)
	}
	return loc
}()

var testNow = func() time.Time {
	return time.Date(2026, 8, 9, 10, 0, 0, 0, testLoc) // 2026-08-09 是周日
}

// ---------- fakes ----------

type fakeReportReader struct {
	entries  []ReportEntry
	err      error
	calls    int
	from, to string
}

func (f *fakeReportReader) WeeklyReports(ctx context.Context, from, to string) ([]ReportEntry, error) {
	f.calls++
	f.from, f.to = from, to
	return f.entries, f.err
}

type fakeIssueStatsReader struct {
	stats IssueStats
	err   error
	calls int
}

func (f *fakeIssueStatsReader) WeeklyIssueStats(ctx context.Context, from, to string) (IssueStats, error) {
	f.calls++
	return f.stats, f.err
}

type fakeExperienceStore struct {
	saved      []*SavedSummary
	findResult *SavedSummary
	findErr    error
	saveErr    error
	callsFind  int
	callsSave  int
}

func (f *fakeExperienceStore) FindWeeklySummary(title string) (*SavedSummary, error) {
	f.callsFind++
	return f.findResult, f.findErr
}

func (f *fakeExperienceStore) SaveWeeklySummary(authorID, title, content string) (*SavedSummary, error) {
	f.callsSave++
	if f.saveErr != nil {
		return nil, f.saveErr
	}
	s := &SavedSummary{ID: "exp_weekly_1", Title: title, Markdown: content, CreatedAt: testNow()}
	f.saved = append(f.saved, s)
	return s, nil
}

type fakeLLM struct {
	resp  *LLMResponse
	err   error
	calls int
}

func (f *fakeLLM) Summarize(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if f.resp != nil {
		return f.resp, nil
	}
	return &LLMResponse{Status: "ok", Title: "周报 2026-08-03 ~ 2026-08-09",
		Summary: "本周完成匹配电路装配。", Markdown: "## 本周进展\n完成匹配电路装配。",
		Highlights: []string{"完成匹配电路装配"}}, nil
}

type fakeNotifier struct {
	calls int
	last  map[string]string
}

func (f *fakeNotifier) Send(topic, title, msg, clickURL, priority string, tags []string) error {
	f.calls++
	f.last = map[string]string{"topic": topic, "title": title, "msg": msg, "priority": priority}
	return nil
}

func newTestService(reports *fakeReportReader, store *fakeExperienceStore) (*Service, *fakeLLM, *fakeNotifier) {
	llm := &fakeLLM{}
	notifier := &fakeNotifier{}
	svc := NewService(reports, &fakeIssueStatsReader{}, store, llm, notifier, testLoc, testNow)
	return svc, llm, notifier
}

func sampleReports() []ReportEntry {
	return []ReportEntry{{ReportDate: "2026-08-03", AuthorName: "张三",
		RawText: "装配匹配电路", Summary: "装配匹配电路"}}
}

// ---------- Generate ----------

func TestGenerateHappyPath(t *testing.T) {
	reports := &fakeReportReader{entries: sampleReports()}
	store := &fakeExperienceStore{}
	svc, llm, notifier := newTestService(reports, store)

	result, err := svc.Generate(context.Background(), "usr_1", "", true)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Reused || result.ID != "exp_weekly_1" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.WeekStart != "2026-08-03" || result.WeekEnd != "2026-08-09" {
		t.Fatalf("week range = %s~%s, want 2026-08-03~2026-08-09", result.WeekStart, result.WeekEnd)
	}
	if reports.from != "2026-08-03" || reports.to != "2026-08-09" {
		t.Fatalf("report range = %s~%s", reports.from, reports.to)
	}
	if llm.calls != 1 || store.callsSave != 1 {
		t.Fatalf("llm=%d save=%d, want 1/1", llm.calls, store.callsSave)
	}
	if notifier.calls != 1 {
		t.Fatalf("notifier calls = %d, want 1", notifier.calls)
	}
	if notifier.last["topic"] != "lab-weekly" {
		t.Fatalf("topic = %q", notifier.last["topic"])
	}
	if store.saved[0].Title != "周报 2026-08-03 ~ 2026-08-09" {
		t.Fatalf("saved title = %q", store.saved[0].Title)
	}
}

func TestGenerateExplicitWeekStart(t *testing.T) {
	reports := &fakeReportReader{entries: sampleReports()}
	store := &fakeExperienceStore{}
	svc, _, _ := newTestService(reports, store)
	result, err := svc.Generate(context.Background(), "usr_1", "2026-07-27", false)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.WeekStart != "2026-07-27" || result.WeekEnd != "2026-08-02" {
		t.Fatalf("week range = %s~%s", result.WeekStart, result.WeekEnd)
	}
}

func TestGenerateInvalidWeekStart(t *testing.T) {
	svc, _, _ := newTestService(&fakeReportReader{}, &fakeExperienceStore{})
	for _, raw := range []string{"2026-08-05", "2026/08/03", "bad"} {
		if _, err := svc.Generate(context.Background(), "u1", raw, true); !errors.Is(err, ErrInvalidWeekStart) {
			t.Fatalf("week_start=%q: got %v, want ErrInvalidWeekStart", raw, err)
		}
	}
}

func TestGenerateNoReports(t *testing.T) {
	store := &fakeExperienceStore{}
	svc, llm, notifier := newTestService(&fakeReportReader{}, store)
	_, err := svc.Generate(context.Background(), "u1", "", true)
	if !errors.Is(err, ErrNoReports) {
		t.Fatalf("got %v, want ErrNoReports", err)
	}
	if llm.calls != 0 || store.callsSave != 0 || notifier.calls != 0 {
		t.Fatal("no reports must not call llm/save/notify")
	}
}

func TestGenerateReusedSameWeek(t *testing.T) {
	store := &fakeExperienceStore{findResult: &SavedSummary{ID: "exp_old", Title: "周报 2026-08-03 ~ 2026-08-09", Markdown: "old body"}}
	svc, llm, notifier := newTestService(&fakeReportReader{}, store)
	result, err := svc.Generate(context.Background(), "u1", "", true)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !result.Reused || result.ID != "exp_old" || result.Markdown != "old body" {
		t.Fatalf("unexpected reused result: %+v", result)
	}
	if llm.calls != 0 || store.callsSave != 0 || notifier.calls != 0 {
		t.Fatal("reused week must not call llm/save/notify")
	}
}

func TestGenerateNotifyFailureTolerated(t *testing.T) {
	reports := &fakeReportReader{entries: sampleReports()}
	svc, _, _ := newTestService(reports, &fakeExperienceStore{})
	notifier := &failNotifier{}
	svc.notify, svc.notifyOn = notifier, true
	if _, err := svc.Generate(context.Background(), "u1", "", true); err != nil {
		t.Fatalf("notify failure must not fail Generate: %v", err)
	}
}

func TestGenerateNotifyOff(t *testing.T) {
	reports := &fakeReportReader{entries: sampleReports()}
	store := &fakeExperienceStore{}
	svc, _, notifier := newTestService(reports, store)
	if _, err := svc.Generate(context.Background(), "u1", "", false); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if notifier.calls != 0 {
		t.Fatal("notify=false must skip ntfy")
	}
}

func TestGenerateLLMErrorPropagates(t *testing.T) {
	reports := &fakeReportReader{entries: sampleReports()}
	svc, llm, _ := newTestService(reports, &fakeExperienceStore{})
	llm.err = ErrUpstream
	if _, err := svc.Generate(context.Background(), "u1", "", true); !errors.Is(err, ErrUpstream) {
		t.Fatalf("got %v, want ErrUpstream", err)
	}
}

func TestGenerateInvalidLLMOutputRejected(t *testing.T) {
	reports := &fakeReportReader{entries: sampleReports()}
	svc, llm, _ := newTestService(reports, &fakeExperienceStore{})
	llm.resp = &LLMResponse{Status: "ok", Title: " ", Summary: "s", Markdown: "m"}
	if _, err := svc.Generate(context.Background(), "u1", "", true); !errors.Is(err, ErrInvalidLLMOutput) {
		t.Fatalf("got %v, want ErrInvalidLLMOutput", err)
	}
}

// ---------- weekStartOf / validateLLMResponse ----------

func TestWeekStartOfMonday(t *testing.T) {
	// 2026-08-05 是周三 → 周一 2026-08-03；2026-08-03 周一 → 自身
	for given, want := range map[string]string{
		"2026-08-05": "2026-08-03",
		"2026-08-03": "2026-08-03",
		"2026-08-09": "2026-08-03",
	} {
		tm, _ := time.ParseInLocation(time.DateOnly, given, testLoc)
		got := weekStartOf(tm).Format(time.DateOnly)
		if got != want {
			t.Fatalf("weekStartOf(%s) = %s, want %s", given, got, want)
		}
	}
}

func TestValidateLLMResponseBounds(t *testing.T) {
	ok := &LLMResponse{Status: "ok", Title: "周报", Summary: "s", Markdown: "m", Highlights: []string{"h"}}
	if err := validateLLMResponse(ok); err != nil {
		t.Fatalf("valid response rejected: %v", err)
	}
	bad := []*LLMResponse{
		{Status: "rejected", Title: "t", Summary: "s", Markdown: "m"},
		{Status: "ok", Title: "", Summary: "s", Markdown: "m"},
		{Status: "ok", Title: "t", Summary: "", Markdown: "m"},
		{Status: "ok", Title: "t", Summary: "s", Markdown: "m", Highlights: make([]string, 26)},
		{Status: "ok", Title: "t", Summary: "s", Markdown: "m", Highlights: []string{" "}},
	}
	for i, item := range bad {
		if err := validateLLMResponse(item); !errors.Is(err, ErrInvalidLLMOutput) {
			t.Fatalf("case %d: got %v, want ErrInvalidLLMOutput", i, err)
		}
	}
}

type failNotifier struct{}

func (failNotifier) Send(topic, title, msg, clickURL, priority string, tags []string) error {
	return errors.New("ntfy down")
}

// ---------- limitReports 载荷收紧 ----------

func TestLimitReportsCountAndFieldCaps(t *testing.T) {
	many := make([]ReportEntry, 120)
	for i := range many {
		many[i] = ReportEntry{ReportDate: "2026-08-03", AuthorName: "张三",
			RawText: "短文本" + strconv.Itoa(i), Summary: "短摘要"}
	}
	out := limitReports(many)
	if len(out) != maxWeeklyReports {
		t.Fatalf("count = %d, want %d", len(out), maxWeeklyReports)
	}
	// 升序输入下保留末尾 100 条（丢最旧保最新）
	if out[len(out)-1].RawText != "短文本119" || out[0].RawText != "短文本20" {
		t.Fatalf("oldest-drop wrong: first=%q last=%q", out[0].RawText, out[len(out)-1].RawText)
	}
	one := limitReports([]ReportEntry{{RawText: strings.Repeat("长", 9000), Summary: strings.Repeat("摘", 3500),
		AuthorName: strings.Repeat("名", 200)}})
	if len([]rune(one[0].RawText)) != maxWeeklyRawText ||
		len([]rune(one[0].Summary)) != maxWeeklySummary ||
		len([]rune(one[0].AuthorName)) != maxWeeklyAuthorName {
		t.Fatal("field truncation not applied")
	}
}

func TestLimitReportsPayloadBudgetKeepsAtLeastOne(t *testing.T) {
	heavy := make([]ReportEntry, 100)
	for i := range heavy {
		heavy[i] = ReportEntry{ReportDate: "2026-08-03", AuthorName: "张三",
			RawText: strings.Repeat("长", maxWeeklyRawText), Summary: strings.Repeat("摘", maxWeeklySummary)}
	}
	out := limitReports(heavy)
	if len(out) == 0 || len(out) >= len(heavy) {
		t.Fatalf("budget drop expected partial, got %d", len(out))
	}
	if out[len(out)-1].RawText != heavy[len(heavy)-1].RawText {
		t.Fatal("must keep newest reports (trailing window)")
	}
}

// ---------- Scheduler ----------

func TestSchedulerFiresSunday20(t *testing.T) {
	svc, _, _ := newTestService(&fakeReportReader{entries: sampleReports()}, &fakeExperienceStore{})
	s := NewScheduler(svc, "usr_1", testLoc, testNow)
	ctx := context.Background()
	at := func(day, hour, minute int) time.Time {
		return time.Date(2026, 8, day, hour, minute, 0, 0, testLoc)
	}
	// 2026-08-09 是周日：20:00 触发
	s.check(ctx, at(9, 20, 0))
	if s.tracker.consecutive != 0 {
		t.Fatalf("consecutive = %d after success", s.tracker.consecutive)
	}
	// 其他时间不触发（失败计数不变）
	s.check(ctx, at(9, 20, 1))
	s.check(ctx, at(9, 19, 59))
	s.check(ctx, at(10, 20, 0)) // 周一
	if s.tracker.consecutive != 0 {
		t.Fatal("non-window ticks must not run")
	}
}

func TestSchedulerSkipWithoutAuthor(t *testing.T) {
	svc, _, _ := newTestService(&fakeReportReader{}, &fakeExperienceStore{})
	s := NewScheduler(svc, "", testLoc, testNow)
	s.check(context.Background(), time.Date(2026, 8, 9, 20, 0, 0, 0, testLoc))
	if s.tracker.consecutive != 0 {
		t.Fatal("scheduler without author must stay idle")
	}
}

func TestSchedulerFailureTrackerAlertsAfterThree(t *testing.T) {
	svc, _, _ := newTestService(&fakeReportReader{entries: sampleReports()}, &fakeExperienceStore{})
	s := NewScheduler(svc, "usr_1", testLoc, testNow)
	alerts := 0
	s.sendAlert = func(title, msg string) error { alerts++; return nil }
	// 连续 3 次失败 → 告警
	s.noteFailure(false)
	s.noteFailure(false)
	s.noteFailure(false)
	if alerts != 1 {
		t.Fatalf("alerts = %d, want 1", alerts)
	}
	if s.tracker.consecutive != 0 {
		t.Fatal("counter must reset after alert")
	}
	s.noteFailure(true) // 成功清零
	if s.tracker.consecutive != 0 {
		t.Fatal("success must reset counter")
	}
}

func TestSchedulerNoReportsNotAFailure(t *testing.T) {
	// 无日报 → ErrNoReports（合法状态）：连续多周不得计入失败链、不得告警。
	svc, llm, _ := newTestService(&fakeReportReader{}, &fakeExperienceStore{})
	s := NewScheduler(svc, "usr_1", testLoc, testNow)
	alerts := 0
	s.sendAlert = func(title, msg string) error { alerts++; return nil }
	for i := 0; i < 3; i++ {
		s.runGenerate(context.Background())
	}
	if alerts != 0 || s.tracker.consecutive != 0 {
		t.Fatalf("no-report weeks must not count as failures: alerts=%d consecutive=%d", alerts, s.tracker.consecutive)
	}
	if llm.calls != 0 {
		t.Fatal("no reports must not call llm")
	}
}
