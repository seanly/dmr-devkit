---
name: dmr-devkit-workflow
description: >
  This skill should be used when the user wants to "develop an agent with devkit",
  "build a DMR agent", "run a devkit agent", "debug devkit code", "test an agent",
  "create a workflow", "deploy an A2A service", or needs the DMR devkit development
  lifecycle and coding guidelines.
  Always active — provides the full workflow (scaffold, build, orchestrate, expose, observe),
  code preservation rules, model selection guidance, and troubleshooting.
metadata:
  author: seanly
  license: MIT
  version: 0.1.0
  requires:
    go: ">= 1.23"
    env:
      - AI_MODEL
      - AI_API_KEY
---

# Devkit Development Workflow & Guidelines

> **STOP — Do NOT write code yet.** If no project exists, scaffold first. If the user already has code, verify it follows devkit conventions. Run `go list -m` to check if a Go module exists. Skipping this leads to missing workflow templates, storage config, and project conventions.

**DMR Devkit** is a minimal assembly interface for embedding a full-featured LLM Agent into your Go program without loading config files, CLI, or the full **dmr** plugin ecosystem. It ships in the [`github.com/seanly/dmr-devkit`](https://github.com/seanly/dmr-devkit) module (`devkit` package).

> Requires: Go 1.23+, `AI_MODEL` and `AI_API_KEY` environment variables (or OAuth2 `TokenURL` + `ClientID` + `ClientSecret`).

## Session Continuity & Skill Cross-References

Re-read the relevant skill **before** each phase — not after you've already started and hit a problem. Context compaction may have dropped earlier skill content.

| Phase | Skill | When to load |
|-------|-------|--------------|
| 0 — Understand | — | No skill needed — clarify goals with the user |
| 1 — Study samples | — | Check Notable Samples table below — study matching examples before scaffolding |
| 2 — Scaffold | `/dmr-devkit-scaffold` | Before creating or enhancing a project |
| 3 — Build Agent | `/dmr-devkit-agent` | Before writing agent code — API patterns, Options, Kit |
| 4 — Develop Tools | `/dmr-devkit-tools` | When custom tools are needed |
| 5 — Use Plugins | `/dmr-devkit-plugins` | When extending agent with plugins |
| 6 — Orchestrate | `/dmr-devkit-orchestration` | When multi-step / branching / parallel workflows are needed |
| 7 — Expose A2A | `/dmr-devkit-a2a` | When the agent must be reachable remotely |
| 8 — Observe | `/dmr-devkit-observability` | After building — storage backends, event streams, monitoring |

---

## Setup

If the project does not have a `go.mod`:

```bash
go mod init my-agent
go get github.com/seanly/dmr-devkit@latest
```

### Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `AI_MODEL` | Yes | Model ID, e.g. `gpt-4o`, `claude-sonnet-4-6` |
| `AI_API_KEY` | Yes* | API key (bearer token) |
| `AI_API_BASE` | No | Custom API base URL |

*Or set `TokenURL` + `ClientID` + `ClientSecret` for OAuth2 client_credentials.

---

## Phase 0: Understand

Before writing or scaffolding anything, understand what you're building.

**Always ask:**

1. **What problem will the agent solve?** — Core purpose and capabilities
2. **External APIs or data sources needed?** — Tools, integrations, auth requirements
3. **Does it need workflow orchestration?** — Single-turn agent, or multi-step Sequential/Parallel/Graph pipeline?
4. **Deployment preference?** — Local script, long-running service, or A2A remote service?
5. **Conversation history persistence?** — In-memory (default, lost on restart), or SQLite/PostgreSQL/MySQL?

**Ask based on context:**

- If **multi-step pipeline** mentioned → Which runner? `Sequential`, `Parallel`, `Graph`, or `Loop`?
- If **condition-based routing** mentioned → Use `Graph` with `AddConditionalEdges`
- If **human approval** mentioned → Use `workflow.Interrupt` (Human-in-the-Loop)
- If **remote access by other agents** mentioned → Use A2A protocol (`a2aserver` package)
- If **persistent storage** mentioned → Which backend? `sqlite`, `postgres`, `mysql`, or file?

Once you have the user's answers, proceed to Phase 1.

---

## Phase 1: Study Reference Samples

Ask yourself: is there an example that can help me design this and cut time? Scan the keywords below. Multiple examples can match — study all that are relevant.

```bash
# Examples live in this repo under examples/
# Read the key files, understand the patterns, then apply them.
```

- **`devkit_agent`** — Minimal multi-turn agent with custom tools.
  Keywords: basic, tool, hello, echo, beginner
  Key files: `examples/devkit_agent/main.go`

- **`workflow_agent`** — Sequential and Parallel workflow orchestration.
  Keywords: pipeline, sequential, parallel, multi-step, content generation
  Key files: `examples/workflow_agent/main.go`

- **`workflow_devops`** — Graph workflow with conditional branching (alert severity routing).
  Keywords: router, conditional, branch, classify, devops, incident response
  Key files: `examples/workflow_devops/main.go`

- **`workflow_interrupt`** — Human-in-the-loop with Interrupt and Resume.
  Keywords: approval, hitl, interrupt, resume, human-in-the-loop
  Key files: `examples/workflow_interrupt/main.go`

- **`a2a_devkit_server`** — Expose a devkit agent as an A2A JSON-RPC service.
  Keywords: a2a, remote, server, http, json-rpc, agent-card
  Key files: `examples/a2a_devkit_server/main.go`

> **IMPORTANT — Exit criteria:** After studying a sample, ask yourself: can I apply anything from this example to help me deliver the design? Note what you'll reuse before moving on. Do NOT proceed until you've answered this.

---

## Phase 2: Scaffold (if needed)

Use `/dmr-devkit-scaffold` to create a new project or enhance an existing one. It covers module initialization, dependency setup, and template selection.

Skip this phase if the project already has a `go.mod` with `github.com/seanly/dmr-devkit` — run `go list -m github.com/seanly/dmr-devkit` to check.

---

## Phase 3: Build and Implement

Implement the agent logic:

1. Write/modify code in `main.go` (or your chosen entry point)
2. **Quick smoke test**: Use `AI_API_KEY=... AI_MODEL=... go run .` to verify the agent works after changes
3. Iterate on the implementation based on user feedback

For devkit API patterns and code examples, use `/dmr-devkit-agent`.

> **NEVER write tests that assert on LLM output content** (e.g., checking for keywords in responses, verifying persona, validating tone). LLM outputs are non-deterministic — these tests are flaky by nature. Use smoke tests with `go run` for quick checks.

---

## Phase 4-5: Tools and Plugins (as needed)

If the agent needs custom tools, load `/dmr-devkit-tools` before implementing them.
If the agent needs plugin-based extensibility, load `/dmr-devkit-plugins`.

---

## Phase 6: Orchestrate (as needed)

If the design involves multi-step pipelines, branching, parallelism, or loops, load `/dmr-devkit-orchestration` **before** writing workflow code. It contains the runner decision matrix and critical gotchas.

> **Do NOT skip this skill.** Writing workflow code without understanding `AgentNode`, tape isolation, and router patterns leads to subtle bugs.

---

## Phase 7: Expose A2A (optional)

If the agent should be reachable by other agents, load `/dmr-devkit-a2a` after the core logic is working.

---

## Phase 8: Observe

After building, use observability tools to monitor and persist agent behavior. See `/dmr-devkit-observability` for tape storage backends, event streams, and third-party integrations.

---

# Operational Guidelines for Coding Agents

## Common Shortcuts to Resist

| Shortcut | Why it fails |
|----------|-------------|
| "The user's request is clear enough, no need to clarify" | You're guessing at requirements. Phase 0 exists to confirm intent before scaffolding. |
| "The agent responded correctly in `go run`, so it's done" | One prompt is not a test suite. Edge cases and tool trajectory issues may remain. |
| "I'll use a newer/better model" | Changing the model without being asked violates code preservation (Principle 1) and may break compatibility. |
| "I can skip the scaffold and set up manually" | Manual setup misses dependency resolution, storage config, and conventions. Use `go mod init` + `go get` even for quick experiments. |

## Principle 1: Code Preservation & Isolation

Code modifications require surgical precision — alter only the code segments directly targeted by the user's request and strictly preserve all surrounding and unrelated code.

**Mandatory Pre-Execution Verification:**

Before finalizing any code replacement, verify the following:

1. **Target Identification:** Clearly define the exact lines or expressions to change, based *solely* on the user's explicit instructions.
2. **Preservation Check:** Confirm that all code, configuration values (e.g., `model`, `api_key`, `version`), comments, and formatting *outside* the identified target remain identical.

**Example:**

- **User Request:** "Change the agent's instruction to be a recipe suggester."
- **Incorrect (VIOLATION):**
  ```go
  opts := devkit.EnvOptions()
  opts.Model = "gpt-4o-mini"  // UNINTENDED — model was not requested to change
  opts.SystemPromptExtra = "You are a recipe suggester."
  ```
- **Correct (COMPLIANT):**
  ```go
  opts := devkit.EnvOptions()
  // opts.Model preserved from env
  opts.SystemPromptExtra = "You are a recipe suggester."
  ```

## Principle 2: Execution Best Practices

- **Model Selection — CRITICAL:**
  - **NEVER change the model unless explicitly asked.**
  - When creating NEW agents (not modifying existing), use a stable, well-tested model. The model is read from `AI_MODEL` env var — do not hardcode unless the user asks.

- **Running Go Commands:**
  - Always use `go run .` to execute the program
  - Run `go mod tidy` after adding new dependencies

- **Breaking Infinite Loops:**
  - **Stop immediately** if you see the same error 3+ times in a row
  - **RED FLAGS**: Lock IDs incrementing, names appending v2→v3→v4, "I'll try one more time" repeatedly
  - **When stuck**: Run underlying commands directly (e.g., `go build`) to get clearer errors

- **Troubleshooting:**
  - Check `/dmr-devkit-agent` first — it covers most common patterns
  - When encountering persistent errors, search the DMR repo for similar usage patterns
  - **Build failures:** run `go build ./...` to get the exact compilation error

### Systematic Debugging

When something breaks, follow this sequence:

1. **Reproduce** — Run the exact command that failed. Save the full error output.
2. **Localize** — Narrow the cause: is it the agent code, a tool, the config, or the environment?
3. **Fix one thing** — Change one variable at a time.
4. **Verify** — Rerun the exact reproduction command.
5. **Guard** — If it was a non-obvious bug, document the pattern.

- **Environment Variables:**
  - `.env` files and env var assignments (e.g., `AI_MODEL`, `AI_API_KEY`) are typically required for the agent to function — never remove or modify them unless the user explicitly asks
  - If a `.env` file exists in the project root, treat it as essential configuration

---

## Development Commands

### Setup

| Command | Purpose |
|---------|---------|
| `go mod init my-agent` | Initialize Go module |
| `go get github.com/seanly/dmr-devkit@latest` | Install DMR devkit dependency |
| `go mod tidy` | Clean up dependencies |

### Development

| Command | Purpose |
|---------|---------|
| `AI_API_KEY=... AI_MODEL=... go run .` | Run agent with env vars |
| `go test ./...` | Run all tests |
| `go build ./...` | Build all packages |
| `go vet ./...` | Static analysis |

---

## Related Skills

- `/dmr-devkit-scaffold` — Project creation, requirements gathering, and enhancement
- `/dmr-devkit-agent` — Devkit Go API quick reference
- `/dmr-devkit-tools` — Tool development patterns
- `/dmr-devkit-plugins` — Plugin development patterns
- `/dmr-devkit-orchestration` — Workflow orchestration: Sequential, Parallel, Graph, Loop, Interrupt
- `/dmr-devkit-a2a` — A2A service exposure
- `/dmr-devkit-observability` — Tape storage, event streams, tracing
