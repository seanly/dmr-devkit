# DMR Devkit Skills

Development skills for building agents with [DMR Devkit](https://github.com/seanly/dmr-devkit). Install into any coding agent to get structured guidance for the full devkit development lifecycle.

Canonical copies live in this repo under [`docs/skills/`](./). They are meant to track the **`devkit` / `agent` API in this module** (e.g. `Hooks` + `OnClose`, not legacy `Options.Plugins`).

## Install (Claude Code)

From the **dmr-devkit** repository root:

```bash
make install-skills           # ~/.claude/skills
# make install-skills-local   # ./.claude/skills in this repo
```

Use `SKILLS_SRC=docs/skills` only if you keep skills elsewhere.

## What is Devkit

`devkit` (module `github.com/seanly/dmr-devkit/devkit`) provides a minimal assembly interface to embed a full-featured LLM Agent into your own Go program without loading config files, CLI, or the full plugin ecosystem. It is the recommended entry point for embedding, prototyping, and testing DMR agents.

## Skills

| Skill | Description |
|-------|-------------|
| `dmr-devkit-workflow` | Development lifecycle, code preservation rules, model selection. **Always active — entrypoint.** |
| `dmr-devkit-scaffold` | Project scaffolding: create, enhance, upgrade Go projects with devkit |
| `dmr-devkit-agent` | Devkit Go API reference: Build, Options, Kit, Agent.Run |
| `dmr-devkit-tools` | Tool development: ToolSpec, Handler, ToolContext, dynamic descriptions |
| `dmr-devkit-plugins` | Plugin development: Plugin interface, hooks, config binding |
| `dmr-devkit-orchestration` | Workflow orchestration: Sequential, Parallel, Graph, Loop, Interrupt |
| `dmr-devkit-a2a` | Expose devkit agents as A2A JSON-RPC services |
| `dmr-devkit-observability` | Observability: tape storage backends, event streams, tracing |

## How to Use

When the user wants to develop a devkit agent, load `/dmr-devkit-workflow` first. It contains the required development phases and cross-references to other skills. Do NOT skip it.

## Prerequisites

- Go 1.23+
- Environment variables: `AI_MODEL`, `AI_API_KEY` (or OAuth2 credentials)
