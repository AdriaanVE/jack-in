package ui

import (
	"math/rand"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/AdriaanVE/jack-in/src/quotes"
)

// noColor is the sentinel value for cells without a color.
const noColor = -1

// MatrixEffect implements Matrix digital rain animation using particle-based streaks.
// Ported from sysc-Go.
type MatrixEffect struct {
	width   int
	height  int
	palette []string
	chars   []rune

	streaks []matrixStreak
	frame   int

	// Pre-computed styles for each palette color (avoids per-cell allocation)
	styles []lipgloss.Style

	// Reusable render buffers
	canvas  [][]rune
	colors  [][]int // index into styles (noColor = no color)
	builder strings.Builder
}

// matrixStreak represents a single vertical streak falling down the screen.
type matrixStreak struct {
	x       int
	y       int
	length  int
	speed   int
	counter int
	active  bool
}

// NewMatrixEffect creates a new Matrix effect with given dimensions.
func NewMatrixEffect(width, height int) *MatrixEffect {
	// Tokyo Night inspired palette (green tones to match Matrix aesthetic)
	palette := []string{
		"#1a472a", // dark green
		"#2d6a4f", // forest green
		"#40916c", // medium green
		"#52b788", // light green
		"#74c69d", // bright green
		"#95d5b2", // pale green
		"#b7e4c7", // very light green
		"#d8f3dc", // near white green
	}

	// Pre-create styles for each palette color
	styles := make([]lipgloss.Style, len(palette))
	for i, hex := range palette {
		styles[i] = lipgloss.NewStyle().Foreground(lipgloss.Color(hex))
	}

	m := &MatrixEffect{
		width:   width,
		height:  height,
		palette: palette,
		styles:  styles,
		chars: []rune{
			'0', '1', '2', '3', '4', '5', '6', '7', '8', '9',
			'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M',
			'N', 'O', 'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z',
			'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm',
			'n', 'o', 'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z',
			'α', 'β', 'γ', 'δ', 'ε', 'ζ', 'η', 'θ', 'ι', 'κ', 'λ', 'μ',
			'ν', 'ξ', 'ο', 'π', 'ρ', 'σ', 'τ', 'υ', 'φ', 'χ', 'ψ', 'ω',
			'░', '▒', '▓', '█', '▀', '▄', '▌', '▐', '■', '□', '▪', '▫',
		},
		streaks: make([]matrixStreak, 0, 100),
		frame:   0,
	}
	m.init()
	return m
}

func (m *MatrixEffect) init() {
	m.initBuffers()
	// Create initial streaks across width
	for i := 0; i < m.width; i++ {
		if rand.Float64() < 0.1 {
			streak := matrixStreak{
				x:       i,
				y:       -rand.Intn(m.height),
				length:  rand.Intn(15) + 5,
				speed:   rand.Intn(3) + 1,
				counter: 0,
				active:  true,
			}
			m.streaks = append(m.streaks, streak)
		}
	}
}

func (m *MatrixEffect) initBuffers() {
	m.canvas = make([][]rune, m.height)
	m.colors = make([][]int, m.height)
	for i := range m.canvas {
		m.canvas[i] = make([]rune, m.width)
		m.colors[i] = make([]int, m.width)
		for j := range m.colors[i] {
			m.colors[i][j] = noColor
		}
	}
	m.builder.Grow(m.width * m.height * 4)
}

func (m *MatrixEffect) clearBuffers() {
	for i := range m.canvas {
		for j := range m.canvas[i] {
			m.canvas[i][j] = ' '
			m.colors[i][j] = noColor
		}
	}
}

// Resize reinitializes the Matrix effect with new dimensions.
func (m *MatrixEffect) Resize(width, height int) {
	m.width = width
	m.height = height
	m.streaks = m.streaks[:0]
	m.init()
}

func (m *MatrixEffect) getHeadColorIdx() int {
	return len(m.palette) - 1
}

func (m *MatrixEffect) getTrailColorIdx(position, length int) int {
	fadeFactor := float64(position) / float64(length)
	if fadeFactor < 0.2 {
		return len(m.palette) - 1
	} else if fadeFactor < 0.5 {
		return len(m.palette) - 2
	}
	return 0
}

// Update advances the Matrix simulation by one frame.
func (m *MatrixEffect) Update() {
	m.frame++

	activeStreaks := m.streaks[:0]
	for _, streak := range m.streaks {
		if !streak.active {
			continue
		}

		streak.counter++
		if streak.counter >= streak.speed {
			streak.y++
			streak.counter = 0

			if streak.y-streak.length > m.height {
				streak.active = false
				continue
			}
		}

		activeStreaks = append(activeStreaks, streak)
	}

	m.streaks = activeStreaks

	for i := 0; i < m.width; i++ {
		if rand.Float64() < 0.02 && len(m.streaks) < 150 {
			streak := matrixStreak{
				x:       i,
				y:       -rand.Intn(5),
				length:  rand.Intn(15) + 5,
				speed:   rand.Intn(3) + 1,
				counter: 0,
				active:  true,
			}
			m.streaks = append(m.streaks, streak)
		}
	}
}

// Render converts the Matrix streaks to colored text output.
func (m *MatrixEffect) Render() string {
	m.clearBuffers()

	for _, streak := range m.streaks {
		if !streak.active {
			continue
		}

		for i := 0; i < streak.length; i++ {
			yPos := streak.y + i
			if yPos >= 0 && yPos < m.height && streak.x >= 0 && streak.x < m.width {
				char := m.chars[rand.Intn(len(m.chars))]

				var colorIdx int
				if i == 0 {
					colorIdx = m.getHeadColorIdx()
				} else {
					colorIdx = m.getTrailColorIdx(i, streak.length)
				}

				m.canvas[yPos][streak.x] = char
				m.colors[yPos][streak.x] = colorIdx
			}
		}
	}

	var lines []string
	for y := 0; y < m.height; y++ {
		m.builder.Reset()
		for x := 0; x < m.width; x++ {
			char := m.canvas[y][x]
			colorIdx := m.colors[y][x]
			if char != ' ' && colorIdx != noColor {
				m.builder.WriteString(m.styles[colorIdx].Render(string(char)))
			} else {
				m.builder.WriteRune(char)
			}
		}
		lines = append(lines, m.builder.String())
	}

	return strings.Join(lines, "\n")
}

// Reset restarts the animation from the beginning.
func (m *MatrixEffect) Reset() {
	m.frame = 0
	m.streaks = m.streaks[:0]
	m.init()
}

// matrixTickMsg triggers the next animation frame.
type matrixTickMsg struct{}

// matrixTickCmd returns a command that sends a tick after a delay.
func matrixTickCmd() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(50 * time.Millisecond)
		return matrixTickMsg{}
	}
}

// RunMatrixIntro runs a brief Matrix rain animation as a startup intro.
// It displays for the specified duration, then exits.
func RunMatrixIntro(duration time.Duration) {
	p := tea.NewProgram(newMatrixIntroModel(duration))
	_, _ = p.Run()
}

// matrixIntroModel is a Bubbletea model for the intro animation.
type matrixIntroModel struct {
	effect    *MatrixEffect
	width     int
	height    int
	start     time.Time
	duration  time.Duration
	done      bool
	dissolved [][]bool // tracks which logo chars have dissolved into rain
	quote     string   // quote shown after logo dissolves
}

func newMatrixIntroModel(duration time.Duration) *matrixIntroModel {
	return &matrixIntroModel{
		duration: duration,
		start:    time.Now(),
		quote:    quotes.Get(quotes.Up),
	}
}

func (m *matrixIntroModel) Init() tea.Cmd {
	return tea.Batch(
		matrixIntroTickCmd(),
	)
}

func (m *matrixIntroModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.effect == nil {
			m.effect = NewMatrixEffect(m.width, m.height)
		} else {
			m.effect.Resize(m.width, m.height)
		}
		return m, nil

	case tea.KeyPressMsg:
		// Any key exits early
		m.done = true
		return m, tea.Quit

	case matrixIntroTickMsg:
		if time.Since(m.start) >= m.duration {
			m.done = true
			return m, tea.Quit
		}
		if m.effect != nil {
			m.effect.Update()
		}
		return m, matrixIntroTickCmd()
	}

	return m, nil
}

func (m *matrixIntroModel) View() tea.View {
	if m.done || m.width == 0 || m.effect == nil {
		v := tea.NewView("")
		v.AltScreen = true
		return v
	}

	content := m.effect.Render()

	// Add "JACK IN" text centered, dissolving into rain
	lines := strings.Split(content, "\n")
	logoLines := []string{
		"   █ ███  ███ █  █   █ █  █",
		"   █ █  █ █   █ █    █ ██ █",
		"   █ ███  █   ██     █ █ ██",
		"█  █ █  █ █   █ █    █ █  █",
		" ██  █  █ ███ █  █   █ █  █",
	}
	logoWidth := 28
	logoHeight := len(logoLines)

	// Initialize dissolved array if needed
	if m.dissolved == nil {
		m.dissolved = make([][]bool, logoHeight)
		for i := range m.dissolved {
			m.dissolved[i] = make([]bool, logoWidth)
		}
	}

	elapsed := time.Since(m.start)
	// Timing: 0.8s solid, 1.7s dissolve, 0.5s quote+rain, then quote only until end
	// Total 4.5s with quote showing for ~2s (1s shorter than before)
	solidDuration := 800 * time.Millisecond
	dissolveDuration := 1700 * time.Millisecond
	dissolveEnd := solidDuration + dissolveDuration
	quoteWithRainEnd := dissolveEnd + 500*time.Millisecond

	// Check phases
	showQuote := elapsed >= dissolveEnd
	quoteOnly := elapsed >= quoteWithRainEnd

	// During dissolve phase, randomly dissolve characters
	var dissolveProgress float64
	if elapsed > solidDuration && elapsed < dissolveEnd {
		dissolveProgress = float64(elapsed-solidDuration) / float64(dissolveDuration)
	} else if elapsed >= dissolveEnd {
		dissolveProgress = 1.0
	}

	if elapsed > solidDuration {
		// Count total solid chars and how many should be dissolved
		totalChars := 0
		for _, line := range logoLines {
			for _, ch := range line {
				if ch != ' ' {
					totalChars++
				}
			}
		}
		targetDissolved := int(float64(totalChars) * dissolveProgress)

		// Count currently dissolved
		currentDissolved := 0
		for y := range m.dissolved {
			for x := range m.dissolved[y] {
				if m.dissolved[y][x] {
					currentDissolved++
				}
			}
		}

		// Dissolve more chars randomly
		for currentDissolved < targetDissolved {
			ry := rand.Intn(logoHeight)
			rx := rand.Intn(logoWidth)
			if rx < len(logoLines[ry]) && logoLines[ry][rx] != ' ' && !m.dissolved[ry][rx] {
				m.dissolved[ry][rx] = true
				currentDissolved++
			}
		}
	}

	centerY := (len(lines) - logoHeight) / 2
	centerX := (m.width - logoWidth) / 2
	if centerX < 0 {
		centerX = 0
	}

	if quoteOnly {
		// Quote only on black background
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("#00d26a")).Bold(true)
		quoteLine := style.Render("\"" + m.quote + "\"")
		// Clear all lines to black
		for i := range lines {
			lines[i] = strings.Repeat(" ", m.width)
		}
		quoteY := len(lines) / 2
		if quoteY >= 0 && quoteY < len(lines) {
			lines[quoteY] = lipgloss.PlaceHorizontal(m.width, lipgloss.Center, quoteLine)
		}
	} else if showQuote {
		// Show quote centered over the rain
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("#00d26a")).Bold(true)
		quoteLine := style.Render("\"" + m.quote + "\"")
		quoteY := len(lines) / 2
		if quoteY >= 0 && quoteY < len(lines) {
			lines[quoteY] = lipgloss.PlaceHorizontal(m.width, lipgloss.Center, quoteLine)
		}
	} else {
		// Render logo with dissolved characters showing rain behind
		for i, logoLine := range logoLines {
			y := centerY + i
			if y >= 0 && y < len(lines) {
				var sb strings.Builder
				sb.WriteString(strings.Repeat(" ", centerX))
				for x, ch := range logoLine {
					if x < len(m.dissolved[i]) && m.dissolved[i][x] {
						// Dissolved: show rain character from background
						if centerX+x < len(lines[y]) {
							sb.WriteRune(rune(lines[y][centerX+x]))
						} else {
							sb.WriteRune(' ')
						}
					} else {
						sb.WriteRune(ch)
					}
				}
				// Apply gradient to the whole line (non-dissolved chars will show styled)
				styled := gradientText(sb.String(), "#40916c", "#d8f3dc")
				lines[y] = styled
			}
		}
	}

	placed := lipgloss.Place(m.width, m.height, lipgloss.Left, lipgloss.Top, strings.Join(lines, "\n"))

	v := tea.NewView(placed)
	v.AltScreen = true
	return v
}

type matrixIntroTickMsg struct{}

func matrixIntroTickCmd() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(50 * time.Millisecond)
		return matrixIntroTickMsg{}
	}
}
