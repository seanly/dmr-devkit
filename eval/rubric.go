package eval

import "time"

// Rubric defines scored evaluation dimensions for agent tapes.
type Rubric struct {
	Name       string      `yaml:"name"`
	PassScore  float64     `yaml:"pass_score"`
	Dimensions []Dimension `yaml:"dimensions"`
}

// EvalMeta carries run-level metadata for a ScoreCard.
type EvalMeta struct {
	StartedAt  time.Time     `json:"started_at,omitempty"`
	FinishedAt time.Time     `json:"finished_at,omitempty"`
	Duration   time.Duration `json:"duration,omitempty"`
	GitSHA     string        `json:"git_sha,omitempty"`
	GitBranch  string        `json:"git_branch,omitempty"`
	Model      string        `json:"model,omitempty"`
	Version    string        `json:"version,omitempty"`
}

// JudgeMeta carries per-judge-call metadata (tokens, latency, cost).
type JudgeMeta struct {
	Tokens   int           `json:"tokens,omitempty"`
	Latency  time.Duration `json:"latency,omitempty"`
	Cost     float64       `json:"cost,omitempty"`
	Model    string        `json:"model,omitempty"`
	CacheHit bool          `json:"cache_hit,omitempty"`
}

// CostMeta aggregates judge costs across dimensions.
type CostMeta struct {
	Calls     int           `json:"calls"`
	Tokens    int           `json:"tokens"`
	Latency   time.Duration `json:"latency"`
	Cost      float64       `json:"cost"`
	CacheHits int           `json:"cache_hits"`
}

// CostTracker collects judge costs across dimensions.
type CostTracker struct {
	calls     int
	tokens    int
	latency   time.Duration
	cost      float64
	cacheHits int
}

// Add records a single judge call's metadata.
func (c *CostTracker) Add(meta JudgeMeta) {
	c.calls++
	c.tokens += meta.Tokens
	c.latency += meta.Latency
	c.cost += meta.Cost
	if meta.CacheHit {
		c.cacheHits++
	}
}

// Snapshot returns an immutable copy of the aggregated cost.
func (c *CostTracker) Snapshot() *CostMeta {
	if c == nil {
		return nil
	}
	return &CostMeta{
		Calls:     c.calls,
		Tokens:    c.tokens,
		Latency:   c.latency,
		Cost:      c.cost,
		CacheHits: c.cacheHits,
	}
}

type Dimension struct {
	ID            string      `yaml:"id"`
	Weight        float64     `yaml:"weight"`
	PassScore     float64     `yaml:"pass_score,omitempty"`
	Aggregation   string      `yaml:"aggregation,omitempty"`
	Assertions    []Assertion `yaml:"assertions,omitempty"`
	SubDimensions []Dimension `yaml:"sub_dimensions,omitempty"`
	Judge         *JudgeSpec  `yaml:"judge,omitempty"`
}

type Assertion struct {
	Type    string         `yaml:"type"`
	Name    string         `yaml:"name,omitempty"`
	Field   string         `yaml:"field,omitempty"`
	Key     string         `yaml:"key,omitempty"`
	Value   string         `yaml:"value,omitempty"`
	Regex   string         `yaml:"regex,omitempty"`
	Role    string         `yaml:"role,omitempty"`
	Min     int            `yaml:"min_count,omitempty"`
	Max     int            `yaml:"max_count,omitempty"`
	Names   []string       `yaml:"names,omitempty"`
	Negate  bool           `yaml:"negate,omitempty"`
	AnyOf   []Assertion    `yaml:"any_of,omitempty"`
	Because string         `yaml:"because,omitempty"`
	Extra   map[string]any `yaml:",inline"`
}

type JudgeSpec struct {
	ModelEnv string `yaml:"model_env"`
	Prompt   string `yaml:"prompt"`
	MinScore int    `yaml:"min_score"`
	MaxScore int    `yaml:"max_score"`
}

// ScoreCard is the outcome of evaluating a tape against a rubric.
type ScoreCard struct {
	Rubric     string            `json:"rubric"`
	Passed     bool              `json:"passed"`
	Score      float64           `json:"score"`
	PassScore  float64           `json:"pass_score"`
	Results    []DimensionResult `json:"results"`
	Metadata   *EvalMeta         `json:"metadata,omitempty"`
	Cost       *CostMeta         `json:"cost,omitempty"`
	Statistics *StochasticStats  `json:"statistics,omitempty"`
}

type DimensionResult struct {
	ID               string            `json:"id"`
	Weight           float64           `json:"weight"`
	Score            float64           `json:"score"`
	Passed           bool              `json:"passed"`
	Skipped          bool              `json:"skipped,omitempty"`
	Detail           string            `json:"detail,omitempty"`
	JudgeMeta        *JudgeMeta        `json:"judge_meta,omitempty"`
	AssertionResults []AssertionResult `json:"assertion_results,omitempty"`
	SubResults       []DimensionResult `json:"sub_results,omitempty"`
}

// AssertionResult captures the outcome of a single assertion.
type AssertionResult struct {
	Type       string `json:"type"`
	Passed     bool   `json:"passed"`
	Expected   string `json:"expected,omitempty"`
	Actual     string `json:"actual,omitempty"`
	Because    string `json:"because,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
}

// StochasticSpec configures repeated runs for non-deterministic evaluation.
type StochasticSpec struct {
	Runs                 int     `yaml:"runs"`
	SuccessRateThreshold float64 `yaml:"success_rate_threshold"`
	ScoreThreshold       float64 `yaml:"score_threshold"`
}

// StochasticStats aggregates repeated evaluation runs.
type StochasticStats struct {
	Runs        int     `json:"runs"`
	SuccessRate float64 `json:"success_rate"`
	Mean        float64 `json:"mean"`
	StdDev      float64 `json:"stddev"`
	Min         float64 `json:"min"`
	Max         float64 `json:"max"`
}

// Options configures tape evaluation (optional LLM judge dimensions).
type Options struct {
	Judge JudgeFunc
	Cost  *CostTracker
}
