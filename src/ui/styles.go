package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/lucasb-eyer/go-colorful"
)

// Color palette -- Tokyo Night inspired.
var (
	// Base
	colorFg     = lipgloss.Color("#d0d8f5")
	colorSubtle = lipgloss.Color("#6a7394")
	colorMuted  = lipgloss.Color("#4d5578")
	colorBorder = lipgloss.Color("#414868")
	colorDimBg  = lipgloss.Color("#16161e")

	// Accents
	colorPurple = lipgloss.Color("#bb9af7")
	colorCyan   = lipgloss.Color("#7dcfff")
	colorGreen  = lipgloss.Color("#9ece6a")
	colorYellow = lipgloss.Color("#e0af68")
	colorOrange = lipgloss.Color("#ff9e64")
	colorRed    = lipgloss.Color("#f7768e")
)

type styles struct {
	app        lipgloss.Style
	header     lipgloss.Style
	headerMeta lipgloss.Style
	headerKeys lipgloss.Style

	cardNormal lipgloss.Style
	cardOrch   lipgloss.Style
	cardSelect lipgloss.Style
	cardName   lipgloss.Style
	cardTask   lipgloss.Style

	logBox lipgloss.Style

	stateIdle    lipgloss.Style
	stateWork    lipgloss.Style
	stateBlocked lipgloss.Style
	stateStuck   lipgloss.Style
	stateRestart lipgloss.Style

	// Pre-allocated styles for render hot path
	approvalLabel lipgloss.Style
	approvalValue lipgloss.Style
	hotkeyKey     lipgloss.Style
	hotkeyRest    lipgloss.Style
	orchHotkey    lipgloss.Style
	agentBadge    lipgloss.Style
	logTitle      lipgloss.Style
	logCopyIcon   lipgloss.Style
	logCopied     lipgloss.Style

	taskBarPrefix lipgloss.Style
	taskBarSeg    lipgloss.Style
}

func newStyles() styles {
	return styles{
		app: lipgloss.NewStyle().
			Padding(1, 3),

		header: lipgloss.NewStyle().
			Padding(0, 1),

		headerMeta: lipgloss.NewStyle().
			Foreground(colorSubtle),

		headerKeys: lipgloss.NewStyle().
			Foreground(colorMuted),

		cardNormal: lipgloss.NewStyle().
			Foreground(colorFg).
			Border(lipgloss.RoundedBorder()).
			Padding(0, 1),

		cardOrch: lipgloss.NewStyle().
			Foreground(colorFg).
			Border(lipgloss.RoundedBorder()).
			Padding(0, 1),

		cardSelect: lipgloss.NewStyle().
			Foreground(colorFg).
			Border(lipgloss.RoundedBorder()).
			Padding(0, 1),

		cardName: lipgloss.NewStyle().
			Bold(true).
			Foreground(colorFg),

		cardTask: lipgloss.NewStyle().
			Foreground(colorSubtle).
			Italic(true),

		logBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Foreground(colorSubtle).
			Padding(0, 1),

		stateIdle: lipgloss.NewStyle().
			Foreground(colorMuted),

		stateWork: lipgloss.NewStyle().
			Foreground(colorGreen).
			Bold(true),

		stateBlocked: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#e5c07b")).
			Bold(true),

		stateStuck: lipgloss.NewStyle().
			Foreground(colorRed).
			Bold(true),

		stateRestart: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#d6acff")).
			Bold(true),

		approvalLabel: lipgloss.NewStyle().
			Foreground(colorMuted),
		approvalValue: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#a9b1f6")).
			Bold(true),
		hotkeyKey: lipgloss.NewStyle().
			Foreground(colorCyan).
			Bold(true).
			Underline(true),
		hotkeyRest: lipgloss.NewStyle().
			Foreground(colorMuted),
		orchHotkey: lipgloss.NewStyle().
			Bold(true).
			Foreground(colorFg).
			Underline(true),
		agentBadge: lipgloss.NewStyle().
			Foreground(colorSubtle),
		logTitle: lipgloss.NewStyle().
			Foreground(colorPurple).
			Bold(true),
		logCopyIcon: lipgloss.NewStyle().
			Foreground(colorMuted),
		logCopied: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff79c6")).
			Bold(true),

		taskBarPrefix: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#e0e4f0")).
			Background(lipgloss.Color("#3b2e6e")).
			Bold(true).
			Padding(0, 1),
		taskBarSeg: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#c0caf5")).
			Background(lipgloss.Color("#24283b")).
			Padding(0, 1),
	}
}

func colorToHex(c colorful.Color) string {
	return fmt.Sprintf("#%02x%02x%02x", int(c.R*255), int(c.G*255), int(c.B*255))
}

// gradientText renders text with a color gradient between two hex colors.
func gradientText(text, colorA, colorB string) string {
	runes := []rune(text)
	if len(runes) == 0 {
		return ""
	}
	cA, _ := colorful.Hex(colorA)
	cB, _ := colorful.Hex(colorB)

	var b strings.Builder
	for i, r := range runes {
		t := float64(i) / float64(max(len(runes)-1, 1))
		c := cA.BlendLuv(cB, t)
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(colorToHex(c))).Bold(true)
		b.WriteString(style.Render(string(r)))
	}
	return b.String()
}
