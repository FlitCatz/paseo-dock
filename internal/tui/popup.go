package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"paseo-dock/internal/config"
)

// selectionBody renders the create wizard's selection look: a bar per section,
// a pink cursor and ○/✓ marks. focus is the row that must stay visible.
type selectionBody struct {
	color bool
	lines []string
	focus int
}

func (b *selectionBody) paint(code, text string) string {
	if !b.color {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}
func (b *selectionBody) bar(active bool) string {
	if active {
		return b.paint(purple, "┃")
	}
	return b.paint(muted, "│")
}
func (b *selectionBody) heading(title, hint string, active bool) string {
	if len(b.lines) > 0 {
		b.lines = append(b.lines, "")
	}
	border := b.bar(active)
	if active {
		b.focus = len(b.lines)
	}
	b.lines = append(b.lines, border+"  "+b.paint(purple, title))
	if hint != "" {
		b.lines = append(b.lines, border+"  "+b.paint(muted, hint))
	}
	return border
}
func (b *selectionBody) section(title, hint string, items []config.Item, active bool, cursor int, selected, compatible func(config.Item) bool) {
	border := b.heading(title, hint, active)
	for index, item := range items {
		mark, pointer, color := "○", " ", ""
		if selected(item) {
			mark, color = "✓", green
		}
		label := item.Label
		if !compatible(item) {
			color = muted
			label += " · 지원 대상 아님"
		}
		if active && index == cursor {
			pointer = b.paint(pink, "›")
			b.focus = len(b.lines)
			if compatible(item) {
				color = green
			}
		}
		b.lines = append(b.lines, border+"  "+pointer+" "+b.paint(color, mark+" "+label))
		if active && index == cursor && item.Note != "" {
			b.lines = append(b.lines, border+"      "+b.paint(muted, item.Note))
		}
	}
}

// field shows free-form rows (an input or the directory picker) under a section heading.
func (b *selectionBody) field(title string, active bool, rows []string) {
	border := b.heading(title, "", active)
	for _, row := range rows {
		if active {
			b.focus = len(b.lines)
		}
		b.lines = append(b.lines, border+"  "+row)
	}
}

// clipAround keeps focus visible in available rows, with a little context after it.
func clipAround(lines []string, focus, available int) []string {
	if available <= 0 {
		return nil
	}
	if len(lines) <= available {
		return lines
	}
	start := min(max(0, focus-available+min(3, available)), len(lines)-available)
	return lines[start : start+available]
}

// popupSize leaves the dashboard visible around the box when there is room;
// tiny terminals give the whole area to the box.
func popupSize(width, height int) (int, int) {
	width, height = max(1, width), max(1, height)
	w, h := min(76, width-4), height-2
	if w < 24 {
		w = width
	}
	if h < 8 {
		h = height
	}
	return w, h
}

// selectionPopup frames a selection body as a box no larger than width×height.
// The footer (help and errors) survives clipping before body rows do.
func selectionPopup(title string, body selectionBody, footer []string, width, height int) string {
	w, maxHeight := popupSize(width, height)
	h := min(maxHeight, len(body.lines)+len(footer)+2)
	contentWidth, contentHeight, _, _ := sectionDimensions(w, h)
	if len(footer) >= contentHeight {
		footer = footer[len(footer)-min(len(footer), max(0, contentHeight-1)):]
	}
	lines := append(clipAround(body.lines, body.focus, contentHeight-len(footer)), footer...)
	popup := strings.Split(renderSection(title, strings.Join(lines, "\n"), w, h, true), "\n")
	// Unframed tiny boxes have a shorter title row; overlays need one width.
	for i, line := range popup {
		popup[i] = line + strings.Repeat(" ", max(0, max(w, contentWidth)-ansi.StringWidth(line)))
	}
	return strings.Join(popup, "\n")
}

// popupScreen centers a popup on an empty screen for the standalone wizard.
func popupScreen(popup string, width, height int) string {
	return overlayPopup(strings.TrimSuffix(strings.Repeat("\n", max(1, height)), "\n"), popup, width, height)
}
