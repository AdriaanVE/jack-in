package tmux

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const cmdTimeout = 10 * time.Second

func run(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func HasSession(name string) bool {
	_, err := run("has-session", "-t", name)
	return err == nil
}

func CreateSession(name string) error {
	_, err := run("new-session", "-d", "-s", name)
	return err
}

func KillSession(name string) error {
	_, err := run("kill-session", "-t", name)
	return err
}

func CreateWindow(session, name string) error {
	_, err := run("new-window", "-t", session, "-n", name)
	return err
}

func CreateWindowDetached(session, name string) error {
	_, err := run("new-window", "-d", "-t", session, "-n", name)
	return err
}

func KillWindow(session, name string) error {
	_, err := run("kill-window", "-t", fmt.Sprintf("%s:%s", session, name))
	return err
}

// KillPane kills a specific pane by its ID (e.g. "%5").
func KillPane(paneID string) error {
	_, err := run("kill-pane", "-t", paneID)
	return err
}

func RenameWindow(session string, index int, name string) error {
	target := fmt.Sprintf("%s:%d", session, index)
	_, err := run("rename-window", "-t", target, name)
	return err
}

func SelectWindow(session, name string) error {
	target := fmt.Sprintf("%s:%s", session, name)
	_, err := run("select-window", "-t", target)
	return err
}

func SplitWindow(session, targetWindow string, percentage int) error {
	args := []string{"split-window", "-t", fmt.Sprintf("%s:%s", session, targetWindow), "-v"}
	if percentage > 0 {
		args = append(args, "-p", strconv.Itoa(percentage))
	}
	_, err := run(args...)
	return err
}

// SendKeys sends text to a tmux pane. Text is prefixed with a space to
// keep commands out of shell history. Text and Enter are separate calls
// for TUI app compatibility.
func SendKeys(target, text string, enter bool) error {
	if text != "" {
		if _, err := run("send-keys", "-t", target, " "+text); err != nil {
			return err
		}
	}
	if enter {
		if _, err := run("send-keys", "-t", target, "Enter"); err != nil {
			return err
		}
	}
	return nil
}

// SendKeysDoubleEnter sends text followed by two Enter presses with a short
// delay between them. Claude Code requires a second Enter to submit a prompt.
func SendKeysDoubleEnter(target, text string) error {
	if text != "" {
		if _, err := run("send-keys", "-t", target, " "+text); err != nil {
			return err
		}
	}
	if _, err := run("send-keys", "-t", target, "Enter"); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	_, err := run("send-keys", "-t", target, "Enter")
	return err
}

func CapturePane(target string, lines int) (string, error) {
	return run("capture-pane", "-t", target, "-p", "-S", fmt.Sprintf("-%d", lines))
}

type WindowInfo struct {
	Name   string
	Index  int
	Active bool
}

func ListWindows(session string) ([]WindowInfo, error) {
	out, err := run("list-windows", "-t", session, "-F", "#{window_index}\t#{window_name}\t#{window_active}")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	var windows []WindowInfo
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		idx, _ := strconv.Atoi(parts[0])
		windows = append(windows, WindowInfo{
			Index:  idx,
			Name:   parts[1],
			Active: parts[2] == "1",
		})
	}
	return windows, nil
}

type PaneInfo struct {
	WindowName     string
	PaneIndex      int
	PaneDead       bool
	CurrentCommand string
}

func ListPanes(session string) ([]PaneInfo, error) {
	out, err := run("list-panes", "-t", session, "-a", "-F", "#{window_name}\t#{pane_index}\t#{pane_dead}\t#{pane_current_command}")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	var panes []PaneInfo
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 4 {
			continue
		}
		idx, _ := strconv.Atoi(parts[1])
		panes = append(panes, PaneInfo{
			WindowName:     parts[0],
			PaneIndex:      idx,
			PaneDead:       parts[2] == "1",
			CurrentCommand: parts[3],
		})
	}
	return panes, nil
}

func DisplayMessage(session, message string) error {
	_, err := run("display-message", "-t", session, message)
	return err
}

func SwitchClient(targetSession string) error {
	_, err := run("switch-client", "-t", targetSession)
	return err
}

func SessionCreated(name string) (int64, error) {
	out, err := run("display-message", "-t", name, "-p", "#{session_created}")
	if err != nil {
		return 0, err
	}
	epoch, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing session_created: %w", err)
	}
	return epoch, nil
}

// SetPaneOption sets a user option on a tmux pane (e.g. @jackin_role).
func SetPaneOption(target, key, value string) error {
	_, err := run("set-option", "-p", "-t", target, key, value)
	return err
}

// FindPaneByRole finds a pane with a matching @jackin_role user option.
// Only matches panes in the specified session to avoid cross-session collisions.
func FindPaneByRole(session, role string) (string, error) {
	out, err := run("list-panes", "-t", session, "-a", "-F", "#{session_name}\t#{pane_id}\t#{@jackin_role}")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) == 3 && parts[0] == session && parts[2] == role {
			return parts[1], nil
		}
	}
	return "", nil
}

// SwapPane swaps two panes by their IDs.
func SwapPane(src, dst string) error {
	_, err := run("swap-pane", "-s", src, "-t", dst)
	return err
}

// PopupOpen opens a tmux popup overlay running the given shell command.
// It blocks until the popup closes (command exits or Escape is pressed).
func PopupOpen(session, shellCmd string, widthPct, heightPct int) error {
	_, err := run("display-popup", "-t", session,
		"-E", "-w", fmt.Sprintf("%d%%", widthPct), "-h", fmt.Sprintf("%d%%", heightPct),
		shellCmd)
	return err
}

// PaneTarget resolves a @jackin_role to a stable pane_id target.
// Falls back to session:role (window-based) if the role is not found.
func PaneTarget(session, role string) string {
	paneID, err := FindPaneByRole(session, role)
	if err != nil || paneID == "" {
		return session + ":" + role
	}
	return paneID
}
