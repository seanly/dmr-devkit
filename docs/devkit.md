# devkit — minimal agent wiring

The [`devkit`](../devkit) package assembles a runnable [`agent.Agent`](../agent/agent.go) with the same core pieces as the **dmr** CLI: `TapeManager`, `ChatClient`, optional [`agent.Hooks`](../agent/hooks.go) (for example `*plugin.Manager` from **dmr** for policy + prompt fragments), tool executor wiring, and default system prompt composition. The `devkit` module does **not** import **dmr**; pass an [`agent.Hooks`](../agent/hooks.go) implementation from your `main` when you need plugin behavior. Use it when you want a **small Go binary or experiment** without loading `~/.dmr/config.toml` or the full CLI stack.

## When to use what

| Approach | Best for |
|----------|----------|
| **devkit** | Embedding a tool-using agent in your own program; quick prototypes; tests that need the full agent loop with an in-memory tape. |
| **dmr** (`cmd/dmr`) | Production setups: config file, workspace, tape driver (file/sqlite/pg), packaged plugins, webserver/cron, etc. |
| **`republic`** | Tape-first **LLM client** only (`Chat`, `Stream`, `If`, `Classify`) — no multi-step agent loop or plugin hooks. See [examples/basic_demo.go](../examples/basic_demo.go). |

devkit is **not** Google ADK; it only reduces wiring on top of this module. Remote ADK agents can still be used via the A2A plugin in a full **dmr** deployment.

## API sketch

- `devkit.Build(ctx, devkit.Options{...})` → `*devkit.Kit` with `Agent`, `Client`, `TapeManager`, `Hooks`, `Store`.
- Default tape name: `devkit.DefaultTapeName` (`"default"`).
- Tape: omit store fields for **in-memory** tape; set `TapeStore` or `TapeConfig` ([`tape.StoreConfig`](../tape/factory.go)) for persistence.
- Optional `Hooks` — typically `*plugin.Manager` from **dmr** (implements [`agent.Hooks`](../agent/hooks.go)) for the same policy/tool/prompt behavior as the CLI; omit for a minimal no-op loop. Use `OnClose` to run shutdown (for example `Manager.ShutdownAll`) when you allocate a manager outside devkit.
- `Kit.Close(ctx)` runs `Options.OnClose` when set.

## Exposing the agent as an A2A server

[`a2aserver`](../a2aserver/server.go) registers the A2A well-known agent card and JSON-RPC invoke handler on an [`http.ServeMux`](https://pkg.go.dev/net/http#ServeMux). Each `SendMessage` maps to one `Runner.Run` (same shape as [`agent.Agent.Run`](../agent/loop.go)), returning [`*agent.RunResult`](../agent/runtime.go). By default **each A2A Task** uses a **distinct tape** (`TapeModeAuto`: flat name `prefix_taskId`) so concurrent clients do not share history; use `TapeModeFixed` + `TapeName` only when you accept shared-tape semantics.

Set **PublicInvokeURL** to the absolute URL clients use for JSON-RPC POST (must match **MountPath** on the public host). Behind NAT or TLS, set `A2A_PUBLIC_INVOKE_URL` as in [examples/a2a_devkit_server/main.go](../examples/a2a_devkit_server/main.go). Callers can use the **dmr** A2A plugin with `base_url` pointing at this server's origin.

## Examples

- [examples/devkit_agent/main.go](../examples/devkit_agent/main.go) — local agent loop with tools.
- [examples/a2a_devkit_server/main.go](../examples/a2a_devkit_server/main.go) — devkit agent plus A2A HTTP server.
