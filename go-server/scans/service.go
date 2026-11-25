package scans

import (
	"bytes"
	"errors"
	"image/png"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

var (
	ErrScanNotFound    = errors.New("扫描记录不存在")
	ErrProjectNotFound = errors.New("项目不存在")
	ErrForbidden       = errors.New("当前用户无权访问该项目")
	ErrInvalidInput    = errors.New("请求参数无效")
	ErrFileNotFound    = errors.New("扫描文件不存在")
)

type ProjectAccessChecker interface {
	ProjectExists(projectID string) (bool, error)
	CanAccessProject(projectID, userID, userRole, minRole string) (bool, error)
	ProjectRole(projectID, userID, userRole string) (string, error)
}

type scanRepository interface {
	List(params ListParams) ([]Scan, int, error)
	GetByID(id string) (*Scan, error)
	FindByUniqueKey(projectID, machine, filename, txtSHA string) (*Scan, error)
	Create(item *Scan) error
	AttachPNG(id, pngPath, pngSHA string, pngSize int64, thumbPath *string) error
	Tags(projectID string) ([]TagCount, error)
	UpdateAnnotation(id, title, description string, tags []string, annotatedAt *time.Time) error
	MarkVoid(id, userID, reason string) error
}

type Service struct {
	repo           scanRepository
	access         ProjectAccessChecker
	storage        *Storage
	defaultMachine string
}

func NewService(repo scanRepository, access ProjectAccessChecker) *Service {
	return &Service{repo: repo, access: access, defaultMachine: "gascell"}
}

func (s *Service) SetStorage(storage *Storage) { s.storage = storage }

func (s *Service) SetDefaultMachine(machine string) {
	if strings.TrimSpace(machine) != "" {
		s.defaultMachine = strings.TrimSpace(machine)
	}
}

const (
	maxScanTXT = 5 << 20
	maxScanPNG = 20 << 20
)

var pngSignature = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

// Upload 是人工上传入口（scan-archive §4.2/§5.1）。
func (s *Service) Upload(projectID, userID, userRole string, req UploadRequest) (*UploadResult, error) {
	projectID = strings.TrimSpace(projectID)
	if uuid.Validate(projectID) != nil {
		return nil, ErrInvalidInput
	}
	if err := s.requireAccess(projectID, userID, userRole, projects.RoleMember); err != nil {
		return nil, err
	}
	return s.upload(projectID, userID, req)
}

// Ingest 是 watcher/backfill 的服务端入口：只校验项目存在，不要求用户 ACL（SERVICE_TOKEN 已鉴权）。
func (s *Service) Ingest(projectID string, req UploadRequest) (*UploadResult, error) {
	projectID = strings.TrimSpace(projectID)
	if uuid.Validate(projectID) != nil {
		return nil, ErrInvalidInput
	}
	exists, err := s.access.ProjectExists(projectID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrProjectNotFound
	}
	return s.upload(projectID, "", req)
}

func (s *Service) upload(projectID, userID string, req UploadRequest) (*UploadResult, error) {
	if s.storage == nil {
		return nil, ErrInvalidInput
	}

	filename := strings.TrimSpace(req.Filename)
	if filename == "" || len([]rune(filename)) > 256 || strings.ContainsAny(filename, `/\`) {
		return nil, ErrInvalidInput
	}
	if len(req.TXT) == 0 || len(req.TXT) > maxScanTXT || len(req.PNG) > maxScanPNG {
		return nil, ErrInvalidInput
	}
	if !utf8.Valid(req.TXT) {
		return nil, ErrInvalidInput
	}
	if len(req.PNG) > 0 {
		if !bytes.HasPrefix(req.PNG, pngSignature) {
			return nil, ErrInvalidInput
		}
		if _, err := png.Decode(bytes.NewReader(req.PNG)); err != nil {
			return nil, ErrInvalidInput
		}
	}

	parse := ParseTXT(req.TXT)
	machine := strings.TrimSpace(req.Machine)
	if machine == "" {
		machine = s.defaultMachine
	}
	ingestSource := strings.TrimSpace(req.IngestSource)
	if ingestSource == "" {
		if userID == "" {
			ingestSource = "watcher"
		} else {
			ingestSource = "manual"
		}
	}
	if !validIngestSource(ingestSource) {
		return nil, ErrInvalidInput
	}
	tags, err := normalizeTags(req.Tags, false)
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(req.Title)
	description := strings.TrimSpace(req.Description)
	if len([]rune(title)) > 200 || len([]rune(description)) > 20000 {
		return nil, ErrInvalidInput
	}
	sourceTag := strings.TrimSpace(req.SourceTag)
	if len([]rune(sourceTag)) > 32 {
		return nil, ErrInvalidInput
	}

	scanAt := req.ScanAt
	scanAtSource := strings.TrimSpace(req.ScanAtSource)
	if scanAt == nil {
		now := time.Now()
		scanAt = &now
		if scanAtSource == "" {
			scanAtSource = "upload"
		}
	}
	if scanAtSource == "" {
		scanAtSource = "upload"
	}
	if scanAtSource != "filename" && scanAtSource != "mtime" && scanAtSource != "upload" {
		return nil, ErrInvalidInput
	}

	txtSHA := sha256Hex(req.TXT)
	existing, err := s.repo.FindByUniqueKey(projectID, machine, filename, txtSHA)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if len(req.PNG) > 0 && existing.PNGPath == nil {
			if err := s.attachPNG(existing, req.PNG); err != nil {
				return nil, err
			}
		}
		return &UploadResult{Scan: existing, Duplicate: true}, nil
	}

	scanID := uuid.NewString()
	saved, err := s.storage.Save(projectID, scanID, scanAt, req.TXT, req.PNG)
	if err != nil {
		return nil, err
	}
	item := &Scan{
		ID: scanID, ProjectID: projectID, Machine: machine, Filename: filename,
		SourceTag: sourceTag, Tags: tags, ScanAt: scanAt, ScanAtSource: scanAtSource,
		Format: parse.Format, Columns: parse.Columns, RowCount: parse.RowCount, BadRows: parse.BadRows,
		ParseWarning: strings.Join(parse.Warnings, ";"), TXTSHA256: txtSHA, TXTSize: saved.TXTSize,
		TXTPath: saved.TXTPath, SourcePath: strings.TrimSpace(req.SourcePath), Title: title,
		Description: description, IngestSource: ingestSource,
	}
	if saved.PNGPath != "" {
		item.PNGPath = &saved.PNGPath
		item.PNGSHA256 = &saved.PNGSHA256
		item.PNGSize = &saved.PNGSize
	}
	if saved.ThumbPath != "" {
		item.ThumbPath = &saved.ThumbPath
	}
	if title != "" || description != "" {
		now := time.Now()
		item.AnnotatedAt = &now
	}
	if ingestSource == "manual" && userID != "" {
		item.UploadedBy = &userID
	}
	if err := s.repo.Create(item); err != nil {
		_ = s.storage.RemoveDir(scanRelDir(projectID, scanID, scanAt))
		// 并发同文件：唯一键冲突时返回已有记录。
		if raced, findErr := s.repo.FindByUniqueKey(projectID, machine, filename, txtSHA); findErr == nil && raced != nil {
			return &UploadResult{Scan: raced, Duplicate: true}, nil
		}
		return nil, err
	}
	return &UploadResult{Scan: item, Duplicate: false}, nil
}

func (s *Service) attachPNG(item *Scan, pngData []byte) error {
	saved, err := s.storage.SavePNG(item.ProjectID, item.ID, item.ScanAt, pngData)
	if err != nil {
		return err
	}
	var thumb *string
	if saved.ThumbPath != "" {
		thumb = &saved.ThumbPath
	}
	if err := s.repo.AttachPNG(item.ID, saved.PNGPath, saved.PNGSHA256, saved.PNGSize, thumb); err != nil {
		return err
	}
	item.PNGPath = &saved.PNGPath
	item.PNGSHA256 = &saved.PNGSHA256
	item.PNGSize = &saved.PNGSize
	item.ThumbPath = thumb
	return nil
}

func (s *Service) List(projectID, userID, userRole string, params ListParams) (*ListResult, error) {
	projectID = strings.TrimSpace(projectID)
	if uuid.Validate(projectID) != nil {
		return nil, ErrInvalidInput
	}
	if err := s.requireAccess(projectID, userID, userRole, projects.RoleViewer); err != nil {
		return nil, err
	}
	tags, err := normalizeTags(params.Tags, true)
	if err != nil {
		return nil, err
	}
	if params.TagMode == "" {
		params.TagMode = "any"
	}
	if params.TagMode != "any" && params.TagMode != "all" {
		return nil, ErrInvalidInput
	}
	if params.Format != "" && !validFormat(params.Format) {
		return nil, ErrInvalidInput
	}
	if params.IngestSource != "" && !validIngestSource(params.IngestSource) {
		return nil, ErrInvalidInput
	}
	if strings.TrimSpace(params.Q) != "" {
		params.Q = strings.TrimSpace(params.Q)
		if len([]rune(params.Q)) > 100 {
			return nil, ErrInvalidInput
		}
	}
	if params.From, err = normalizeBoundary(params.From, false); err != nil {
		return nil, err
	}
	if params.To, err = normalizeBoundary(params.To, true); err != nil {
		return nil, err
	}
	if params.IncludeVoid && userRole != auth.RoleAdmin {
		params.IncludeVoid = false
	}
	params.ProjectID = projectID
	params.Tags = tags
	params.Page, params.PerPage = normalizePage(params.Page, params.PerPage)

	items, total, err := s.repo.List(params)
	if err != nil {
		return nil, err
	}
	return &ListResult{Items: items, Total: total, Page: params.Page, PerPage: params.PerPage}, nil
}

func (s *Service) Get(id, userID, userRole string) (*Scan, error) {
	item, err := s.repo.GetByID(strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if err := s.requireAccess(item.ProjectID, userID, userRole, projects.RoleViewer); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) OpenFile(id, kind, userID, userRole string) (*os.File, *Scan, string, error) {
	item, err := s.Get(id, userID, userRole)
	if err != nil {
		return nil, nil, "", err
	}
	if item.IsVoid {
		return nil, nil, "", ErrForbidden
	}
	if s.storage == nil {
		return nil, nil, "", ErrFileNotFound
	}
	var rel, contentType string
	switch kind {
	case "txt":
		rel, contentType = item.TXTPath, "text/plain; charset=utf-8"
	case "png":
		if item.PNGPath == nil {
			return nil, nil, "", ErrFileNotFound
		}
		rel, contentType = *item.PNGPath, "image/png"
	case "thumb":
		if item.ThumbPath == nil {
			return nil, nil, "", ErrFileNotFound
		}
		rel, contentType = *item.ThumbPath, "image/png"
	default:
		return nil, nil, "", ErrFileNotFound
	}
	file, err := s.storage.Open(rel)
	if err != nil {
		return nil, nil, "", ErrFileNotFound
	}
	return file, item, contentType, nil
}

func (s *Service) Tags(projectID, userID, userRole string) ([]TagCount, error) {
	projectID = strings.TrimSpace(projectID)
	if uuid.Validate(projectID) != nil {
		return nil, ErrInvalidInput
	}
	if err := s.requireAccess(projectID, userID, userRole, projects.RoleViewer); err != nil {
		return nil, err
	}
	return s.repo.Tags(projectID)
}

func (s *Service) Update(id, userID, userRole string, req UpdateScanRequest) (*Scan, error) {
	item, err := s.repo.GetByID(strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if err := s.requireAccess(item.ProjectID, userID, userRole, projects.RoleMember); err != nil {
		return nil, err
	}
	if req.Title == nil && req.Description == nil && req.Tags == nil {
		return nil, ErrInvalidInput
	}

	title, description := item.Title, item.Description
	if req.Title != nil {
		title = strings.TrimSpace(*req.Title)
		if len([]rune(title)) > 200 {
			return nil, ErrInvalidInput
		}
	}
	if req.Description != nil {
		description = strings.TrimSpace(*req.Description)
		if len([]rune(description)) > 20000 {
			return nil, ErrInvalidInput
		}
	}
	tags := item.Tags
	if req.Tags != nil {
		if tags, err = normalizeTags(*req.Tags, false); err != nil {
			return nil, err
		}
	}

	annotatedAt := item.AnnotatedAt
	if req.Title != nil || req.Description != nil {
		if title == "" && description == "" {
			annotatedAt = nil
		} else {
			now := time.Now()
			annotatedAt = &now
		}
	}
	if err := s.repo.UpdateAnnotation(item.ID, title, description, tags, annotatedAt); err != nil {
		return nil, err
	}
	return s.repo.GetByID(item.ID)
}

func (s *Service) Void(id, userID, userRole, reason string) (*Scan, error) {
	if userRole == auth.RoleAgent {
		return nil, ErrForbidden
	}
	item, err := s.repo.GetByID(strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if err := s.requireAccess(item.ProjectID, userID, userRole, projects.RoleMember); err != nil {
		return nil, err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 500 {
		return nil, ErrInvalidInput
	}
	if item.IsVoid {
		return nil, ErrInvalidInput
	}
	if err := s.repo.MarkVoid(item.ID, userID, reason); err != nil {
		return nil, err
	}
	return s.repo.GetByID(item.ID)
}

func (s *Service) requireAccess(projectID, userID, userRole, minRole string) error {
	exists, err := s.access.ProjectExists(projectID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrProjectNotFound
	}
	ok, err := s.access.CanAccessProject(projectID, userID, userRole, minRole)
	if err != nil {
		return err
	}
	if !ok {
		return ErrForbidden
	}
	return nil
}

func normalizeTags(tags []string, forFilter bool) ([]string, error) {
	out := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		if len([]rune(tag)) > 32 {
			return nil, ErrInvalidInput
		}
		if seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	if len(out) > 10 {
		return nil, ErrInvalidInput
	}
	if forFilter && len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func normalizeBoundary(raw string, endOfDay bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.UTC
	}
	if len(raw) == 10 {
		day, err := time.ParseInLocation("2006-01-02", raw, loc)
		if err != nil {
			return "", ErrInvalidInput
		}
		if endOfDay {
			day = day.Add(24*time.Hour - time.Second)
		}
		return day.Format(time.RFC3339), nil
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return "", ErrInvalidInput
	}
	return value.Format(time.RFC3339), nil
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

func validFormat(value string) bool {
	switch value {
	case FormatLegacy3, FormatV7, FormatV11, FormatV12, FormatUnknown:
		return true
	default:
		return false
	}
}

func validIngestSource(value string) bool {
	switch value {
	case "watcher", "manual", "backfill":
		return true
	default:
		return false
	}
}

type ProjectAccessAdapter struct {
	Repo interface {
		GetByID(id string) (*projects.Project, error)
		GetMember(projectID, userID string) (*projects.ProjectMember, error)
	}
}

func (a ProjectAccessAdapter) ProjectExists(projectID string) (bool, error) {
	project, err := a.Repo.GetByID(projectID)
	return project != nil, err
}

func (a ProjectAccessAdapter) CanAccessProject(projectID, userID, userRole, minRole string) (bool, error) {
	if userRole == auth.RoleAdmin {
		return true, nil
	}
	member, err := a.Repo.GetMember(projectID, userID)
	if err != nil {
		return false, err
	}
	rank := map[string]int{projects.RoleViewer: 1, projects.RoleMember: 2, projects.RoleMaintainer: 3, projects.RoleOwner: 4}
	return member != nil && member.Status == projects.MemberStatusActive && rank[member.Role] >= rank[minRole], nil
}

func (a ProjectAccessAdapter) ProjectRole(projectID, userID, userRole string) (string, error) {
	if userRole == auth.RoleAdmin {
		return projects.RoleOwner, nil
	}
	member, err := a.Repo.GetMember(projectID, userID)
	if err != nil || member == nil || member.Status != projects.MemberStatusActive {
		return "", err
	}
	return member.Role, nil
}
