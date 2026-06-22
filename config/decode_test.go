package config

import (
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

type agentDoc struct {
	Agent AgentConfig `toml:"agent"`
}

// TestParseSystemPrompt_directUnmarshalArrayFails documents go-toml v2 behavior:
// file path arrays require UnmarshalDocument pre-extraction.
func TestParseSystemPrompt_directUnmarshalArrayFails(t *testing.T) {
	const data = "[agent]\nsystem_prompt = [\"etc/agents/origin.md\"]\n"
	var cfg agentDoc
	err := toml.Unmarshal([]byte(data), &cfg)
	if err == nil {
		t.Fatal("expected direct unmarshal to fail on system_prompt file array")
	}
	if !strings.Contains(err.Error(), "cannot decode TOML array") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUnmarshalDocument_systemPrompt(t *testing.T) {
	tests := []struct {
		name    string
		toml    string
		wantRaw string
		wantFiles []string
		wantSteps int
		wantProfile string
		wantPerTape []SystemPromptEntry
		wantErr   string
	}{
		{
			name: "no agent section",
			toml: "[tape]\ndriver = \"mem\"\n",
		},
		{
			name:      "agent without system_prompt",
			toml:      "[agent]\nmax_steps = 42\n",
			wantSteps: 42,
		},
		{
			name:    "global string",
			toml:    "[agent]\nsystem_prompt = \"hello\"\n",
			wantRaw: "hello",
		},
		{
			name:    "global multiline string",
			toml:    "[agent]\nsystem_prompt = \"\"\"line1\nline2\"\"\"\n",
			wantRaw: "line1\nline2",
		},
		{
			name:      "global single file array",
			toml:      "[agent]\nsystem_prompt = [\"etc/agents/origin.md\"]\n",
			wantFiles: []string{"etc/agents/origin.md"},
		},
		{
			name:      "global multi file array",
			toml:      "[agent]\nsystem_prompt = [\"a.md\", \"b.md\"]\n",
			wantFiles: []string{"a.md", "b.md"},
		},
		{
			name:        "preserves other agent fields",
			toml:        "[agent]\nmax_steps = 50\nsystem_prompt = \"role\"\n[agent.scaffolding]\nprofile = \"minimal\"\n",
			wantRaw:     "role",
			wantSteps:   50,
			wantProfile: "minimal",
		},
		{
			name: "per-tape string",
			toml: `[agent]
[[agent.system_prompts]]
tape = "web:*"
system_prompt = "web helper"
`,
			wantPerTape: []SystemPromptEntry{{
				Tape:         "web:*",
				SystemPrompt: SystemPromptValue{Raw: "web helper"},
			}},
		},
		{
			name: "per-tape file array",
			toml: `[agent]
[[agent.system_prompts]]
tape = "cron:*"
system_prompt = ["prompts/cron.md", "prompts/safety.md"]
`,
			wantPerTape: []SystemPromptEntry{{
				Tape:         "cron:*",
				SystemPrompt: SystemPromptValue{Files: []string{"prompts/cron.md", "prompts/safety.md"}},
			}},
		},
		{
			name: "per-tape without system_prompt",
			toml: `[agent]
[[agent.system_prompts]]
tape = "ops:*"
profile = "minimal"
`,
			wantPerTape: []SystemPromptEntry{{
				Tape:    "ops:*",
				Profile: "minimal",
			}},
		},
		{
			name: "multiple per-tape mixed formats",
			toml: `[agent]
[[agent.system_prompts]]
tape = "a:*"
system_prompt = "inline"

[[agent.system_prompts]]
tape = "b:*"
system_prompt = ["b.md"]
`,
			wantPerTape: []SystemPromptEntry{
				{Tape: "a:*", SystemPrompt: SystemPromptValue{Raw: "inline"}},
				{Tape: "b:*", SystemPrompt: SystemPromptValue{Files: []string{"b.md"}}},
			},
		},
		{
			name:    "invalid global type",
			toml:    "[agent]\nsystem_prompt = 42\n",
			wantErr: "agent.system_prompt",
		},
		{
			name: "invalid per-tape array element",
			toml: `[agent]
[[agent.system_prompts]]
tape = "x:*"
system_prompt = [1]
`,
			wantErr: "agent.system_prompts[0].system_prompt",
		},
		{
			name:    "invalid toml",
			toml:    "[agent]\nsystem_prompt = \n",
			wantErr: "toml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg agentDoc
			err := UnmarshalDocument([]byte(tt.toml), &cfg)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.wantErr)) {
					t.Fatalf("error %q should contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}

			got := cfg.Agent.SystemPrompt
			if got.Raw != tt.wantRaw {
				t.Fatalf("SystemPrompt.Raw=%q want %q", got.Raw, tt.wantRaw)
			}
			if len(got.Files) != len(tt.wantFiles) {
				t.Fatalf("SystemPrompt.Files=%v want %v", got.Files, tt.wantFiles)
			}
			for i, f := range tt.wantFiles {
				if got.Files[i] != f {
					t.Fatalf("Files[%d]=%q want %q", i, got.Files[i], f)
				}
			}
			if tt.wantSteps != 0 && cfg.Agent.MaxSteps != tt.wantSteps {
				t.Fatalf("MaxSteps=%d want %d", cfg.Agent.MaxSteps, tt.wantSteps)
			}
			if tt.wantProfile != "" && cfg.Agent.Scaffolding.Profile != tt.wantProfile {
				t.Fatalf("Scaffolding.Profile=%q want %q", cfg.Agent.Scaffolding.Profile, tt.wantProfile)
			}
			if tt.wantPerTape != nil {
				if len(cfg.Agent.SystemPrompts) != len(tt.wantPerTape) {
					t.Fatalf("SystemPrompts len=%d want %d", len(cfg.Agent.SystemPrompts), len(tt.wantPerTape))
				}
				for i, want := range tt.wantPerTape {
					gotEntry := cfg.Agent.SystemPrompts[i]
					if gotEntry.Tape != want.Tape {
						t.Fatalf("[%d].Tape=%q want %q", i, gotEntry.Tape, want.Tape)
					}
					if gotEntry.Profile != want.Profile {
						t.Fatalf("[%d].Profile=%q want %q", i, gotEntry.Profile, want.Profile)
					}
					if gotEntry.SystemPrompt.Raw != want.SystemPrompt.Raw {
						t.Fatalf("[%d].SystemPrompt.Raw=%q want %q", i, gotEntry.SystemPrompt.Raw, want.SystemPrompt.Raw)
					}
					if len(gotEntry.SystemPrompt.Files) != len(want.SystemPrompt.Files) {
						t.Fatalf("[%d].SystemPrompt.Files=%v want %v", i, gotEntry.SystemPrompt.Files, want.SystemPrompt.Files)
					}
					for j, f := range want.SystemPrompt.Files {
						if gotEntry.SystemPrompt.Files[j] != f {
							t.Fatalf("[%d].Files[%d]=%q want %q", i, j, gotEntry.SystemPrompt.Files[j], f)
						}
					}
				}
			}
		})
	}
}

func TestUnmarshalDocument_applyTargetErrors(t *testing.T) {
	t.Run("nil pointer", func(t *testing.T) {
		var cfg *agentDoc
		err := UnmarshalDocument([]byte("[agent]\nmax_steps = 1\n"), cfg)
		if err == nil {
			t.Fatal("expected error for nil decode target")
		}
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "nil") {
			t.Fatalf("expected nil-related error, got %v", err)
		}
	})

	t.Run("no agent field", func(t *testing.T) {
		var cfg struct {
			Tape TapeConfig `toml:"tape"`
		}
		if err := UnmarshalDocument([]byte("[agent]\nsystem_prompt = \"x\"\n"), &cfg); err != nil {
			t.Fatal(err)
		}
	})
}

func TestSystemPromptFromAny(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		sp, err := systemPromptFromAny("hello")
		if err != nil {
			t.Fatal(err)
		}
		if sp.Raw != "hello" || len(sp.Files) != 0 {
			t.Fatalf("got Raw=%q Files=%v", sp.Raw, sp.Files)
		}
	})

	t.Run("file array", func(t *testing.T) {
		sp, err := systemPromptFromAny([]any{"a.md", "b.md"})
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"a.md", "b.md"}
		if len(sp.Files) != len(want) {
			t.Fatalf("got Files=%v want %v", sp.Files, want)
		}
	})

	t.Run("empty file array", func(t *testing.T) {
		sp, err := systemPromptFromAny([]any{})
		if err != nil {
			t.Fatal(err)
		}
		if len(sp.Files) != 0 || sp.Raw != "" {
			t.Fatalf("got Raw=%q Files=%v", sp.Raw, sp.Files)
		}
	})

	t.Run("non-string array element", func(t *testing.T) {
		_, err := systemPromptFromAny([]any{"ok", 1})
		if err == nil || !strings.Contains(err.Error(), "strings") {
			t.Fatalf("expected string element error, got %v", err)
		}
	})

	t.Run("unsupported type", func(t *testing.T) {
		_, err := systemPromptFromAny(42)
		if err == nil {
			t.Fatal("expected error for int type")
		}
	})
}

func TestExtractSystemPromptFields_errors(t *testing.T) {
	doc := map[string]any{
		"agent": map[string]any{
			"system_prompts": "not-an-array",
		},
	}
	_, err := extractSystemPromptFields(doc)
	if err == nil || !strings.Contains(err.Error(), "agent.system_prompts") {
		t.Fatalf("expected system_prompts error, got %v", err)
	}
}
