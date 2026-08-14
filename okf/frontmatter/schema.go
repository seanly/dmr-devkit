package frontmatter

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// TrustTier 是从 verified 字段派生的信任层级，匹配 OKF v0.2 §5.3。
type TrustTier string

const (
	Unverified       TrustTier = "unverified"
	MachineConfirmed TrustTier = "machine-confirmed"
	HumanReviewed    TrustTier = "human-reviewed"
)

// ActorEvent 记录某个动作的执行者和时间，匹配 OKF v0.2 §5.2 / §7。
type ActorEvent struct {
	By string    `yaml:"by"`
	At time.Time `yaml:"at"`
}

// VerifiedList 是 verified 字段的容器，兼容 §5.2 的两种写法：
// 列表形式（- { by, at }）与 bare mapping 形式（{ by, at }）。
// 消费者 MUST 把 bare mapping 当作单元素列表（§5.2）。
type VerifiedList []ActorEvent

// UnmarshalYAML 按 §5.2 同时接受 mapping（单次验证）与 sequence（多次验证）。
func (v *VerifiedList) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode && value.Value == "" {
		return nil // 空值 / null
	}
	if value.Kind == yaml.MappingNode {
		var ae ActorEvent
		if err := value.Decode(&ae); err != nil {
			return err
		}
		*v = append(*v, ae)
		return nil
	}
	if value.Kind == yaml.SequenceNode {
		var aes []ActorEvent
		if err := value.Decode(&aes); err != nil {
			return err
		}
		*v = aes
		return nil
	}
	return fmt.Errorf("verified: expected mapping or sequence, got %v", value.Kind)
}

// Source 记录概念的数据来源及可信度信号，匹配 OKF v0.2 §5.1。
type Source struct {
	ID           string `yaml:"id"`
	Resource     string `yaml:"resource"`
	Title        string `yaml:"title"`
	Author       string `yaml:"author"`
	UsageCount   int    `yaml:"usage_count"`
	LastModified string `yaml:"last_modified"`
}

// UsageWindow 框定 sources 中 usage_count 的统计周期。
type UsageWindow struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

// NotEntry 用于消歧义，微信文章中的扩展字段。
type NotEntry struct {
	Term    string `yaml:"term"`
	Why     string `yaml:"why"`
	Instead string `yaml:"instead"`
}

// Meta 是 OKF concept 的完整 frontmatter 结构。
type Meta struct {
	// 必填（OKF §4.1）
	Type string `yaml:"type"`

	// 推荐
	Title       string   `yaml:"title"`
	Description string   `yaml:"description"`
	Resource    string   `yaml:"resource"`
	Tags        []string `yaml:"tags"`

	// 信任与生命周期（OKF §5）
	Status     string       `yaml:"status"`
	StaleAfter *string      `yaml:"stale_after"` // YYYY-MM-DD
	Generated  *ActorEvent  `yaml:"generated"`
	Verified   VerifiedList `yaml:"verified"`

	// 来源（OKF §5.1）
	Sources     []Source     `yaml:"sources"`
	UsageWindow *UsageWindow `yaml:"usage_window"`

	// Attested Computation（OKF §10）
	Runtime    string           `yaml:"runtime"`
	Parameters []map[string]any `yaml:"parameters"`
	Executor   any              `yaml:"executor"`
	Attester   any              `yaml:"attester"`

	// 版本声明（OKF §12，仅 bundle-root index.md 允许）
	OKFVersion string `yaml:"okf_version"`

	// 扩展
	Not []NotEntry `yaml:"not"`
}

// ParseTrustTier maps a trust-tier string to a TrustTier constant. Matching is
// case-insensitive and tolerant of spaces vs. hyphens (e.g. "human reviewed",
// "Human-Reviewed" all map to HumanReviewed). An empty string maps to the
// default HumanReviewed. The bool is false when the input is unrecognized.
func ParseTrustTier(s string) (TrustTier, bool) {
	canon := strings.ToLower(strings.ReplaceAll(s, " ", "-"))
	switch canon {
	case "", "human-reviewed", "humanreviewed":
		return HumanReviewed, true
	case "machine-confirmed", "machineconfirmed":
		return MachineConfirmed, true
	case "unverified":
		return Unverified, true
	}
	return Unverified, false
}

// DeriveTrustTier 根据 OKF §5.3 从 verified 字段派生信任层级。
func (m *Meta) DeriveTrustTier() TrustTier {
	if len(m.Verified) == 0 {
		return Unverified
	}
	for _, v := range m.Verified {
		if strings.HasPrefix(v.By, "human:") {
			return HumanReviewed
		}
	}
	return MachineConfirmed
}

// IsStale 判断概念是否过期。stale_after 为 YYYY-MM-DD 绝对日期。
func (m *Meta) IsStale() bool {
	if m.StaleAfter == nil {
		return false
	}
	threshold, err := time.Parse("2006-01-02", *m.StaleAfter)
	if err != nil {
		return false
	}
	return !time.Now().Before(threshold)
}

// ValidateRequired 检查合规性（OKF §11）：type 字段不能为空。
// type 是唯一始终必填的键（§4.1）。
func (m *Meta) ValidateRequired() error {
	if strings.TrimSpace(m.Type) == "" {
		return fmt.Errorf("missing required field: type")
	}
	return nil
}

// ValidateStatus 检查 §5.4：status 非空时必须是 draft/stable/deprecated 之一。
// 缺省视为 stable，不报错。
func (m *Meta) ValidateStatus() error {
	if m.Status == "" {
		return nil
	}
	switch m.Status {
	case "draft", "stable", "deprecated":
		return nil
	}
	return fmt.Errorf("invalid status %q (want draft|stable|deprecated)", m.Status)
}

// ValidateStaleAfter 检查 §5.5：stale_after 存在时必须是 YYYY-MM-DD。
func (m *Meta) ValidateStaleAfter() error {
	if m.StaleAfter == nil {
		return nil
	}
	if _, err := time.Parse("2006-01-02", *m.StaleAfter); err != nil {
		return fmt.Errorf("stale_after %q is not YYYY-MM-DD", *m.StaleAfter)
	}
	return nil
}

// ValidateGenerated 检查 §5.2：generated 存在时 generated.by 必填。
func (m *Meta) ValidateGenerated() error {
	if m.Generated == nil {
		return nil
	}
	if strings.TrimSpace(m.Generated.By) == "" {
		return fmt.Errorf("generated.by is required when generated is present")
	}
	return nil
}

// ValidateSources 检查 §5.1：每个 sources entry 的 resource 必填。
func (m *Meta) ValidateSources() error {
	for i, s := range m.Sources {
		if strings.TrimSpace(s.Resource) == "" {
			return fmt.Errorf("sources[%d].resource is required", i)
		}
	}
	return nil
}

// IsDeprecated 判断概念是否已废弃。
func (m *Meta) IsDeprecated() bool {
	return m.Status == "deprecated"
}

// IsAttestedComputation 判断 type 是否为 Attested Computation。
// SPEC §10 规范形式为 "Attested Computation"（带空格），历史 bundle 常用
// snake_case "attested_computation"。两者均接受（大小写/空格归一后比较）。
func IsAttestedComputation(typ string) bool {
	canon := strings.ToLower(strings.ReplaceAll(typ, " ", "_"))
	return canon == "attested_computation"
}
