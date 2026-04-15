package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/AdriaanVE/jack-in/src/domain"
	"github.com/AdriaanVE/jack-in/src/quotes"
	"github.com/AdriaanVE/jack-in/src/taskqueue"
	"github.com/AdriaanVE/jack-in/src/tmux"
	"github.com/lucasb-eyer/go-colorful"
)

const (
	cardsPerRow = 3
	cardInner   = 34                      // inner content width (excluding border+padding)
	cardOuter   = 35                      // rendered card width: border(2) + margin(1) + padding is inside Width
	cardGridW   = cardsPerRow * cardOuter // total display width of one card row
	maxCards    = 6
	logLines    = 10

	remoteWinTTYD  = "remote-ttyd"
	remoteWinNgrok = "remote-ngrok"
)

// Model is the single Bubble Tea model for the jackin dashboard.
type Model struct {
	width    int
	height   int
	styles   styles
	selected int
	quitting bool

	session    string
	base       string // project root path
	approval   domain.ApprovalMode
	cards      []cardData
	tasks      domain.TaskCounts
	tokens     domain.TokenUsage
	logs       []string
	bottomRole string // role currently shown in the bottom pane

	events      <-chan any
	logoCache   string
	copyFlash   bool
	hasLazygit  bool
	hasRemote   bool
	popupOpen   bool
	hotkeys     []hotkeyDef
	hotkeyFlash string // action key currently flashing ("a", "c", etc.)

	remoteURL   string // ngrok public URL (non-empty = remote active)
	remoteFlash bool   // true while flashing remote URL in header

	taskPopupCategory domain.TaskState      // "" = closed
	taskPopupEntries  []taskqueue.TaskEntry // loaded entries for popup
	confirmDown       bool                  // true while showing down confirmation
	confirmDownQuote  string                // cached quote for down confirmation

	matrixEffect *MatrixEffect // Matrix animation instance (nil = inactive)
}

type cardData struct {
	name     string
	isOrch   bool
	state    string // one of domain.LabelIdle, domain.LabelWorking, domain.LabelStuck
	agent    domain.AgentType
	task     string
	tokens   domain.TokenUsage // session token usage
	spawnCmd string            // full shell command to (re)start the agent
}

// NewModel creates a dashboard model wired to a daemon events channel.
// If orchAgent is non-empty, an orchestrator card is prepended.
func NewModel(session, base string, approval domain.ApprovalMode, orchAgent domain.AgentType, orchSpawnCmd string, workers []WorkerInit, events <-chan any) Model {
	var cards []cardData

	// Orchestrator card first (if configured)
	if orchAgent != "" {
		cards = append(cards, cardData{
			name:     "orchestrator",
			isOrch:   true,
			agent:    orchAgent,
			state:    domain.LabelWorking,
			spawnCmd: orchSpawnCmd,
		})
	}

	for _, w := range workers {
		cards = append(cards, cardData{
			name:     w.Name,
			agent:    w.Agent,
			state:    domain.LabelIdle,
			spawnCmd: w.SpawnCmd,
		})
	}

	lazygit := hasCommand("lazygit")
	remote := hasCommand("ttyd") && hasCommand("ngrok")

	hotkeys := []hotkeyDef{
		{"a", "dd", "a"},
		{"c", "li", "c"},
	}
	if lazygit {
		hotkeys = append(hotkeys, hotkeyDef{"g", "it", "g"})
	}
	if remote {
		hotkeys = append(hotkeys, hotkeyDef{"r", "emote", "r"})
	}
	hotkeys = append(hotkeys, hotkeyDef{"x", "reset", "x"})
	hotkeys = append(hotkeys, hotkeyDef{"d", "own", "d"})

	return Model{
		styles:     newStyles(),
		session:    session,
		base:       base,
		approval:   approval,
		cards:      cards,
		bottomRole: "orchestrator",
		events:     events,
		logoCache:  gradientText("JACK-IN", "#bb9af7", "#7dcfff"),
		hasLazygit: lazygit,
		hasRemote:  remote,
		hotkeys:    hotkeys,
	}
}

// WorkerInit is the initial worker info needed to create cards.
type WorkerInit struct {
	Name     string
	Agent    domain.AgentType
	SpawnCmd string // full shell command to (re)start the agent
}

// waitForEvent returns a Cmd that blocks until the next daemon event.
func waitForEvent(ch <-chan any) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return tea.Quit()
		}
		return msg
	}
}

func (m Model) Init() tea.Cmd {
	return waitForEvent(m.events)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.matrixEffect != nil {
			m.matrixEffect.Resize(msg.Width, msg.Height)
		}
		return m, nil

	case swapDoneMsg:
		m.bottomRole = msg.role
		return m, nil

	case tea.MouseClickMsg:
		if m.confirmDown {
			m.confirmDown = false
			return m, nil
		}
		// Close task popup on any click outside the task bar
		if cat := m.taskBarSegmentAtPosition(msg.X, msg.Y); cat != "" {
			if cat == m.taskPopupCategory {
				// Toggle off
				m.taskPopupCategory = ""
				m.taskPopupEntries = nil
				return m, nil
			}
			return m, m.loadTaskPopupCmd(cat)
		}
		if m.taskPopupCategory != "" {
			m.taskPopupCategory = ""
			m.taskPopupEntries = nil
			// Fall through so the click still reaches cards/hotkeys/etc.
		}
		if m.isCopyIconClick(msg.X, msg.Y) {
			return m, m.copyLogsCmd()
		}
		if action := m.hotkeyAtPosition(msg.X, msg.Y); action != "" {
			// Clicking active remote button stops it
			if action == "r" && m.remoteURL != "" {
				return m.dispatchHotkey("R")
			}
			return m.dispatchHotkey(action)
		}
		if idx, ok := m.cardAtPosition(msg.X, msg.Y); ok {
			m.selected = idx
			return m, m.activateCardCmd(idx)
		}
		return m, nil

	case tea.KeyPressMsg:
		// Handle confirmDown first (has matrix backdrop)
		if m.confirmDown {
			switch msg.String() {
			case "y", "Y":
				m.quitting = true
				return m, tea.Sequence(downCmd(m.session), tea.Quit)
			default:
				m.confirmDown = false
				m.matrixEffect = nil
				return m, waitForEvent(m.events)
			}
		}
		// Any key exits standalone matrix mode (easter egg)
		if m.matrixEffect != nil {
			m.matrixEffect = nil
			return m, waitForEvent(m.events)
		}
		if m.taskPopupCategory != "" && (msg.String() == "escape" || msg.String() == "esc") {
			m.taskPopupCategory = ""
			m.taskPopupEntries = nil
			return m, nil
		}
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "a", "c", "d", "g", "m", "r", "x":
			return m.dispatchHotkey(msg.String())
		case "R", "shift+r":
			return m.dispatchHotkey("R")
		case "left", "h":
			if m.selected > 0 {
				m.selected--
			}
		case "right", "l":
			if m.selected < len(m.cards)-1 {
				m.selected++
			}
		case "up", "k":
			if m.selected >= cardsPerRow {
				m.selected -= cardsPerRow
			}
		case "down", "j":
			if m.selected+cardsPerRow < len(m.cards) {
				m.selected += cardsPerRow
			}
		case "o":
			// Select the orchestrator card (first card if it exists)
			for i, c := range m.cards {
				if c.isOrch {
					m.selected = i
					break
				}
			}
		case "y":
			return m, m.copyLogsCmd()
		case "enter":
			return m, m.activateCardCmd(m.selected)
		}
		return m, nil
	}

	switch msg := msg.(type) {
	case matrixTickMsg:
		if m.matrixEffect != nil {
			m.matrixEffect.Update()
			return m, matrixTickCmd()
		}
		return m, nil
	case taskPopupMsg:
		m.taskPopupCategory = msg.category
		m.taskPopupEntries = msg.entries
		m.tasks = msg.counts // sync taskbar with fresh counts
		return m, nil
	case popupClosedMsg:
		m.popupOpen = false
		return m, nil
	case copyDoneMsg:
		m.copyFlash = true
		return m, flashTimer()
	case copyFlashDoneMsg:
		m.copyFlash = false
		return m, nil
	case hotkeyFlashDoneMsg:
		m.hotkeyFlash = ""
		return m, nil
	case remoteReadyMsg:
		m.remoteURL = msg.url
		m.remoteFlash = true
		return m, tea.Batch(copyToClipboard(msg.url), remoteFlashTimer())
	case remoteFlashDoneMsg:
		m.remoteFlash = false
		return m, nil
	case remoteStoppedMsg:
		m.remoteURL = ""
		return m, nil
	case resetDoneMsg:
		if msg.bottomRole != "" {
			m.bottomRole = msg.bottomRole
		}
		m.selected = msg.selected
		for i := range m.cards {
			if m.cards[i].name == msg.name {
				m.cards[i].state = domain.LabelWorking
				m.cards[i].task = ""
				break
			}
		}
		return m, nil
	}

	// Daemon events
	switch msg := msg.(type) {
	case domain.StateEvent:
		m.approval = msg.Approval
		m.tasks = msg.Tasks
		m.tokens = msg.Tokens
		// Update orchestrator card
		if msg.Orchestrator != nil {
			for i := range m.cards {
				if m.cards[i].isOrch {
					m.cards[i].state = msg.Orchestrator.State
					m.cards[i].tokens = msg.Orchestrator.Tokens
					break
				}
			}
		}
		// Update worker cards
		for i := range m.cards {
			if m.cards[i].isOrch {
				continue
			}
			for _, w := range msg.Workers {
				if w.Name == m.cards[i].name {
					m.cards[i].state = w.State
					m.cards[i].task = w.CurrentTaskSummary
					m.cards[i].tokens = w.Tokens
					break
				}
			}
		}
		return m, waitForEvent(m.events)

	case domain.LogEvent:
		m.logs = append(m.logs, msg.Line)
		if len(m.logs) > 50 {
			trimmed := make([]string, 50)
			copy(trimmed, m.logs[len(m.logs)-50:])
			m.logs = trimmed
		}
		return m, waitForEvent(m.events)
	}

	return m, nil
}

// downCmd kills the tmux session (equivalent to `jackin down`).
func downCmd(session string) tea.Cmd {
	return func() tea.Msg {
		_ = tmux.KillSession(session)
		return nil
	}
}

// swapDoneMsg is sent after a pane swap completes so the model can update bottomRole.
type swapDoneMsg struct {
	role string
}

type popupClosedMsg struct{}
type taskPopupMsg struct {
	category domain.TaskState
	entries  []taskqueue.TaskEntry
	counts   domain.TaskCounts // fresh counts to sync taskbar
}
type copyDoneMsg struct{}
type copyFlashDoneMsg struct{}
type hotkeyFlashDoneMsg struct{}
type remoteReadyMsg struct{ url string }
type remoteFlashDoneMsg struct{}
type remoteStoppedMsg struct{}
type resetDoneMsg struct {
	name       string // worker/orchestrator that was reset
	bottomRole string // new bottom pane role after reset
	selected   int    // card index to highlight
}

func copyToClipboard(text string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run()
		return copyDoneMsg{}
	}
}

func flashTimer() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(2 * time.Second)
		return copyFlashDoneMsg{}
	}
}

func hotkeyFlashTimer() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(150 * time.Millisecond)
		return hotkeyFlashDoneMsg{}
	}
}

func remoteFlashTimer() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(2 * time.Second)
		return remoteFlashDoneMsg{}
	}
}

// activateCardCmd returns a Cmd that swaps the bottom pane of the
// dashboard-orchestrator window to show the selected card's pane.
// Uses a two-step swap: restore current -> swap in target (like Deno).
func (m Model) activateCardCmd(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.cards) {
		return nil
	}
	c := m.cards[idx]
	targetRole := c.name
	if c.isOrch {
		targetRole = "orchestrator"
	}
	if targetRole == m.bottomRole {
		return nil
	}
	session := m.session
	currentRole := m.bottomRole
	return func() tea.Msg {
		orchPane, _ := tmux.FindPaneByRole(session, "orchestrator")
		if orchPane == "" {
			return nil
		}

		// Step 1: If the bottom pane isn't the orchestrator, restore it
		if currentRole != "orchestrator" {
			currentPane, _ := tmux.FindPaneByRole(session, currentRole)
			if currentPane != "" {
				_ = tmux.SwapPane(currentPane, orchPane)
			}
		}

		// Step 2: If the target isn't the orchestrator, swap it in
		if targetRole != "orchestrator" {
			targetPane, _ := tmux.FindPaneByRole(session, targetRole)
			if targetPane != "" {
				_ = tmux.SwapPane(targetPane, orchPane)
			}
		}

		return swapDoneMsg{role: targetRole}
	}
}

// swapToRole returns a Cmd that swaps the given role into the bottom pane.
func (m Model) swapToRole(targetRole string) tea.Cmd {
	if targetRole == m.bottomRole {
		return nil
	}
	session := m.session
	currentRole := m.bottomRole
	return func() tea.Msg {
		orchPane, _ := tmux.FindPaneByRole(session, "orchestrator")
		if orchPane == "" {
			return nil
		}
		if currentRole != "orchestrator" {
			curPane, _ := tmux.FindPaneByRole(session, currentRole)
			if curPane != "" {
				_ = tmux.SwapPane(curPane, orchPane)
			}
		}
		if targetRole != "orchestrator" {
			targetPane, _ := tmux.FindPaneByRole(session, targetRole)
			if targetPane != "" {
				_ = tmux.SwapPane(targetPane, orchPane)
			}
		}
		return swapDoneMsg{role: targetRole}
	}
}

// resetWorkerCmd kills a worker/orchestrator and respawns it with the original command.
func (m *Model) resetWorkerCmd(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.cards) {
		return nil
	}
	c := m.cards[idx]
	if c.spawnCmd == "" {
		return nil
	}

	// Show "restarting" immediately on the card
	m.cards[idx].state = domain.LabelRestarting
	m.cards[idx].task = ""

	session := m.session
	name := c.name
	spawnCmd := c.spawnCmd
	currentRole := m.bottomRole

	if c.isOrch {
		// Swap selection to the cover worker card
		coverIdx := m.cardIndexByName(m.orchResetCoverWorker())
		if coverIdx >= 0 {
			m.selected = coverIdx
		}
		return m.resetOrchCmd(session, spawnCmd, currentRole, idx)
	}
	// Swap selection to orchestrator card
	orchIdx := m.cardIndexByName("orchestrator")
	if orchIdx >= 0 {
		m.selected = orchIdx
	}
	return m.resetCardCmd(session, name, spawnCmd, currentRole, idx)
}

func (m Model) cardIndexByName(name string) int {
	for i, c := range m.cards {
		if c.name == name {
			return i
		}
	}
	return -1
}

// orchResetCoverWorker returns the name of the first worker to use as cover during orchestrator reset.
func (m Model) orchResetCoverWorker() string {
	for _, c := range m.cards {
		if !c.isOrch && c.spawnCmd != "" {
			return c.name
		}
	}
	return ""
}

// resetCardCmd kills a worker's tmux window and respawns it.
func (m Model) resetCardCmd(session, name, spawnCmd, currentRole string, originalIdx int) tea.Cmd {
	return func() tea.Msg {
		// Swap orchestrator into bottom pane as cover while we reset
		orchPane, _ := tmux.FindPaneByRole(session, "orchestrator")
		if currentRole == name && orchPane != "" {
			curPane, _ := tmux.FindPaneByRole(session, name)
			if curPane != "" {
				_ = tmux.SwapPane(curPane, orchPane)
			}
		} else if currentRole != "orchestrator" && orchPane != "" {
			// Restore current non-orchestrator pane, then we're on orchestrator
			curPane, _ := tmux.FindPaneByRole(session, currentRole)
			if curPane != "" {
				_ = tmux.SwapPane(curPane, orchPane)
			}
		}

		// Kill and recreate the worker window (detached so it doesn't steal focus)
		_ = tmux.KillWindow(session, name)
		if err := tmux.CreateWindowDetached(session, name); err != nil {
			return nil
		}
		target := session + ":" + name
		_ = tmux.SetPaneOption(target+".0", "@jackin_role", name)
		_ = tmux.SendKeys(target, spawnCmd, true)

		// Give the agent a moment to start, then swap back
		time.Sleep(2 * time.Second)

		orchPane, _ = tmux.FindPaneByRole(session, "orchestrator")
		newWorkerPane, _ := tmux.FindPaneByRole(session, name)
		if orchPane != "" && newWorkerPane != "" {
			_ = tmux.SwapPane(newWorkerPane, orchPane)
		}

		return resetDoneMsg{name: name, bottomRole: name, selected: originalIdx}
	}
}

// resetOrchCmd swaps a worker into the bottom pane, kills the orchestrator
// pane via the worker window, re-splits, and respawns both.
func (m Model) resetOrchCmd(session, orchSpawnCmd, currentRole string, originalIdx int) tea.Cmd {
	// Pick a worker to use as swap cover
	var worker *cardData
	for i := range m.cards {
		if !m.cards[i].isOrch && m.cards[i].spawnCmd != "" {
			worker = &m.cards[i]
			break
		}
	}
	if worker == nil {
		return nil
	}

	workerName := worker.name
	workerSpawnCmd := worker.spawnCmd

	return func() tea.Msg {
		orchPane, _ := tmux.FindPaneByRole(session, "orchestrator")
		if orchPane == "" {
			return nil
		}

		// 1. Swap worker into dashboard bottom (orchestrator moves to worker's window)
		if currentRole != workerName {
			// Restore current bottom pane first
			if currentRole != "orchestrator" {
				curPane, _ := tmux.FindPaneByRole(session, currentRole)
				if curPane != "" {
					_ = tmux.SwapPane(curPane, orchPane)
				}
				orchPane, _ = tmux.FindPaneByRole(session, "orchestrator")
			}
			workerPane, _ := tmux.FindPaneByRole(session, workerName)
			if workerPane != "" {
				_ = tmux.SwapPane(workerPane, orchPane)
			}
		}

		// 2. Kill the worker window (orchestrator pane dies with it)
		_ = tmux.KillWindow(session, workerName)

		// 3. Kill the stale worker pane left in the dashboard bottom
		stalePane, _ := tmux.FindPaneByRole(session, workerName)
		if stalePane != "" {
			_ = tmux.KillPane(stalePane)
		}

		// 4. Re-split dashboard for a fresh orchestrator pane
		_ = tmux.SplitWindow(session, "dashboard-orchestrator", 70)
		orchTarget := session + ":dashboard-orchestrator.1"
		_ = tmux.SetPaneOption(orchTarget, "@jackin_role", "orchestrator")
		_ = tmux.SendKeys(orchTarget, orchSpawnCmd, true)

		// 5. Recreate worker window (detached so it doesn't steal focus)
		_ = tmux.CreateWindowDetached(session, workerName)
		workerTarget := session + ":" + workerName
		_ = tmux.SetPaneOption(workerTarget+".0", "@jackin_role", workerName)
		_ = tmux.SendKeys(workerTarget, workerSpawnCmd, true)

		// 6. Show worker cover for 2s, then swap orchestrator back into bottom
		time.Sleep(2 * time.Second)

		newOrchPane, _ := tmux.FindPaneByRole(session, "orchestrator")
		newWorkerPane, _ := tmux.FindPaneByRole(session, workerName)
		if newOrchPane != "" && newWorkerPane != "" {
			_ = tmux.SwapPane(newWorkerPane, newOrchPane)
		}

		return resetDoneMsg{name: "orchestrator", bottomRole: "orchestrator", selected: originalIdx}
	}
}

// cardAtPosition returns the card index at the given terminal coordinates.
func (m Model) cardAtPosition(x, y int) (int, bool) {
	// Cards start after header (1 line + 1 padding top from app style)
	// App padding: 1 top, 3 left
	cardStartY := 2
	cardStartX := 3

	// Each card row: border (2) + 3 content lines + MarginBottom(1) = 6 lines
	cardRowHeight := 6
	cardWidth := cardOuter

	relX := x - cardStartX
	relY := y - cardStartY

	if relX < 0 || relY < 0 {
		return 0, false
	}

	col := relX / cardWidth
	row := relY / cardRowHeight

	if col >= cardsPerRow {
		return 0, false
	}

	idx := row*cardsPerRow + col
	if idx >= len(m.cards) {
		return 0, false
	}

	return idx, true
}

// copyLogsCmd copies the visible log lines to the system clipboard.
func (m Model) copyLogsCmd() tea.Cmd {
	lines := m.logs
	if len(lines) > logLines {
		lines = lines[len(lines)-logLines:]
	}
	return copyToClipboard(strings.Join(lines, "\n"))
}

// dispatchHotkey handles hotkey actions from both keyboard and mouse clicks.
func (m Model) dispatchHotkey(action string) (tea.Model, tea.Cmd) {
	// Flash the hotkey
	m.hotkeyFlash = action

	switch action {
	case "a":
		// Add task — send command to orchestrator pane
		session := m.session
		cmd := func() tea.Msg {
			target := tmux.PaneTarget(session, "orchestrator")
			_ = tmux.SendKeys(target, "jackin tasks add", true)
			return nil
		}
		return m, tea.Batch(cmd, hotkeyFlashTimer())
	case "c":
		// CLI — create cli pane if needed, then swap it into the bottom pane
		if m.bottomRole == "cli" {
			// Already showing CLI — swap orchestrator back
			return m, tea.Batch(m.swapToRole("orchestrator"), hotkeyFlashTimer())
		}
		session := m.session
		base := m.base
		currentRole := m.bottomRole
		cmd := func() tea.Msg {
			// Create CLI window if it doesn't exist
			cliPane, _ := tmux.FindPaneByRole(session, "cli")
			if cliPane == "" {
				_ = tmux.CreateWindowDetached(session, "cli")
				target := session + ":cli.0"
				_ = tmux.SetPaneOption(target, "@jackin_role", "cli")
				_ = tmux.SendKeys(target, "cd "+base, true)
			}

			// Two-step swap: restore current, swap in cli
			orchPane, _ := tmux.FindPaneByRole(session, "orchestrator")
			if orchPane == "" {
				return nil
			}
			if currentRole != "orchestrator" {
				curPane, _ := tmux.FindPaneByRole(session, currentRole)
				if curPane != "" {
					_ = tmux.SwapPane(curPane, orchPane)
				}
			}
			cliPane, _ = tmux.FindPaneByRole(session, "cli")
			if cliPane != "" {
				_ = tmux.SwapPane(cliPane, orchPane)
			}
			return swapDoneMsg{role: "cli"}
		}
		return m, tea.Batch(cmd, hotkeyFlashTimer())
	case "g":
		if !m.hasLazygit || m.popupOpen {
			m.hotkeyFlash = ""
			return m, nil
		}
		m.popupOpen = true
		session := m.session
		base := m.base
		cmd := func() tea.Msg {
			_ = tmux.PopupOpen(session, "cd "+base+" && lazygit", 80, 80)
			return popupClosedMsg{}
		}
		return m, tea.Batch(cmd, hotkeyFlashTimer())
	case "r":
		if !m.hasRemote {
			m.hotkeyFlash = ""
			return m, nil
		}
		if m.remoteURL != "" {
			// Already active — re-flash URL + copy
			m.remoteFlash = true
			return m, tea.Batch(copyToClipboard(m.remoteURL), hotkeyFlashTimer(), remoteFlashTimer())
		}
		// Start remote
		cmd := m.startRemoteCmd()
		return m, tea.Batch(cmd, hotkeyFlashTimer())
	case "R":
		if m.remoteURL == "" {
			return m, nil
		}
		m.hotkeyFlash = "r"
		return m, tea.Batch(m.stopRemoteCmd(), hotkeyFlashTimer())
	case "m":
		// Toggle Matrix rain effect
		if m.matrixEffect != nil {
			m.matrixEffect = nil
			return m, tea.Batch(waitForEvent(m.events), hotkeyFlashTimer())
		}
		m.matrixEffect = NewMatrixEffect(m.width, m.height)
		return m, tea.Batch(matrixTickCmd(), hotkeyFlashTimer())
	case "x":
		cmd := m.resetWorkerCmd(m.selected)
		if cmd == nil {
			m.hotkeyFlash = ""
			return m, nil
		}
		return m, tea.Batch(cmd, hotkeyFlashTimer())
	case "d":
		m.confirmDown = true
		m.confirmDownQuote = quotes.Get(quotes.Down)
		m.matrixEffect = NewMatrixEffect(m.width, m.height)
		return m, matrixTickCmd()
	}
	m.hotkeyFlash = ""
	return m, nil
}

// startRemoteCmd launches ttyd + ngrok in tmux windows and polls for the public URL.
func (m *Model) startRemoteCmd() tea.Cmd {
	session := m.session
	return func() tea.Msg {
		if err := tmux.CreateWindowDetached(session, remoteWinTTYD); err != nil {
			return nil
		}
		ttydCmd := fmt.Sprintf("ttyd -W -p 7681 env TERMINFO=/usr/share/terminfo TERM=xterm-256color tmux attach -t %s", session)
		if err := tmux.SendKeys(session+":"+remoteWinTTYD, ttydCmd, true); err != nil {
			killRemoteWindows(session)
			return nil
		}

		if err := tmux.CreateWindowDetached(session, remoteWinNgrok); err != nil {
			killRemoteWindows(session)
			return nil
		}
		if err := tmux.SendKeys(session+":"+remoteWinNgrok, "ngrok http 7681", true); err != nil {
			killRemoteWindows(session)
			return nil
		}

		// Poll ngrok API for tunnel URL (up to 10 seconds)
		var url string
		for i := 0; i < 20; i++ {
			time.Sleep(500 * time.Millisecond)
			url = fetchNgrokURL()
			if url != "" {
				break
			}
		}
		if url == "" {
			killRemoteWindows(session)
			return nil
		}

		return remoteReadyMsg{url: url}
	}
}

// stopRemoteCmd kills remote tmux windows asynchronously.
func (m *Model) stopRemoteCmd() tea.Cmd {
	session := m.session
	return func() tea.Msg {
		killRemoteWindows(session)
		return remoteStoppedMsg{}
	}
}

func killRemoteWindows(session string) {
	_ = tmux.KillWindow(session, remoteWinNgrok)
	_ = tmux.KillWindow(session, remoteWinTTYD)
}

// fetchNgrokURL queries the ngrok local API for the first tunnel's public URL.
func fetchNgrokURL() string {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://localhost:4040/api/tunnels")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var result struct {
		Tunnels []struct {
			PublicURL string `json:"public_url"`
		} `json:"tunnels"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ""
	}
	if len(result.Tunnels) > 0 {
		return result.Tunnels[0].PublicURL
	}
	return ""
}

// taskBarCategories maps segment index to task state.
var taskBarCategories = []domain.TaskState{
	domain.StatePending,
	domain.StateCurrent,
	domain.StateReview,
	domain.StateComplete,
	domain.StateRejected,
}

// taskBarSegmentAtPosition checks if a click is on the task bar and returns the category.
func (m Model) taskBarSegmentAtPosition(x, y int) domain.TaskState {
	// Task bar Y = app padding top (1) + header (1) + card rows * 6 (5 card + 1 margin)
	cardRows := (len(m.cards) + cardsPerRow - 1) / cardsPerRow
	taskBarY := 1 + 1 + cardRows*6
	if y != taskBarY {
		return ""
	}

	// Prefix width
	prefix := m.styles.taskBarPrefix.Render("= Tasks")
	prefixW := lipgloss.Width(prefix)

	cardsW := cardGridW
	gradW := cardsW - prefixW
	if gradW < 40 {
		gradW = 40
	}

	segStartX := 3 + prefixW // app left padding + prefix
	relX := x - segStartX
	if relX < 0 {
		return ""
	}

	segW := gradW / len(taskBarCategories)
	idx := relX / segW
	if idx < 0 || idx >= len(taskBarCategories) {
		return ""
	}
	return taskBarCategories[idx]
}

// loadTaskPopupCmd loads tasks for the given category.
// Also refreshes task counts to keep taskbar in sync with filesystem.
func (m Model) loadTaskPopupCmd(category domain.TaskState) tea.Cmd {
	base := m.base
	return func() tea.Msg {
		entries, err := taskqueue.List(base, category)
		if err != nil {
			entries = nil
		}
		slices.SortFunc(entries, func(a, b taskqueue.TaskEntry) int {
			return b.Task.TransitionedAt.Compare(a.Task.TransitionedAt)
		})
		// Refresh counts to keep taskbar in sync
		counts, _ := taskqueue.Counts(base)
		return taskPopupMsg{category: category, entries: entries, counts: counts}
	}
}

// isCopyIconClick returns true if the click is on the copy icon in the log title line.
func (m Model) isCopyIconClick(x, y int) bool {
	// Log title Y = app padding top (1) + header (1) + card rows * 6 (5 card + 1 margin) + task bar (1)
	cardRows := (len(m.cards) + cardsPerRow - 1) / cardsPerRow
	cardLines := cardRows * 6
	logTitleY := 1 + 1 + cardLines + 1

	// Accept a small range to be forgiving
	if y < logTitleY || y > logTitleY+1 {
		return false
	}

	// Copy icon is right-aligned within contentWidth, starting at X=3 (app left padding)
	logW := m.contentWidth()
	copyW := 7 // display width of "<icon> copy"
	iconStart := 3 + logW - copyW
	return x >= iconStart
}

func (m Model) View() tea.View {
	if m.quitting {
		v := tea.NewView("Shutting down...\n")
		v.AltScreen = true
		v.MouseMode = tea.MouseModeCellMotion
		return v
	}
	if m.width == 0 {
		v := tea.NewView("Loading...")
		v.AltScreen = true
		v.MouseMode = tea.MouseModeCellMotion
		return v
	}

	// Matrix rain mode - full screen effect
	if m.matrixEffect != nil {
		matrixContent := m.matrixEffect.Render()
		lines := strings.Split(matrixContent, "\n")

		if m.confirmDown {
			// Exit confirmation overlay on matrix rain
			style := lipgloss.NewStyle().Foreground(lipgloss.Color("#00d26a")).Bold(true)
			quoteLine := style.Render("\"" + m.confirmDownQuote + "\"")
			yKey := style.Underline(true).Render("y")
			nKey := style.Underline(true).Render("n")
			exitLine := style.Render("Exit the Matrix? ") + yKey + style.Render("/") + nKey

			// Center both lines vertically
			centerY := len(lines) / 2
			if centerY > 1 && centerY < len(lines)-1 {
				lines[centerY-1] = lipgloss.PlaceHorizontal(m.width, lipgloss.Center, quoteLine)
				lines[centerY+1] = lipgloss.PlaceHorizontal(m.width, lipgloss.Center, exitLine)
			}
		} else {
			// Easter egg mode - just show exit hint
			hint := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#d8f3dc")).
				Bold(true).
				Render("Press any key to exit the Matrix...")
			if len(lines) > 2 {
				hintLine := lipgloss.PlaceHorizontal(m.width, lipgloss.Center, hint)
				lines[len(lines)-2] = hintLine
			}
		}

		placed := lipgloss.Place(m.width, m.height, lipgloss.Left, lipgloss.Top, strings.Join(lines, "\n"))
		v := tea.NewView(placed)
		v.AltScreen = true
		v.MouseMode = tea.MouseModeCellMotion
		return v
	}

	var sections []string
	sections = append(sections, m.renderHeader())
	sections = append(sections, m.renderCards())
	sections = append(sections, m.renderTaskBar())
	sections = append(sections, "")
	if m.taskPopupCategory != "" {
		sections = append(sections, m.renderTaskPopup())
	} else {
		sections = append(sections, m.renderLogs())
	}

	content := lipgloss.JoinVertical(lipgloss.Left, sections...)
	placed := lipgloss.Place(
		m.width, m.height,
		lipgloss.Left, lipgloss.Top,
		m.styles.app.Render(content),
	)

	v := tea.NewView(placed)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// --- Header ---

// hotkeyDef describes a clickable hotkey pill in the header.
type hotkeyDef struct {
	key    string // trigger letter
	rest   string // remaining text
	action string // key name for dispatch
}

func (m Model) renderHeader() string {
	session := m.styles.headerMeta.Render(m.session)

	var middle string
	if m.confirmDown {
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("#00d26a")).Bold(true)
		middle = style.Render("\"" + m.confirmDownQuote + "\"")
	} else if m.popupOpen {
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("#d6acff")).Bold(true)
		qKey := style.Underline(true).Render("q")
		middle = style.Render("lazygit (") + qKey + style.Render(" to close)")
	} else if m.remoteFlash && m.remoteURL != "" {
		middle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#d6acff")).
			Bold(true).
			Render(m.remoteURL + "  (R to stop)")
	} else {
		approvalLabel := m.styles.approvalLabel.Render("approval: ")
		approvalValue := m.styles.approvalValue.Render(string(m.approval))
		middle = approvalLabel + approvalValue
		// Always show token usage (even if 0)
		tokLabel := m.styles.approvalLabel.Render("  tokens: ")
		tokValue := m.styles.approvalValue.Render(formatTokens(m.tokens.InputTokens + m.tokens.OutputTokens))
		middle += tokLabel + tokValue
		if m.tokens.CostUSD > 0 {
			costLabel := m.styles.approvalLabel.Render(" ($")
			costValue := m.styles.approvalValue.Render(formatCost(m.tokens.CostUSD))
			middle += costLabel + costValue + m.styles.approvalLabel.Render(")")
		}
	}

	// Render hotkey bar (like task bar, gradient reversed: blue -> purple)
	keys := m.renderHotkeyBar()

	lineW := m.width - 6
	if lineW < 60 {
		lineW = 60
	}

	if m.confirmDown {
		// Two-line header: quote on first line, Exit? y/n on second
		left := m.logoCache + "  " + session
		leftW := lipgloss.Width(left)
		keysW := lipgloss.Width(keys)
		middleW := lipgloss.Width(middle)
		totalUsed := leftW + middleW + keysW
		space := lineW - totalUsed
		leftGap := space / 2
		rightGap := space - leftGap
		if leftGap < 2 {
			leftGap = 2
		}
		if rightGap < 2 {
			rightGap = 2
		}
		line1 := left + strings.Repeat(" ", leftGap) + middle + strings.Repeat(" ", rightGap) + keys

		// Build centered second line with Exit? y/n
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("#00d26a")).Bold(true)
		yKey := style.Underline(true).Render("y")
		nKey := style.Underline(true).Render("n")
		line2Content := style.Render("Exit? ") + yKey + style.Render("/") + nKey
		line2W := lipgloss.Width(line2Content)
		line2Pad := (lineW - line2W) / 2
		if line2Pad < 0 {
			line2Pad = 0
		}
		line2 := strings.Repeat(" ", line2Pad) + line2Content

		return line1 + "\n" + line2
	}

	if m.popupOpen {
		// Center the hint between left info and hotkey bar
		left := m.logoCache + "  " + session
		leftW := lipgloss.Width(left)
		keysW := lipgloss.Width(keys)
		middleW := lipgloss.Width(middle)
		totalUsed := leftW + middleW + keysW
		space := lineW - totalUsed
		leftGap := space / 2
		rightGap := space - leftGap
		if leftGap < 2 {
			leftGap = 2
		}
		if rightGap < 2 {
			rightGap = 2
		}
		return left + strings.Repeat(" ", leftGap) + middle + strings.Repeat(" ", rightGap) + keys
	}

	left := m.logoCache + "  " + session + "  " + middle
	leftW := lipgloss.Width(left)
	keysW := lipgloss.Width(keys)
	gap := ""
	space := lineW - leftW - keysW
	if space > 0 {
		gap = strings.Repeat(" ", space)
	}

	return left + gap + keys
}

func (m Model) renderHotkeyBar() string {
	// Build segments with padding, track underline positions and highlight ranges
	var segments []string
	var underlinePos []int
	highlightSet := make(map[int]bool)
	pos := 0

	for _, hk := range m.hotkeys {
		seg := " " + hk.key + hk.rest + " "
		segments = append(segments, seg)
		underlinePos = append(underlinePos, pos+1) // +1 to skip leading space

		segLen := len([]rune(seg))
		highlight := hk.action == m.hotkeyFlash ||
			(hk.action == "c" && m.bottomRole == "cli") ||
			(hk.action == "r" && m.remoteURL != "") ||
			(hk.action == "g" && m.popupOpen)
		if highlight {
			for j := pos; j < pos+segLen; j++ {
				highlightSet[j] = true
			}
		}
		pos += segLen
	}

	text := strings.Join(segments, "")
	return hotkeyGradientBar(text, underlinePos, highlightSet)
}

// hotkeyGradientBar renders text with a blue->purple gradient and underlines at specified positions.
// Characters in highlightSet are rendered in flashy purple.
func hotkeyGradientBar(text string, underlinePos []int, highlightSet map[int]bool) string {
	runes := []rune(text)

	// Reversed gradient: blue -> purple
	bgA, _ := colorful.Hex("#1a6daa")
	bgB, _ := colorful.Hex("#6c3dbb")
	fgA, _ := colorful.Hex("#e0e4f0")
	fgB, _ := colorful.Hex("#c0caf5")
	flashColor, _ := colorful.Hex("#d6acff")

	// Build set of underline positions for O(1) lookup
	underlineSet := make(map[int]bool)
	for _, p := range underlinePos {
		underlineSet[p] = true
	}

	var b strings.Builder
	n := max(len(runes)-1, 1)
	for i, r := range runes {
		t := float64(i) / float64(n)
		bg := bgA.BlendLuv(bgB, t)
		var fg colorful.Color
		if highlightSet[i] {
			fg = flashColor
		} else {
			fg = fgA.BlendLuv(fgB, t)
		}
		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorToHex(fg))).
			Background(lipgloss.Color(colorToHex(bg))).
			Bold(true)
		if underlineSet[i] {
			style = style.Underline(true)
		}
		b.WriteString(style.Render(string(r)))
	}
	return b.String()
}

// hotkeyAtPosition checks if a click is on the hotkey bar.
// Returns the action key ("a", "c", "d", "g", "r") or empty string.
func (m Model) hotkeyAtPosition(x, y int) string {
	// Header is at Y=1 (app top padding)
	if y != 1 {
		return ""
	}

	// Build segments to compute widths (same logic as renderHotkeyBar)
	var segments []string
	for _, hk := range m.hotkeys {
		segments = append(segments, " "+hk.key+hk.rest+" ")
	}
	keys := strings.Join(segments, "")
	keysW := lipgloss.Width(keys)

	lineW := m.width - 6
	if lineW < 60 {
		lineW = 60
	}
	keysStartX := 3 + lineW - keysW // app left padding + line width - keys width

	if x < keysStartX {
		return ""
	}

	// Walk through segments to find which one was clicked
	pos := keysStartX
	for i, seg := range segments {
		sw := lipgloss.Width(seg)
		if x >= pos && x < pos+sw {
			return m.hotkeys[i].action
		}
		pos += sw
	}
	return ""
}

// --- Cards ---

func (m Model) renderCards() string {
	var rows []string
	for i := 0; i < len(m.cards); i += cardsPerRow {
		end := min(i+cardsPerRow, len(m.cards))
		var row []string
		for j := i; j < end; j++ {
			row = append(row, m.renderCard(j))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, row...))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (m Model) renderCard(idx int) string {
	c := m.cards[idx]
	selected := idx == m.selected
	// Only show flashy purple when selected AND CLI is not active
	highlight := selected && m.bottomRole != "cli"

	icon := styledIcon(c.state, m.styles)

	// Flashy purple for highlighted card text
	highlightColor := lipgloss.Color("#d6acff")
	var name string
	if c.isOrch {
		if highlight {
			o := lipgloss.NewStyle().Foreground(highlightColor).Bold(true).Underline(true).Render("o")
			rest := lipgloss.NewStyle().Foreground(highlightColor).Bold(true).Render(c.name[1:])
			name = o + rest
		} else {
			o := m.styles.orchHotkey.Render("o")
			rest := m.styles.cardName.Render(c.name[1:])
			name = o + rest
		}
	} else {
		if highlight {
			name = lipgloss.NewStyle().Foreground(highlightColor).Bold(true).Render(c.name)
		} else {
			name = m.styles.cardName.Render(c.name)
		}
	}

	var agentBadge string
	if highlight {
		agentBadge = lipgloss.NewStyle().Foreground(highlightColor).Render(string(c.agent))
	} else {
		agentBadge = m.styles.agentBadge.Render(string(c.agent))
	}

	line1 := icon + "  " + name + " " + agentBadge

	var line2 string
	if c.task != "" {
		if highlight {
			line2 = lipgloss.NewStyle().Foreground(highlightColor).Render(truncate(c.task, cardInner-2))
		} else {
			line2 = m.styles.cardTask.Render(truncate(c.task, cardInner-2))
		}
	} else {
		if highlight {
			line2 = lipgloss.NewStyle().Foreground(highlightColor).Render(stateLabelText(c.state))
		} else {
			line2 = stateLabel(c.state, m.styles)
		}
	}

	// Line 3: token usage + cost (always show, even if 0)
	totalTok := c.tokens.InputTokens + c.tokens.OutputTokens
	tokStr := fmt.Sprintf("\u26A1 %s", formatTokens(totalTok))
	if c.tokens.CostUSD > 0 {
		tokStr += fmt.Sprintf(" ($%s)", formatCost(c.tokens.CostUSD))
	}
	var line3 string
	if highlight {
		line3 = lipgloss.NewStyle().Foreground(highlightColor).Render(tokStr)
	} else {
		line3 = lipgloss.NewStyle().Foreground(colorMuted).Render(tokStr)
	}

	// Gradient border color based on column position
	col := idx % cardsPerRow
	t := float64(col) / float64(max(cardsPerRow-1, 1))
	borderColor := lipgloss.Color(cardBorderColor(t, highlight))

	style := m.styles.cardNormal.
		BorderForeground(borderColor).
		Width(cardInner).Height(3).MarginRight(1).MarginBottom(1)

	content := line1 + "\n" + line2 + "\n" + line3
	return style.Render(content)
}

// cardBorderColor samples the purple-to-blue gradient for border color.
// Selected cards get a brighter border.
func cardBorderColor(t float64, selected bool) string {
	gradA, _ := colorful.Hex("#6c3dbb")
	gradB, _ := colorful.Hex("#1a6daa")
	c := gradA.BlendLuv(gradB, t)
	if selected {
		white, _ := colorful.Hex("#e0e4f0")
		c = c.BlendLuv(white, 0.4)
	}
	return colorToHex(c)
}

func styledIcon(state string, s styles) string {
	switch state {
	case domain.LabelIdle:
		return s.stateIdle.Render("\u25CB")
	case domain.LabelWorking:
		return s.stateWork.Render("\u25CF")
	case domain.LabelBlocked:
		return s.stateBlocked.Render("\u25D0")
	case domain.LabelStuck:
		return s.stateStuck.Render("\u25A0")
	case domain.LabelRestarting:
		return s.stateRestart.Render("\u21BB")
	default:
		return "?"
	}
}

func stateLabel(state string, s styles) string {
	switch state {
	case domain.LabelIdle:
		return s.stateIdle.Render("waiting for task...")
	case domain.LabelWorking:
		return s.stateWork.Render("active")
	case domain.LabelBlocked:
		return s.stateBlocked.Render("awaiting review")
	case domain.LabelStuck:
		return s.stateStuck.Render("needs attention!")
	case domain.LabelRestarting:
		return s.stateRestart.Render("restarting...")
	default:
		return state
	}
}

func stateLabelText(state string) string {
	switch state {
	case domain.LabelIdle:
		return "waiting for task..."
	case domain.LabelWorking:
		return "active"
	case domain.LabelBlocked:
		return "awaiting review"
	case domain.LabelStuck:
		return "needs attention!"
	case domain.LabelRestarting:
		return "restarting..."
	default:
		return state
	}
}

// --- Task bar ---

func (m Model) renderTaskBar() string {
	prefix := m.styles.taskBarPrefix.Render("= Tasks")
	prefixW := lipgloss.Width(prefix)

	// Width(cardInner) is the total rendered card width (including border + padding)
	// Grid = 3 cards + 2 inter-card margins
	cardsW := cardGridW
	gradW := cardsW - prefixW
	if gradW < 40 {
		gradW = 40
	}

	segments := []string{
		fmt.Sprintf(" \u00b7 pending %d", m.tasks.Pending),
		fmt.Sprintf(" \u25b8 current %d", m.tasks.Current),
		fmt.Sprintf(" \u25c6 review %d", m.tasks.Review),
		fmt.Sprintf(" \u2713 done %d", m.tasks.Complete),
		fmt.Sprintf(" \u2717 rejected %d", m.tasks.Rejected),
	}

	// Pad each segment to equal width within the gradient
	segW := gradW / len(segments)
	highlightSet := make(map[int]bool)
	pos := 0
	var bar strings.Builder
	for i, seg := range segments {
		w := segW
		if i == len(segments)-1 {
			w = gradW - segW*(len(segments)-1)
		}
		padded := seg
		if lipgloss.Width(seg) < w {
			padded = seg + strings.Repeat(" ", w-lipgloss.Width(seg))
		}
		// Mark characters for highlight if this segment's category is open
		if m.taskPopupCategory != "" && i < len(taskBarCategories) && taskBarCategories[i] == m.taskPopupCategory {
			for j := 0; j < len([]rune(padded)); j++ {
				highlightSet[pos+j] = true
			}
		}
		bar.WriteString(padded)
		pos += len([]rune(padded))
	}

	return prefix + gradientBar(bar.String(), "#6c3dbb", "#1a6daa", gradW, highlightSet)
}

// --- Task Popup ---

func (m Model) renderTaskPopup() string {
	logW := m.contentWidth()

	// Title
	categoryLabel := string(m.taskPopupCategory)
	title := m.styles.logTitle.Render(" " + strings.ToUpper(categoryLabel[:1]) + categoryLabel[1:] + " Tasks ")
	countLabel := lipgloss.NewStyle().Foreground(colorSubtle).Render(fmt.Sprintf(" (%d)", len(m.taskPopupEntries)))
	escHint := lipgloss.NewStyle().Foreground(colorMuted).Render("esc to close")
	titleW := lipgloss.Width(title) + lipgloss.Width(countLabel) + lipgloss.Width(escHint)
	gap := max(logW-titleW, 0)
	titleLine := title + countLabel + strings.Repeat(" ", gap) + escHint

	// Content lines
	var lines []string
	if len(m.taskPopupEntries) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorMuted).Italic(true).Render("No tasks"))
	} else {
		for _, e := range m.taskPopupEntries {
			line := m.renderTaskPopupEntry(e, logW-4) // -4 for box padding/border
			lines = append(lines, line)
		}
	}

	// Pad to fill log area height
	for len(lines) < logLines {
		lines = append(lines, "")
	}
	// Trim if too many
	if len(lines) > logLines {
		lines = lines[:logLines]
	}

	content := strings.Join(lines, "\n")
	box := m.styles.logBox.Width(logW).Render(content)
	return titleLine + "\n" + box
}

func (m Model) renderTaskPopupEntry(e taskqueue.TaskEntry, maxW int) string {
	bullet := lipgloss.NewStyle().Foreground(colorPurple).Bold(true).Render("\u2022 ")

	// Timestamp suffix for review/done/rejected
	var ts string
	if !e.Task.TransitionedAt.IsZero() {
		ts = lipgloss.NewStyle().Foreground(colorMuted).Render(" " + e.Task.TransitionedAt.Format("15:04:05"))
	}
	tsW := lipgloss.Width(ts)

	summary := m.styles.cardName.Render(truncate(e.Task.Summary, maxW-4-tsW))

	if m.taskPopupCategory == domain.StateRejected && e.Task.Feedback != "" {
		reason := lipgloss.NewStyle().Foreground(colorRed).Render(" -- " + truncate(e.Task.Feedback, maxW-lipgloss.Width(summary)-8-tsW))
		return bullet + summary + reason + ts
	}

	if e.Task.Assignee != "" {
		assignee := lipgloss.NewStyle().Foreground(colorSubtle).Render(" [" + e.Task.Assignee + "]")
		return bullet + summary + assignee + ts
	}

	return bullet + summary + ts
}

// --- Log ---

func (m Model) renderLogs() string {
	lines := m.logs
	if len(lines) > logLines {
		lines = lines[len(lines)-logLines:]
	}

	// Reverse: newest on top
	var rendered []string
	for i := len(lines) - 1; i >= 0; i-- {
		rendered = append(rendered, lines[i])
	}
	for len(rendered) < logLines {
		rendered = append(rendered, "")
	}

	content := strings.Join(rendered, "\n")
	logW := m.contentWidth()

	// Title line: "Log" left, copy icon right
	title := m.styles.logTitle.Render(" Log ")
	var copyIcon string
	if m.copyFlash {
		copyIcon = m.styles.logCopied.Render("Copied!")
	} else {
		copyIcon = m.styles.logCopyIcon.Render("\u2398 copy")
	}
	var remoteLabel string
	if m.remoteURL != "" {
		remoteLabel = "  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color("#d6acff")).
			Bold(true).
			Render(m.remoteURL) + "  "
	}
	rightSide := remoteLabel + copyIcon
	titleW := lipgloss.Width(title) + lipgloss.Width(rightSide)
	gap := max(logW-titleW, 0)
	titleLine := title + strings.Repeat(" ", gap) + rightSide

	// Standard bordered box for log content
	box := m.styles.logBox.Width(logW).Render(content)
	return titleLine + "\n" + box
}

// --- Helpers ---

// gradientBar renders text with a smooth background gradient.
// Width is clamped to exactly the target display columns.
// Characters in highlightSet are rendered with a brighter foreground.
func gradientBar(text string, bgHexA, bgHexB string, width int, highlightSet map[int]bool) string {
	// Pad or trim to exact display width
	dw := lipgloss.Width(text)
	if dw < width {
		text += strings.Repeat(" ", width-dw)
	}
	runes := []rune(text)

	bgA, _ := colorful.Hex(bgHexA)
	bgB, _ := colorful.Hex(bgHexB)
	fgA, _ := colorful.Hex("#e0e4f0")
	fgB, _ := colorful.Hex("#c0caf5")
	flashColor, _ := colorful.Hex("#d6acff")

	var b strings.Builder
	n := max(len(runes)-1, 1)
	for i, r := range runes {
		t := float64(i) / float64(n)
		bg := bgA.BlendLuv(bgB, t)
		var fg colorful.Color
		if highlightSet[i] {
			fg = flashColor
		} else {
			fg = fgA.BlendLuv(fgB, t)
		}
		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorToHex(fg))).
			Background(lipgloss.Color(colorToHex(bg))).
			Bold(true)
		b.WriteString(style.Render(string(r)))
	}
	return b.String()
}

func (m Model) contentWidth() int {
	w := m.width - 8 // app padding + some margin
	if w < 60 {
		w = 60
	}
	return w
}

func hasCommand(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func truncate(s string, maxLen int) string {
	if lipgloss.Width(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string([]rune(s)[:maxLen])
	}
	// Trim runes until display width fits within maxLen-3 (room for "...")
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes)) > maxLen-3 {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "..."
}

// formatTokens formats token count with K/M suffixes for readability.
func formatTokens(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

// formatCost formats USD cost for display.
func formatCost(usd float64) string {
	if usd >= 1.0 {
		return fmt.Sprintf("%.2f", usd)
	}
	if usd >= 0.01 {
		return fmt.Sprintf("%.2f", usd)
	}
	// Show more precision for tiny amounts
	return fmt.Sprintf("%.3f", usd)
}
