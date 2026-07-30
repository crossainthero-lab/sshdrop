package transfer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQueueStateAndRetryFailure(t *testing.T) {
	m := NewManager()
	ids := m.EnqueueJobs(context.Background(), nil, []Job{{Direction: Direction("bad"), Source: "x", Destination: "y"}})
	if len(ids) != 1 {
		t.Fatalf("expected id")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s := m.Snapshot()
		if s.Failed == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job did not fail: %+v", m.Snapshot())
}

func TestPartialLocalName(t *testing.T) {
	name := partialLocalName(filepath.Join("tmp", "file.txt"))
	if !strings.Contains(name, ".sshdrop-partial-") {
		t.Fatalf("partial name missing marker: %s", name)
	}
}

func TestDirectJobsUpload(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	jobs, err := DirectJobs(Upload, []string{file}, "/remote")
	if err != nil {
		t.Fatalf("direct jobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Destination != "/remote/video.mp4" {
		t.Fatalf("bad job: %+v", jobs)
	}
}

func TestConflictResolverRename(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, skip, err := resolveLocalConflict(dst, Download, func(string, Direction) ConflictAction {
		return ConflictRename
	})
	if err != nil || skip {
		t.Fatalf("resolve conflict: skip=%v err=%v", skip, err)
	}
	if got == dst || !strings.Contains(got, "file-1.txt") {
		t.Fatalf("unexpected rename target: %s", got)
	}
}
