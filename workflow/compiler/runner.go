package compiler

import (
	"context"

	"github.com/seanly/dmr-devkit/devkit"
	"github.com/seanly/dmr-devkit/workflow"
	"github.com/seanly/dmr-devkit/workflow/dsl"
)

// Run executes the compiled workflow with the given input.
// It injects the input into workflow state, runs the graph, and then renders
// the return template.
func (cw *CompiledWorkflow) Run(ctx context.Context, kit *devkit.Kit, input map[string]any) (*workflow.Result, error) {
	wctx := workflow.NewContext()
	wctx.State["input"] = input

	res, err := kit.RunWorkflow(ctx, cw.Graph, input)
	if err != nil && !workflow.IsInterrupt(err) {
		return res, err
	}

	// Render return template.
	if cw.ReturnT != nil {
		if rendered, rerr := cw.RenderReturn(wctx.State); rerr == nil {
			if res == nil {
				res = &workflow.Result{Output: rendered}
			} else {
				res.Output = rendered
			}
		}
	}

	return res, err
}

// RunStream executes the compiled workflow and yields events for real-time observation.
func (cw *CompiledWorkflow) RunStream(ctx context.Context, kit *devkit.Kit, input map[string]any) (*workflow.Result, error) {
	wctx := workflow.NewContext()
	wctx.State["input"] = input

	// Collect events.
	var lastResult *workflow.Result
	for ev, err := range kit.RunWorkflowStream(ctx, cw.Graph, input) {
		if err != nil {
			return nil, err
		}
		if ev.Type == workflow.EventTypeWorkflowEnd && ev.Result != nil {
			lastResult = ev.Result
		}
		_ = ev
	}

	// Render return template.
	if cw.ReturnT != nil && lastResult != nil {
		if rendered, rerr := cw.RenderReturn(wctx.State); rerr == nil {
			lastResult.Output = rendered
		}
	}

	return lastResult, nil
}

// ExtractYAML pulls a YAML code block from raw LLM output.
func ExtractYAML(raw string) string {
	const fence = "```yaml"
	if i := findFence(raw, fence); i >= 0 {
		raw = raw[i+len(fence):]
		if j := findFence(raw, "```"); j >= 0 {
			return trim(raw[:j])
		}
	}
	if i := findFence(raw, "```"); i >= 0 {
		raw = raw[i+3:]
		if j := findFence(raw, "```"); j >= 0 {
			return trim(raw[:j])
		}
	}
	return trim(raw)
}

func findFence(s, fence string) int {
	for i := 0; i <= len(s)-len(fence); i++ {
		if s[i:i+len(fence)] == fence {
			return i
		}
	}
	return -1
}

func trim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

// SystemPromptForGenerator returns the system prompt used by the workflow
// generator agent.
func SystemPromptForGenerator() string {
	return `You are a Workflow Design Expert. Your job is to convert a user's natural language request into a valid workflow YAML definition.

Rules:
1. Output ONLY valid YAML. Do not include explanations or markdown outside the YAML block.
2. Every stage must have a unique id and description.
3. If a stage contains multiple agents that are independent, set parallel: true.
4. If a stage needs output from previous stages, list their ids in depends_on.
5. In prompt fields, use Go template syntax to reference prior results:
   - {{.input.KEY}} for input variables
   - {{.stages.STAGE_ID.agents.AGENT_ID.output}} for agent outputs
6. If a human should review an agent's output before continuing, set interrupt: true.
7. Available tools the agent can use: web_search, file_read, file_write, exec, git_diff

` + dsl.SchemaDoc
}
