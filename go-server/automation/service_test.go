package automation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// service 层白名单校验（一期边界，与迁移 032 CHECK 一致）；校验失败发生在触达 repository 之前。
func TestCreateValidation(t *testing.T) {
	svc := NewService(nil)
	ctx := context.Background()

	cases := []struct {
		name string
		req  CreateRuleRequest
		want error
	}{
		{"空名称", CreateRuleRequest{Name: " ", TriggerEvent: TriggerDailyReportSubmitted,
			Action: json.RawMessage(`{"type":"enqueue_agent_task"}`)}, ErrInvalidName},
		{"超长名称", CreateRuleRequest{Name: strings.Repeat("x", 129), TriggerEvent: TriggerDailyReportSubmitted,
			Action: json.RawMessage(`{"type":"enqueue_agent_task"}`)}, ErrInvalidName},
		{"非白名单事件", CreateRuleRequest{Name: "r", TriggerEvent: "issue.created",
			Action: json.RawMessage(`{"type":"enqueue_agent_task"}`)}, ErrInvalidTrigger},
		{"action 非 JSON", CreateRuleRequest{Name: "r", TriggerEvent: TriggerDailyReportSubmitted,
			Action: json.RawMessage(`not-json`)}, ErrInvalidAction},
		{"action 缺 type", CreateRuleRequest{Name: "r", TriggerEvent: TriggerDailyReportSubmitted,
			Action: json.RawMessage(`{"mode":"parse_issues"}`)}, ErrInvalidAction},
		{"action 非白名单 type", CreateRuleRequest{Name: "r", TriggerEvent: TriggerDailyReportSubmitted,
			Action: json.RawMessage(`{"type":"webhook"}`)}, ErrInvalidAction},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Create(ctx, "user-1", tc.req); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestSetEnabledRequiresField(t *testing.T) {
	svc := NewService(nil)
	if _, err := svc.SetEnabled(context.Background(), "rule-1", UpdateRuleRequest{}); !errors.Is(err, ErrNothingToUpdate) {
		t.Fatalf("err = %v, want ErrNothingToUpdate", err)
	}
}
