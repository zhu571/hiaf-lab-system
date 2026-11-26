package assistant

import (
	"context"
	"errors"
	"testing"
)

func TestCreateRejectsProjectSessionsUntilEnabled(t *testing.T) {
	svc := NewService(nil)
	_, err := svc.Create(context.Background(), "u1", CreateSessionRequest{ProjectID: "p1"})
	if !errors.Is(err, ErrProjectUnsupported) {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateValidatesTitleLength(t *testing.T) {
	svc := NewService(nil)
	long := make([]rune, 201)
	for i := range long {
		long[i] = 'a'
	}
	_, err := svc.Create(context.Background(), "u1", CreateSessionRequest{Title: string(long)})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateValidatesStatus(t *testing.T) {
	svc := NewService(nil)
	bad := "deleted"
	_, err := svc.Update(context.Background(), "u1", "s1", UpdateSessionRequest{Status: &bad})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestAddMessageRejectsEmpty(t *testing.T) {
	svc := NewService(nil)
	_, err := svc.AddMessage(context.Background(), "u1", "s1", "   ")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestTruncateTitle(t *testing.T) {
	long := ""
	for i := 0; i < 50; i++ {
		long += "测"
	}
	if got := len([]rune(truncateTitle(long))); got != 40 {
		t.Fatalf("len = %d", got)
	}
}
