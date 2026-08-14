package frontmatter

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Result 是提取 frontmatter 的返回结构。
type Result struct {
	Meta *Meta
	Body string // frontmatter 之后的原始 Markdown
}

// Extract 从 Markdown 文本中提取 YAML frontmatter 和正文。
// 格式：---\nYAML\n---\nBody
//
// 闭合分隔符必须独占一行（OKF §4），因此逐行扫描而非用 strings.Index，
// 避免 YAML 值中出现的 "---" 被误判为闭合符。
func Extract(raw string) (*Result, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "---") {
		return nil, fmt.Errorf("document does not start with frontmatter delimiter '---'")
	}

	// 跳过开头的 --- 行
	rest := raw[3:]
	rest = strings.TrimLeft(rest, "\n")

	lines := strings.Split(rest, "\n")
	endIdx := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "---" {
			endIdx = i
			break
		}
	}
	if endIdx == -1 {
		return nil, fmt.Errorf("frontmatter closing delimiter '---' not found")
	}

	yamlPart := strings.Join(lines[:endIdx], "\n")
	bodyPart := strings.Join(lines[endIdx+1:], "\n")

	var m Meta
	// yaml.Unmarshal 默认忽略未知键（§4.1 MUST NOT reject documents with unrecognized fields）。
	if err := yaml.Unmarshal([]byte(yamlPart), &m); err != nil {
		return nil, fmt.Errorf("invalid YAML frontmatter: %w", err)
	}

	return &Result{
		Meta: &m,
		Body: strings.TrimSpace(bodyPart),
	}, nil
}
