---
name: dmr-devkit-code-review
description: >
  This skill should be used when the user wants to "review code", "evaluate code quality",
  "check for security issues", "assess design", "code review", "review PR", "check diff",
  or needs a structured evaluation of uncommitted or pending code changes.
  Part of the DMR devkit skills suite.
  Covers code quality assessment, security review, design evaluation, Go-specific checks,
  and devkit framework compliance.
  Do NOT use for writing new code (use dmr-devkit-workflow) or debugging runtime issues
  (use dmr-devkit-observability).
metadata:
  author: seanly
  license: MIT
  version: 0.1.0
  requires:
    go: ">= 1.23"
---

# Devkit Code Review Guide

> **Before using this skill**, read the full diff or the files to be reviewed. Do not evaluate code you have not seen.

## When to Use

- User asks to "review", "evaluate", "assess", or "check" code (committed or uncommitted)
- User presents a diff, PR, or specific files for critique
- Before merging changes that touch `plugin/`, `agent/`, `tool/`, or `workflow/` packages
- When refactoring existing devkit components

## Review Workflow

1. **Read all relevant files** — Never review code from memory or summaries
2. **Check cross-file consistency** — Interfaces declared in one file, implemented in another
3. **Run tests** — `go test ./...` must pass before sign-off
4. **Check compilation** — `go build ./...` must pass
5. **Use `simplify` skill** — After fixing issues, run `/simplify` to catch remaining quality problems

---

## Review Dimensions

### 1. Design & Architecture

| Check | Question |
|-------|----------|
| **Interface cohesion** | Does each interface have a single, clear purpose? |
| **Separation of concerns** | Is storage logic separate from execution logic? |
| **Coupling** | Does this change introduce new import cycles or cross-package dependencies? |
| **Extensibility** | Can new capabilities be added without modifying existing code? (Open/Closed) |
| **Naming** | Do types, functions, and variables accurately describe their purpose? |

**Devkit-specific:**
- Plugin capabilities must be declared via `Capabilities()` AND implemented via typed interfaces
- Registry queries should use capability index, not O(n) scans
- Hooks bridge (`RegistryHooks`) must not depend on concrete agent internals

### 2. Type Safety & Correctness

| Check | Question |
|-------|----------|
| **Nil safety** | Are nil receivers, nil maps, and nil slices handled? |
| **Type assertions** | Are type assertions (`.(T)`) checked with `ok`? |
| **Interface compliance** | Does the implementation satisfy all methods of the interface? |
| **Generics usage** | Are type parameters constrained appropriately? |

**Devkit-specific:**
- `Plugin.Capabilities()` must exactly match implemented interfaces (use `ValidateCapabilities`)
- `any` typed DI setters (`SetAgentRunner`, `SetTapeReader`) should document expected types

### 3. Concurrency Safety

| Check | Question |
|-------|----------|
| **Mutex scope** | Is the lock held for the minimum necessary duration? |
| **Lock ordering** | Are multiple locks always acquired in the same order? |
| **Map safety** | Are maps protected during read AND write? |
| **Goroutine leaks** | Are background goroutines properly stopped on shutdown? |

**Devkit-specific:**
- `Registry.mu` must not be held during plugin `Init()` or `Shutdown()` calls
- Capability index (`byCap`) must stay consistent with `plugins` map under lock

### 4. Error Handling

| Check | Question |
|-------|----------|
| **Error wrapping** | Are errors wrapped with `fmt.Errorf("...: %w", err)`? |
| **Aggregation** | Are multiple errors joined with `errors.Join` (Go 1.20+)? |
| **Silent failures** | Are errors logged or returned, not swallowed? |
| **Context cancellation** | Does long-running work respect `ctx.Done()`? |

**Devkit-specific:**
- `RegistryHooks` should report plugin errors via `ErrorHandler`, not silently `continue`
- `InitAll` / `ShutdownAll` must attempt every plugin even if one fails

### 5. Performance

| Check | Question |
|-------|----------|
| **Allocation** | Are hot paths allocating unnecessarily? |
| **Algorithmic complexity** | Is there accidental O(n²) or worse? |
| **Map vs slice** | Is iteration order deterministic where it matters? |
| **Caching** | Are repeated computations cached with appropriate invalidation? |

**Devkit-specific:**
- Registry capability queries should use `byCap` index, not full scans
- `List()` should return plugins in deterministic insertion order

### 6. Security

| Check | Severity | Question |
|-------|----------|----------|
| **Command injection** | Critical | Are user inputs passed to `exec.Command` or shell? |
| **Path traversal** | Critical | Are file paths sanitized (no `..`, absolute path checks)? |
| **SQL injection** | Critical | Are queries parameterized? |
| **XSS / unsafe HTML** | High | Is user content rendered without escaping? |
| **Secrets in code** | High | Are API keys, tokens, or passwords hardcoded? |
| **Race conditions** | Medium | Can TOCTOU bugs lead to privilege escalation? |
| **Resource exhaustion** | Medium | Are unbounded loops, channels, or buffers possible? |

**Devkit-specific:**
- `ResolvePath` must clean and validate paths before use
- `BeforeToolCall` policy checks must run before any tool execution
- Approval flows must not be bypassable by batch operations

---

## Security Checklist for Devkit Plugins

When reviewing code in `plugin/` or `tool/`:

- [ ] **Policy enforcement** — `PolicyChecker.BeforeToolCall` blocks before execution, not after
- [ ] **Approval integrity** — `ApprovalRequiredError` cannot be accidentally caught and ignored by callers
- [ ] **Tool argument validation** — All tool handlers validate args before use (type + semantic)
- [ ] **Path restrictions** — File tools restrict to workspace, no `..` traversal
- [ ] **No secrets in state** — `ToolContext.State` must not contain credentials
- [ ] **Batch operations** — `BatchBeforeToolCall` validates every item, not just the first

---

## Common Go Issues to Flag

| Issue | Example | Fix |
|-------|---------|-----|
| Map iteration order | `for k, v := range m` for ordered output | Use `sort` or a slice |
| `any` without validation | `runner any` cast without check | Add `ok` check + typed fallback |
| `fmt.Sprintf` for errors | `fmt.Sprintf("err: %v", err)` | Use `fmt.Errorf("...: %w", err)` |
| `errors.New` in a loop | Creating new errors instead of wrapping | Wrap the root cause once |
| Missing `json` tags | Struct fields used for config binding | Add `json:"field_name"` tags |
| Unused parameters | `_ context.Context` | Remove or document why ignored |

---

## Output Format

Structure your review as:

```
## Summary
Overall assessment (Good / Needs Improvement / Blocker)

## Strengths
- Point 1
- Point 2

## Issues Found

### [Severity] Category: Title
File:line — Description and recommendation.

Example:
### [High] Security: Path traversal in file tool
plugin/util.go:42 — `ResolvePath` does not reject `..` segments. Use `filepath.Clean` + prefix check.

## Action Items
1. Fix path traversal (plugin/util.go)
2. Add validation to `Register` (plugin/registry.go)
3. Write tests for edge cases

## Related Skills
- `/dmr-devkit-workflow` — Development workflow
- `/dmr-devkit-plugins` — Plugin development patterns
- `/simplify` — Automated quality cleanup
```

---

## Related Skills

- `/dmr-devkit-workflow` — Development workflow and operational rules
- `/dmr-devkit-plugins` — Plugin development patterns
- `/dmr-devkit-tools` — Tool development patterns
- `/dmr-devkit-agent` — Agent API reference
