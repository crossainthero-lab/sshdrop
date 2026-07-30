package filesystem

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Entry struct {
	Name     string
	Path     string
	Size     int64
	IsDir    bool
	Mode     fs.FileMode
	ModTime  time.Time
	TypeName string
}

func ParentEntry(current string, remote bool) Entry {
	parent := filepath.Dir(current)
	if remote {
		parent = path.Dir(current)
	}
	if parent == current {
		parent = current
	}
	return Entry{Name: "..", Path: parent, IsDir: true, TypeName: "dir"}
}

func ListLocal(dir string) ([]Entry, error) {
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	dir = filepath.Clean(dir)
	infos, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	entries := []Entry{ParentEntry(dir, false)}
	for _, de := range infos {
		info, err := de.Info()
		if err != nil {
			continue
		}
		entries = append(entries, Entry{
			Name:     de.Name(),
			Path:     filepath.Join(dir, de.Name()),
			Size:     info.Size(),
			IsDir:    info.IsDir(),
			Mode:     info.Mode(),
			ModTime:  info.ModTime(),
			TypeName: typeName(info),
		})
	}
	sortEntries(entries[1:])
	return entries, nil
}

func ValidateLocalPath(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", errors.New("local path is empty")
	}
	clean := filepath.Clean(p)
	if strings.Contains(clean, "\x00") {
		return "", errors.New("local path contains a null byte")
	}
	return clean, nil
}

func ValidateRemotePath(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", errors.New("remote path is empty")
	}
	if strings.Contains(p, "\x00") {
		return "", errors.New("remote path contains a null byte")
	}
	clean := path.Clean(p)
	if !strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("remote path must be absolute: %s", p)
	}
	return clean, nil
}

func JoinRemote(base, elem string) (string, error) {
	if strings.Contains(elem, "\x00") {
		return "", errors.New("remote path contains a null byte")
	}
	if path.IsAbs(elem) {
		return path.Clean(elem), nil
	}
	return path.Clean(path.Join(base, elem)), nil
}

func FormatSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for next := div * unit; n >= next && exp < 4; next *= unit {
		div = next
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func FormatMode(mode fs.FileMode) string {
	if mode&fs.ModeSymlink != 0 {
		return "link"
	}
	if mode.IsDir() {
		return "dir"
	}
	return "file"
}

func sortEntries(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
}

func typeName(info fs.FileInfo) string {
	return FormatMode(info.Mode())
}
