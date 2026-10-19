package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// A viewport offset is a rendered row. Keep its source record (and approximate
// column within that record) when expansion or resize reflows the log.
func reflowLogOffset(source string, offset, oldWidth, newWidth int) int {
	if oldWidth <= 0 || oldWidth == newWidth {
		return offset
	}
	oldStart, newStart := 0, 0
	for _, line := range strings.Split(source, "\n") {
		oldRows := strings.Count(ansi.Hardwrap(line, oldWidth, true), "\n") + 1
		newRows := strings.Count(ansi.Hardwrap(line, max(1, newWidth), true), "\n") + 1
		if offset < oldStart+oldRows {
			column := (offset - oldStart) * oldWidth
			return newStart + min(newRows-1, column/max(1, newWidth))
		}
		oldStart += oldRows
		newStart += newRows
	}
	return newStart
}
