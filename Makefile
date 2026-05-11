# Devkit skills ship under docs/skills/ — install copies into Claude / Cursor skill dirs.

SKILLS_SRC    ?= docs/skills
CLAUDE_GLOBAL := $(HOME)/.claude/skills
CLAUDE_LOCAL  := .claude/skills
SKILL_NAMES   := dmr-devkit-workflow dmr-devkit-scaffold dmr-devkit-agent \
                 dmr-devkit-tools dmr-devkit-plugins dmr-devkit-orchestration \
                 dmr-devkit-a2a dmr-devkit-observability

.PHONY: install-skills uninstall-skills install-skills-local uninstall-skills-local

install-skills:
	@test -d "$(SKILLS_SRC)" || (echo "error: $(SKILLS_SRC) not found (run from dmr-devkit repo root)" >&2; exit 1)
	@echo "=== Installing devkit skills from $(SKILLS_SRC) to $(CLAUDE_GLOBAL) ==="
	@for skill in $(SKILL_NAMES); do \
		mkdir -p "$(CLAUDE_GLOBAL)/$$skill"; \
		cp "$(SKILLS_SRC)/$$skill/SKILL.md" "$(CLAUDE_GLOBAL)/$$skill/"; \
		if [ -d "$(SKILLS_SRC)/$$skill/references" ]; then \
			cp -r "$(SKILLS_SRC)/$$skill/references" "$(CLAUDE_GLOBAL)/$$skill/"; \
		fi; \
		echo "  ✓ $$skill"; \
	done
	@echo ""
	@echo "Done. Run '/skills' in Claude Code to verify."

uninstall-skills:
	@echo "=== Removing devkit skills from $(CLAUDE_GLOBAL) ==="
	@for skill in $(SKILL_NAMES); do \
		if [ -d "$(CLAUDE_GLOBAL)/$$skill" ]; then \
			rm -rf "$(CLAUDE_GLOBAL)/$$skill"; \
			echo "  ✗ $$skill"; \
		fi; \
	done
	@echo "Done."

install-skills-local:
	@test -d "$(SKILLS_SRC)" || (echo "error: $(SKILLS_SRC) not found (run from dmr-devkit repo root)" >&2; exit 1)
	@echo "=== Installing devkit skills from $(SKILLS_SRC) to $(CLAUDE_LOCAL) (project-local) ==="
	@mkdir -p $(CLAUDE_LOCAL)
	@for skill in $(SKILL_NAMES); do \
		mkdir -p "$(CLAUDE_LOCAL)/$$skill"; \
		cp "$(SKILLS_SRC)/$$skill/SKILL.md" "$(CLAUDE_LOCAL)/$$skill/"; \
		if [ -d "$(SKILLS_SRC)/$$skill/references" ]; then \
			cp -r "$(SKILLS_SRC)/$$skill/references" "$(CLAUDE_LOCAL)/$$skill/"; \
		fi; \
		echo "  ✓ $$skill"; \
	done
	@echo ""
	@echo "Done. Claude Code will auto-load these when working in this project."

uninstall-skills-local:
	@echo "=== Removing devkit skills from $(CLAUDE_LOCAL) ==="
	@for skill in $(SKILL_NAMES); do \
		if [ -d "$(CLAUDE_LOCAL)/$$skill" ]; then \
			rm -rf "$(CLAUDE_LOCAL)/$$skill"; \
			echo "  ✗ $$skill"; \
		fi; \
	done
	@echo "Done."
