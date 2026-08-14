package bundle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seanly/dmr-devkit/okf/config"
	"github.com/seanly/dmr-devkit/okf/frontmatter"
)

// buildBundle writes files into a temp dir and loads them, returning the bundle.
func buildBundle(t *testing.T, files map[string]string) *Bundle {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	loader := NewLoader()
	b, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return b
}

func issuesBySeverity(issues []Issue, sev IssueSeverity) []Issue {
	var out []Issue
	for _, i := range issues {
		if i.Severity == sev {
			out = append(out, i)
		}
	}
	return out
}

func hasIssue(issues []Issue, substr string) bool {
	for _, i := range issues {
		if containsString(i.Message, substr) {
			return true
		}
	}
	return false
}

func containsString(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestValidateTypeEmptyIsError(t *testing.T) {
	b := buildBundle(t, map[string]string{
		"a.md": "---\ntype: \"\"\n---\nbody\n",
	})
	v := NewValidator()
	issues := v.Validate(b)
	if len(issuesBySeverity(issues, SeverityError)) == 0 {
		t.Errorf("expected an error for empty type, got %v", issues)
	}
}

func TestValidateTypePresentOK(t *testing.T) {
	b := buildBundle(t, map[string]string{
		"index.md": "# Index\n",
		"a.md":     "---\ntype: Metric\ntitle: A\n---\nbody\n",
	})
	v := NewValidator()
	issues := v.Validate(b)
	if len(issuesBySeverity(issues, SeverityError)) != 0 {
		t.Errorf("expected no errors, got %v", issues)
	}
}

func TestValidateUnparseableFrontmatterIsError(t *testing.T) {
	b := buildBundle(t, map[string]string{
		"a.md": "# no frontmatter here\n",
	})
	v := NewValidator()
	issues := v.Validate(b)
	if !hasIssue(issues, "unparseable frontmatter") {
		t.Errorf("expected parse error, got %v", issues)
	}
}

func TestValidateSubdirIndexWithFrontmatterIsError(t *testing.T) {
	b := buildBundle(t, map[string]string{
		"tables/index.md": "---\ntype: table\n---\n# Tables\n",
	})
	v := NewValidator()
	issues := v.Validate(b)
	if !hasIssue(issues, "non-root index.md must not contain frontmatter") {
		t.Errorf("expected subdir index FM error, got %v", issues)
	}
}

func TestValidateRootIndexOKFVersionOK(t *testing.T) {
	b := buildBundle(t, map[string]string{
		"index.md": "---\nokf_version: \"0.2\"\n---\n# Index\n",
		"a.md":     "---\ntype: Metric\n---\nbody\n",
	})
	v := NewValidator()
	issues := v.Validate(b)
	if hasIssue(issues, "okf_version") {
		t.Errorf("okf_version 0.2 should not produce an issue, got %v", issues)
	}
	if b.OKFVersion != "0.2" {
		t.Errorf("OKFVersion = %q", b.OKFVersion)
	}
}

func TestValidateRootIndexExtraFrontmatterWarns(t *testing.T) {
	b := buildBundle(t, map[string]string{
		"index.md": "---\nokf_version: \"0.2\"\ntype: Metric\n---\n# Index\n",
	})
	v := NewValidator()
	issues := v.Validate(b)
	if !hasIssue(issues, "should only carry okf_version") {
		t.Errorf("expected warning about extra FM, got %v", issues)
	}
}

func TestValidateLogBadDateWarns(t *testing.T) {
	b := buildBundle(t, map[string]string{
		"index.md": "# Index\n",
		"log.md":   "# Log\n\n## 2026-13-45\n- entry\n",
	})
	v := NewValidator()
	issues := v.Validate(b)
	if !hasIssue(issues, "not YYYY-MM-DD") {
		t.Errorf("expected log date warning, got %v", issues)
	}
}

func TestValidateLogGoodDateOK(t *testing.T) {
	b := buildBundle(t, map[string]string{
		"index.md": "# Index\n",
		"log.md":   "# Log\n\n## 2026-08-12\n- entry\n",
	})
	v := NewValidator()
	issues := v.Validate(b)
	if hasIssue(issues, "not YYYY-MM-DD") {
		t.Errorf("good date should not warn, got %v", issues)
	}
}

func TestValidateStatusInvalidWarns(t *testing.T) {
	b := buildBundle(t, map[string]string{
		"index.md": "# Index\n",
		"a.md":     "---\ntype: Metric\nstatus: archived\n---\nbody\n",
	})
	v := NewValidator()
	issues := v.Validate(b)
	if !hasIssue(issues, "invalid status") {
		t.Errorf("expected status warning, got %v", issues)
	}
}

func TestValidateStaleAfterMalformedWarns(t *testing.T) {
	b := buildBundle(t, map[string]string{
		"index.md": "# Index\n",
		"a.md":     "---\ntype: Metric\nstale_after: 2026-9-9\n---\nbody\n",
	})
	v := NewValidator()
	issues := v.Validate(b)
	if !hasIssue(issues, "stale_after") {
		t.Errorf("expected stale_after warning, got %v", issues)
	}
}

func TestValidateGeneratedMissingByWarns(t *testing.T) {
	b := buildBundle(t, map[string]string{
		"index.md": "# Index\n",
		"a.md":     "---\ntype: Metric\ngenerated: { at: 2026-06-20T22:53:05Z }\n---\nbody\n",
	})
	v := NewValidator()
	issues := v.Validate(b)
	if !hasIssue(issues, "generated.by") {
		t.Errorf("expected generated.by warning, got %v", issues)
	}
}

func TestValidateSourcesMissingResourceWarns(t *testing.T) {
	b := buildBundle(t, map[string]string{
		"index.md": "# Index\n",
		"a.md":     "---\ntype: Metric\nsources:\n  - id: x\n---\nbody\n",
	})
	v := NewValidator()
	issues := v.Validate(b)
	if !hasIssue(issues, "resource is required") {
		t.Errorf("expected sources.resource warning, got %v", issues)
	}
}

func TestValidateAttestedMissingRuntimeWarns(t *testing.T) {
	b := buildBundle(t, map[string]string{
		"index.md": "# Index\n",
		"a.md":     "---\ntype: Attested Computation\nexecutor: { resource: x }\n---\nbody\n",
	})
	v := NewValidator()
	issues := v.Validate(b)
	if !hasIssue(issues, "requires runtime") {
		t.Errorf("expected runtime warning, got %v", issues)
	}
}

func TestValidateAttestedSnakeCaseRecognized(t *testing.T) {
	b := buildBundle(t, map[string]string{
		"index.md": "# Index\n",
		"a.md":     "---\ntype: attested_computation\nexecutor: { resource: x }\n---\nbody\n",
	})
	v := NewValidator()
	issues := v.Validate(b)
	if !hasIssue(issues, "requires runtime") {
		t.Errorf("expected snake_case attested to be recognized, got %v", issues)
	}
}

func TestValidateAttestedCompleteOK(t *testing.T) {
	b := buildBundle(t, map[string]string{
		"index.md": "# Index\n",
		"a.md": `---
type: Attested Computation
runtime: bigquery
executor:
  resource: references/run.md
  receipt: [job_id]
attester:
  resource: references/att.py
---
# Computation

    SELECT 1
`,
	})
	v := NewValidator()
	issues := v.Validate(b)
	if hasIssue(issues, "runtime") || hasIssue(issues, "executor") {
		t.Errorf("complete attested should not warn, got %v", issues)
	}
}

func TestValidateVerifiedBareMappingNoIssue(t *testing.T) {
	b := buildBundle(t, map[string]string{
		"index.md": "# Index\n",
		"a.md":     "---\ntype: Metric\nverified: { by: human:ahormati, at: 2026-06-25T09:00:00Z }\n---\nbody\n",
	})
	v := NewValidator()
	issues := v.Validate(b)
	if hasIssue(issues, "verified") {
		t.Errorf("bare mapping verified should not produce issues, got %v", issues)
	}
	c := b.Concepts["a"]
	if c.TrustTier() != frontmatter.HumanReviewed {
		t.Errorf("trust tier = %q, want human-reviewed", c.TrustTier())
	}
}

func TestValidateUnknownKeysNotRejected(t *testing.T) {
	b := buildBundle(t, map[string]string{
		"index.md": "# Index\n",
		"a.md":     "---\ntype: Metric\ncustom_field: hello\n---\nbody\n",
	})
	v := NewValidator()
	issues := v.Validate(b)
	if len(issuesBySeverity(issues, SeverityError)) != 0 {
		t.Errorf("unknown keys must not be rejected, got %v", issues)
	}
}

func TestValidateMissingIndexInfoDefault(t *testing.T) {
	b := buildBundle(t, map[string]string{
		"a.md": "---\ntype: Metric\n---\nbody\n",
	})
	v := NewValidator()
	issues := v.Validate(b)
	// No config ⇒ missing index is info, not error/warning.
	found := false
	for _, i := range issues {
		if containsString(i.Message, "missing an index.md") {
			if i.Severity != SeverityInfo {
				t.Errorf("missing index should be info by default, got %s", i.Severity)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("expected missing-index info, got %v", issues)
	}
}

func TestValidateMissingIndexErrorWhenRequired(t *testing.T) {
	b := buildBundle(t, map[string]string{
		"a.md": "---\ntype: Metric\n---\nbody\n",
	})
	b.Config = &config.Config{Rules: config.Rules{RequireIndex: true}}
	v := NewValidator()
	issues := v.Validate(b)
	found := false
	for _, i := range issues {
		if containsString(i.Message, "missing index.md") && i.Severity == SeverityError {
			found = true
		}
	}
	if !found {
		t.Errorf("expected missing-index error when RequireIndex, got %v", issues)
	}
}

func TestValidateOrphanLinkWarns(t *testing.T) {
	b := buildBundle(t, map[string]string{
		"index.md": "# Index\n",
		"a.md":     "---\ntype: Metric\n---\n[missing](missing.md)\n",
	})
	v := NewValidator()
	issues := v.Validate(b)
	if !hasIssue(issues, "broken link") {
		t.Errorf("expected broken-link warning, got %v", issues)
	}
}

func TestValidateOrphanLinkSuppressedWhenDisabled(t *testing.T) {
	b := buildBundle(t, map[string]string{
		"index.md": "# Index\n",
		"a.md":     "---\ntype: Metric\n---\n[missing](missing.md)\n",
	})
	b.Config = &config.Config{Rules: config.Rules{WarnOrphan: false}}
	v := NewValidator()
	issues := v.Validate(b)
	if hasIssue(issues, "broken link") {
		t.Errorf("broken link should be suppressed, got %v", issues)
	}
}

func TestValidateCustomTypeRequiredFieldError(t *testing.T) {
	b := buildBundle(t, map[string]string{
		"index.md": "# Index\n",
		"a.md":     "---\ntype: table\ntitle: orders\n---\nbody\n",
	})
	b.Config = &config.Config{
		CustomTypes: []config.CustomType{
			{Name: "table", Required: []string{"title", "description"}},
		},
	}
	v := NewValidator()
	issues := v.Validate(b)
	if !hasIssue(issues, "requires field") {
		t.Errorf("expected custom-type required-field error, got %v", issues)
	}
}
