package scans

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Storage 管理 gascell 上的扫描文件单副本（scan-archive §5.1）。
// Root 是扫描根目录（默认 /app/uploads/scans），DB 里的 txt_path 相对该目录。
type Storage struct {
	Root string
}

type Saved struct {
	TXTPath   string
	PNGPath   string
	ThumbPath string
	TXTSize   int64
	PNGSize   int64
	TXTSHA256 string
	PNGSHA256 string
}

func NewStorage(root string) *Storage { return &Storage{Root: strings.TrimSpace(root)} }

func (s *Storage) Save(projectID, scanID string, scanAt *time.Time, txt, pngData []byte) (Saved, error) {
	if s.Root == "" {
		return Saved{}, fmt.Errorf("scan storage root is empty")
	}
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return Saved{}, fmt.Errorf("create scan storage root: %w", err)
	}
	tmpDir, err := os.MkdirTemp(s.Root, ".upload-*")
	if err != nil {
		return Saved{}, fmt.Errorf("create temp scan dir: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	if err := os.WriteFile(filepath.Join(tmpDir, "scan.txt"), txt, 0o644); err != nil {
		return Saved{}, fmt.Errorf("write scan txt: %w", err)
	}
	saved := Saved{TXTSize: int64(len(txt)), TXTSHA256: sha256Hex(txt)}
	if len(pngData) > 0 {
		if err := os.WriteFile(filepath.Join(tmpDir, "scan.png"), pngData, 0o644); err != nil {
			return Saved{}, fmt.Errorf("write scan png: %w", err)
		}
		saved.PNGSize = int64(len(pngData))
		saved.PNGSHA256 = sha256Hex(pngData)
		if thumb, err := makeThumb(pngData); err == nil {
			if err := os.WriteFile(filepath.Join(tmpDir, "thumb.png"), thumb, 0o644); err == nil {
				saved.ThumbPath = filepath.ToSlash(filepath.Join(scanRelDir(projectID, scanID, scanAt), "thumb.png"))
			}
		}
	}

	relDir := scanRelDir(projectID, scanID, scanAt)
	finalDir := filepath.Join(s.Root, relDir)
	if err := os.MkdirAll(filepath.Dir(finalDir), 0o755); err != nil {
		return Saved{}, fmt.Errorf("create scan parent dir: %w", err)
	}
	if err := os.Rename(tmpDir, finalDir); err != nil {
		return Saved{}, fmt.Errorf("commit scan dir: %w", err)
	}
	cleanup = false
	saved.TXTPath = filepath.ToSlash(filepath.Join(relDir, "scan.txt"))
	if saved.PNGSize > 0 {
		saved.PNGPath = filepath.ToSlash(filepath.Join(relDir, "scan.png"))
	}
	return saved, nil
}

// SavePNG 把迟到的 png 补进已存在的扫描目录（同名同内容重复上传场景）。
func (s *Storage) SavePNG(projectID, scanID string, scanAt *time.Time, pngData []byte) (Saved, error) {
	relDir := scanRelDir(projectID, scanID, scanAt)
	dir := filepath.Join(s.Root, relDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Saved{}, fmt.Errorf("create scan dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scan.png"), pngData, 0o644); err != nil {
		return Saved{}, fmt.Errorf("write scan png: %w", err)
	}
	saved := Saved{
		PNGPath:   filepath.ToSlash(filepath.Join(relDir, "scan.png")),
		PNGSize:   int64(len(pngData)),
		PNGSHA256: sha256Hex(pngData),
	}
	if thumb, err := makeThumb(pngData); err == nil {
		if err := os.WriteFile(filepath.Join(dir, "thumb.png"), thumb, 0o644); err == nil {
			saved.ThumbPath = filepath.ToSlash(filepath.Join(relDir, "thumb.png"))
		}
	}
	return saved, nil
}

func (s *Storage) Open(rel string) (*os.File, error) {
	clean, err := s.safePath(rel)
	if err != nil {
		return nil, err
	}
	return os.Open(clean)
}

func (s *Storage) RemoveDir(relDir string) error {
	clean, err := s.safePath(relDir)
	if err != nil {
		return err
	}
	return os.RemoveAll(clean)
}

func (s *Storage) safePath(rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" || filepath.IsAbs(rel) {
		return "", fmt.Errorf("invalid scan path")
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || clean == ".." {
		return "", fmt.Errorf("invalid scan path")
	}
	return filepath.Join(s.Root, clean), nil
}

func scanRelDir(projectID, scanID string, scanAt *time.Time) string {
	year := time.Now().Year()
	if scanAt != nil {
		year = scanAt.Year()
	}
	return filepath.ToSlash(filepath.Join(projectID, fmt.Sprintf("%04d", year), scanID))
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func makeThumb(data []byte) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("empty image")
	}
	targetW := 320
	if w <= targetW {
		return data, nil
	}
	targetH := h * targetW / w
	if targetH < 1 {
		targetH = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, targetW, targetH))
	for y := 0; y < targetH; y++ {
		srcY := bounds.Min.Y + y*h/targetH
		for x := 0; x < targetW; x++ {
			srcX := bounds.Min.X + x*w/targetW
			dst.Set(x, y, img.At(srcX, srcY))
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
