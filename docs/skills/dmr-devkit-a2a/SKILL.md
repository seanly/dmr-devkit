---
name: dmr-devkit-a2a
description: >
  This skill should be used when the user wants to "expose an agent as A2A",
  "A2A service", "remote agent", "agent-to-agent", "agent card",
  "JSON-RPC agent", or needs guidance on exposing DMR devkit agents via the
  A2A protocol.
  Part of the DMR devkit skills suite.
  Covers a2aserver.Mount, Agent Card, configuration, and cross-agent calling.
  Do NOT use for basic agent setup (use dmr-devkit-agent) or workflow
  orchestration (use dmr-devkit-orchestration).
metadata:
  author: seanly
  license: MIT
  version: 0.1.0
  requires:
    go: ">= 1.23"
---

# Devkit A2A Service Exposure Guide

> **Before using this skill**, activate `/dmr-devkit-workflow` first — it contains the required development phases. The agent core logic should be working before exposing it as A2A.

## What is A2A

A2A (Agent-to-Agent) is an open protocol for interoperability between agents. DMR implements A2A via the `a2aserver` package, which exposes a devkit-built agent as an HTTP JSON-RPC service with a well-known Agent Card.

> DMR's A2A is based on `a2a-go` 2.0, which may differ from Google ADK's built-in A2A implementation. Use DMR's `a2aserver` package for community interoperability.

## Quick Start

```go
package main

import (
    "context"
    "log"
    "net/http"
    "os"

    "github.com/seanly/dmr-devkit/a2aserver"
    "github.com/seanly/dmr-devkit/devkit"
)

func main() {
    ctx := context.Background()

    // 1. Build the agent with devkit
    opts := devkit.EnvOptions()
    if opts.APIKey == "" || opts.Model == "" {
        log.Fatal("AI_API_KEY and AI_MODEL are required")
    }
    opts.SystemPromptExtra = "You are reachable via the A2A protocol. Keep replies concise."

    kit, err := devkit.Build(ctx, opts)
    if err != nil {
        log.Fatal(err)
    }
    defer func() { _ = kit.Close(ctx) }()

    // 2. Configure A2A server
    addr := ":8080"
    publicURL := os.Getenv("A2A_PUBLIC_INVOKE_URL")
    if publicURL == "" {
        publicURL = "http://127.0.0.1" + addr + "/invoke"
    }

    mux := http.NewServeMux()
    if err := a2aserver.Mount(mux, a2aserver.Options{
        AgentName:       "my-a2a-agent",
        Description:     "DMR agent exposed via A2A",
        PublicInvokeURL: publicURL,
        MountPath:       "/invoke",
        // Default TapeMode is auto: one DMR tape per A2A Task (flat name prefix_taskId).
    }, kit.Agent); err != nil {
        log.Fatal(err)
    }

    // 3. Start server
    log.Printf("A2A listening on %s", addr)
    log.Printf("Agent Card: http://127.0.0.1%s%s", addr, a2aserver.WellKnownAgentCardPath)
    log.Fatal(http.ListenAndServe(addr, mux))
}
```

Run:

```bash
AI_API_KEY=your-key AI_MODEL=gpt-4o-mini go run .
```

---

## a2aserver.Options

| Field | Required | Description |
|-------|----------|-------------|
| `AgentName` | Yes | Agent identifier |
| `Description` | No | Human-readable description |
| `PublicInvokeURL` | Yes | Absolute URL clients use to invoke this agent |
| `MountPath` | No | JSON-RPC endpoint path, default `/invoke` |
| `TapeMode` | No | `auto` (default) or `fixed` — auto isolates history per A2A Task |
| `TapePrefix` | No | Prefix for auto tape names, default `a2a` |
| `TapeName` | No | **Only when `TapeMode=fixed`:** shared tape, default `default` |
| `DefaultInputModes` | No | Supported input modes |
| `DefaultOutputModes` | No | Supported output modes |

### Port Configuration

The A2A server listen port should be configured under the `[a2a]` config block, not as a CLI flag or embedded in domain-specific stanzas.

**Resolution order (first non-empty wins):**
1. `PORT` environment variable
2. `[a2a].port` (or `[[agents]].a2a.port` for multi-agent configs)
3. Agent-specific default (e.g. `8081`, `8082`, `8083`, `8084`)

**TOML example:**

```toml
[a2a]
port = "8082"
public_invoke_url = "http://acr-agent:8082/invoke"
mount_path = "/invoke"
bearer_token = "${A2A_BEARER_TOKEN}"
```

**Go wiring:**

```go
a2a := mergeA2A(spec.A2A, tomlCfg.A2A)
port := dmrconfig.FirstNonEmpty(os.Getenv("PORT"), a2a.Port, "8082")

addr := ":" + port
log.Printf("A2A listening on %s", addr)
log.Fatal(http.ListenAndServe(addr, handler))
```

> Do not add a `-port` CLI flag. Keep A2A settings centralized under `[a2a]` so that `public_invoke_url`, `mount_path`, `bearer_token`, and `port` are co-located.

---

### Environment Variables

| Variable | Description |
|----------|-------------|
| `PORT` | A2A server listen port (overrides config file) |
| `A2A_PUBLIC_INVOKE_URL` | Public invoke URL. Required when behind NAT, load balancer, or TLS terminator |

---

## Agent Card

The Agent Card is a well-known JSON document describing the agent's capabilities:

```
GET /.well-known/agent-card.json
```

It is served automatically by `a2aserver.Mount`. Clients discover agents via this endpoint.

---

## Calling from Another DMR Instance

Configure the `a2a` plugin in another DMR instance:

```toml
[[plugins]]
name = "a2a"
enabled = true
[plugins.config]
base_url = "http://your-server:8080"
```

Then use the `a2a` tool in conversations to call the remote agent.

---

## Production Checklist

- [ ] Use HTTPS in production (terminate TLS at reverse proxy / load balancer)
- [ ] Set `A2A_PUBLIC_INVOKE_URL` to the public-facing URL
- [ ] Configure authentication if needed (see **Authentication** section below)
- [ ] Set appropriate resource limits (CPU, memory) for the Go process
- [ ] Use persistent tape storage (not in-memory) if conversation history must survive restarts

## Authentication

A2A protocol does not mandate auth — add it in your HTTP handler wrapper.

### Bearer Token Authentication

Implement token auth as middleware wrapping the HTTP mux:

```go
func bearerAuthMiddleware(handler http.Handler, validToken string) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        auth := r.Header.Get("Authorization")
        if auth == "" {
            http.Error(w, "Authorization header required", http.StatusUnauthorized)
            return
        }
        if !strings.HasPrefix(auth, "Bearer ") {
            http.Error(w, "Bearer token required", http.StatusUnauthorized)
            return
        }
        token := strings.TrimPrefix(auth, "Bearer ")
        if token != validToken {
            http.Error(w, "Invalid token", http.StatusUnauthorized)
            return
        }
        handler.ServeHTTP(w, r)
    })
}
```

Usage:

```go
bearerToken := os.Getenv("A2A_BEARER_TOKEN")

mux := http.NewServeMux()
if err := a2aserver.Mount(mux, opts, kit.Agent); err != nil {
    log.Fatal(err)
}

var handler http.Handler = mux
if bearerToken != "" {
    handler = bearerAuthMiddleware(mux, bearerToken)
    log.Printf("Bearer authentication enabled")
}

log.Fatal(http.ListenAndServe(":"+port, handler))
```

Read the token from config:

```toml
[agents.a2a]
bearer_token = "${A2A_BEARER_TOKEN}"
```

```go
bearerToken := ""
if spec != nil && spec.A2A.BearerToken != "" {
    bearerToken = spec.A2A.BearerToken
}
```

### Important: mergeA2A must merge BearerToken

When using multi-agent configs with a global `[a2a]` fallback, your `mergeA2A` helper **must** propagate `BearerToken` from the global config. Otherwise the token is silently dropped and authentication is disabled.

```go
func mergeA2A(agent, file A2A) A2A {
    out := agent
    if out.PublicInvokeURL == "" {
        out.PublicInvokeURL = file.PublicInvokeURL
    }
    if out.MountPath == "" {
        out.MountPath = file.MountPath
    }
    if out.BearerToken == "" {          // <-- REQUIRED
        out.BearerToken = file.BearerToken
    }
    if out.Port == "" {
        out.Port = file.Port
    }
    return out
}
```

> **Bug pattern:** Forgetting to merge `BearerToken` causes the agent to start without auth, making all clients appear to "work" even when sending wrong/missing tokens. Only clients that correctly enforce `Bearer ` prefix (like other DMR agents) will see `401 Unauthorized`.

### Other Auth Patterns

- **Reverse proxy / API gateway**: Terminate auth at the proxy (AWS ALB, nginx, Kong)
- **mTLS**: Terminate client certificates at the load balancer
- **Network isolation**: Deploy in a private VPC with no public internet access

---

## Security Notes

- The A2A endpoint itself does not implement authentication — add it at the infrastructure layer (IAP, API gateway, mTLS)
- Do not expose `A2A_PUBLIC_INVOKE_URL` with internal-only addresses in production
- Keep API keys in environment variables, never in source code

---

## Troubleshooting

| Issue | Cause | Fix |
|-------|-------|-----|
| Agent Card 404 | Wrong well-known path | Verify `a2aserver.WellKnownAgentCardPath` is mounted |
| Client cannot invoke | Wrong `PublicInvokeURL` | Set `A2A_PUBLIC_INVOKE_URL` to the externally reachable URL |
| Tape history lost on restart | Using in-memory store | Configure `TapeConfig` with SQLite/PostgreSQL |
| Model not found errors | Env vars not set | Ensure `AI_MODEL` and `AI_API_KEY` are in the server environment |
| `401 Unauthorized` from some agents only | `mergeA2A` missing `BearerToken` merge | Check `mergeA2A` propagates `BearerToken` from global `[a2a]` to per-agent `spec.A2A` |

---

## Related Skills

- `/dmr-devkit-workflow` — Development workflow and operational rules
- `/dmr-devkit-agent` — Agent API quick reference
- `/dmr-devkit-observability` — Persistent storage configuration
