// Package ui holds the interactive picker and table rendering.
package ui

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// ErrNoTTY is returned when an interactive prompt is requested without a terminal.
var ErrNoTTY = errors.New("stdin is not a terminal; pass the context name explicitly")

var (
	styleKey    = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	styleMuted  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleHeader = lipgloss.NewStyle().Bold(true)
	styleWarn   = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleOK     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
)

// IsTTY reports whether stdin is a terminal.
func IsTTY() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// Pick shows a filterable select of names; current is preselected.
func Pick(names []string, current string) (string, error) {
	if !IsTTY() {
		return "", ErrNoTTY
	}
	opts := make([]huh.Option[string], 0, len(names))
	for _, n := range names {
		label := n
		if n == current {
			label = n + " (current)"
		}
		opts = append(opts, huh.NewOption(label, n))
	}
	choice := current
	sel := huh.NewSelect[string]().Title("Switch context").Options(opts...).Filtering(true).Value(&choice)
	if err := huh.NewForm(huh.NewGroup(sel)).Run(); err != nil {
		return "", err
	}
	return choice, nil
}

// Table renders rows with aligned columns; the first row is the header.
func Table(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	widths := make([]int, len(rows[0]))
	for _, r := range rows {
		for i, c := range r {
			if i < len(widths) && lipgloss.Width(c) > widths[i] {
				widths[i] = lipgloss.Width(c)
			}
		}
	}
	var b strings.Builder
	for ri, r := range rows {
		for i, c := range r {
			pad := strings.Repeat(" ", widths[i]-lipgloss.Width(c))
			if ri == 0 {
				c = styleHeader.Render(c)
			}
			b.WriteString(c)
			if i < len(r)-1 {
				b.WriteString(pad + "  ")
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// KV renders an aligned key/value block.
func KV(pairs [][2]string) string {
	w := 0
	for _, p := range pairs {
		if len(p[0]) > w {
			w = len(p[0])
		}
	}
	var b strings.Builder
	for _, p := range pairs {
		fmt.Fprintf(&b, "%s%s  %s\n", styleKey.Render(p[0]), strings.Repeat(" ", w-len(p[0])), p[1])
	}
	return b.String()
}

// Muted, Warn, OK style helpers.
func Muted(s string) string { return styleMuted.Render(s) }
func Warn(s string) string  { return styleWarn.Render(s) }
func OK(s string) string    { return styleOK.Render(s) }

var styleFail = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))

// Fail styles a failure marker.
func Fail(s string) string { return styleFail.Render(s) }
