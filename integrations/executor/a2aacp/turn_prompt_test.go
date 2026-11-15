package a2aacp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/internal/acp"
)

// An empty part has no content, and the ACP blocks marshal their content fields
// with omitempty, so sending one produces a block that is missing its required
// field: {"type":"text"} with no text. opencode rejects the whole prompt with
// "Invalid params" when that happens, which fails the task for every part in
// the message, including the valid ones next to it.
func TestToPromptDropsEmptyParts(t *testing.T) {
	t.Parallel()
	caps := acp.PromptCapabilities{Image: true, Audio: true, EmbeddedContext: true}
	text := a2a.Text("hello")
	helloFile := a2a.Raw("hello")
	emptyFile := a2a.Raw("")

	tests := []struct {
		name    string
		parts   []*a2a.Part
		want    []acp.ContentBlock
		wantErr string
	}{
		{
			name:    "an empty text part alone leaves nothing to send",
			parts:   []*a2a.Part{{Content: a2a.Text("")}},
			wantErr: "the message has no content the agent accepts",
		},
		{
			name:    "an empty file alone leaves nothing to send",
			parts:   []*a2a.Part{{Content: emptyFile, MediaType: "text/plain", Filename: "empty.txt"}},
			wantErr: "the message has no content the agent accepts",
		},
		{
			name:    "an empty image alone leaves nothing to send",
			parts:   []*a2a.Part{{Content: emptyFile, MediaType: "image/png", Filename: "empty.png"}},
			wantErr: "the message has no content the agent accepts",
		},
		{
			name:    "an empty url alone leaves nothing to send",
			parts:   []*a2a.Part{{Content: a2a.URL("")}},
			wantErr: "the message has no content the agent accepts",
		},
		{
			name:  "an empty text part does not spoil a valid one",
			parts: []*a2a.Part{{Content: a2a.Text("")}, {Content: text}},
			want:  []acp.ContentBlock{{Type: "text", Text: "hello"}},
		},
		{
			// The case the issue calls out: one empty file next to a valid file
			// failed the whole message.
			name: "an empty file does not spoil a valid one",
			parts: []*a2a.Part{
				{Content: emptyFile, MediaType: "text/plain", Filename: "empty.txt"},
				{Content: helloFile, MediaType: "text/plain", Filename: "hello.txt"},
			},
			want: []acp.ContentBlock{{
				Type: "resource",
				Resource: &acp.EmbeddedResource{
					URI:      "attachment:hello.txt",
					MimeType: "text/plain",
					Text:     "hello",
				},
			}},
		},
		{
			name:  "an empty image does not spoil a valid text part",
			parts: []*a2a.Part{{Content: emptyFile, MediaType: "image/png", Filename: "empty.png"}, {Content: text}},
			want:  []acp.ContentBlock{{Type: "text", Text: "hello"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := toPrompt(context.Background(), tc.parts, caps)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error containing %q, got %v (blocks: %#v)", tc.wantErr, err, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("want %d blocks, got %d: %#v", len(tc.want), len(got), got)
			}
			for i := range got {
				if got[i].Type != tc.want[i].Type {
					t.Errorf("block %d: want type %q, got %q", i, tc.want[i].Type, got[i].Type)
				}
				if got[i].Resource != nil && tc.want[i].Resource != nil {
					if *got[i].Resource != *tc.want[i].Resource {
						t.Errorf("block %d: want resource %#v, got %#v", i, *tc.want[i].Resource, *got[i].Resource)
					}
				}
			}
		})
	}
}

// Whatever toPrompt returns has to survive the wire form: a block whose content
// field is dropped by omitempty is the defect, so the blocks that remain must
// each still carry the field their type requires.
func TestToPromptBlocksCarryTheirContent(t *testing.T) {
	t.Parallel()
	caps := acp.PromptCapabilities{Image: true, Audio: true, EmbeddedContext: true}
	parts := []*a2a.Part{
		{Content: a2a.Text("")},
		{Content: a2a.Raw(""), MediaType: "image/png", Filename: "empty.png"},
		{Content: a2a.URL("")},
		{Content: a2a.Text("hello")},
		{Content: a2a.URL("https://example.com/a.txt"), MediaType: "text/plain"},
		{Content: a2a.Raw("bytes"), MediaType: "image/png"},
	}
	blocks, err := toPrompt(context.Background(), parts, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 3 {
		t.Fatalf("want 3 blocks (the three non-empty parts), got %d: %#v", len(blocks), blocks)
	}
	for i, b := range blocks {
		raw, err := json.Marshal(b)
		if err != nil {
			t.Fatalf("block %d does not marshal: %v", i, err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("block %d is not an object: %v", i, err)
		}
		switch b.Type {
		case "text":
			if s, _ := m["text"].(string); s == "" {
				t.Errorf("block %d marshalled without text: %s", i, raw)
			}
		case "resource_link":
			if s, _ := m["uri"].(string); s == "" {
				t.Errorf("block %d marshalled without uri: %s", i, raw)
			}
		case "image", "audio":
			if s, _ := m["data"].(string); s == "" {
				t.Errorf("block %d marshalled without data: %s", i, raw)
			}
		}
	}
}
