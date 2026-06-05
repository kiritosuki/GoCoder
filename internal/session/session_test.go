package session

import (
	"testing"

	"github.com/kiritosuki/gocoder/internal/types"
)

func TestAppendAgentMessagePreservesToolRound(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	s, err := New()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	defer s.Close()

	if err := s.AppendAgentMessage(types.Message{Role: "user", Content: "read file"}); err != nil {
		t.Fatalf("append user: %v", err)
	}
	if err := s.AppendAgentMessage(types.Message{
		Role: "assistant",
		ToolCalls: []types.ToolCall{{
			ID:   "call_1",
			Type: "function",
			Function: types.FunctionCall{
				Name:      "read_file",
				Arguments: `{"path":"main.go"}`,
			},
		}},
	}); err != nil {
		t.Fatalf("append assistant tool call: %v", err)
	}
	if err := s.AppendAgentMessage(types.Message{
		Role:       "tool",
		ToolCallID: "call_1",
		Name:       "read_file",
		Content:    "package main",
	}); err != nil {
		t.Fatalf("append tool result: %v", err)
	}

	events, err := LoadEvents(s.Path())
	if err != nil {
		t.Fatalf("load events: %v", err)
	}
	msgs := EventsToMessages(events)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d: %+v", len(msgs), msgs)
	}
	if len(msgs[1].ToolCalls) != 1 || msgs[1].ToolCalls[0].Function.Name != "read_file" {
		t.Fatalf("tool call not restored: %+v", msgs[1])
	}
	if msgs[2].Role != "tool" || msgs[2].ToolCallID != "call_1" || msgs[2].Name != "read_file" {
		t.Fatalf("tool result not restored: %+v", msgs[2])
	}
}
