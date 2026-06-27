package workflow

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// concurrencyProbe sleeps for a short time and tracks the maximum number of
// concurrently running instances via an atomic counter.
type concurrencyProbe struct {
	name       string
	duration   time.Duration
	current    *atomic.Int64
	maxCurrent *atomic.Int64
}

func (n concurrencyProbe) Name() string { return n.name }

func (n concurrencyProbe) Run(_ context.Context, _ *Context, _ any) (any, error) {
	cur := n.current.Add(1)
	for {
		max := n.maxCurrent.Load()
		if cur <= max || n.maxCurrent.CompareAndSwap(max, cur) {
			break
		}
	}
	defer n.current.Add(-1)
	time.Sleep(n.duration)
	return n.name, nil
}

func TestParallel_MaxConcurrentLimitsConcurrency(t *testing.T) {
	current := &atomic.Int64{}
	max := &atomic.Int64{}
	nodes := make([]Node, 5)
	for i := range nodes {
		nodes[i] = concurrencyProbe{
			name:       fmt.Sprintf("n%d", i),
			duration:   100 * time.Millisecond,
			current:    current,
			maxCurrent: max,
		}
	}

	p := &Parallel{
		WorkflowName:  "limited",
		Nodes:         nodes,
		MaxConcurrent: 2,
	}
	_, err := p.Run(context.Background(), NewContext(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := max.Load(); got > 2 {
		t.Fatalf("expected max concurrency 2, got %d", got)
	}
}

func TestParallel_MaxConcurrentZeroIsUnlimited(t *testing.T) {
	current := &atomic.Int64{}
	max := &atomic.Int64{}
	nodes := make([]Node, 5)
	for i := range nodes {
		nodes[i] = concurrencyProbe{
			name:       fmt.Sprintf("n%d", i),
			duration:   50 * time.Millisecond,
			current:    current,
			maxCurrent: max,
		}
	}

	p := &Parallel{
		WorkflowName: "unlimited",
		Nodes:        nodes,
	}
	_, err := p.Run(context.Background(), NewContext(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := max.Load(); got < 3 {
		t.Fatalf("expected high concurrency without limit, got %d", got)
	}
}

func TestGraph_MaxConcurrentLimitsFanOut(t *testing.T) {
	current := &atomic.Int64{}
	max := &atomic.Int64{}
	g := &Graph{
		Name:          "fanout",
		MaxConcurrent: 2,
	}
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("branch%d", i)
		g.AddNode(name, concurrencyProbe{
			name:       name,
			duration:   100 * time.Millisecond,
			current:    current,
			maxCurrent: max,
		})
		g.AddEdge("START", name)
	}

	_, err := g.Run(context.Background(), NewContext(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := max.Load(); got > 2 {
		t.Fatalf("expected max concurrency 2, got %d", got)
	}
}
