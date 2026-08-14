package bundle

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/seanly/dmr-devkit/okf/frontmatter"
)

// IssueSeverity indicates how serious a validation issue is.
type IssueSeverity string

const (
	SeverityError   IssueSeverity = "error"
	SeverityWarning IssueSeverity = "warning"
	SeverityInfo    IssueSeverity = "info"
)

// Issue is a single validation finding.
type Issue struct {
	Severity IssueSeverity `json:"severity"`
	File     string        `json:"file,omitempty"`
	Concept  string        `json:"concept,omitempty"`
	Message  string        `json:"message"`
}

// Validator checks a bundle for OKF compliance.
type Validator struct {
	Issues []Issue
}

// NewValidator creates a new Validator.
func NewValidator() *Validator {
	return &Validator{}
}

// Validate performs all validation checks on the bundle.
//
// 检查分两层：SPEC §11 baseline（始终运行，severity 严格遵循 §11 的
// MUST/MUST-NOT-reject 约束）与可选的 .okf.yaml 加严层（Rules 升级严重度、
// custom_types required 字段）。
func (v *Validator) Validate(b *Bundle) []Issue {
	v.Issues = nil
	// SPEC §11 baseline
	v.checkParseErrors(b)         // §11.1
	v.checkRequiredFields(b)      // §11.2
	v.checkReservedFiles(b)       // §8, §9, §11.3
	v.checkStatus(b)              // §5.4
	v.checkStaleAfter(b)          // §5.5
	v.checkGenerated(b)           // §5.2
	v.checkSources(b)             // §5.1
	v.checkAttestedComputation(b) // §10
	v.checkOKFVersion(b)          // §12
	// 软结构（§6.1 MUST tolerate / §8）
	v.checkIndex(b)   // §8
	v.checkOrphans(b) // §6.1
	v.checkNaming(b)  // 非 SPEC，软建议
	// .okf.yaml 加严层
	v.checkRules(b)       // Rules 升级严重度
	v.checkCustomTypes(b) // custom_types required 字段
	return v.Issues
}

func (v *Validator) add(sev IssueSeverity, file, concept, msg string) {
	v.Issues = append(v.Issues, Issue{
		Severity: sev,
		File:     file,
		Concept:  concept,
		Message:  msg,
	})
}

// checkParseErrors surfaces frontmatter parse failures as §11.1 errors.
func (v *Validator) checkParseErrors(b *Bundle) {
	for _, pe := range b.ParseErrors {
		v.add(SeverityError, pe.RelPath, "", fmt.Sprintf("unparseable frontmatter: %v", pe.Err))
	}
}

func (v *Validator) checkRequiredFields(b *Bundle) {
	for id, c := range b.Concepts {
		if c.Meta == nil {
			v.add(SeverityError, c.FilePath, id, "missing frontmatter")
			continue
		}
		if strings.TrimSpace(c.Meta.Type) == "" {
			v.add(SeverityError, c.FilePath, id, "required field 'type' is empty")
		}
		if strings.TrimSpace(c.Meta.Title) == "" {
			v.add(SeverityWarning, c.FilePath, id, "title is empty; search results will be degraded")
		}
	}
}

// checkReservedFiles validates §8 (index.md) and §9 (log.md) structure.
//
// §8: index.md 不得含 frontmatter，唯一例外是 bundle-root index.md 可携带 okf_version。
// §9: log.md 日期标题必须为 YYYY-MM-DD。
func (v *Validator) checkReservedFiles(b *Bundle) {
	for dir, raw := range b.Indexes {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if !hasFrontmatter(raw) {
			continue
		}
		fileLabel := "index.md"
		if dir != "." {
			fileLabel = dir + "/index.md"
		}
		if dir != "." {
			// 非 root index.md 不得含任何 frontmatter
			v.add(SeverityError, fileLabel, "", "non-root index.md must not contain frontmatter (§8)")
			continue
		}
		// root index.md：仅允许 okf_version
		if hasKeysOtherThan(raw, "okf_version") {
			v.add(SeverityWarning, fileLabel, "", "root index.md frontmatter should only carry okf_version (§8)")
		}
	}
	for dir, raw := range b.Logs {
		fileLabel := "log.md"
		if dir != "." {
			fileLabel = dir + "/log.md"
		}
		v.checkLogDates(fileLabel, raw)
	}
}

var isoDateHeading = regexp.MustCompile(`(?m)^##\s+(\d{4}-\d{2}-\d{2})\s*$`)

// checkLogDates verifies §9: date headings under ## must be YYYY-MM-DD.
// Non-conforming ## headings that look like dates are warned.
func (v *Validator) checkLogDates(fileLabel, raw string) {
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "##") {
			continue
		}
		// Only inspect level-2 headings (## ...), not deeper.
		if strings.HasPrefix(trimmed, "###") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "##"))
		if rest == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", rest); err != nil {
			v.add(SeverityWarning, fileLabel, "", fmt.Sprintf("log date heading %q is not YYYY-MM-DD (§9)", rest))
		}
	}
}

func (v *Validator) checkStatus(b *Bundle) {
	for id, c := range b.Concepts {
		if c.Meta == nil {
			continue
		}
		if err := c.Meta.ValidateStatus(); err != nil {
			v.add(SeverityWarning, c.FilePath, id, err.Error())
		}
	}
}

func (v *Validator) checkStaleAfter(b *Bundle) {
	for id, c := range b.Concepts {
		if c.Meta == nil {
			continue
		}
		if err := c.Meta.ValidateStaleAfter(); err != nil {
			v.add(SeverityWarning, c.FilePath, id, err.Error())
		}
	}
}

func (v *Validator) checkGenerated(b *Bundle) {
	for id, c := range b.Concepts {
		if c.Meta == nil {
			continue
		}
		if err := c.Meta.ValidateGenerated(); err != nil {
			v.add(SeverityWarning, c.FilePath, id, err.Error())
		}
	}
}

func (v *Validator) checkSources(b *Bundle) {
	for id, c := range b.Concepts {
		if c.Meta == nil {
			continue
		}
		if err := c.Meta.ValidateSources(); err != nil {
			v.add(SeverityWarning, c.FilePath, id, err.Error())
		}
	}
}

func (v *Validator) checkAttestedComputation(b *Bundle) {
	for id, c := range b.Concepts {
		if c.Meta == nil {
			continue
		}
		if !frontmatter.IsAttestedComputation(c.Meta.Type) {
			continue
		}
		if strings.TrimSpace(c.Meta.Runtime) == "" {
			v.add(SeverityWarning, c.FilePath, id, "Attested Computation requires runtime (§10.2)")
		}
		if c.Meta.Executor == nil {
			v.add(SeverityWarning, c.FilePath, id, "Attested Computation missing executor (§10.2)")
		}
	}
}

func (v *Validator) checkOKFVersion(b *Bundle) {
	if b.OKFVersion == "" {
		return
	}
	if b.OKFVersion != "0.2" {
		v.add(SeverityInfo, "index.md", "", fmt.Sprintf("okf_version %q; devkit targets 0.2, attempting best-effort (§12)", b.OKFVersion))
	}
}

func (v *Validator) checkIndex(b *Bundle) {
	if !b.HasIndex() {
		// §11: MUST NOT reject for missing index.md → info by default.
		v.add(SeverityInfo, "index.md", "", "bundle is missing an index.md; progressive disclosure will not work")
		return
	}
	missing := b.MissingFromIndex()
	for _, id := range missing {
		v.add(SeverityInfo, "", id, fmt.Sprintf("concept %q is not referenced in index.md", id))
	}
}

func (v *Validator) checkOrphans(b *Bundle) {
	// §6.1: consumers MUST tolerate broken links. Warn by default; suppress when
	// .okf.yaml explicitly sets warn_orphan: false.
	if b.Config != nil && !b.Config.Rules.WarnOrphan {
		return
	}
	orphans := b.OrphanedLinks()
	for src, targets := range orphans {
		for _, t := range targets {
			v.add(SeverityWarning, "", src, fmt.Sprintf("broken link to %q", t))
		}
	}
}

func (v *Validator) checkNaming(b *Bundle) {
	for id, c := range b.Concepts {
		base := filepathBase(c.FilePath)
		if strings.Contains(base, " ") {
			v.add(SeverityWarning, c.FilePath, id, "filename contains spaces; prefer kebab-case")
		}
	}
}

// checkRules applies .okf.yaml Rules to escalate severities.
func (v *Validator) checkRules(b *Bundle) {
	if b.Config == nil {
		return
	}
	r := b.Config.Rules
	if r.RequireIndex && !b.HasIndex() {
		v.add(SeverityError, "index.md", "", "bundle is missing index.md (require_index=true)")
	}
	if r.RequireLog && len(b.Logs) == 0 {
		v.add(SeverityWarning, "log.md", "", "bundle is missing log.md (require_log=true)")
	}
	if r.WarnUntagged {
		for id, c := range b.Concepts {
			if c.Meta != nil && len(c.Meta.Tags) == 0 {
				v.add(SeverityWarning, c.FilePath, id, "concept has no tags (warn_untagged=true)")
			}
		}
	}
}

func (v *Validator) checkCustomTypes(b *Bundle) {
	if b.Config == nil {
		return
	}
	customs := b.Config.TypeNames()
	if len(customs) == 0 {
		return
	}
	for id, c := range b.Concepts {
		if c.Meta == nil {
			continue
		}
		if !customs[c.Meta.Type] {
			continue
		}
		for _, field := range b.Config.RequiredFields(c.Meta.Type) {
			switch field {
			case "title":
				if strings.TrimSpace(c.Meta.Title) == "" {
					v.add(SeverityError, c.FilePath, id, fmt.Sprintf("custom type %q requires field %q", c.Meta.Type, field))
				}
			case "description":
				if strings.TrimSpace(c.Meta.Description) == "" {
					v.add(SeverityError, c.FilePath, id, fmt.Sprintf("custom type %q requires field %q", c.Meta.Type, field))
				}
			case "resource":
				if strings.TrimSpace(c.Meta.Resource) == "" {
					v.add(SeverityError, c.FilePath, id, fmt.Sprintf("custom type %q requires field %q", c.Meta.Type, field))
				}
			case "runtime":
				if strings.TrimSpace(c.Meta.Runtime) == "" {
					v.add(SeverityError, c.FilePath, id, fmt.Sprintf("custom type %q requires field %q", c.Meta.Type, field))
				}
			case "executor":
				if isAnyEmpty(c.Meta.Executor) {
					v.add(SeverityError, c.FilePath, id, fmt.Sprintf("custom type %q requires field %q", c.Meta.Type, field))
				}
			case "attester":
				if isAnyEmpty(c.Meta.Attester) {
					v.add(SeverityError, c.FilePath, id, fmt.Sprintf("custom type %q requires field %q", c.Meta.Type, field))
				}
			default:
				v.add(SeverityWarning, c.FilePath, id, fmt.Sprintf("unknown required field %q for type %q", field, c.Meta.Type))
			}
		}
	}
}

// hasFrontmatter reports whether raw markdown begins with a frontmatter block.
func hasFrontmatter(raw string) bool {
	return strings.HasPrefix(strings.TrimSpace(raw), "---")
}

// hasKeysOtherThan reports whether the frontmatter block contains any key other
// than the single allowed key.
func hasKeysOtherThan(raw, allowed string) bool {
	res, err := frontmatter.Extract(raw)
	if err != nil || res.Meta == nil {
		return false
	}
	// Re-parse as a generic map to detect any keys.
	// We approximate by checking known Meta fields for non-zero values other than OKFVersion.
	m := res.Meta
	switch allowed {
	case "okf_version":
		if m.Type != "" || m.Title != "" || m.Description != "" || m.Resource != "" ||
			len(m.Tags) != 0 || m.Status != "" || m.StaleAfter != nil || m.Generated != nil ||
			len(m.Verified) != 0 || len(m.Sources) != 0 || m.Runtime != "" ||
			m.Executor != nil || m.Attester != nil || len(m.Parameters) != 0 {
			return true
		}
	}
	return false
}

// filepathBase is a thin wrapper to keep imports minimal in this file.
func filepathBase(p string) string {
	idx := strings.LastIndexByte(p, '/')
	if idx == -1 {
		return p
	}
	return p[idx+1:]
}

// isAnyEmpty reports whether a frontmatter field decoded as `any` is absent or
// empty: nil, empty string, empty map, or empty slice.
func isAnyEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(x) == ""
	case map[string]any:
		return len(x) == 0
	case []any:
		return len(x) == 0
	default:
		return false
	}
}
