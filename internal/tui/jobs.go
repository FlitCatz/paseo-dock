package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"paseo-dock/internal/config"
	"paseo-dock/internal/operations"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

const jobLogLimit = 256 << 10
const jobHistoryLimit = 20

type job struct {
	id                      uint64
	request                 operations.Request
	state, stage, errorText string
	started, finished       time.Time
	result                  operations.Result
	log                     *jobLog
	cancel                  context.CancelFunc
	previewFlow             uint64
}
type jobDoneMsg struct {
	id     uint64
	result operations.Result
	err    error
}
type noticeExpiredMsg uint64
type jobPulseMsg struct {
	id       uint64
	finished bool
}
type shellReadyMsg struct {
	command operations.ShellProcess
	err     error
}
type shellDoneMsg struct{ err error }
type action struct {
	label            string
	kind             operations.Kind
	workspace, shell bool
	reason           string
}
type confirmation struct {
	request        operations.Request
	label, warning string
	typed          string
}

func (c *confirmation) requiredText() string {
	if c.request.Kind == operations.KindRemove {
		return c.request.Target.Profile
	}
	if c.request.Kind == operations.KindPrune && c.request.RemoveRuntimes {
		return "prune"
	}
	return ""
}

func freezeRequest(r operations.Request) operations.Request {
	r.Ports = slices.Clone(r.Ports)
	r.Target.Entry.Ports = slices.Clone(r.Target.Entry.Ports)
	r.Target.Entry.Utilities = slices.Clone(r.Target.Entry.Utilities)
	r.Targets = freezeTargets(r.Targets)
	r.RestoreTargets = freezeTargets(r.RestoreTargets)
	r.PruneTargets = freezeTargets(r.PruneTargets)

	r.Selection.Pairs = slices.Clone(r.Selection.Pairs)
	r.Selection.Utilities = slices.Clone(r.Selection.Utilities)
	return r
}
func freezeTargets(targets []operations.Target) []operations.Target {
	targets = slices.Clone(targets)
	for i := range targets {
		targets[i].Entry.Ports = slices.Clone(targets[i].Entry.Ports)
		targets[i].Entry.Utilities = slices.Clone(targets[i].Entry.Utilities)
	}
	return targets
}
func (m *Dashboard) busy() bool {
	for _, j := range m.jobs {
		if j.state == "queued" || j.state == "running" || j.state == "cancel requested" {
			return true
		}
	}
	return false
}
func (m *Dashboard) enqueue(r operations.Request) tea.Cmd {
	if m.shellPending || m.shellRunning {
		m.actionError = "shell이 터미널을 사용 중"
		return nil
	}
	if len(m.jobs) >= jobHistoryLimit {
		index := -1
		for i, j := range m.jobs {
			if j.finished.IsZero() {
				continue
			}
			index = i
			break
		}
		if index < 0 {
			m.actionError = "작업 대기열이 가득 찼습니다 (20)"
			return nil
		}
		m.jobs = append(m.jobs[:index], m.jobs[index+1:]...)
	}
	m.nextJobID++
	m.jobs = append(m.jobs, &job{id: m.nextJobID, request: freezeRequest(r), state: "queued", log: newJobLog(), previewFlow: m.flowGeneration})
	return m.dispatchJob()
}
func (m *Dashboard) dispatchJob() tea.Cmd {
	if m.runningJob != 0 {
		return nil
	}
	for _, j := range m.jobs {
		if j.state != "queued" {
			continue
		}
		ctx, cancel := context.WithCancel(m.ctx)
		j.cancel = cancel
		j.state = "running"
		j.started = time.Now()
		m.runningJob = j.id
		b, r, id, log := m.Backend, freezeRequest(j.request), j.id, j.log
		dir := m.logDir
		execute := func() tea.Msg {
			log.openFile(dir)
			log.record(fmt.Sprintf("%s %s workspace=%s target=%s started\n", time.Now().Format(time.RFC3339), r.Kind, r.Target.Workspace, r.Target.ResourceIdentity()))
			result, err := b.Execute(ctx, r, log.emit)
			log.record("\n" + completionText(r, result, err) + "\n")
			log.finish()
			return jobDoneMsg{id, result, err}
		}
		return tea.Batch(execute, waitJobPulse(id, log))
	}
	return nil
}
func waitJobPulse(id uint64, log *jobLog) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-log.wake:
		case <-log.done:
		}
		select {
		case <-log.done:
			return jobPulseMsg{id, true}
		default:
			return jobPulseMsg{id, false}
		}
	}
}
func (m *Dashboard) findJob(id uint64) *job {
	for _, j := range m.jobs {
		if j.id == id {
			return j
		}
	}
	return nil
}
func (m *Dashboard) cancelJobs() {
	m.newFlow()
	for _, j := range m.jobs {
		switch j.state {
		case "running":
			j.state = "cancel requested"
			j.cancel()
		case "queued":
			j.state = "cancelled"
			j.finished = time.Now()
		}
	}
}
func (m *Dashboard) jobMessage(msg tea.Msg) (bool, tea.Cmd) {
	switch v := msg.(type) {
	case noticeExpiredMsg:
		if uint64(v) == m.noticeID {
			m.notice = ""
		}
		return true, nil
	case jobPulseMsg:
		if v.finished {
			return true, nil
		}
		if j := m.findJob(v.id); j != nil && m.runningJob == v.id {
			j.stage = j.log.currentStage()
			return true, waitJobPulse(v.id, j.log)
		}
		return true, nil
	case jobDoneMsg:
		j := m.findJob(v.id)
		if j == nil || m.runningJob != v.id {
			return true, nil
		}
		j.cancel()
		j.finished = time.Now()
		j.result = v.result
		j.stage = j.log.currentStage()
		j.state = "completed"
		if v.err != nil {
			j.errorText = safeText(v.err.Error())
			j.state = "failed"
		}
		if j.result.NeedsVerification {
			j.state = "needs verification"
		} else if errors.Is(v.err, context.Canceled) || errors.Is(v.err, context.DeadlineExceeded) {
			j.state = "cancelled"
		}
		m.runningJob = 0
		m.notice = completionText(j.request, v.result, v.err)
		m.noticeID++
		id := m.noticeID
		expire := tea.Tick(8*time.Second, func(time.Time) tea.Msg { return noticeExpiredMsg(id) })
		if v.err == nil && j.request.Kind == operations.KindPrune && j.request.DryRun {
			m.prunePreview(v.result, j.previewFlow, j.request.RemoveRuntimes)
		}
		if m.exitAfterJobs && !m.busy() {
			m.cancel()
			return true, tea.Quit
		}
		return true, tea.Batch(m.dispatchJob(), m.refreshAfterWork(), expire)
	case shellReadyMsg:
		m.shellPending = false
		if v.err != nil {
			m.actionError = safeText(v.err.Error())
			return true, m.refreshAfterWork()
		}
		m.shellRunning = true
		return true, tea.Exec(v.command, func(err error) tea.Msg { return shellDoneMsg{err} })
	case shellDoneMsg:
		m.shellRunning = false
		if v.err != nil {
			m.actionError = "Shell: " + safeText(v.err.Error())
		}
		return true, m.refreshAfterWork()
	}
	return false, nil
}

func completionText(r operations.Request, result operations.Result, err error) string {
	status := "완료"
	if err != nil {
		status = "실패: " + err.Error()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		status = "취소됨"
	}
	if result.NeedsVerification {
		status = "확인 필요"
		if err != nil {
			status += ": " + err.Error()
		}
	}
	return strings.Join(strings.Fields(safeText(fmt.Sprintf("%s · %s %s", status, r.Kind, r.Target.ResourceIdentity()))), " ")
}
func (m *Dashboard) refreshAfterWork() tea.Cmd {
	if m.pending {
		m.workRefreshWanted = true
		return nil
	}
	return m.refresh(time.Now(), false)
}
func (m *Dashboard) selectedSandbox() *operations.Sandbox {
	for _, s := range m.rows() {
		if sandboxID(s) == m.ProfileID {
			return &s
		}
	}
	return nil
}
func (m *Dashboard) actions() []action {
	a := []action{{label: "대상 업데이트", kind: operations.KindUpdate}, {label: "workspace 활성 조합 업데이트", kind: operations.KindUpdate, workspace: true}, {label: "shell / 로그인", shell: true}, {label: "대상 중지", kind: operations.KindStop}, {label: "guest 버전 확인", kind: operations.KindVersions}, {label: "Paseo 등록", kind: operations.KindRegister, workspace: true}, {label: "Paseo 해제", kind: operations.KindUnregister, workspace: true}, {label: "대상 삭제", kind: operations.KindRemove}}
	w, s := m.selectedWorkspace(), m.selectedSandbox()
	for i := range a {
		r := &a[i]
		switch {
		case m.Unmanaged:
			r.reason = "읽기 전용 · 미등록 sandbox"
		case m.shellPending || m.shellRunning:
			r.reason = "shell이 터미널을 사용 중"
		case w == nil:
			r.reason = "workspace 선택 필요"
		case w.Error != "":
			r.reason = "registry 확인 불가"
		case w.Missing && r.kind != operations.KindStop && r.kind != operations.KindRemove:
			r.reason = "workspace 경로 없음"
		case s == nil:
			r.reason = "sandbox 선택 필요"
		case s.Target.Entry.Sandbox == "" && s.Target.Entry.ContainerID == "":
			r.reason = "sandbox 대상 없음"
		case !r.workspace && s.RuntimeState == "missing" && r.kind != operations.KindRemove:
			r.reason = "sandbox 없음"
		case r.kind == operations.KindRemove && s.RuntimeState == "running":
			r.reason = "먼저 중지(x) 필요"
		case r.kind == operations.KindRemove && s.RuntimeState != "stopped" && s.RuntimeState != "missing":
			r.reason = "상태 확인 불가 · 삭제 불가"
		case r.shell && s.Target.Entry.EffectiveCategory() == config.CategoryContainer && s.RuntimeState != "running":
			r.reason = "container shell requires an explicitly started running container"
		case r.shell && m.busy():
			r.reason = "변경 작업과 대기열 완료 후 사용"
		}
		if r.reason != "" {
			continue
		}
		if r.kind == operations.KindRemove && s.RuntimeState == "missing" {
			r.label = "registry 항목만 정리"
		}
		if r.workspace {
			active := 0
			for _, s := range w.Sandboxes {
				if s.Active {
					active++
				}
			}
			if r.kind == operations.KindUpdate && active == 0 {
				r.reason = "완료된 활성 조합 없음"
			}
			if r.kind == operations.KindRegister && active == 0 {
				if _, err := m.restoreTargets(w); err != nil {
					r.reason = err.Error()
				}
			}
		}
	}
	return a
}
func (m *Dashboard) chooseAction(a action) tea.Cmd {
	if a.reason != "" {
		m.actionError = a.reason
		return nil
	}
	m.actionError = ""
	m.newFlow()
	m.actionMenu = false
	s, w := m.selectedSandbox(), m.selectedWorkspace()
	if s == nil || w == nil {
		return nil
	}
	if a.shell {
		target := s.Target
		b, ctx := m.Backend, m.ctx
		m.shellPending = true
		return func() tea.Msg { cmd, err := b.ShellCommand(ctx, target); return shellReadyMsg{cmd, err} }
	}
	r := operations.Request{Kind: a.kind, Target: s.Target, PaseoHome: "~/.paseo"}
	if a.workspace {
		r.Target = operations.Target{Workspace: w.Path, Runtime: w.Runtime}
		// A nonnil empty slice is an explicit inactive baseline, distinct from
		// omitted CLI targets that resolve activation at execution time.
		r.Targets = make([]operations.Target, 0)
		for _, s := range w.Sandboxes {
			if s.Active {
				r.Targets = append(r.Targets, s.Target)
			}
		}
	}
	if a.kind == operations.KindRegister {
		r.Apply = true
		if len(r.Targets) == 0 {
			targets, err := m.restoreTargets(w)
			if err != nil {
				m.actionError = err.Error()
				return nil
			}
			r.RestoreSelection = true
			r.Selection = *w.Selection
			r.RestoreTargets = targets
		}
	}
	warning := actionWarning(a.kind)
	if a.kind != operations.KindRemove && s.Target.Entry.EffectiveCategory() == config.CategoryContainer {
		warning = "Docker exec requires a running container; stopped containers are not automatically started."
	}
	m.confirm = &confirmation{request: freezeRequest(r), label: a.label, warning: warning}
	m.modal.GotoTop()
	return nil
}
func actionWarning(kind operations.Kind) string {
	switch kind {
	case operations.KindStop:
		return "중지는 실행 중인 세션에 영향을 줍니다."
	case operations.KindRemove:
		return "삭제 후 되돌릴 수 없습니다. 실패 시 provider가 이미 비활성화되었을 수 있습니다."
	case operations.KindUpdate:
		return "갱신: agent·methodology·utility(paseo-dock 관리 도구)\n제외: OS 패키지·Node·Python/uv·base image·omp/gjc·ACP adapter\n제외 항목은 이미지 재빌드 후 재생성으로만 바뀌며 재로그인이 필요합니다.\nsbx exec는 중지된 sandbox를 시작할 수 있습니다."
	case operations.KindVersions:
		return "sbx exec는 중지된 sandbox를 시작할 수 있습니다."
	case operations.KindRegister, operations.KindUnregister:
		return "workspace 활성 조합 및 Paseo home ~/.paseo/config.json 설정 변경 (backup / merge). 다른 workspace의 provider는 유지됩니다."
	}
	return ""
}
func (m *Dashboard) managementKey(k string, input ...string) (bool, tea.Cmd) {
	blocked := len(m.pruneBlocked) > 0
	typed := m.confirm != nil && m.confirm.requiredText() != ""
	quit := k == "ctrl+c" || k == "q" && !typed && !blocked
	if k == "esc" || quit {
		m.newFlow()
	}
	if m.exitPrompt {
		switch k {
		case "esc", "k":
			m.exitPrompt = false
		case "c", "enter":
			m.exitPrompt = false
			m.exitAfterJobs = true
			m.cancelJobs()
			if !m.busy() {
				m.cancel()
				return true, tea.Quit
			}
		}
		return true, nil
	}
	if quit {
		m.confirm = nil
		if m.busy() {
			m.exitPrompt = true
			m.modal.GotoTop()
			return true, nil
		}
		m.cancel()
		return true, tea.Quit
	}
	if blocked {
		switch k {
		case "down", "j":
			m.modal.ScrollDown(1)
		case "up", "k":
			m.modal.ScrollUp(1)
		case "pgdown":
			m.modal.ScrollDown(max(1, m.modal.Height()-1))
		case "pgup":
			m.modal.ScrollUp(max(1, m.modal.Height()-1))
		}
		return true, nil
	}
	if m.confirm != nil {
		c := m.confirm
		if required := c.requiredText(); required != "" {
			switch k {
			case "esc":
				m.confirm = nil
			case "backspace":
				r := []rune(c.typed)
				if len(r) > 0 {
					c.typed = string(r[:len(r)-1])
				}
			case "enter":
				if c.typed == required {
					r := c.request
					m.confirm = nil
					m.newFlow()
					return true, m.enqueue(r)
				}
			case "down":
				m.modal.ScrollDown(1)
			case "up":
				m.modal.ScrollUp(1)
			case "pgdown":
				m.modal.ScrollDown(max(1, m.modal.Height()-1))
			case "pgup":
				m.modal.ScrollUp(max(1, m.modal.Height()-1))
			default:
				if len(input) > 0 && input[0] != "" && strings.IndexFunc(input[0], unicode.IsControl) < 0 {
					c.typed += input[0]
				}
			}
			return true, nil
		}
		switch k {
		case "down", "j":
			m.modal.ScrollDown(1)
		case "up", "k":
			m.modal.ScrollUp(1)
		case "pgdown":
			m.modal.ScrollDown(max(1, m.modal.Height()-1))
		case "pgup":
			m.modal.ScrollUp(max(1, m.modal.Height()-1))
		case "esc":
			m.confirm = nil
		case "enter":
			r := m.confirm.request
			m.confirm = nil
			m.newFlow()
			return true, m.enqueue(r)
		}
		return true, nil
	}
	if m.actionMenu {
		switch k {
		case "esc":
			m.actionMenu = false
		case "up", "k":
			m.actionIndex = (m.actionIndex + len(m.actions()) - 1) % len(m.actions())
		case "down", "j":
			m.actionIndex = (m.actionIndex + 1) % len(m.actions())
		case "enter":
			return true, m.chooseAction(m.actions()[m.actionIndex])
		}
		return true, nil
	}
	if handled, cmd := m.advancedKey(k); handled {
		return true, cmd
	}
	switch k {
	case "enter":
		m.newFlow()
		m.actionMenu = true
		m.actionIndex = 0
		m.modal.GotoTop()
		return true, nil
	case "u":
		return true, m.chooseAction(m.actions()[0])
	case "s":
		return true, m.chooseAction(m.actions()[2])
	case "x", "d":
		kind := operations.KindStop
		if k == "d" {
			kind = operations.KindRemove
		}
		for _, a := range m.actions() {
			if a.kind == kind && !a.workspace {
				return true, m.chooseAction(a)
			}
		}
	case "c":
		m.cancelJobs()
		return true, nil
	case "R":
		for i := len(m.jobs) - 1; i >= 0; i-- {
			j := m.jobs[i]
			if j.state == "failed" || j.state == "needs verification" || j.state == "cancelled" {
				m.newFlow()
				m.confirm = &confirmation{request: freezeRequest(j.request), label: "고정된 실패 작업 재시도", warning: actionWarning(j.request.Kind)}
				m.modal.GotoTop()
				return true, nil
			}
		}
		m.actionError = "재시도할 실패 작업 없음"
		return true, nil
	}
	return false, nil
}
func (m *Dashboard) managementContent() string {
	if m.exitPrompt {
		return "작업 진행 중 · 종료 결정\n[k / Esc] 계속 실행하며 dashboard 유지\n[c / Enter] 취소 요청 후 완료까지 기다리고 종료"
	}
	if m.confirm != nil {
		c := m.confirm
		if c.request.Kind == operations.KindRemove {
			t := c.request.Target
			lines := []string{"확인: " + c.label, "Category: " + string(t.Entry.EffectiveCategory()), "Workspace: " + t.Workspace, "Profile: " + t.Profile}
			if t.Entry.EffectiveCategory() == config.CategoryContainer {
				lines = append(lines, "Container ID: "+t.Entry.ContainerID)
			} else {
				lines = append(lines, "Sandbox: "+t.Entry.Sandbox)
			}
			lines = append(lines, "Image: "+t.Entry.Image, "삭제되는 것: sandbox/container 및 로그인 상태·세션·guest 파일, sandbox 범위 secret(SBX)", "registry 항목, 업데이트·버전 기록, 해당 Paseo provider", "보존되는 것: workspace 폴더(bind mount 원본), image·template, 다른 profile", c.warning, "정확히 입력: "+t.Profile, "> "+c.typed, "Enter 실행(일치할 때만) · Esc 취소 · ↑↓ 스크롤")
			return strings.Join(lines, "\n")
		}
		if c.request.Kind == operations.KindPrune && c.request.RemoveRuntimes {
			lines := []string{"확인: " + c.label, "삭제되는 것: 남은 sandbox/container와 로그인 상태·세션·guest 파일, sandbox 범위 secret(SBX)", "registry 항목, 업데이트·버전 기록, 해당 Paseo provider", "보존되는 것: workspace 폴더(bind mount 원본), image·template, 다른 profile", "삭제 후 되돌릴 수 없습니다."}
			for _, t := range c.request.PruneTargets {
				lines = append(lines, t.Workspace+" / "+t.Profile+" / "+t.ResourceIdentity())
			}
			return strings.Join(append(lines, "정확히 입력: prune", "> "+c.typed, "Enter 실행(일치할 때만) · Esc 취소 · ↑↓ 스크롤"), "\n")
		}
		lines := []string{"확인: " + c.label, "Workspace: " + c.request.Target.Workspace, "Runtime: " + c.request.Target.Runtime, "Sandbox: " + c.request.Target.Entry.Sandbox, "Image: " + c.request.Target.Entry.Image, c.warning}
		lines = append(lines, runtimeSettings(c.request.Target.Entry)...)
		for _, t := range c.request.Targets {
			lines = append(lines, runtimeSettings(t.Entry)...)
			lines = append(lines, t.Profile+" → "+t.Entry.Sandbox+" / "+t.Entry.Image)
		}
		for _, t := range c.request.RestoreTargets {
			lines = append(lines, "복원: "+t.Profile+" → "+t.Entry.Sandbox+" / "+t.Entry.Image)
		}
		for _, t := range c.request.PruneTargets {
			lines = append(lines, "누락: "+t.Workspace+" / "+t.Runtime+" / "+t.Profile+" / "+t.Entry.Sandbox)
		}
		if c.request.Selection.Schema != 0 {
			lines = append(lines, "Profiles: "+strings.Join(c.request.Selection.Profiles(), ", "), "Utilities: "+strings.Join(c.request.Selection.Utilities, ", "))
		}
		lines = append(lines, "Enter 실행 · Esc 취소 · ↑↓/jk 스크롤")
		return strings.Join(lines, "\n")
	}
	if len(m.pruneBlocked) > 0 {
		lines := []string{"정리 + 남은 환경 삭제 불가 · 실행하지 않음", "먼저 중지(x)하거나 상태를 확인하세요."}
		for _, t := range m.pruneBlocked {
			lines = append(lines, t.Workspace+" / "+t.Profile+" / "+t.ResourceIdentity())
		}
		lines = append(lines, "Esc 닫기 · ↑↓/jk 스크롤")
		return strings.Join(lines, "\n")
	}
	if m.actionMenu {
		lines := []string{"작업 메뉴 · Enter 선택 · Esc 닫기"}
		for i, a := range m.actions() {
			mark := "  "
			if i == m.actionIndex {
				mark = "› "
			}
			line := mark + a.label
			if a.reason != "" {
				line += " [불가: " + a.reason + "]"
			}
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n")
	}
	if content := m.advancedContent(); content != "" {
		return content
	}
	return ""
}
func (m *Dashboard) logContent() string {
	if len(m.jobs) == 0 {
		return "조회 dashboard · 작업 없음"
	}
	lines := []string{"c 취소 요청 · R 고정 대상 재시도 · 최대 20 작업 / 작업별 256 KiB"}
	for _, j := range m.jobs {
		elapsed := j.finished.Sub(j.started)
		if j.finished.IsZero() {
			elapsed = time.Since(j.started)
		}
		if j.started.IsZero() {
			elapsed = 0
		}
		status := map[string]string{"queued": "대기", "running": "실행 중", "cancel requested": "취소 요청", "cancelled": "중단 확인 (호스트)", "needs verification": "확인 필요", "failed": "실패", "completed": "완료"}[j.state]
		if j.state == "running" {
			frames := []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")
			status = string(frames[int(elapsed/(100*time.Millisecond))%len(frames)]) + " " + status
		}
		lines = append(lines, fmt.Sprintf("#%d %s %s · %s · %s · %s", j.id, j.request.Kind, j.request.Target.ResourceIdentity(), status, j.stage, elapsed.Round(time.Second)))
		if j.errorText != "" {
			lines = append(lines, j.errorText, "대상/선택 보존 · R 키로 확인 후 재시도")
			if j.request.Kind == operations.KindRemove || j.request.Kind == operations.KindPrune && j.request.RemoveRuntimes && !j.request.DryRun {
				lines = append(lines, "provider가 이미 비활성화되었을 수 있습니다 · 남은 대상 확인 후 R 재시도")
			}
		}
		for _, t := range j.result.Remaining {
			lines = append(lines, "남은 대상: "+t.Profile+" / "+t.ResourceIdentity()+" · registry: "+filepath.Join(t.Runtime, "registry.json"))
		}
		for _, t := range j.result.Missing {
			lines = append(lines, "누락: "+t.Workspace+" / "+t.ResourceIdentity())
		}
		lines = append(lines, versionLines("확인 버전", j.result.Versions)...)
		lines = append(lines, versionLines("확인 오류", j.result.VersionErrors)...)
		if j.result.NeedsVerification {
			if j.request.Kind == operations.KindRemove || j.request.Kind == operations.KindPrune && j.request.RemoveRuntimes {
				lines = append(lines, "삭제 결과 확인 필요 · registry 항목 보존")
			} else {
				lines = append(lines, "guest 중단/부분 설치 확인 필요 · 상태 조회로 증명되지 않음")
			}
		}
		lines = append(lines, j.log.text())
		if info := j.log.fileInfo(); info != "" {
			lines = append(lines, info)
		}
	}
	// Version probes and resource identities also cross an external boundary.
	// Sanitize the complete rendered log, including structured result metadata.
	return safeText(strings.Join(lines, "\n"))
}

// jobLog is owned by the worker and read by the UI. Wakeups coalesce; writers
// never wait for the view or event receiver. Sanitizers retain parser state per
// stream so even split UTF-8, CSI, OSC and string controls stay safe.
type jobLog struct {
	mu         sync.Mutex
	data       []byte
	stage      string
	streams    map[string]*logSanitizer
	wake, done chan struct{}
	file       *os.File
	filePath   string
	fileErr    error
	fileSize   int
}

func newJobLog() *jobLog {
	return &jobLog{streams: map[string]*logSanitizer{}, wake: make(chan struct{}, 1), done: make(chan struct{})}
}
func (l *jobLog) emit(e operations.Event) {
	l.mu.Lock()
	stream := e.Stage
	if stream != "stdout" && stream != "stderr" {
		stream = "events"
	}
	s := l.streams[stream]
	if s == nil {
		s = &logSanitizer{}
		l.streams[stream] = s
	}
	clean := s.clean([]byte(e.Text))
	if e.Stage != "stdout" && e.Stage != "stderr" {
		l.stage = safeText(e.Stage)
	}
	l.appendLocked(clean)
	l.mu.Unlock()
}

// TUI records cannot inherit unfinished subprocess escape sequences or change
// the backend's current stage.
func (l *jobLog) record(text string) {
	l.mu.Lock()
	l.appendLocked([]byte(safeText(text)))
	l.mu.Unlock()
}

func (l *jobLog) appendLocked(clean []byte) {
	l.data = append(l.data, clean...)
	if len(l.data) > jobLogLimit {
		l.data = l.data[len(l.data)-jobLogLimit:]
		for len(l.data) > 0 && !utf8.RuneStart(l.data[0]) {
			l.data = l.data[1:]
		}
		l.data = slices.Clone(l.data)
	}
	l.writeFile(clean)
	select {
	case l.wake <- struct{}{}:
	default:
	}
}
func (l *jobLog) finish() {
	l.mu.Lock()
	if l.file != nil {
		if err := l.file.Close(); err != nil && l.fileErr == nil {
			l.fileErr = err
		}
		l.file = nil
	}
	l.mu.Unlock()
	close(l.done)
}
func (l *jobLog) text() string         { l.mu.Lock(); defer l.mu.Unlock(); return string(l.data) }
func (l *jobLog) currentStage() string { l.mu.Lock(); defer l.mu.Unlock(); return l.stage }
func safeText(s string) string         { p := logSanitizer{}; return string(p.clean([]byte(s))) }

type logSanitizer struct {
	state   int
	pending []byte
}

func (s *logSanitizer) clean(p []byte) []byte {
	p = append(s.pending, p...)
	s.pending = nil
	out := make([]byte, 0, len(p))
	for len(p) > 0 {
		if !utf8.FullRune(p) {
			s.pending = slices.Clone(p)
			break
		}
		r, n := utf8.DecodeRune(p)
		p = p[n:]
		switch s.state {
		case 1:
			switch r {
			case '[':
				s.state = 2
			case ']':
				s.state = 3
			case 'P', 'X', '^', '_':
				s.state = 4
			default:
				if r >= 0x20 && r <= 0x2f {
					s.state = 6
				} else {
					s.state = 0
				}
			}
			continue
		case 2:
			if r >= 0x40 && r <= 0x7e {
				s.state = 0
			}
			continue
		case 3, 4:
			if r == 0x1b {
				s.state = 5
			} else if r == 0x9c || r == 7 {
				s.state = 0
			}
			continue
		case 5:
			if r == '\\' {
				s.state = 0
			} else if r != 0x1b {
				s.state = 4
			}
			continue
		case 6:
			if r >= 0x30 && r <= 0x7e {
				s.state = 0
			}
			continue
		}
		switch r {
		case 0x1b:
			s.state = 1
			continue
		case 0x9b:
			s.state = 2
			continue
		case 0x9d:
			s.state = 3
			continue
		case 0x90, 0x98, 0x9e, 0x9f:
			s.state = 4
			continue
		}
		if (unicode.IsControl(r) && r != '\n' && r != '\t') || r == utf8.RuneError && n == 1 {
			continue
		}
		out = utf8.AppendRune(out, r)
	}
	return out
}

func portText(ports []config.PortMapping) string {
	if len(ports) == 0 {
		return "none"
	}
	var parts []string
	for _, p := range ports {
		parts = append(parts, fmt.Sprintf("%s:%d → %d", p.HostAddress, p.HostPort, p.ContainerPort))
	}
	return strings.Join(parts, ", ")
}
func runtimeSettings(e config.Entry) []string {
	network := e.Network
	if network == "" {
		if e.EffectiveCategory() == config.CategoryContainer {
			network = "bridge"
		} else {
			network = "SBX policy"
		}
	}
	return []string{"Category: " + string(e.EffectiveCategory()), "Container ID: " + e.ContainerID, "Mount: " + e.Workspace + " (read/write)", "Network: " + network, "Ports: " + portText(e.Ports)}
}
