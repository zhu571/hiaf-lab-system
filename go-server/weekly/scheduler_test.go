package weekly

import (
	"context"
	"testing"
	"time"
)

func TestRunGenerateSkipsWhenAlreadyClaimed(t *testing.T) {
	s := &Scheduler{
		loc: time.UTC,
		now: time.Now,
		claim: func(context.Context, string, string) (bool, error) {
			return false, nil
		},
	}
	// claim=false 时必须在调用 svc.Generate 前返回，svc 为 nil 也不会 panic。
	s.runGenerate(context.Background())
}
