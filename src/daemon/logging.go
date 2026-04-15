package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/AdriaanVE/jack-in/src/domain"
)

const logFile = ".jack-in/daemon.log"

// LoggingOptions configures the daemon logger.
type LoggingOptions struct {
	// Events channel for TUI log lines. If nil, logs go to console.
	Events chan<- any
}

// SetupLogging configures slog with handlers:
// - file (.jack-in/daemon.log): debug and above (always)
// - console (stderr): info and above (when Events is nil)
// - TUI tee: info and above as LogMsg (when Events is set)
func SetupLogging(base string, lopts LoggingOptions) (cleanup func(), err error) {
	logPath := filepath.Join(base, logFile)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}

	file := slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug})

	var handlers []slog.Handler
	handlers = append(handlers, file)

	if lopts.Events != nil {
		handlers = append(handlers, &teeHandler{events: lopts.Events})
	} else {
		console := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
		handlers = append(handlers, console)
	}

	slog.SetDefault(slog.New(&multiHandler{handlers: handlers}))

	return func() { f.Close() }, nil
}

// Log line styles -- uses the same Tokyo Night palette as the TUI (styles.go).
var (
	logTimestamp = lipgloss.NewStyle().Foreground(lipgloss.Color("#4d5578"))            // colorMuted
	logLevelInfo = lipgloss.NewStyle().Foreground(lipgloss.Color("#bb9af7")).Bold(true) // colorPurple
	logLevelWarn = lipgloss.NewStyle().Foreground(lipgloss.Color("#e0af68")).Bold(true) // colorYellow
	logLevelErr  = lipgloss.NewStyle().Foreground(lipgloss.Color("#f7768e")).Bold(true) // colorRed
	logMsgStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#d0d8f5"))            // colorFg
	logKeyStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#7dcfff"))            // colorCyan
	logValStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#6a7394"))            // colorSubtle
)

// teeHandler sends styled log lines as LogEvent to the events channel.
type teeHandler struct {
	events chan<- any
	attrs  []slog.Attr
}

func (t *teeHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelInfo
}

func (t *teeHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder

	// Timestamp
	b.WriteString(logTimestamp.Render(r.Time.Format("15:04:05")))
	b.WriteString(" ")

	// Level
	switch r.Level {
	case slog.LevelWarn:
		b.WriteString(logLevelWarn.Render("WARN"))
	case slog.LevelError:
		b.WriteString(logLevelErr.Render("ERRO"))
	default:
		b.WriteString(logLevelInfo.Render("INFO"))
	}
	b.WriteString(" ")

	// Message
	b.WriteString(logMsgStyle.Render(r.Message))

	// Attrs
	for _, a := range t.attrs {
		fmt.Fprintf(&b, " %s%s", logKeyStyle.Render(a.Key+"="), logValStyle.Render(fmt.Sprint(a.Value)))
	}
	r.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(&b, " %s%s", logKeyStyle.Render(a.Key+"="), logValStyle.Render(fmt.Sprint(a.Value)))
		return true
	})

	sendEvent(t.events, domain.LogEvent{Line: b.String()})
	return nil
}

func (t *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	combined := make([]slog.Attr, len(t.attrs)+len(attrs))
	copy(combined, t.attrs)
	copy(combined[len(t.attrs):], attrs)
	return &teeHandler{events: t.events, attrs: combined}
}

func (t *teeHandler) WithGroup(_ string) slog.Handler {
	return &teeHandler{events: t.events, attrs: t.attrs}
}

var _ slog.Handler = (*teeHandler)(nil)

// multiHandler fans out log records to multiple handlers.
type multiHandler struct {
	handlers []slog.Handler
}

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range m.handlers {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithAttrs(attrs)
	}
	return &multiHandler{handlers: handlers}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithGroup(name)
	}
	return &multiHandler{handlers: handlers}
}

var _ slog.Handler = (*multiHandler)(nil)
