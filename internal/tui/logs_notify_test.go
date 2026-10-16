package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"paseo-dock/internal/config"
	"paseo-dock/internal/operations"
)

func TestPersistentJobLogsBoundedAndSanitized(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < jobHistoryLimit+2; i++ {
		l := newJobLog()
		l.openFile(dir)
		l.emit(operations.Event{Stage: "stdout", Text: "\x1b[31m" + strings.Repeat("한", jobLogLimit) + "\x1b[0m\n"})
		l.emit(operations.Event{Stage: "stdout", Text: "last line\n"})
		l.finish()
		raw, err := os.ReadFile(l.filePath)
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) > jobLogLimit || !utf8.Valid(raw) || strings.ContainsRune(string(raw), '\x1b') || !strings.HasSuffix(string(raw), "last line\n") {
			t.Fatalf("invalid persisted tail (%d bytes)", len(raw))
		}
		info, err := os.Stat(l.filePath)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("permissions: %v %v", info, err)
		}
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.log"))
	if err != nil || len(files) != jobHistoryLimit {
		t.Fatalf("retained %d files: %v", len(files), err)
	}
}

func TestLogPersistenceFailureVisible(t *testing.T) {
	l := newJobLog()
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	l.openFile(path)
	l.emit(operations.Event{Text: "still visible\n"})
	l.finish()
	if !strings.Contains(l.fileInfo(), "로그 저장 오류") || !strings.Contains(l.text(), "still visible") {
		t.Fatal("failure lost output or diagnostic")
	}
}

func TestLogViewportFollowsUntilScrolledUp(t *testing.T) {
	m := NewDashboard(&jobBackend{}, config.DefaultCatalog(), "")
	defer m.cancel()
	l := newJobLog()
	m.jobs = []*job{{id: 1, state: "running", log: l}}
	m.Focus, m.rightTab = 3, 1
	for i := 0; i < 80; i++ {
		l.emit(operations.Event{Text: fmt.Sprintf("line %d\n", i)})
	}
	m.View()
	if !m.logs.AtBottom() {
		t.Fatal("initial view did not follow")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	offset := m.logs.YOffset()
	l.emit(operations.Event{Text: "new output\n"})
	m.View()
	if !m.logPaused || m.logs.YOffset() != offset {
		t.Fatal("output moved paused viewport")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	l.emit(operations.Event{Text: "newest line\n"})
	m.View()
	if m.logPaused || !m.logs.AtBottom() || !strings.Contains(m.logs.View(), "newest line") {
		t.Fatal("return to bottom did not resume following")
	}
	m.Height = 12
	m.View()
	if !m.logs.AtBottom() {
		t.Fatal("resize lost tail")
	}
}

func TestCompletionNotificationsAndPersistentResult(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			b := &jobBackend{execute: func(_ context.Context, _ operations.Request, emit func(operations.Event)) (operations.Result, error) {
				emit(operations.Event{Text: "operation output\n"})
				// A subprocess may exit in the middle of a terminal control sequence.
				emit(operations.Event{Stage: "stdout", Text: "\x1b]unfinished"})
				if fail {
					return operations.Result{}, errors.New("test failure")
				}
				return operations.Result{}, nil
			}}
			m := NewDashboard(b, config.DefaultCatalog(), "")
			defer m.cancel()
			m.logDir = t.TempDir()
			runJobCommand(m, m.enqueue(operations.Request{Kind: operations.KindCreate}))
			want := "완료"
			if fail {
				want = "실패: test failure"
			}
			if !strings.Contains(m.View().Content, want) {
				t.Fatalf("notice missing: %s", m.notice)
			}
			raw, err := os.ReadFile(m.jobs[0].log.filePath)
			if err != nil || !strings.Contains(string(raw), want) || !strings.Contains(string(raw), "operation output") {
				t.Fatalf("missing persisted result: %s %v", raw, err)
			}
			id := m.noticeID
			m.Update(noticeExpiredMsg(id - 1))
			if m.notice == "" {
				t.Fatal("old timeout cleared latest notice")
			}
			m.Update(noticeExpiredMsg(id))
			if m.notice != "" {
				t.Fatal("notice did not expire")
			}
			m.notice = "another result"
			keyCmd(m, "?")
			if m.notice != "" || !m.Help {
				t.Fatal("dismiss key did not retain normal behavior")
			}
		})
	}
}
