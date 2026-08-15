package attachments

import (
	"errors"
	"testing"

	"github.com/zhu571/hiaf-lab-system/go-server/auth"
)

// recordingChecker 记录 Check 入参并按表返回结果（R3：注入化权限检查契约）。
type recordingChecker struct {
	gotEntityType, gotEntityID, gotUserID, gotUserRole, gotAction string
	allowed                                                        bool
	err                                                            error
}

func (c *recordingChecker) Check(entityType, entityID, userID, userRole, action string) (bool, error) {
	c.gotEntityType, c.gotEntityID, c.gotUserID, c.gotUserRole, c.gotAction =
		entityType, entityID, userID, userRole, action
	return c.allowed, c.err
}

// R3 回归：实体权限检查改构造期注入——拒绝即 ErrForbidden（不再有 404/501
// fail-open 回环分支），且调用方角色透传给检查器（支撑各模块自身角色语义）。
func TestInjectedPermissionCheckerContract(t *testing.T) {
	const entityID = "7e0128b5-ff65-4f7c-bdc1-4c2f419ed5c0"
	checker := &recordingChecker{allowed: false}
	svc := NewService(nil, checker, t.TempDir())

	if err := svc.requireEntityPermission(EntityIssue, entityID, "user-1", auth.RoleMaintainer, "write"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("denied check must map to ErrForbidden, got %v", err)
	}
	if checker.gotEntityType != EntityIssue || checker.gotEntityID != entityID ||
		checker.gotUserID != "user-1" || checker.gotUserRole != auth.RoleMaintainer || checker.gotAction != "write" {
		t.Fatalf("checker args mismatch: %+v", checker)
	}

	// 检查器错误原样上抛（DB 故障可观测，不吞成拒绝也不放行）。
	boom := errors.New("db down")
	checker.err = boom
	if err := svc.requireEntityPermission(EntityIssue, entityID, "user-1", auth.RoleMember, "read"); !errors.Is(err, boom) {
		t.Fatalf("checker error must propagate, got %v", err)
	}

	// admin 在 service 层短路，不触达检查器。
	checker.err, checker.allowed = nil, false
	checker.gotUserID = ""
	if err := svc.requireEntityPermission(EntityIssue, entityID, attAdminIDInTests, auth.RoleAdmin, "write"); err != nil {
		t.Fatalf("admin must bypass checker, got %v", err)
	}
	if checker.gotUserID != "" {
		t.Fatal("admin shortcut must not reach the injected checker")
	}
}

const attAdminIDInTests = "00000000-0000-0000-0000-00000000d105"
