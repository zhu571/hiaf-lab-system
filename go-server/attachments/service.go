package attachments

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
)

var (
	ErrAttachmentNotFound = errors.New("附件不存在")
	ErrLinkNotFound       = errors.New("附件绑定不存在")
	ErrForbidden          = errors.New("当前用户无权操作该附件")
	ErrInvalidInput       = errors.New("请求参数无效")
	ErrFileNotFound       = errors.New("附件文件不存在")
)

type attachmentRepository interface {
	Create(attachment *Attachment) error
	GetByID(id string) (*Attachment, error)
	GetBySha256(sha256 string) (*Attachment, error)
	AddLink(link *AttachmentLink) error
	RemoveLink(linkID, userID string) error
	GetLinks(attachmentID string) ([]AttachmentLink, error)
	GetByEntity(entityType, entityID string) ([]Attachment, error)
	ListUnlinked(userID string, page, perPage int) ([]Attachment, int, error)
	SoftDelete(id string) error
}

type PermissionChecker interface {
	Check(entityType, entityID, userID, action string) (bool, error)
}

type Service struct {
	repo        attachmentRepository
	permissions PermissionChecker
	storageDir  string
}

func NewService(repo attachmentRepository, permissions PermissionChecker, storageDir string) *Service {
	return &Service{repo: repo, permissions: permissions, storageDir: storageDir}
}

func (s *Service) Upload(file multipart.File, header *multipart.FileHeader, userID, userRole, entityType, entityID, description string) (*UploadResponse, error) {
	entityType, entityID = strings.TrimSpace(entityType), strings.TrimSpace(entityID)
	if file == nil || header == nil || !validOptionalEntity(entityType, entityID) {
		return nil, ErrInvalidInput
	}
	if entityType != "" {
		if err := validateEntity(entityType, entityID); err != nil {
			return nil, err
		}
		if err := s.requireEntityPermission(entityType, entityID, userID, userRole, "write"); err != nil {
			return nil, err
		}
	}
	name := cleanFilename(header.Filename)
	if len(name) > 256 {
		return nil, ErrInvalidInput
	}
	if err := os.MkdirAll(s.storageDir, 0o750); err != nil {
		return nil, fmt.Errorf("create attachment directory: %w", err)
	}
	temp, err := os.CreateTemp(s.storageDir, ".upload-*")
	if err != nil {
		return nil, fmt.Errorf("create attachment temp file: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)

	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(temp, hash), file)
	if err != nil {
		temp.Close()
		return nil, fmt.Errorf("store attachment: %w", err)
	}
	if _, err := temp.Seek(0, io.SeekStart); err != nil {
		temp.Close()
		return nil, fmt.Errorf("inspect attachment: %w", err)
	}
	peek := make([]byte, 512)
	n, readErr := temp.Read(peek)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		temp.Close()
		return nil, fmt.Errorf("inspect attachment: %w", readErr)
	}
	if err := temp.Close(); err != nil {
		return nil, fmt.Errorf("close attachment temp file: %w", err)
	}

	digest := hex.EncodeToString(hash.Sum(nil))
	if existing, err := s.repo.GetBySha256(digest); err != nil {
		return nil, err
	} else if existing != nil {
		return s.finishUpload(existing, userID, entityType, entityID, description)
	}

	attachment := &Attachment{
		StorageKey:   newStorageKey(name),
		OriginalName: name,
		Sha256:       digest,
		Description:  strings.TrimSpace(description),
		MimeType:     http.DetectContentType(peek[:n]),
		FileSize:     size,
		UploadedBy:   &userID,
	}
	finalName := filepath.Join(s.storageDir, attachment.StorageKey)
	if err := os.Rename(tempName, finalName); err != nil {
		return nil, fmt.Errorf("finalize attachment file: %w", err)
	}
	if err := s.repo.Create(attachment); err != nil {
		_ = os.Remove(finalName)
		if existing, lookupErr := s.repo.GetBySha256(digest); lookupErr == nil && existing != nil {
			return s.finishUpload(existing, userID, entityType, entityID, description)
		}
		return nil, err
	}
	return s.finishUpload(attachment, userID, entityType, entityID, description)
}

func (s *Service) finishUpload(attachment *Attachment, userID, entityType, entityID, description string) (*UploadResponse, error) {
	if entityType != "" {
		link := &AttachmentLink{
			AttachmentID: attachment.ID, EntityType: entityType, EntityID: entityID,
			Description: strings.TrimSpace(description), CreatedBy: &userID,
		}
		if err := s.repo.AddLink(link); err != nil && !errors.Is(err, ErrLinkExists) {
			return nil, err
		}
	}
	links, err := s.repo.GetLinks(attachment.ID)
	if err != nil {
		return nil, err
	}
	return &UploadResponse{Attachment: *attachment, Links: links}, nil
}

func (s *Service) AddLink(attachmentID, userID, userRole string, req CreateLinkRequest) (*AttachmentLink, error) {
	attachment, err := s.getReadable(attachmentID, userID, userRole)
	if err != nil {
		return nil, err
	}
	if err := validateEntity(req.EntityType, req.EntityID); err != nil {
		return nil, err
	}
	if err := s.requireEntityPermission(req.EntityType, req.EntityID, userID, userRole, "write"); err != nil {
		return nil, err
	}
	link := &AttachmentLink{
		AttachmentID: attachment.ID, EntityType: strings.TrimSpace(req.EntityType),
		EntityID: strings.TrimSpace(req.EntityID), Description: strings.TrimSpace(req.Description), CreatedBy: &userID,
	}
	if err := s.repo.AddLink(link); err != nil {
		return nil, err
	}
	return link, nil
}

func (s *Service) RemoveLink(attachmentID, linkID, userID, userRole string) error {
	attachment, err := s.get(attachmentID)
	if err != nil {
		return err
	}
	if uuid.Validate(linkID) != nil {
		return ErrInvalidInput
	}
	links, err := s.repo.GetLinks(attachment.ID)
	if err != nil {
		return err
	}
	var target *AttachmentLink
	for i := range links {
		if links[i].ID == linkID {
			target = &links[i]
			break
		}
	}
	if target == nil {
		return ErrLinkNotFound
	}
	allowed, err := s.canOperate(attachment, links, userID, userRole)
	if err != nil {
		return err
	}
	if !allowed {
		allowed, err = s.entityAllowed(target.EntityType, target.EntityID, userID, userRole, "write")
		if err != nil {
			return err
		}
	}
	if !allowed {
		return ErrForbidden
	}
	if err := s.repo.RemoveLink(linkID, userID); errors.Is(err, sql.ErrNoRows) {
		return ErrLinkNotFound
	} else {
		return err
	}
}

func (s *Service) GetByID(id, userID, userRole string) (*Attachment, error) {
	return s.getReadable(id, userID, userRole)
}

func (s *Service) Download(id, userID, userRole string) (*Attachment, *os.File, error) {
	attachment, err := s.getReadable(id, userID, userRole)
	if err != nil {
		return nil, nil, err
	}
	file, err := os.Open(filepath.Join(s.storageDir, attachment.StorageKey))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, ErrFileNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("open attachment file: %w", err)
	}
	return attachment, file, nil
}

func (s *Service) List(userID, userRole string, params ListParams) (*ListResult, error) {
	params.EntityType, params.EntityID = strings.TrimSpace(params.EntityType), strings.TrimSpace(params.EntityID)
	params.Page, params.PerPage = normalizePage(params.Page, params.PerPage)
	if !validOptionalEntity(params.EntityType, params.EntityID) {
		return nil, ErrInvalidInput
	}
	if params.EntityType == "" {
		owner := userID
		if userRole == auth.RoleAdmin {
			owner = ""
		}
		items, total, err := s.repo.ListUnlinked(owner, params.Page, params.PerPage)
		return &ListResult{Items: items, Total: total, Page: params.Page, PerPage: params.PerPage}, err
	}
	if err := validateEntity(params.EntityType, params.EntityID); err != nil {
		return nil, err
	}
	if err := s.requireEntityPermission(params.EntityType, params.EntityID, userID, userRole, "read"); err != nil {
		return nil, err
	}
	items, err := s.repo.GetByEntity(params.EntityType, params.EntityID)
	if err != nil {
		return nil, err
	}
	total := len(items)
	// ponytail: repository contract returns all entity attachments; add SQL pagination when entity attachment counts justify it.
	start := min((params.Page-1)*params.PerPage, total)
	end := min(start+params.PerPage, total)
	return &ListResult{Items: items[start:end], Total: total, Page: params.Page, PerPage: params.PerPage}, nil
}

func (s *Service) SoftDelete(id, userID, userRole string) error {
	attachment, err := s.get(id)
	if err != nil {
		return err
	}
	if userRole != auth.RoleAdmin && (attachment.UploadedBy == nil || *attachment.UploadedBy != userID) {
		return ErrForbidden
	}
	if err := s.repo.SoftDelete(attachment.ID); errors.Is(err, sql.ErrNoRows) {
		return ErrAttachmentNotFound
	} else {
		return err
	}
}

func (s *Service) get(id string) (*Attachment, error) {
	if uuid.Validate(strings.TrimSpace(id)) != nil {
		return nil, ErrInvalidInput
	}
	attachment, err := s.repo.GetByID(strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if attachment == nil {
		return nil, ErrAttachmentNotFound
	}
	return attachment, nil
}

func (s *Service) getReadable(id, userID, userRole string) (*Attachment, error) {
	attachment, err := s.get(id)
	if err != nil {
		return nil, err
	}
	if userRole == auth.RoleAdmin {
		return attachment, nil
	}
	links, err := s.repo.GetLinks(attachment.ID)
	if err != nil {
		return nil, err
	}
	if len(links) == 0 {
		if attachment.UploadedBy != nil && *attachment.UploadedBy == userID {
			return attachment, nil
		}
		return nil, ErrForbidden
	}
	for _, link := range links {
		allowed, err := s.permissions.Check(link.EntityType, link.EntityID, userID, "read")
		if err != nil {
			return nil, err
		}
		if allowed {
			return attachment, nil
		}
	}
	return nil, ErrForbidden
}

func (s *Service) canOperate(attachment *Attachment, links []AttachmentLink, userID, userRole string) (bool, error) {
	if userRole == auth.RoleAdmin {
		return true, nil
	}
	if len(links) == 0 {
		return attachment.UploadedBy != nil && *attachment.UploadedBy == userID, nil
	}
	for _, link := range links {
		allowed, err := s.permissions.Check(link.EntityType, link.EntityID, userID, "write")
		if err != nil {
			return false, err
		}
		if allowed {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) requireEntityPermission(entityType, entityID, userID, userRole, action string) error {
	allowed, err := s.entityAllowed(entityType, entityID, userID, userRole, action)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

func (s *Service) entityAllowed(entityType, entityID, userID, userRole, action string) (bool, error) {
	if userRole == auth.RoleAdmin {
		return true, nil
	}
	return s.permissions.Check(entityType, entityID, userID, action)
}

func validateEntity(entityType, entityID string) error {
	if !validEntityType(strings.TrimSpace(entityType)) || uuid.Validate(strings.TrimSpace(entityID)) != nil {
		return ErrInvalidInput
	}
	return nil
}

func validOptionalEntity(entityType, entityID string) bool {
	return entityType == "" && entityID == "" || entityType != "" && entityID != ""
}

func validEntityType(entityType string) bool {
	switch entityType {
	case EntityAssemblyStep, EntityDailyReport, EntityIssue, EntityLog, EntityTestData, EntityExperimentRun, EntityRFMatchingRecord:
		return true
	default:
		return false
	}
}

func normalizePage(page, perPage int) (int, int) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	if perPage > 100 {
		perPage = 100
	}
	return page, perPage
}

func cleanFilename(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	if name == "" || name == "." {
		return "attachment"
	}
	return name
}

type HTTPPermissionChecker struct {
	baseURL string
	client  *http.Client
}

func NewHTTPPermissionChecker(baseURL string) *HTTPPermissionChecker {
	return &HTTPPermissionChecker{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 3 * time.Second},
	}
}

func (c *HTTPPermissionChecker) Check(entityType, entityID, userID, action string) (bool, error) {
	endpoint := fmt.Sprintf("%s/api/v1/%s/%s/permission-check", c.baseURL,
		url.PathEscape(entityType+"s"), url.PathEscape(entityID))
	query := url.Values{"user_id": {userID}, "action": {action}}
	req, err := http.NewRequest(http.MethodGet, endpoint+"?"+query.Encode(), nil)
	if err != nil {
		return false, fmt.Errorf("create permission request: %w", err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("check entity permission: %w", err)
	}
	defer resp.Body.Close()
	// TODO: remove permissive fallback after every target module implements permission-check.
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNotImplemented {
		return true, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("permission check returned status %d", resp.StatusCode)
	}
	var result struct {
		Allowed bool `json:"allowed"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, fmt.Errorf("decode permission response: %w", err)
	}
	return result.Allowed, nil
}
