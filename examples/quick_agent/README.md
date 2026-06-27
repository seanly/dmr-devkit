# QuickAgent / QuickRun / QuickCrew Demo

This example demonstrates the minimal Facade API added to `pkg/devkit`.

## Run

```bash
export AI_API_KEY=...
export AI_MODEL=gpt-4o-mini   # optional
export AI_API_BASE=...        # optional, for OpenAI-compatible proxies

go run ./examples/quick_agent
```

## What it shows

1. **QuickAgent + QuickRun** — create an agent and run a single prompt in two lines.
2. **QuickAgent with tool** — register a custom tool and let the agent call it.
3. **QuickCrew** — chain two agents in a pipeline (researcher → summarizer).

## Code at a glance

```go
agent, err := devkit.QuickAgent(ctx, devkit.QuickAgentConfig{
    Model:  os.Getenv("AI_MODEL"),
    APIKey: os.Getenv("AI_API_KEY"),
})
out, err := devkit.QuickRun(ctx, agent, "What is the capital of France?")
```

```go
res, err := devkit.QuickCrew(ctx, devkit.QuickCrewConfig{
    Agents: []devkit.QuickAgentConfig{researcher, summarizer},
}, "Tell me about Go.")
```
