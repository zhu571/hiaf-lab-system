package scans

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

const testProject = "11111111-1111-1111-1111-111111111111"

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 400, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 400; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 100, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestUploadSavesFilesAndParses(t *testing.T) {
	root := t.TempDir()
	repo := &fakeRepo{}
	svc := NewService(repo, fakeAccess{allowed: true})
	svc.SetStorage(NewStorage(root))
	svc.SetDefaultMachine("gascell")

	txt := "ref\tpA\tsigma\tVpp1\tVpp2\tVpp3\tVpp4\n0.1\t1\t2\t3\t4\t5\t6\n"
	res, err := svc.Upload(testProject, "u1", projects.RoleMember, UploadRequest{
		Filename: "gc261125-101500-qpig.txt",
		TXT:      []byte(txt),
		PNG:      testPNG(t),
		Tags:     []string{" QPIG "},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ID == "" || res.Duplicate {
		t.Fatalf("result = %+v", res)
	}
	if res.Format != FormatV7 || res.RowCount != 1 || len(res.Tags) != 1 || res.Tags[0] != "qpig" {
		t.Fatalf("scan = %+v", res)
	}
	if res.TXTPath == "" || res.PNGPath == nil || res.ThumbPath == nil {
		t.Fatalf("paths missing: %+v", res)
	}
	txtPath := filepath.Join(root, filepath.FromSlash(res.TXTPath))
	if data, err := os.ReadFile(txtPath); err != nil || !strings.Contains(string(data), "ref\tpA") {
		t.Fatalf("stored txt: err=%v data=%q", err, data)
	}
	thumbPath := filepath.Join(root, filepath.FromSlash(*res.ThumbPath))
	if info, err := os.Stat(thumbPath); err != nil || info.Size() == 0 {
		t.Fatalf("thumb missing: %v", err)
	}
}

func TestIngestDoesNotRequireUserACL(t *testing.T) {
	root := t.TempDir()
	repo := &fakeRepo{}
	svc := NewService(repo, fakeAccess{allowed: false})
	svc.SetStorage(NewStorage(root))

	res, err := svc.Ingest(testProject, UploadRequest{
		Filename: "gc261125-101500-qpig.txt",
		TXT:      []byte("ref\tpA\tsigma\tVpp1\tVpp2\tVpp3\tVpp4\n0.1\t1\t2\t3\t4\t5\t6\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IngestSource != "watcher" || res.UploadedBy != nil {
		t.Fatalf("ingest source/uploaded_by = %q/%v", res.IngestSource, res.UploadedBy)
	}
}

func TestUploadDuplicateReturnsExisting(t *testing.T) {
	root := t.TempDir()
	existing := &Scan{ID: "existing-id", ProjectID: testProject, Filename: "gc.txt", TXTSHA256: "abc"}
	repo := &fakeRepo{existing: existing}
	svc := NewService(repo, fakeAccess{allowed: true})
	svc.SetStorage(NewStorage(root))

	res, err := svc.Upload(testProject, "u1", projects.RoleMember, UploadRequest{
		Filename: "gc.txt", TXT: []byte("1\t2\t3\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Duplicate || res.ID != "existing-id" {
		t.Fatalf("result = %+v", res)
	}
}

func TestUploadRejectsInvalidPNG(t *testing.T) {
	svc := NewService(&fakeRepo{}, fakeAccess{allowed: true})
	svc.SetStorage(NewStorage(t.TempDir()))
	_, err := svc.Upload(testProject, "u1", projects.RoleMember, UploadRequest{
		Filename: "gc.txt", TXT: []byte("1\t2\t3\n"), PNG: []byte("not a png"),
	})
	if err == nil || !strings.Contains(err.Error(), "参数") {
		t.Fatalf("err = %v", err)
	}
}

func TestStorageSaveAndRemove(t *testing.T) {
	root := t.TempDir()
	storage := NewStorage(root)
	saved, err := storage.Save(testProject, "scan-1", nil, []byte("txt"), testPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{saved.TXTPath, saved.PNGPath, saved.ThumbPath} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
	}
	file, err := storage.Open(saved.TXTPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	if err := storage.RemoveDir(filepath.Dir(filepath.FromSlash(saved.TXTPath))); err != nil {
		t.Fatal(err)
	}
}
