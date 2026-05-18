---
name: dmr-devkit-evaluation
description: >
  This skill should be used when the user wants to "evaluate an agent",
  "assess agent maturity", "test agent reliability", "agent evaluation harness",
  "evaluate state management", "test memory consistency", or needs a structured
  framework to assess how mature and production-ready a DMR devkit agent is.
  Part of the DMR devkit skills suite.
  Covers the State / Memory / Consistency evaluation dimensions, maturity levels,
  test case design, and CI/CD integration for agent quality assurance.
  Do NOT use for writing agent code (use dmr-devkit-agent) or debugging
  runtime issues (use dmr-devkit-observability).
metadata:
  author: seanly
  license: MIT
  version: 0.1.0
  requires:
    go: ">= 1.23"
---

# Agent Evaluation Harness: Maturity Assessment Guide

> **Before using this skill**, the agent being evaluated must already be built and runnable. See `/dmr-devkit-workflow` and `/dmr-devkit-agent` for development guidance.

---

## Why Evaluate?

A single-turn smoke test (`go run .` with one prompt) only proves the agent can respond. It does **not** prove the agent can:

- Survive a crash and resume without re-asking confirmed information
- Handle user mid-task corrections without throwing away valid work
- Keep long-term memory from rotting or contradicting itself
- Resolve conflicts between stored preferences and live external data

This skill provides a **three-dimensional evaluation framework** to assess whether a devkit agent is ready for production.

---

## Evaluation Dimensions

| Dimension | What It Tests | Failure Symptom | Harness Test Method |
|-----------|---------------|-----------------|---------------------|
| **State Correctness** | Current task state is accurate, complete, recoverable | Re-asks after crash, loses step, constraint drift | Checkpoint injection + interruption recovery |
| **Memory Reliability** | Short-term and long-term memory CRUD is correct | Suggests outdated options, repeats confirmed info, misses preferences | Multi-turn session injection + memory audit assertions |
| **Consistency** | Conflict resolution between State, Memory, and external systems | Ignores user correction, overrides live data with stale memory | Constraint change injection + system-of-record mock |

These dimensions are **orthogonal** — an agent can score high on State but fail on Memory retrieval. Evaluate all three.

---

## Dimension 1: State Correctness

State is the agent's picture of **NOW**: current step, active constraints, completed work, next actions.

### 1.1 State Schema Freeze Test

The agent must output an explicit state snapshot after each step. The harness validates:

```yaml
# state_schema.yaml — reference schema for evaluation
workflow: trip_booking
steps:
  - id: 1
    name: confirm_dates_destination
    status: [pending, in_progress, done, invalid]
    constraints:
      - field: departure_date
        required: true
      - field: destination
        required: true
  - id: 2
    name: select_outbound_flight
    depends_on: [1]
    status: [pending, in_progress, done, invalid]
```

**Validation rules:**
- Structure: fields complete, enum values valid
- Semantics: `done` steps have their constraint fields populated; only one step is `in_progress`
- Dependencies: a step cannot be `done` unless all `depends_on` steps are also `done`

### 1.2 Checkpoint & Recovery Test

Simulate failures and verify graceful recovery:

| Fault Scenario | Injection Method | Expected Recovery |
|----------------|------------------|-------------------|
| Process crash | Kill agent process before tool call returns | Restart from last checkpoint, do not re-ask confirmed info |
| Context reset | Simulate Context Window full → reset | Outer Loop re-injects original prompt + current state, agent continues |
| User rollback | User says "go back one step" | Affected step and all downstream marked `invalid`; upstream steps preserved |

**Key assertion:** After recovery, the agent must not re-ask for information already confirmed (e.g., dates, destination) unless the user explicitly changes it.

### 1.3 State Versioning Test (if applicable)

If the agent's state schema evolves:
- Old-format state must still be readable after schema change
- In-progress workflows must not break due to schema migration

---

## Dimension 2: Memory Reliability

Memory spans across tasks: past decisions, user preferences, conversation history.

### 2.1 Memory Lifecycle Four-Stage Test

```
Create  → New info arrives. Is it written to the correct memory slot?
Update  → Info changes. Is it appended or overwritten? Is old version retained?
Summarize → Memory bloats. Does summary keep key decisions, drop noise?
Delete/Expire → Retention window ends. Is memory truly invalidated?
```

**Concrete test case:**

```yaml
memory_lifecycle_test:
  session_1:
    - turn: 1
      user: "Book me a flight to Paris, I prefer window seats"
      memory_assert:
        - type: long_term
          topic: seating_preference
          value: window
          confidence: confirmed
    - turn: 3
      user: "Actually make it aisle this time"
      memory_assert:
        - type: short_term
          topic: seating_preference
          value: aisle
          confidence: inferred   # "this time" implies temporary
        - type: long_term
          topic: seating_preference
          value: window         # long-term must NOT be overwritten by one temporary change
          confidence: confirmed
```

### 2.2 Retrieval Precision Test

Agents don't load all memory. They query. Test relevance:

| Current Task Signal | Memory That Should Be Retrieved | Memory That Must NOT Leak In |
|---------------------|--------------------------------|------------------------------|
| Selecting flight | Airline preference, seat preference, budget limit | Hotel preference, dietary restriction |
| Booking hotel | City-center preference, budget limit, room type | Airline preference, flight time preference |
| User says "always window" | Update long-term seating preference to window | Discard old aisle preference as default |

**Key assertion:** Top-3 retrieved memories must all be directly relevant to the current decision; unrelated memory leakage rate must be below threshold (e.g., 5%).

### 2.3 Memory Rot Detection

Inject stale data and verify expiration:

1. Seed memory: "User prefers Airline X"
2. 10 turns later, external system mock says "Airline X discontinued route Y"
3. Agent must not recommend Airline X for route Y, even though memory says user prefers it

**Key assertion:** Live external data must override stale memory without deleting the preference itself.

---

## Dimension 3: Consistency

Consistency tests how the agent resolves conflicts between State, Memory, and external systems.

### 3.1 Three-Layer Priority Resolution

Define and test this fixed priority:

```
Priority (high → low):
  1. User's current explicit instruction (State active_constraints)
  2. External system of record real-time data
  3. Long-term memory user preferences
  4. Short-term memory session context
```

**Conflict test matrix:**

| Conflict Scenario | Expected Agent Behavior |
|-------------------|------------------------|
| User says "window this time", long-term memory is aisle | Follow current instruction (window); do NOT update long-term memory yet |
| User prefers window, but airline system shows no window seats | Inform user; follow live data; keep preference in memory |
| Short-term memory has flight selected, but external system shows flight cancelled | Roll back to flight selection step; re-query live data |
| User says "always window", then later "aisle is fine too" | Detect preference change confidence; update long-term only after repeated confirmation |

### 3.2 Slow Learning vs Fast Reaction Test

Test that the agent does not overwrite long-term memory too quickly:

```yaml
consistency_temporal_test:
  - turn: 1
    user: "I like morning flights"
    expect_long_term_update: false  # first mention, observe only
  - turn: 3
    user: "Next time also morning please"
    expect_long_term_update: true   # repeated confirmation, write to long-term
  - turn: 5
    user: "This time evening is okay"
    expect_long_term_update: false  # "this time" = temporary, only update State
  - turn: 7
    user: "Evening works better for me in general"
    expect_long_term_update: true   # pattern established, update long-term
```

**Key assertion:** Long-term memory must not update on the first correction. Fast reaction belongs to State; slow learning belongs to Memory.

---

## Maturity Levels

Use this rubric to grade the agent:

| Level | State | Memory | Consistency | Overall |
|-------|-------|--------|-------------|---------|
| **L0 — Demo** | No explicit state; single-turn only | No persistence | None | Toy / prototype |
| **L1 — Basic** | In-memory state, lost on restart | Short-term only (session scope) | User instruction overrides all | Personal assistant |
| **L2 — Stateful** | Checkpoints to TapeStore; survives restart | Long-term memory with CRUD | System-of-record wins in conflict | Internal tool |
| **L3 — Production** | Schema versioning; structured state migration | Lifecycle management (summarize, expire) | Slow-learning memory; rollback on correction | Customer-facing |
| **L4 — Enterprise** | Cross-session audit; state replay | Multi-user isolation; retrieval precision metrics | Full priority arbitration; memory rot alerts | Mission-critical |

---

## CI/CD Integration

Integrate evaluation into your pipeline:

```bash
# Run after agent logic changes
go test ./... -run TestAgentEvaluation

# Run after model upgrades (baseline comparison)
AI_MODEL=claude-opus-4-7 go test ./... -run TestAgentEvaluation
```

**Test pyramid for agents:**

| Layer | Frequency | Scope | Example |
|-------|-----------|-------|---------|
| Smoke | Every commit | Single-turn sanity | `go run . "Hello"` |
| State | Every PR | Checkpoint / recovery | Inject crash, verify resume |
| Memory | Nightly | Multi-turn lifecycle | 20-turn conversation, audit memory |
| Consistency | Before release | Conflict resolution | Full priority matrix |

**Golden rule:** Never assert on exact LLM output text (non-deterministic). Assert on:
- State schema validity
- Memory presence / absence / value
- Tool call sequence correctness
- Checkpoint recovery behavior

---

## Quick Checklist

Before calling an agent "production-ready":

- [ ] **State** — Crash and restart does not lose confirmed information
- [ ] **State** — User correction rolls back only affected steps, preserves valid work
- [ ] **Memory** — Short-term changes do not immediately overwrite long-term preferences
- [ ] **Memory** — Retrieval returns only relevant memories for the current task
- [ ] **Memory** — Stale or expired memory is not used for decision-making
- [ ] **Consistency** — External system-of-record data overrides stored memory in conflict
- [ ] **Consistency** — User's current explicit instruction has highest priority
- [ ] **Consistency** — "This time" / "always" intent is correctly distinguished
- [ ] **Context** — Tool schemas are not all loaded into every prompt (selective retrieval)
- [ ] **Observability** — Every decision, tool call, and state change is logged / taped

---

## Related Skills

- `/dmr-devkit-workflow` — Development workflow and operational rules
- `/dmr-devkit-agent` — Agent API quick reference
- `/dmr-devkit-observability` — Tape storage, event streams, tracing
- `/dmr-devkit-orchestration` — Multi-step workflow state management
