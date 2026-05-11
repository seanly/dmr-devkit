---
name: dmr-devkit-scaffold
description: >
  This skill should be used when the user wants to "create a devkit project",
  "start a new DMR agent project", "build me a new agent", "init a Go agent",
  "set up devkit", or "scaffold an agent".
  Part of the DMR devkit skills suite.
  Covers Go module initialization, dependency setup, template selection, and
  project structure conventions.
  Do NOT use for writing agent code (use dmr-devkit-agent) or workflow
  orchestration (use dmr-devkit-orchestration).
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

# Devkit Project Scaffolding Guide

> **Requires:** Go 1.23+, environment variables `AI_MODEL` and `AI_API_KEY`.

Use standard Go tooling to create new devkit agent projects or enhance existing ones with the devkit structure.

---

## Prerequisite: Clarify Requirements (MANDATORY for new projects)

**Before scaffolding a new project, load `/dmr-devkit-workflow` and complete Phase 0** — clarify the user's requirements before running any commands. Ask what the agent should do, what tools/APIs it needs, and whether they want a prototype or full deployment.

---

## Step 1: Choose a Template

Mapping user choices to the correct starting template:

| User Need | Starting Point | Key Example |
|-----------|---------------|-------------|
| Single agent with tools | `examples/devkit_agent` | Basic agent + custom tool |
| Multi-step pipeline | `examples/workflow_agent` | Sequential / Parallel |
| Condition-based routing | `examples/workflow_devops` | Graph + router |
| Human approval required | `examples/workflow_interrupt` | Interrupt + Resume |
| Remote service (A2A) | `examples/a2a_devkit_server` | HTTP + A2A protocol |

---

## Step 2: Create or Enhance the Project

### Create a New Project

```bash
# 1. Create project directory
mkdir my-agent && cd my-agent

# 2. Initialize Go module
go mod init my-agent

# 3. Add devkit dependency
go get github.com/seanly/dmr-devkit@latest

# 4. Create main.go from the chosen template
#    (Copy the relevant example from examples/ and adapt)
```

> **Private Repository Note:** If you depend on the full **product** module (`github.com/seanly/dmr`, CLI/plugins), it may be private. Configure Go before `go get`:

If you only use **dmr-devkit** (public), you do not need `GOPRIVATE` for devkit.

> ```bash
> # Mark product repo as private (bypass public proxy / checksum db). Devkit is public.
> go env -w GOPRIVATE=github.com/seanly/dmr
> go env -w GONOSUMDB=github.com/seanly/dmr
> go env -w GONOPROXY=github.com/seanly/dmr
>
> # Ensure Git uses SSH for authentication (recommended)
> git config --global url."git@github.com:seanly/".insteadOf "https://github.com/seanly/"
>
> # Verify access to the private product module
> git ls-remote git.com:seanly/dmr.git HEAD
> ```
> **CI / Container environments:** set `GOPRIVATE`, `GONOSUMDB`, and `GONOPROXY` as environment variables instead.

**Constraints:**
- Do NOT create nested `go.mod` files — one module per project
- Module name should be lowercase, use `-` as separator if needed
- For embedding agents with **devkit**, use `github.com/seanly/dmr-devkit`
- For the full **CLI + plugins** product, use `github.com/seanly/dmr` (often private)

### Enhance an Existing Project

If the user has an existing Go project and wants to add devkit:

```bash
# From the project root
go get github.com/seanly/dmr-devkit@latest
go mod tidy
```

> If `go get` fails with authentication errors, ensure private module config is applied (see **Private Repository Note** under *Create a New Project* above).

Then add a new `cmd/agent/main.go` (or adapt existing main) using the devkit `Build` pattern.

### Project Structure Convention

A well-structured devkit project:

```
my-agent/
├── cmd/
│   └── myagent/
│       └── main.go        # Entry point: devkit.Build + agent.Run
│       └── prompts/       # System prompt .md files (use //go:embed)
│       └── config.go      # TOML config structs + merge logic
├── pkg/
│   ├── tool/              # Custom tool implementations
│   └── ...                # Other domain packages
├── internal/
│   └── dmrconfig/         # LLM resolve, env var helpers
├── bin/                   # Build output (gitignored)
├── data/                  # Runtime data (gitignored)
├── go.mod
├── go.sum
├── .env                   # Environment variables (gitignored)
├── .gitignore
├── Makefile
├── Dockerfile
├── docker-compose.yml
├── config.toml.example
└── README.md
```

#### Makefile (standard targets)

```makefile
.PHONY: build run clean test tidy fmt vet docker docker-push

build:
	go build -o bin/myagent ./cmd/myagent

run: build
	./bin/myagent

clean:
	rm -rf bin/

test:
	go test ./...

tidy:
	go mod tidy

fmt:
	go fmt ./...

vet:
	go vet ./...

docker:
	docker build -t myagent:latest .

docker-push:
	docker tag myagent:latest $(IMAGE_TAG)
	docker push $(IMAGE_TAG)
```

#### .gitignore

```
# Binaries
bin/

# SQLite database
data/*.db

# Data directory
data/*
!data/_example.json

# Environment (contains secrets)
.env

# IDE
.idea/
.vscode/
*.swp

# OS
.DS_Store

# Go
go.work.sum

# Local config
config.toml
```

#### Dockerfile (multi-stage)

```dockerfile
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

# Optional: copy a private dmr checkout if you use `replace github.com/seanly/dmr => ...`
COPY dmr /src/dmr

COPY go.mod go.sum ./

# Point replace at the copied path inside the image (adjust if your replace line differs)
RUN sed -i 's|replace github.com/seanly/dmr => ../dmr|replace github.com/seanly/dmr => /src/dmr|' go.mod

RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags "-s -w" -o /app/myagent ./cmd/myagent/

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/myagent /usr/local/bin/myagent
ENTRYPOINT ["myagent"]
```

#### docker-compose.yml

```yaml
services:
  myagent:
    build: .
    container_name: myagent
    ports:
      - "8080:8080"
    environment:
      - AI_API_KEY=${AI_API_KEY}
      - AI_API_BASE=${AI_API_BASE:-https://api.openai.com/v1}
      - AI_MODEL=${AI_MODEL:-gpt-4o-mini}
      - DMR_VERBOSE=${DMR_VERBOSE:-0}
      - A2A_PUBLIC_INVOKE_URL=${A2A_PUBLIC_INVOKE_URL:-http://localhost:8080/invoke}
    volumes:
      - ./data:/data
      - ./config.toml:/config/config.toml:ro
    working_dir: /config
    command: ["-config", "/config/config.toml"]
```

#### config.toml.example

```toml
# my-agent — DMR-style TOML only (no YAML).
# Copy to config.toml. Strings support ${VAR} expansion from the environment.

workspace = "."
verbose = 1

[[models]]
name = "default"
model = "gpt-4o-mini"
api_key = "${AI_API_KEY}"
api_base = "${AI_API_BASE}"
default = true

# --- Single agent via [[agents]] (recommended) ---
[[agents]]
name = "primary"
model = "default"
description = "My agent description"

[agents.agent]
max_steps = 20

[agents.a2a]
# public_invoke_url = "https://myagent.example.com/invoke"
# mount_path = "/invoke"
# bearer_token = "${A2A_BEARER_TOKEN}"

# [agents.tape]
# driver = "file"
# dir = "./var/dmr-tape"

# [agents.tape_timezone]
# Asia/Shanghai
```

#### Embedding System Prompts

Use `//go:embed` to load prompt files at compile time:

```go
//go:embed prompts/my_agent.md
var myAgentPrompt string

opts.SystemPromptBase = myAgentPrompt
```

Directory layout:
```
cmd/myagent/
├── main.go
└── prompts/
    └── my_agent.md
```

#### Config Struct Pattern

Define TOML config structs with merge helpers for multi-agent support:

```go
type TomlConfig struct {
    Workspace string `toml:"workspace"`
    Verbose  *int   `toml:"verbose"`
    Models   []TomlModel
    Agents   []AgentSpec
    Agent    AgentPolicy `toml:"agent"`
    A2A      A2A         `toml:"a2a"`
    Tape     Tape        `toml:"tape"`
}

type AgentSpec struct {
    Name        string `toml:"name"`
    Model       string `toml:"model"`
    Description string `toml:"description"`
    AgentName   string `toml:"agent_name"`
    A2A         A2A    `toml:"a2a"`
    Agent       AgentPolicy
    Tape        Tape
}
```

For small prototypes, all code in `main.go` is acceptable.

### Required Project Files

Every scaffolded agent project **MUST** include:

| File | Purpose |
|------|---------|
| `README.md` | Project documentation: features, quick start, config, tools, A2A endpoints, CLI usage |
| `config.toml.example` | TOML config template with all options documented (copy to `config.toml`) |

`README.md` should include:
- Brief description of the agent
- Features list
- Quick start commands
- Configuration说明
- Environment variables
- Available tools
- A2A endpoints (if exposed)
- CLI mode usage (if supported)
- Docker / docker-compose instructions

`config.toml.example` should include:
- All `[[models]]` options
- All `[[agents]]` sections
- All fallback sections (`[agent]`, `[a2a]`, `[tape]`)
- Comments explaining each option

---

## Step 3: Environment Setup

Create a `.env` file (and add it to `.gitignore`):

```bash
AI_MODEL=gpt-4o-mini
AI_API_KEY=your-api-key
# AI_API_BASE=https://api.openai.com/v1  # optional
```

Load it with:

```bash
export $(cat .env | xargs)
```

Or use a tool like `godotenv` if you prefer loading from code.

---

## Step 4: Verify Scaffold

```bash
# Should compile without errors
go build ./...

# Should run (will fail at runtime if API key is missing — that's expected)
go run .
```

If `go build ./...` fails:
- Check Go version: `go version` (must be >= 1.23)
- Check module path: `go list -m`
- Check DMR is in go.mod: `grep dmr go.mod`

---

## Critical Rules

- **NEVER skip requirements clarification** — load `/dmr-devkit-workflow` Phase 0 before scaffolding
- **NEVER hardcode API keys** in source code — always use env vars or secure storage
- **NEVER commit `.env` files** — add to `.gitignore`
- **Always run `go mod tidy`** after adding/removing dependencies
- **Start with the simplest template** matching the user's need — add complexity later
- **Every project MUST have `README.md` and `config.toml.example`** — these are required deliverables, not optional

---

## Troubleshooting

| Issue | Solution |
|-------|----------|
| `go: module github.com/seanly/dmr-devkit@latest found, but does not contain package` | Check Go version (`go version`) — must be >= 1.23 |
| `go mod tidy` hangs / auth failure on `go get` | Check `GOPRIVATE` setting and SSH key access to `github.com/seanly/dmr`. See **Private Repository Note** above. |
| `undefined: devkit.Build` | Ensure `go get github.com/seanly/dmr-devkit@latest` succeeded |
| Multiple `go.mod` in subdirectories | Remove nested `go.mod` — devkit projects use a single module |

---

## Related Skills

- `/dmr-devkit-workflow` — Development workflow, coding guidelines, and the full lifecycle
- `/dmr-devkit-agent` — Devkit Go API quick reference for writing agent code
- `/dmr-devkit-tools` — Tool development patterns
- `/dmr-devkit-orchestration` — Workflow orchestration guide
