package agent

import (
	"testing"

	"github.com/seanly/dmr-devkit/config"
)

func TestShouldAutoHandoffByEstimate(t *testing.T) {
	// Create a test agent with config
	a := &Agent{
		config: Config{
			AgentPolicy: config.AgentConfig{
				MaxToken:         100000,
				HandoffThreshold: 0.8,
			},
			Models: []config.ModelConfig{
				{
					Name:             "test-model",
					Model:            "test-model",
					Default:          true,
					MaxToken:         100000,
					HandoffThreshold: 0.8,
				},
			},
		},
	}

	tests := []struct {
		name            string
		estimatedTokens int
		expected        bool
	}{
		{"below threshold", 70000, false}, // 70k < 80k (100k * 0.8)
		{"at threshold", 80000, true},     // 80k = 80k
		{"above threshold", 90000, true},  // 90k > 80k
		{"zero tokens", 0, false},         // 0 should not trigger
		{"negative tokens", -1, false},    // negative should not trigger
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := a.shouldAutoHandoffByEstimate("test-tape", test.estimatedTokens)
			if result != test.expected {
				t.Errorf("shouldAutoHandoffByEstimate(%d) = %v, expected %v",
					test.estimatedTokens, result, test.expected)
			}
		})
	}
}

func TestShouldAutoHandoffByEstimateNoConfig(t *testing.T) {
	// Test with no model config
	a := &Agent{
		config: Config{
			AgentPolicy: config.AgentConfig{
				MaxToken:         0, // No limit
				HandoffThreshold: 0.8,
			},
		},
	}

	result := a.shouldAutoHandoffByEstimate("test-tape", 90000)
	if result != false {
		t.Error("Should not trigger when limit is 0")
	}
}

func TestPreemptiveCompactScenario(t *testing.T) {
	// Integration test scenario:
	// This is more of a documentation of the expected flow

	scenario := struct {
		contextWindow   int
		threshold       float64
		estimatedTokens int
		shouldTrigger   bool
	}{
		contextWindow:   16000,
		threshold:       0.7,
		estimatedTokens: 12000, // 12k > 11.2k (16k * 0.7)
		shouldTrigger:   true,
	}

	threshold := int(float64(scenario.contextWindow) * scenario.threshold)
	if scenario.estimatedTokens >= threshold != scenario.shouldTrigger {
		t.Errorf("Scenario failed: %d tokens with %d threshold should trigger=%v",
			scenario.estimatedTokens, threshold, scenario.shouldTrigger)
	}
}
