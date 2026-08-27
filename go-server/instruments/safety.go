package instruments

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrLeaseBusy          = errors.New("instrument already leased")
	ErrLeaseInvalid       = errors.New("instrument lease invalid")
	ErrApprovalInvalid    = errors.New("instrument approval invalid")
	ErrApprovalSeparation = errors.New("approver must be a different maintainer or admin")
)

type SafetyService struct{ repo *Repository }

func NewSafetyService(repo *Repository) *SafetyService { return &SafetyService{repo: repo} }

func (s *SafetyService) CreateLease(ctx context.Context, instrument, user, purpose string, duration time.Duration, force bool, role string) (*Lease, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("instrument safety repository unavailable")
	}
	purpose = strings.TrimSpace(purpose)
	if purpose == "" || len(purpose) > 256 {
		return nil, fmt.Errorf("purpose is required and must not exceed 256 bytes")
	}
	if duration == 0 {
		duration = 15 * time.Minute
	}
	if duration < time.Minute || duration > 2*time.Hour {
		return nil, fmt.Errorf("lease duration must be between 1 minute and 2 hours")
	}
	if force && role != "admin" {
		return nil, ErrLeaseInvalid
	}
	lease, err := s.repo.CreateLease(ctx, instrument, user, purpose, time.Now().Add(duration), force)
	if err != nil && strings.Contains(err.Error(), "instrument_leases_one_active") {
		return nil, ErrLeaseBusy
	}
	return lease, err
}

func (s *SafetyService) RenewLease(ctx context.Context, id, user, reason string, duration time.Duration) (*Lease, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 256 {
		return nil, fmt.Errorf("renewal reason is required")
	}
	if duration == 0 {
		duration = 15 * time.Minute
	}
	if duration < time.Minute || duration > 2*time.Hour {
		return nil, fmt.Errorf("lease duration must be between 1 minute and 2 hours")
	}
	lease, err := s.repo.RenewLease(ctx, id, user, reason, time.Now().Add(duration))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrLeaseInvalid
	}
	return lease, err
}

func (s *SafetyService) ReleaseLease(ctx context.Context, id, user, role string) error {
	err := s.repo.ReleaseLease(ctx, id, user, role == "admin")
	if errors.Is(err, sql.ErrNoRows) {
		return ErrLeaseInvalid
	}
	return err
}

func canonicalHash(value any) (string, []byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), raw, nil
}

func (s *SafetyService) RequestCommandApproval(ctx context.Context, instrument, leaseID, user, acting, command string, params map[string]any) (*Approval, error) {
	def, err := GetCommand(instrument, command)
	if err != nil || def.Risk == "red" {
		return nil, ErrApprovalInvalid
	}
	normalized, err := NormalizeParams(instrument, command, params)
	if err != nil {
		return nil, err
	}
	if _, err = s.repo.ValidLease(ctx, leaseID, instrument, acting); err != nil {
		return nil, ErrLeaseInvalid
	}
	hash, raw, err := canonicalHash(map[string]any{"instrument_id": instrument, "lease_id": leaseID, "acting_user_id": acting, "command": command, "params": normalized, "whitelist_version": whitelistVersion})
	if err != nil {
		return nil, err
	}
	a := &Approval{LeaseID: &leaseID, CommandName: command, ParamsHash: hash, RequestedBy: user, ActingUserID: &acting, Envelope: raw, EnvelopeHash: hash, Status: "pending", ExpiresAt: time.Now().Add(5 * time.Minute)}
	return a, s.repo.CreateApproval(ctx, a)
}

func (s *SafetyService) Approve(ctx context.Context, id, user, role string) (*Approval, error) {
	if role != "maintainer" && role != "admin" {
		return nil, ErrApprovalSeparation
	}
	a, err := s.repo.Approve(ctx, id, user)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrApprovalSeparation
	}
	return a, err
}

func (s *SafetyService) AuthorizeCommand(ctx context.Context, instrument, user, leaseID, approvalID, command string, params map[string]any) error {
	def, err := GetCommand(instrument, command)
	if err != nil || def.Risk == "red" {
		return ErrApprovalInvalid
	}
	if def.Risk != "yellow" {
		return nil
	}
	if leaseID == "" || approvalID == "" {
		return ErrLeaseInvalid
	}
	if _, err = s.repo.ValidLease(ctx, leaseID, instrument, user); err != nil {
		return ErrLeaseInvalid
	}
	hash, _, err := canonicalHash(map[string]any{"instrument_id": instrument, "lease_id": leaseID, "acting_user_id": user, "command": command, "params": params, "whitelist_version": whitelistVersion})
	if err != nil {
		return err
	}
	if _, err = s.repo.ValidApproval(ctx, approvalID, leaseID, user, command, hash); err != nil {
		return ErrApprovalInvalid
	}
	return nil
}
