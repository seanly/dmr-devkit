package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/seanly/dmr-devkit/agent"
	"github.com/seanly/dmr-devkit/tool"
)

// Manager provides skill discovery, loading, and agent hook integration.
type Manager struct {
	config Config

	skills        []*Skill
	resolvedRoots []string
	lastScanMtime time.Time
	ensureSkillsMu sync.Mutex

	// toolGroup determines whether skill tools are core or extended.
	toolGroup tool.ToolGroup
}

// NewManager creates a new skill manager from config.
func NewManager(cfg Config) *Manager {
	roots := append([]string{}, cfg.Paths...)
	if cfg.AutoCreatePath != "" {
		_ = os.MkdirAll(cfg.AutoCreatePath, 0o755)
		roots = append(roots, cfg.AutoCreatePath)
	}

	return &Manager{
		config:        cfg,
		resolvedRoots: dedupeRoots(roots),
		skills:        discoverSkillsFromRoots(dedupeRoots(roots)),
		lastScanMtime: maxFileMtimeUnderRoots(dedupeRoots(roots)),
		toolGroup:     tool.ToolGroupCore,
	}
}

// NewManagerWithToolGroup creates a manager with a specific tool group.
func NewManagerWithToolGroup(cfg Config, tg tool.ToolGroup) *Manager {
	m := NewManager(cfg)
	m.toolGroup = tg
	return m
}

// SetToolGroup changes the tool group after construction.
func (m *Manager) SetToolGroup(tg tool.ToolGroup) {
	m.toolGroup = tg
}

// ToolGroup returns the current tool group.
func (m *Manager) ToolGroup() tool.ToolGroup {
	return m.toolGroup
}

// Skills returns the currently discovered skills.
func (m *Manager) Skills() []*Skill {
	m.ensureSkillsFresh()
	return append([]*Skill{}, m.skills...)
}

// RegisterBuiltin registers an in-code skill (e.g. from embed.FS).
// Built-ins are prepended so they appear first and can be overridden by disk skills.
func (m *Manager) RegisterBuiltin(sk *Skill) {
	m.ensureSkillsMu.Lock()
	defer m.ensureSkillsMu.Unlock()

	// If a disk skill with the same name exists, the built-in is shadowed.
	m.skills = append([]*Skill{sk}, m.skills...)
}

// --- agent.Hooks implementation ---

var _ agent.Hooks = (*Manager)(nil)

// ComposeSystemPrompt injects available core skills into the system prompt.
func (m *Manager) ComposeSystemPrompt(_ context.Context, base string) string {
	fragment, _ := m.buildSystemPrompt()
	if fragment == "" {
		return base
	}
	if base == "" {
		return fragment
	}
	return base + "\n" + fragment
}

// CollectAllTools returns skill tools.
func (m *Manager) CollectAllTools(_ context.Context, includeCore, includeExtended bool) []*tool.Tool {
	var out []*tool.Tool
	if includeCore && m.toolGroup == tool.ToolGroupCore {
		out = append(out, m.allTools()...)
	}
	if includeExtended && m.toolGroup == tool.ToolGroupExtended {
		out = append(out, m.allTools()...)
	}
	return out
}

// AfterAgentRun implements agent.Hooks.
func (m *Manager) AfterAgentRun(context.Context, agent.AfterAgentRunArgs) error { return nil }

// InterceptInput implements agent.Hooks.
func (m *Manager) InterceptInput(context.Context, agent.InterceptInputArgs) (*agent.InterceptResult, error) {
	return nil, nil
}

// OnDiscoveredToolsCleared implements agent.Hooks.
func (m *Manager) OnDiscoveredToolsCleared(context.Context, string) error { return nil }

// BeforeToolCall implements agent.Hooks.
func (m *Manager) BeforeToolCall(context.Context, *tool.Tool, map[string]any, *tool.ToolContext) error {
	return nil
}

// BatchBeforeToolCall implements agent.Hooks.
func (m *Manager) BatchBeforeToolCall(context.Context, []tool.BatchCheckItem) map[int]error {
	return nil
}

// --- tools ---

func (m *Manager) allTools() []*tool.Tool {
	tools := []*tool.Tool{
		m.skillTool(),
		m.skillCreateTool(),
		m.skillPromoteTool(),
		m.skillDemoteTool(),
		m.skillListTool(),
		m.skillEditTool(),
		m.skillDeleteTool(),
		m.skillDelegateTool(),
	}
	tools = append(tools, m.synthesizeDelegationTools()...)
	return tools
}

func (m *Manager) skillTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "skill",
			Description: "Load a skill by name. Returns the skill content.",
			Group:       m.toolGroup,
			SearchHint:  "skill, capability, skill.md, load skill, specialized, 技能, 加载, 能力",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Skill name"},
				},
				"required": []string{"name"},
			},
		},
		Handler: m.skillHandler,
	}
}

func (m *Manager) skillHandler(_ *tool.ToolContext, args map[string]any) (any, error) {
	m.ensureSkillsFresh()
	name, _ := args["name"].(string)
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "(no such skill)", nil
	}

	var loc string
	for _, s := range m.skills {
		if strings.ToLower(s.Name) == name {
			loc = s.Location
			break
		}
	}
	if loc == "" {
		return "(no such skill)", nil
	}
	s, err := parseSkillFile(loc)
	if err != nil {
		return nil, fmt.Errorf("read skill: %w", err)
	}
	return fmt.Sprintf("Location: %s\n---\n%s", s.Location, s.Content), nil
}

// --- system prompt ---

const structuredReasoningPrompt = `
## Structured Reasoning Protocol

When using a skill agent or tackling a complex multi-step task, follow this protocol:

### Phase 1: Plan
Before taking any action, output your plan:
<plan>
1. [First step and why]
2. [Second step and why]
...
</plan>

### Phase 2: Execute with Self-Check
After every 3 tool calls, perform a facts survey:
<facts_survey>
Confirmed: [what you know for sure]
Unresolved: [what you still need to find out]
Plan status: [which steps are done, which remain]
</facts_survey>

### Phase 3: Conclude
Only provide a final answer when all plan steps are complete or you've determined the remaining steps are unnecessary.
`

func (m *Manager) buildSystemPrompt() (string, error) {
	m.ensureSkillsFresh()
	var coreSkills []*Skill
	for _, s := range m.skills {
		if skillIsCore(s) {
			coreSkills = append(coreSkills, s)
		}
	}
	if len(coreSkills) == 0 {
		return "", nil
	}

	var promptSkills, agentSkills []*Skill
	for _, s := range coreSkills {
		if s.Type == "agent" {
			agentSkills = append(agentSkills, s)
		} else {
			promptSkills = append(promptSkills, s)
		}
	}

	var lines []string

	if len(promptSkills) > 0 {
		lines = append(lines, "<available_skills>")
		for _, s := range promptSkills {
			lines = append(lines, "  <skill>")
			lines = append(lines, fmt.Sprintf("    <name>%s</name>", escapeXML(s.Name)))
			lines = append(lines, fmt.Sprintf("    <description>%s</description>", escapeXML(s.Description)))
			lines = append(lines, fmt.Sprintf("    <location>%s</location>", escapeXML(s.Location)))
			if len(s.Secrets) > 0 {
				sum := secretsSummaryForPrompt(s.Secrets)
				if sum != "" {
					lines = append(lines, fmt.Sprintf(`    <secrets count="%d">%s</secrets>`, len(s.Secrets), escapeXML(sum)))
				} else {
					lines = append(lines, fmt.Sprintf(`    <secrets count="%d"></secrets>`, len(s.Secrets)))
				}
			}
			lines = append(lines, "  </skill>")
		}
		lines = append(lines, "</available_skills>")
	}

	if len(agentSkills) > 0 {
		lines = append(lines, "<available_specialists>")
		for _, s := range agentSkills {
			lines = append(lines, "  <specialist>")
			lines = append(lines, fmt.Sprintf("    <name>%s</name>", escapeXML(s.Name)))
			lines = append(lines, fmt.Sprintf("    <description>%s</description>", escapeXML(s.Description)))
			if s.WhenToUse != "" {
				lines = append(lines, fmt.Sprintf("    <when_to_use>%s</when_to_use>", escapeXML(s.WhenToUse)))
			}
			lines = append(lines, "  </specialist>")
		}
		lines = append(lines, "</available_specialists>")
		lines = append(lines, "To delegate to a specialist, call skillDelegate(skill=<name>, task=<description>).")
	}

	// Inject structured reasoning prompt when agent skills are active.
	if len(agentSkills) > 0 {
		lines = append(lines, structuredReasoningPrompt)
	}

	return strings.Join(lines, "\n"), nil
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

// --- helpers for tools ---

func (m *Manager) findSkillLocation(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, s := range m.skills {
		if strings.ToLower(s.Name) == name {
			return s.Location
		}
	}
	return ""
}

// resolveSkillRoots resolves skill root paths from config.
func resolveSkillRoots(configPaths []string) []string {
	home, _ := os.UserHomeDir()

	var paths []string
	if len(configPaths) > 0 {
		for _, p := range configPaths {
			expanded := os.ExpandEnv(p)
			if strings.HasPrefix(expanded, "~/") {
				expanded = filepath.Join(home, expanded[2:])
			}
			if !filepath.IsAbs(expanded) {
				expanded = filepath.Join(home, ".dmr", expanded)
			}
			paths = append(paths, expanded)
		}
	} else {
		paths = []string{
			filepath.Join(home, ".dmr", "skills", "local"),
		}
	}
	return paths
}
