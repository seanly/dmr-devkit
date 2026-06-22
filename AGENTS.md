# AGENTS.md — DMR Devkit Progressive Disclosure Guide

> **dmr-devkit** is an embeddable Go LLM Agent runtime: agent loop, tool system, tape audit trail, workflow orchestration, A2A server, LLM client, and OpenAI-compatible provider — without the full CLI or plugin ecosystem.

---

## Reading Guide (Progressive Disclosure)

This document uses a **progressive disclosure** structure. Choose your reading depth based on current needs:

| Level | Time | Best For | Document |
|-------|------|----------|----------|
| **L1 — Core Overview** | 5 min | First contact; quick architecture understanding | [docs/agents/01-overview.md](docs/agents/01-overview.md) |
| **L2 — Module Deep Dive** | 15–30 min | Building agents, adding tools, orchestrating workflows | [docs/agents/02-devkit.md](docs/agents/02-devkit.md) onward |
| **L3 — Advanced Topics** | As needed | Performance tuning, custom storage, contributing code | [docs/agents/08-compact.md](docs/agents/08-compact.md) onward |

> **Rule**: When working on tasks related to this project, AI Agents **must read L1 first**, then select the appropriate L2 document based on task type. Only enter L3 for specific problems (performance tuning, storage customization).

---

## L1 — Core Overview (Required)

### One-Liner

`dmr-devkit` lets you embed a fully-featured LLM Agent with **no config files, no CLI** — just tens of lines of Go.

### Relationship with dmr

| Project | Purpose | Dependency |
|---------|---------|------------|
| **dmr-devkit** (this repo) | Embeddable Agent runtime library | Does not depend on dmr |
| **dmr** (private CLI) | Production deployment: config, Web, Cron, packaged plugins | Depends on this module |

### Core Architecture (4 Components)

```
┌─────────────────────────────────────────────────────────────┐
│                         Kit (devkit wiring)                 │
│  ┌─────────┐  ┌─────────┐  ┌──────────┐  ┌─────────────┐   │
│  │  Agent  │  │ Client  │  │ TapeMgr  │  │ Hooks/Plugins│  │
│  │ (loop)  │  │(LLM comm)│  │ (storage)│  │ (extension) │   │
│  └────┬────┘  └────┬────┘  └────┬─────┘  └──────┬──────┘   │
│       │            │            │               │          │
│       └────────────┴────────────┴───────────────┘          │
│                         devkit.Build                       │
└─────────────────────────────────────────────────────────────┘
```

- **Agent** (`agent/`): Multi-turn conversation loop — LLM call → tool execution → result feedback → repeat
- **Client** (`client/`): LLM communication layer, streaming/non-streaming, OpenAI-compatible protocol
- **TapeManager** (`tape/`): Persistent audit trail storage (memory/file/SQLite/PostgreSQL)
- **Hooks** (`agent/hooks.go`): Extension point; dmr's `plugin.Manager` injects through here

### Key Design Decisions

1. **Minimal dependencies** — `devkit.Build` only needs `Model` + `APIKey`
2. **Tape isolation** — Each conversation uses an independent tape, concurrency-safe
3. **Tool discovery** — Non-core tools are lazily loaded, reducing context footprint
4. **Auto-compaction** — Automatically summarizes history when context exceeds thresholds
5. **A2A interoperability** — Based on `a2a-go` v2 protocol; interoperable with the community, not Google ADK

### Code Comparison

```go
// Minimal runnable Agent (~20 lines of effective code)
kit, _ := devkit.Build(ctx, devkit.Options{
    Model:  "gpt-4o-mini",
    APIKey: os.Getenv("AI_API_KEY"),
    Tools:  []*tool.Tool{{ /* ... */ }},
})
res, _ := kit.Agent.Run(ctx, "default", "Hello", 0)
```

---

## L2 — Module Deep Dive (Read as Needed)

### Getting Started & Wiring

- **[docs/agents/02-devkit.md](docs/agents/02-devkit.md)** — `devkit.Build`, `Options`, `Kit` complete guide
  - Environment configuration, auth methods (APIKey / OAuth2)
  - Storage backend selection (memory/file/database)
  - System prompt customization
  - Complete example code

### Agent Core

- **[docs/agents/03-agent-loop.md](docs/agents/03-agent-loop.md)** — Agent loop deep dive
  - Execution flow: Run → LLM → Tool → Result → Loop
  - Max step limits and anti-loop mechanisms
  - Tool whitelist and run modes
  - Subagent delegation
  - Reactive handoff

- **[docs/agents/04-tools.md](docs/agents/04-tools.md)** — Tool system
  - ToolSpec / Handler / ToolContext
  - Tool groups: core / extended / mcp
  - Dynamic descriptions and lazy loading
  - Tool result truncation and externalization
  - Built-in tools (toolSearch, vision, etc.)

- **[docs/agents/05-workflow.md](docs/agents/05-workflow.md)** — Workflow orchestration
  - Sequential: sequential execution, previous output feeds next input
  - Parallel: parallel branches, result aggregation
  - Graph: directed graph with conditional branching and loops
  - Loop: loop nodes with interrupt and resume support
  - devkit integration: `kit.AsAgentNode`, dedicated tape isolation

### Storage & Observability

- **[docs/agents/06-tape.md](docs/agents/06-tape.md)** — Tape storage system
  - Entry types: message, tool_call, tool_result, anchor, event
  - Context window and anchor mechanism
  - Storage backends: Memory, File, SQLite (with FTS5), PostgreSQL
  - Query and export

- **[docs/agents/07-a2a.md](docs/agents/07-a2a.md)** — A2A service exposure
  - Agent Card and JSON-RPC endpoint
  - Tape modes: per-Task isolation vs fixed shared
  - Streaming output (SSE)
  - Interoperability with dmr A2A plugin

- **[docs/agents/08-plugins.md](docs/agents/08-plugins.md)** — Plugins and extensions
  - `agent.Hooks` interface and lifecycle
  - Capabilities model (CapHTTP, etc.)
  - Tool registration hooks
  - System prompt fragment injection
  - Integration with dmr `plugin.Manager`

---

## L3 — Advanced Topics (Read as Needed)

- **[docs/agents/09-compact.md](docs/agents/09-compact.md)** — Context compaction and optimization
  - Preemptive compaction
  - Prompt compaction strategies
  - Micro-compaction and tool result externalization
  - Token estimation and threshold configuration

- **[docs/agents/10-internals.md](docs/agents/10-internals.md)** — Internal implementation details
  - Package dependency graph and interface boundaries
  - Tape serialization format
  - Concurrency model and locking strategy
  - Testing strategy and mocking approaches

---

## Quick Reference

### Environment Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `AI_MODEL` | Model ID | `gpt-4o`, `claude-sonnet-4-6` |
| `AI_API_KEY` | API key | `sk-...` |
| `AI_API_BASE` | Custom API base URL | `https://api.example.com/v1` |

### Key Entry Points

| Package | Key Type/Function | Purpose |
|---------|-------------------|---------|
| `devkit` | `Build(ctx, opts)` → `*Kit` | Wire up Agent |
| `agent` | `Agent.Run(tape, prompt, 0)` | Execute conversation |
| `workflow` | `Sequential`, `Parallel`, `Graph` | Orchestrate multi-step tasks |
| `tool` | `Tool{Spec, Handler}` | Define callable tools |
| `tape` | `TapeManager`, `TapeStore` | Store conversation history |
| `a2aserver` | `Mount(mux, opts, runner)` | Expose A2A HTTP service |

Filesystem-first agent authoring (`agent/` layout, SO tools, webhook channels) lives in the sibling project **[dmr-forge](../dmr-forge)** — use `forge run`, `forge serve`, `forge info`.

### Example Code Locations

- `examples/devkit_agent/` — Minimal agent + tools
- `examples/workflow_agent/` — Sequential + Parallel workflows
- `examples/a2a_devkit_server/` — A2A HTTP service
- `examples/mcp_agent/` — MCP tool integration
- `examples/basic_demo.go` — Pure LLM client (`republic` package)

---

## Existing Documentation Index

This project already has the following documentation. Their relationship with AGENTS.md:

| Document | Content | AGENTS.md Level |
|----------|---------|-----------------|
| `README.md` | Project intro, installation, skill installation | L0 |
| `docs/README.md` | Documentation index | L0 |
| `docs/devkit.md` | devkit English overview | L1–L2 |
| `docs/devkit/README.md` | devkit Chinese docs entry | L1–L2 |
| `docs/devkit/*.md` | Per-topic detailed Chinese docs | L2–L3 |
| `docs/skills/README.md` | Claude Code Skills index | Development aid |
| `docs/cwd-management.md` | Working directory management | L2 |

> AGENTS.md does not replace the above documents. It provides AI Agents with a **reading-order-constrained** progressive navigation. Human developers can still read detailed docs directly under `docs/devkit/`.
