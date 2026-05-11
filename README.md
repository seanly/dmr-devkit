# dmr-devkit

Public, embeddable agent runtime for DMR: agent loop, tools, tape, workflows, A2A server, LLM client, and OpenAI-compatible provider — **without** the plugin system (`pkg/plugin`) or built-in `plugins/*`.

Product CLI and plugins live in **`github.com/seanly/dmr`**, which depends on this module.

```bash
go get github.com/seanly/dmr-devkit
```

See `examples/` for devkit, workflow, and A2A samples.

Documentation: **[docs/README.md](docs/README.md)** (skills, devkit guides).

Install **devkit skills** into Claude Code (global `~/.claude/skills`) or project-local `.claude/skills`:

```bash
make install-skills           # global
# make install-skills-local   # this repo only
```

Override the source tree with `SKILLS_SRC=path/to/docs/skills` if needed.
