package filesystem

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListLocalAndFormatting(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := ListLocal(dir)
	if err != nil {
		t.Fatalf("list local: %v", err)
	}
	if entries[0].Name != ".." {
		t.Fatalf("missing parent entry: %+v", entries[0])
	}
	if FormatSize(1024) != "1.0 KiB" || FormatSize(5) != "5 B" {
		t.Fatalf("unexpected size formatting")
	}
}

func TestPathValidation(t *testing.T) {
	if _, err := ValidateLocalPath(""); err == nil {
		t.Fatal("empty local path accepted")
	}
	if _, err := ValidateRemotePath("relative"); err == nil {
		t.Fatal("relative remote path accepted")
	}
	if got, err := JoinRemote("/var", "../log"); err != nil || got != "/log" {
		t.Fatalf("join remote = %q, %v", got, err)
	}
}
