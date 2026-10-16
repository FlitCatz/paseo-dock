package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Each execution owns its file. The UI only reads metadata under the log mutex.
func (l *jobLog) openFile(dir string) {
	if dir == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(dir, 0700); err != nil {
		l.fileErr = err
		return
	}
	f, err := os.CreateTemp(dir, "job-"+time.Now().UTC().Format("20060102T150405.000000000")+"-*.log")
	if err != nil {
		l.fileErr = err
		return
	}
	l.file, l.filePath = f, f.Name()
	entries, err := os.ReadDir(dir)
	if err != nil {
		l.fileErr = err
		return
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "job-") && strings.HasSuffix(e.Name(), ".log") {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	for _, name := range names[:max(0, len(names)-jobHistoryLimit)] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && l.fileErr == nil {
			l.fileErr = err
		}
	}
}

// Append normally; on overflow replace with the same bounded UTF-8 tail shown
// in the viewport. Disk usage stays bounded even for a single very noisy job.
func (l *jobLog) writeFile(clean []byte) {
	if l.file == nil || l.fileErr != nil {
		return
	}
	p := clean
	if l.fileSize+len(p) > jobLogLimit {
		if err := l.file.Truncate(0); err != nil {
			l.fileErr = err
			return
		}
		if _, err := l.file.Seek(0, 0); err != nil {
			l.fileErr = err
			return
		}
		l.fileSize = 0
		p = l.data
	}
	n, err := l.file.Write(p)
	l.fileSize += n
	l.fileErr = err
}

func (l *jobLog) fileInfo() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fileErr != nil {
		return "로그 저장 오류: " + safeText(l.fileErr.Error())
	}
	if l.filePath != "" {
		return fmt.Sprintf("로그 파일: %s", l.filePath)
	}
	return ""
}
