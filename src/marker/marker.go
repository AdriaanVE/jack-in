package marker

import "strings"

const Prefix = "JACKIN_TASK_COMPLETE:"

// CompletionMarker returns the marker string for a given task ID.
func CompletionMarker(taskID string) string {
	return Prefix + taskID
}

// StripFormatting removes markdown code fences, backticks, quotes, and
// trailing punctuation that LLMs sometimes wrap around markers.
func StripFormatting(line string) string {
	s := strings.TrimSpace(line)
	// Strip markdown code fences
	if strings.HasPrefix(s, "```") && strings.HasSuffix(s, "```") {
		s = strings.TrimSpace(s[3 : len(s)-3])
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimSpace(s[3:])
	} else if strings.HasSuffix(s, "```") {
		s = strings.TrimSpace(s[:len(s)-3])
	}
	// Strip backticks
	if len(s) >= 2 && s[0] == '`' && s[len(s)-1] == '`' {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	// Strip quotes
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	// Strip trailing period
	if strings.HasSuffix(s, ".") {
		s = strings.TrimSpace(s[:len(s)-1])
	}
	return s
}

// HasCompletionMarker checks if text contains the completion marker for
// the given task. Scans the last 10 non-empty lines; tries cheap contains
// first, then expensive StripFormatting on the first 5.
func HasCompletionMarker(text, taskID string) bool {
	expected := Prefix + taskID
	lines := strings.Split(text, "\n")

	checked := 0
	for i := len(lines) - 1; i >= 0 && checked < 10; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}
		checked++
		if strings.Contains(trimmed, expected) {
			return true
		}
		if checked <= 5 && StripFormatting(trimmed) == expected {
			return true
		}
	}
	return false
}
