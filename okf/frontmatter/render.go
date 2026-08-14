package frontmatter

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Render serializes a Meta into a YAML frontmatter body (without the surrounding
// "---" delimiters and without the markdown body). It is the inverse of the
// YAML-parsing half of [Extract]: empty fields are dropped so generated files
// stay clean, and struct field order is preserved (type first), matching how a
// human would author the file.
func Render(m *Meta) (string, error) {
	if m == nil {
		return "", fmt.Errorf("render: meta is nil")
	}
	if err := m.ValidateRequired(); err != nil {
		return "", err
	}

	// Marshal the struct directly: yaml.v3 preserves struct field order, so
	// `type` lands first. Then round-trip through a Node tree to prune empty
	// values (the struct has no omitempty tags).
	raw, err := yaml.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("marshal meta: %w", err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return "", fmt.Errorf("reparse meta: %w", err)
	}
	pruneEmpty(&doc)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return "", fmt.Errorf("encode meta: %w", err)
	}
	_ = enc.Close()
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

// RenderDocument composes a full OKF markdown document: frontmatter delimiters
// wrapping the rendered YAML, followed by the body. It is the strict inverse
// of [Extract] for documents that round-trip cleanly.
func RenderDocument(m *Meta, body string) (string, error) {
	yml, err := Render(m)
	if err != nil {
		return "", err
	}
	body = strings.TrimRight(body, "\n")
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString(yml)
	sb.WriteString("\n---\n")
	if body != "" {
		sb.WriteString(body)
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// pruneEmpty walks a yaml.Node tree in place, dropping empty scalars, empty
// sequences, and empty mappings so rendered frontmatter contains only fields
// that carry a value. The root document node is never dropped.
func pruneEmpty(n *yaml.Node) bool {
	switch n.Kind {
	case yaml.DocumentNode:
		// Keep the document; prune its single mapping child.
		for _, c := range n.Content {
			pruneEmpty(c)
		}
		return false
	case yaml.SequenceNode:
		kept := n.Content[:0]
		for _, c := range n.Content {
			if pruneEmpty(c) {
				continue // drop empty element
			}
			kept = append(kept, c)
		}
		n.Content = kept
		return len(n.Content) == 0
	case yaml.MappingNode:
		// Content is [key, value, key, value, ...]. Keys are scalars; drop a
		// pair when its value prunes to empty.
		kept := n.Content[:0]
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			v := n.Content[i+1]
			if pruneEmpty(v) {
				continue // drop key+value
			}
			kept = append(kept, k, v)
		}
		n.Content = kept
		return len(n.Content) == 0
	case yaml.ScalarNode:
		// Empty string scalar (the common empty case). Non-string empties
		// (e.g. a null) also surface as an empty Value.
		return n.Value == "" && n.Tag != "!!bool"
	}
	return false
}
