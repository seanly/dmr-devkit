package eval

// Rubric defines scored evaluation dimensions for agent tapes.
type Rubric struct {
	Name      string      `yaml:"name"`
	PassScore float64     `yaml:"pass_score"`
	Dimensions []Dimension `yaml:"dimensions"`
}

type Dimension struct {
	ID         string      `yaml:"id"`
	Weight     float64     `yaml:"weight"`
	Assertions []Assertion `yaml:"assertions"`
	Judge      *JudgeSpec  `yaml:"judge,omitempty"`
}

type Assertion struct {
	Type  string         `yaml:"type"`
	Name  string         `yaml:"name,omitempty"`
	Field string         `yaml:"field,omitempty"`
	Key   string         `yaml:"key,omitempty"`
	Value string         `yaml:"value,omitempty"`
	Regex string         `yaml:"regex,omitempty"`
	Role  string         `yaml:"role,omitempty"`
	Min   int            `yaml:"min_count,omitempty"`
	Extra map[string]any `yaml:",inline"`
}

type JudgeSpec struct {
	ModelEnv string `yaml:"model_env"`
	Prompt   string `yaml:"prompt"`
	MinScore int    `yaml:"min_score"`
	MaxScore int    `yaml:"max_score"`
}

// ScoreCard is the outcome of evaluating a tape against a rubric.
type ScoreCard struct {
	Rubric   string            `json:"rubric"`
	Passed   bool              `json:"passed"`
	Score    float64           `json:"score"`
	PassScore float64          `json:"pass_score"`
	Results  []DimensionResult `json:"results"`
}

type DimensionResult struct {
	ID      string  `json:"id"`
	Weight  float64 `json:"weight"`
	Score   float64 `json:"score"`
	Passed  bool    `json:"passed"`
	Skipped bool    `json:"skipped,omitempty"`
	Detail  string  `json:"detail,omitempty"`
}

// Options configures tape evaluation (optional LLM judge dimensions).
type Options struct {
	Judge JudgeFunc
}
