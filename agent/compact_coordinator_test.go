package agent

import (
	"testing"

	"github.com/seanly/dmr-devkit/config"
)

func TestCompactCoordinator_CooldownAndFirstAllow(t *testing.T) {
	c := newCompactCoordinator()
	cfg := config.ContextConfig{CompactGap: 3, PressureOverrideGap: 1}

	if !c.ShouldCompact(1, 0, 0, 0, cfg, triggerPreemptive) {
		t.Fatal("first compact should be allowed")
	}
	c.RecordCompact(1, triggerPreemptive)

	// Within cooldown: rejected without pressure.
	if c.ShouldCompact(2, 100, 10_000, 0.8, cfg, triggerPreemptive) {
		t.Fatal("compact within cooldown should be rejected")
	}
	// Pressure override: tokens past threshold and gap >= pressure gap.
	if !c.ShouldCompact(2, 9000, 10_000, 0.8, cfg, triggerPreemptive) {
		t.Fatal("pressure override should allow compact")
	}
	// After cooldown elapses, allowed again.
	if !c.ShouldCompact(5, 100, 10_000, 0.8, cfg, triggerPreemptive) {
		t.Fatal("compact after cooldown should be allowed")
	}
}

func TestCompactCoordinator_MaxCompactsCap(t *testing.T) {
	c := newCompactCoordinator()
	cfg := config.ContextConfig{CompactGap: 1, MaxCompactsPerAnchor: 2}

	c.RecordCompact(1, triggerProactive)
	c.RecordCompact(2, triggerProactive)
	// Two voluntary compacts recorded → cap reached.
	if c.ShouldCompact(10, 0, 0, 0, cfg, triggerProactive) {
		t.Fatal("voluntary compact should be rejected after reaching cap")
	}
	// Reactive bypasses the cap.
	if !c.ShouldCompact(10, 0, 0, 0, cfg, triggerReactive) {
		t.Fatal("reactive compact should bypass cap")
	}
	// Manual bypasses the cap.
	if !c.ShouldCompact(10, 0, 0, 0, cfg, triggerManual) {
		t.Fatal("manual compact should bypass cap")
	}
}

func TestCompactCoordinator_CycleReset(t *testing.T) {
	c := newCompactCoordinator()
	cfg := config.ContextConfig{CompactGap: 3, MaxCompactsPerAnchor: 1}

	c.RecordCompact(5, triggerPreemptive)
	if c.ShouldCompact(10, 0, 0, 0, cfg, triggerPreemptive) {
		t.Fatal("cap should block after one voluntary compact")
	}
	// New cycle: step wraps below lastCompactStep → reset.
	if !c.ShouldCompact(1, 0, 0, 0, cfg, triggerPreemptive) {
		t.Fatal("new cycle should reset coordinator and allow compact")
	}
	if c.CompactCount() != 0 {
		t.Fatalf("compact count should be reset, got %d", c.CompactCount())
	}
}

func TestCompactCoordinator_RecordCountsOnlyVoluntary(t *testing.T) {
	c := newCompactCoordinator()
	c.RecordCompact(1, triggerReactive)
	if c.CompactCount() != 0 {
		t.Fatalf("reactive should not count, got %d", c.CompactCount())
	}
	c.RecordCompact(2, triggerManual)
	if c.CompactCount() != 0 {
		t.Fatalf("manual should not count, got %d", c.CompactCount())
	}
	c.RecordCompact(3, triggerProactive)
	if c.CompactCount() != 1 {
		t.Fatalf("proactive should count, got %d", c.CompactCount())
	}
}

func TestCompactCoordinator_SnipAttempted(t *testing.T) {
	c := newCompactCoordinator()
	if c.SnipAttempted() {
		t.Fatal("snip should start unattempted")
	}
	c.MarkSnipAttempted()
	if !c.SnipAttempted() {
		t.Fatal("snip should be marked attempted")
	}
	c.RecordCompact(1, triggerPreemptive)
	if c.SnipAttempted() {
		t.Fatal("recording compact should clear snip flag")
	}
}
