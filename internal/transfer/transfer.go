package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/crossainthero-lab/sshdrop/internal/filesystem"
	"github.com/pkg/sftp"
)

type Direction string

const (
	Upload   Direction = "upload"
	Download Direction = "download"
)

type ConflictAction string

const (
	ConflictOverwrite ConflictAction = "overwrite"
	ConflictSkip      ConflictAction = "skip"
	ConflictRename    ConflictAction = "rename"
)

type Job struct {
	ID          int64
	Direction   Direction
	Source      string
	Destination string
	Size        int64
	Transferred int64
	Status      string
	Error       string
	StartedAt   time.Time
	FinishedAt  time.Time
}

type Snapshot struct {
	Jobs        []Job
	Active      *Job
	Queued      int
	Completed   int
	Failed      int
	Transferred int64
	Speed       float64
}

type Manager struct {
	mu       sync.Mutex
	cond     *sync.Cond
	nextID   int64
	jobs     []*Job
	active   *Job
	cancel   context.CancelFunc
	resolver func(dst string, dir Direction) ConflictAction
}

func NewManager() *Manager {
	m := &Manager{resolver: func(string, Direction) ConflictAction { return ConflictOverwrite }}
	m.cond = sync.NewCond(&m.mu)
	return m
}

func (m *Manager) SetConflictResolver(fn func(dst string, dir Direction) ConflictAction) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if fn == nil {
		fn = func(string, Direction) ConflictAction { return ConflictOverwrite }
	}
	m.resolver = fn
}

func (m *Manager) EnqueueUpload(ctx context.Context, s *sftp.Client, localPaths []string, remoteDir string) ([]int64, error) {
	var jobs []Job
	for _, p := range localPaths {
		if err := collectUpload(s, p, remoteDir, &jobs); err != nil {
			return nil, err
		}
	}
	return m.enqueue(ctx, s, jobs), nil
}

func (m *Manager) EnqueueDownload(ctx context.Context, s *sftp.Client, remotePaths []string, localDir string) ([]int64, error) {
	var jobs []Job
	for _, p := range remotePaths {
		if err := collectDownload(s, p, localDir, &jobs); err != nil {
			return nil, err
		}
	}
	return m.enqueue(ctx, s, jobs), nil
}

func (m *Manager) EnqueueJobs(ctx context.Context, s *sftp.Client, jobs []Job) []int64 {
	return m.enqueue(ctx, s, jobs)
}

func (m *Manager) CancelActive() {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Unlock()
}

func (m *Manager) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	var snap Snapshot
	start := time.Time{}
	for _, j := range m.jobs {
		c := *j
		snap.Jobs = append(snap.Jobs, c)
		switch c.Status {
		case "queued":
			snap.Queued++
		case "done":
			snap.Completed++
		case "failed", "cancelled":
			snap.Failed++
		}
		snap.Transferred += c.Transferred
		if !c.StartedAt.IsZero() && (start.IsZero() || c.StartedAt.Before(start)) {
			start = c.StartedAt
		}
	}
	if m.active != nil {
		a := *m.active
		snap.Active = &a
	}
	if !start.IsZero() {
		sec := time.Since(start).Seconds()
		if sec > 0 {
			snap.Speed = float64(snap.Transferred) / sec
		}
	}
	return snap
}

func (m *Manager) enqueue(ctx context.Context, s *sftp.Client, jobs []Job) []int64 {
	m.mu.Lock()
	ids := make([]int64, 0, len(jobs))
	for i := range jobs {
		m.nextID++
		j := jobs[i]
		j.ID = m.nextID
		j.Status = "queued"
		m.jobs = append(m.jobs, &j)
		ids = append(ids, j.ID)
	}
	shouldStart := m.active == nil
	m.mu.Unlock()
	if shouldStart {
		go m.worker(ctx, s)
	}
	return ids
}

func (m *Manager) worker(ctx context.Context, s *sftp.Client) {
	for {
		m.mu.Lock()
		var job *Job
		for _, candidate := range m.jobs {
			if candidate.Status == "queued" {
				job = candidate
				break
			}
		}
		if job == nil {
			m.active = nil
			m.cancel = nil
			m.mu.Unlock()
			return
		}
		job.Status = "running"
		job.StartedAt = time.Now()
		runCtx, cancel := context.WithCancel(ctx)
		m.active = job
		m.cancel = cancel
		resolver := m.resolver
		m.mu.Unlock()

		err := copyJob(runCtx, s, job, resolver, func(n int64) {
			m.mu.Lock()
			job.Transferred += n
			m.mu.Unlock()
		})
		cancel()

		m.mu.Lock()
		job.FinishedAt = time.Now()
		if err != nil {
			if errors.Is(err, context.Canceled) {
				job.Status = "cancelled"
			} else {
				job.Status = "failed"
				job.Error = err.Error()
			}
		} else {
			job.Status = "done"
		}
		m.active = nil
		m.cancel = nil
		m.mu.Unlock()
	}
}

func copyJob(ctx context.Context, s *sftp.Client, job *Job, resolver func(string, Direction) ConflictAction, progress func(int64)) error {
	switch job.Direction {
	case Upload:
		return uploadFile(ctx, s, job, resolver, progress)
	case Download:
		return downloadFile(ctx, s, job, resolver, progress)
	default:
		return fmt.Errorf("unsupported transfer direction %q", job.Direction)
	}
}

func collectUpload(s *sftp.Client, localPath, remoteDir string, jobs *[]Job) error {
	info, err := os.Lstat(localPath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("unsupported symbolic link: %s", localPath)
	}
	if !info.IsDir() {
		*jobs = append(*jobs, Job{Direction: Upload, Source: localPath, Destination: path.Join(remoteDir, filepath.Base(localPath)), Size: info.Size()})
		return nil
	}
	rootParent := filepath.Dir(localPath)
	return filepath.WalkDir(localPath, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsupported symbolic link: %s", p)
		}
		rel, err := filepath.Rel(rootParent, p)
		if err != nil {
			return err
		}
		dst := path.Join(remoteDir, filepath.ToSlash(rel))
		if d.IsDir() {
			return s.MkdirAll(dst)
		}
		*jobs = append(*jobs, Job{Direction: Upload, Source: p, Destination: dst, Size: info.Size()})
		return nil
	})
}

func collectDownload(s *sftp.Client, remotePath, localDir string, jobs *[]Job) error {
	info, err := s.Lstat(remotePath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("unsupported symbolic link: %s", remotePath)
	}
	if !info.IsDir() {
		*jobs = append(*jobs, Job{Direction: Download, Source: remotePath, Destination: filepath.Join(localDir, path.Base(remotePath)), Size: info.Size()})
		return nil
	}
	rootParent := path.Dir(remotePath)
	walker := s.Walk(remotePath)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			return err
		}
		p := walker.Path()
		info := walker.Stat()
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsupported symbolic link: %s", p)
		}
		rel, err := filepath.Rel(filepath.FromSlash(rootParent), filepath.FromSlash(p))
		if err != nil {
			return err
		}
		dst := filepath.Join(localDir, rel)
		if info.IsDir() {
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
			continue
		}
		*jobs = append(*jobs, Job{Direction: Download, Source: p, Destination: dst, Size: info.Size()})
	}
	return nil
}

func uploadFile(ctx context.Context, s *sftp.Client, job *Job, resolver func(string, Direction) ConflictAction, progress func(int64)) error {
	src, err := os.Open(job.Source)
	if err != nil {
		return err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return err
	}
	dst := job.Destination
	final, skip, err := resolveRemoteConflict(s, dst, Upload, resolver)
	if err != nil || skip {
		return err
	}
	if err := s.MkdirAll(path.Dir(final)); err != nil {
		return err
	}
	tmp := partialRemoteName(final)
	out, err := s.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY)
	if err != nil {
		return err
	}
	_, err = copyWithProgress(ctx, out, src, progress)
	closeErr := out.Close()
	if err != nil {
		_ = s.Remove(tmp)
		return err
	}
	if closeErr != nil {
		_ = s.Remove(tmp)
		return closeErr
	}
	if err := s.Rename(tmp, final); err != nil {
		_ = s.Remove(tmp)
		return err
	}
	_ = s.Chtimes(final, info.ModTime(), info.ModTime())
	return nil
}

func downloadFile(ctx context.Context, s *sftp.Client, job *Job, resolver func(string, Direction) ConflictAction, progress func(int64)) error {
	src, err := s.Open(job.Source)
	if err != nil {
		return err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return err
	}
	final, skip, err := resolveLocalConflict(job.Destination, Download, resolver)
	if err != nil || skip {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return err
	}
	tmp := partialLocalName(final)
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = copyWithProgress(ctx, out, src, progress)
	closeErr := out.Close()
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Chtimes(final, info.ModTime(), info.ModTime())
	return nil
}

func copyWithProgress(ctx context.Context, dst io.Writer, src io.Reader, progress func(int64)) (int64, error) {
	buf := make([]byte, 256*1024)
	var total int64
	for {
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[:nr])
			if nw > 0 {
				total += int64(nw)
				progress(int64(nw))
			}
			if ew != nil {
				return total, ew
			}
			if nr != nw {
				return total, io.ErrShortWrite
			}
		}
		if er != nil {
			if er == io.EOF {
				return total, nil
			}
			return total, er
		}
	}
}

func resolveRemoteConflict(s *sftp.Client, dst string, dir Direction, resolver func(string, Direction) ConflictAction) (string, bool, error) {
	if _, err := s.Stat(dst); err != nil {
		return dst, false, nil
	}
	switch resolver(dst, dir) {
	case ConflictSkip:
		return dst, true, nil
	case ConflictRename:
		return nextRemoteName(s, dst), false, nil
	default:
		return dst, false, nil
	}
}

func resolveLocalConflict(dst string, dir Direction, resolver func(string, Direction) ConflictAction) (string, bool, error) {
	if _, err := os.Stat(dst); err != nil {
		return dst, false, nil
	}
	switch resolver(dst, dir) {
	case ConflictSkip:
		return dst, true, nil
	case ConflictRename:
		return nextLocalName(dst), false, nil
	default:
		return dst, false, nil
	}
}

func nextLocalName(dst string) string {
	ext := filepath.Ext(dst)
	base := strings.TrimSuffix(dst, ext)
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s-%d%s", base, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func nextRemoteName(s *sftp.Client, dst string) string {
	ext := path.Ext(dst)
	base := strings.TrimSuffix(dst, ext)
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s-%d%s", base, i, ext)
		if _, err := s.Stat(candidate); err != nil {
			return candidate
		}
	}
}

func partialLocalName(final string) string {
	return fmt.Sprintf("%s.sshdrop-partial-%d", final, time.Now().UnixNano())
}

func partialRemoteName(final string) string {
	return fmt.Sprintf("%s.sshdrop-partial-%d", final, time.Now().UnixNano())
}

func DirectJobs(direction Direction, srcs []string, dst string) ([]Job, error) {
	var jobs []Job
	for _, src := range srcs {
		if direction == Upload {
			clean, err := filesystem.ValidateLocalPath(src)
			if err != nil {
				return nil, err
			}
			info, err := os.Stat(clean)
			if err != nil {
				return nil, err
			}
			if info.IsDir() {
				return nil, fmt.Errorf("directory direct jobs require SFTP expansion: %s", clean)
			}
			jobs = append(jobs, Job{Direction: Upload, Source: clean, Destination: path.Join(dst, filepath.Base(clean)), Size: info.Size()})
		} else {
			remote, err := filesystem.ValidateRemotePath(src)
			if err != nil {
				return nil, err
			}
			jobs = append(jobs, Job{Direction: Download, Source: remote, Destination: filepath.Join(dst, path.Base(remote))})
		}
	}
	return jobs, nil
}
