package tui

import (
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Semantic tokens shared by the dashboard, wizard and overlays. The terminal
// renderer handles color-depth conversion; text and glyphs also carry meaning.
const (
	canvas           = "#101724"
	surface          = "#172233"
	selectionSurface = "#263B50"
	ink              = "#E2EAF4"
	secondary        = "#9AAEC4"
	rule             = "#40536B"
	accent           = "#79DCD2"
	violet           = "#B6ABF5"
	success          = "#9CD6A2"
	warning          = "#EBC07B"
	danger           = "#F18F9A"

	purple   = "38;2;182;171;245;1"
	green    = "38;2;156;214;162"
	pink     = "38;2;121;220;210;1"
	muted    = "38;2;154;174;196"
	errorRed = "38;2;241;143;154"
)

func colorsEnabled() bool { return os.Getenv("NO_COLOR") == "" }

func tone(color, text string) string {
	if !colorsEnabled() {
		return text
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(text)
}

func fillLine(text string, width int, bg string) string {
	text = ansi.Truncate(text, max(1, width), "…")
	text += strings.Repeat(" ", max(0, width-ansi.StringWidth(text)))
	if !colorsEnabled() {
		return ansi.Strip(text)
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(ink)).Background(lipgloss.Color(bg)).Render(text)
}

func stateTone(state string) string {
	switch state {
	case "running", "connected", "completed", "ready":
		return success
	case "error", "failed", "missing":
		return danger
	case "stopped", "idle", "cancelled":
		return secondary
	default:
		return warning
	}
}

func stateMark(state string) string {
	switch state {
	case "running", "connected", "completed", "ready":
		return "●"
	case "stopped", "idle", "cancelled":
		return "○"
	case "error", "failed", "missing":
		return "✕"
	default:
		return "?"
	}
}

func sectionLabel(label string) string { return tone(violet, label) }
