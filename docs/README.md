# dmr-devkit documentation

- **[Skills](skills/README.md)** — structured guidance for agents (workflow, scaffold, tools, plugins, orchestration, A2A, observability).
- **[Devkit 中文文档](devkit/README.md)** — architecture, API, tools, workflow, A2A, examples.
- **[Minimal wiring (English)](devkit.md)** — `devkit.Build` overview and when to use devkit vs **dmr**.
- **[Working directory management](cwd-management.md)** — `cwd` package and tool context.

The product CLI and built-in plugins live in the **dmr** repository, which depends on this module.

**Skills:** from this repo root, `make install-skills` copies `docs/skills/` into `~/.claude/skills` (see `Makefile`).
