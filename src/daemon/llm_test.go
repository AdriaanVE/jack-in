package daemon

import (
	"encoding/json"
	"testing"
)

func TestPaneEvalToolSchema(t *testing.T) {
	// Verify the tool schema can be marshaled to JSON
	data, err := json.Marshal(paneEvalTool)
	if err != nil {
		t.Fatalf("marshal tool schema: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal tool schema: %v", err)
	}
	if parsed["name"] != "pane_evaluation" {
		t.Errorf("tool name = %v, want pane_evaluation", parsed["name"])
	}
	schema, ok := parsed["input_schema"].(map[string]any)
	if !ok {
		t.Fatal("missing input_schema")
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("missing properties")
	}
	required := []string{"status", "safe_to_approve", "approval_keystroke", "response_text", "reason"}
	for _, r := range required {
		if _, ok := props[r]; !ok {
			t.Errorf("missing required property %q", r)
		}
	}
}

func TestPaneEvaluationDecode(t *testing.T) {
	raw := `{
		"status": "permission_prompt",
		"safe_to_approve": true,
		"approval_keystroke": "Enter",
		"response_text": "",
		"reason": "File read is safe"
	}`
	var pe PaneEvaluation
	if err := json.Unmarshal([]byte(raw), &pe); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if pe.Status != "permission_prompt" {
		t.Errorf("status = %q", pe.Status)
	}
	if !pe.SafeToApprove {
		t.Error("expected safe_to_approve = true")
	}
	if pe.ApprovalKeystroke != "Enter" {
		t.Errorf("approval_keystroke = %q", pe.ApprovalKeystroke)
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "clean JSON",
			input: `{"status":"working","safe_to_approve":false,"approval_keystroke":"","response_text":"","reason":"agent is processing"}`,
			want:  `{"status":"working","safe_to_approve":false,"approval_keystroke":"","response_text":"","reason":"agent is processing"}`,
		},
		{
			name:  "JSON with surrounding text",
			input: "Here's the evaluation:\n{\"status\":\"idle\",\"safe_to_approve\":false,\"approval_keystroke\":\"\",\"response_text\":\"\",\"reason\":\"done\"}\nLet me know if you need more.",
			want:  `{"status":"idle","safe_to_approve":false,"approval_keystroke":"","response_text":"","reason":"done"}`,
		},
		{
			name:  "JSON with markdown fences",
			input: "```json\n{\"status\":\"error\",\"safe_to_approve\":false,\"approval_keystroke\":\"\",\"response_text\":\"\",\"reason\":\"compile error\"}\n```",
			want:  `{"status":"error","safe_to_approve":false,"approval_keystroke":"","response_text":"","reason":"compile error"}`,
		},
		{
			name:  "nested braces in reason",
			input: `{"status":"working","safe_to_approve":false,"approval_keystroke":"","response_text":"","reason":"parsing {foo}"}`,
			want:  `{"status":"working","safe_to_approve":false,"approval_keystroke":"","response_text":"","reason":"parsing {foo}"}`,
		},
		{
			name:  "no JSON",
			input: "I couldn't evaluate the pane.",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSON(tt.input)
			if got != tt.want {
				t.Errorf("extractJSON() = %q, want %q", got, tt.want)
			}
		})
	}
}
