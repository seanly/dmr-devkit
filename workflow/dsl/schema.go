// Package dsl provides the declarative schema for workflow definitions.
// It is intentionally free of runtime dependencies so that definitions can be
// serialized, shared, and generated independently of the execution engine.
package dsl

import "fmt"

// WorkflowDef is the top-level declarative description of a multi-agent workflow.
type WorkflowDef struct {
	Name        string      `yaml:"name" json:"name"`
	Description string      `yaml:"description" json:"description"`
	Input       InputSchema `yaml:"input,omitempty" json:"input,omitempty"`
	Stages      []StageDef  `yaml:"stages" json:"stages"`
	Return      ReturnDef   `yaml:"return,omitempty" json:"return,omitempty"`
}

// InputSchema describes the expected input variables for a workflow.
type InputSchema struct {
	Schema map[string]string `yaml:"schema,omitempty" json:"schema,omitempty"`
}

// StageDef describes a single stage in the workflow.
// A stage groups one or more agents with a common execution mode (parallel or sequential).
type StageDef struct {
	ID          string     `yaml:"id" json:"id"`
	Description string     `yaml:"description" json:"description"`
	Parallel    bool       `yaml:"parallel,omitempty" json:"parallel,omitempty"`
	DependsOn   []string   `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`
	Agents      []AgentDef `yaml:"agents" json:"agents"`
}

// AgentDef describes a single agent invocation within a stage.
type AgentDef struct {
	ID           string            `yaml:"id" json:"id"`
	SystemPrompt string            `yaml:"system_prompt,omitempty" json:"system_prompt,omitempty"`
	Prompt       string            `yaml:"prompt" json:"prompt"`
	Model        string            `yaml:"model,omitempty" json:"model,omitempty"`
	MaxTokens    int               `yaml:"max_tokens,omitempty" json:"max_tokens,omitempty"`
	Temperature  float64           `yaml:"temperature,omitempty" json:"temperature,omitempty"`
	// Tools, when omitted (nil), does not restrict tool visibility for this step.
	// When present (including an empty YAML list), execution uses a whitelist: only the
	// listed tools are exposed; empty list means no tools (text-only).
	Tools *[]string `yaml:"tools,omitempty" json:"tools,omitempty"`
	Interrupt    bool              `yaml:"interrupt,omitempty" json:"interrupt,omitempty"`
	// Variables is an optional map of extra key-value pairs available as
	// {{.vars.KEY}} in the prompt template.
	Variables map[string]string `yaml:"variables,omitempty" json:"variables,omitempty"`
}

// ReturnDef describes how to aggregate the final workflow output.
type ReturnDef struct {
	Template  string            `yaml:"template,omitempty" json:"template,omitempty"`
	SaveFiles map[string]string `yaml:"save_files,omitempty" json:"save_files,omitempty"`
}

// SchemaDoc is a concise reference of the YAML schema, embedded into generator
// system prompts so the LLM knows the exact expected output format.
const SchemaDoc = `
Workflow YAML Schema:

workflow:
  name: string           # required, workflow identifier
  description: string    # required, what this workflow does
  input:                 # optional, describes expected input keys
    schema:
      key: "string"      # key name and type hint
  stages:                # required, list of stages
    - id: string         # required, stage unique identifier
      description: string
      parallel: false    # true = agents run concurrently
      depends_on: []     # list of preceding stage ids
      agents:            # list of agents in this stage
        - id: string
          system_prompt: string
          prompt: string           # Go template, can use {{.input.X}} and {{.stages.Y.agents.Z.output}}
          model: string
          max_tokens: int
          temperature: float
          tools: [tool_name]     # omit = all eligible tools; [] = none; ["a"] = whitelist
          interrupt: false         # pause for human approval after this agent
          variables:               # extra template variables
            key: value
  return:                # optional, how to aggregate final output
    template: |
      # Go template for the final result
    save_files:            # optional, files to write on completion
      filename.md: |
        # Go template for file content

Template variables available in prompt/return:
  {{.input.KEY}}                         — input variable
  {{.stages.STAGE_ID.agents.AGENT_ID.output}} — output of a preceding agent
  {{.vars.KEY}}                          — extra variable from AgentDef.Variables
`

// AgentOutputKey returns the state key where an agent's output is stored.
func AgentOutputKey(stageID, agentID string) string {
	return fmt.Sprintf("stages.%s.agents.%s.output", stageID, agentID)
}
