package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/AdriaanVE/jack-in/src/domain"
	"github.com/AdriaanVE/jack-in/src/taskqueue"
)

// helper to build a minimal Model with the fields needed for task bar hit testing.
func testModel(cards int) Model {
	m := Model{
		width:  120,
		height: 40,
		styles: newStyles(),
	}
	for i := 0; i < cards; i++ {
		m.cards = append(m.cards, cardData{name: "w" + string(rune('0'+i))})
	}
	return m
}

func TestTaskBarSegmentAtPosition(t *testing.T) {
	m := testModel(3) // 1 row of 3 cards

	// Task bar Y = app padding (1) + header (1) + cardRows * 6 (5 card + 1 margin)
	taskBarY := 1 + 1 + 1*6 // = 8

	// Prefix "= Tasks" rendered width
	prefix := m.styles.taskBarPrefix.Render("= Tasks")
	prefixW := lipgloss.Width(prefix)
	segStartX := 3 + prefixW // app left padding + prefix

	cardRenderedW := cardInner
	cardsW := cardsPerRow*cardRenderedW + (cardsPerRow - 1)
	gradW := cardsW - prefixW
	if gradW < 40 {
		gradW = 40
	}
	segW := gradW / 5

	tests := []struct {
		name string
		x, y int
		want domain.TaskState
	}{
		{"pending segment", segStartX + 1, taskBarY, domain.StatePending},
		{"current segment", segStartX + segW + 1, taskBarY, domain.StateCurrent},
		{"review segment", segStartX + 2*segW + 1, taskBarY, domain.StateReview},
		{"complete segment", segStartX + 3*segW + 1, taskBarY, domain.StateComplete},
		{"rejected segment", segStartX + 4*segW + 1, taskBarY, domain.StateRejected},
		{"wrong Y", segStartX + 1, taskBarY + 1, ""},
		{"before prefix", 2, taskBarY, ""},
		{"negative X", -1, taskBarY, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.taskBarSegmentAtPosition(tt.x, tt.y)
			if got != tt.want {
				t.Errorf("taskBarSegmentAtPosition(%d, %d) = %q, want %q", tt.x, tt.y, got, tt.want)
			}
		})
	}
}

func TestTaskBarSegmentAtPosition_TwoRows(t *testing.T) {
	m := testModel(5) // 2 rows of cards (3 + 2)

	taskBarY := 1 + 1 + 2*6 // = 14
	prefix := m.styles.taskBarPrefix.Render("= Tasks")
	prefixW := lipgloss.Width(prefix)
	segStartX := 3 + prefixW

	got := m.taskBarSegmentAtPosition(segStartX+1, taskBarY)
	if got != domain.StatePending {
		t.Errorf("expected pending, got %q", got)
	}

	// Old Y from 1-row layout should miss
	got = m.taskBarSegmentAtPosition(segStartX+1, 6)
	if got != "" {
		t.Errorf("expected empty at old Y, got %q", got)
	}
}

func TestRenderTaskPopupEntry_Plain(t *testing.T) {
	m := testModel(3)
	e := taskqueue.TaskEntry{
		Task:  taskqueue.Task{Summary: "Fix login bug"},
		State: domain.StatePending,
	}
	m.taskPopupCategory = domain.StatePending

	out := m.renderTaskPopupEntry(e, 60)
	if !strings.Contains(out, "Fix login bug") {
		t.Errorf("expected summary in output, got %q", out)
	}
}

func TestRenderTaskPopupEntry_Rejected(t *testing.T) {
	m := testModel(3)
	m.taskPopupCategory = domain.StateRejected
	e := taskqueue.TaskEntry{
		Task: taskqueue.Task{
			Summary:  "Refactor auth",
			Feedback: "Tests are failing",
		},
		State: domain.StateRejected,
	}

	out := m.renderTaskPopupEntry(e, 80)
	if !strings.Contains(out, "Refactor auth") {
		t.Errorf("expected summary in output, got %q", out)
	}
	if !strings.Contains(out, "Tests are failing") {
		t.Errorf("expected feedback in output, got %q", out)
	}
}

func TestRenderTaskPopupEntry_Assignee(t *testing.T) {
	m := testModel(3)
	m.taskPopupCategory = domain.StateCurrent
	e := taskqueue.TaskEntry{
		Task: taskqueue.Task{
			Summary:  "Build dashboard",
			Assignee: "worker-1",
		},
		State: domain.StateCurrent,
	}

	out := m.renderTaskPopupEntry(e, 60)
	if !strings.Contains(out, "Build dashboard") {
		t.Errorf("expected summary in output, got %q", out)
	}
	if !strings.Contains(out, "worker-1") {
		t.Errorf("expected assignee in output, got %q", out)
	}
}
