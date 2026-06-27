package agent

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSupervisorRunRetriesOnError(t *testing.T) {
	s := NewSupervisor(OneForOne, 2, time.Minute)
	calls := 0
	run := func(ctx context.Context) (*Result, error) {
		calls++
		if calls < 2 {
			return nil, errors.New("transient")
		}
		return &Result{Output: "ok"}, nil
	}

	res, err := s.Run(context.Background(), "a", run)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Output != "ok" {
		t.Fatalf("expected ok, got %q", res.Output)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}

func TestSupervisorRunPermanentAfterBudget(t *testing.T) {
	s := NewSupervisor(OneForOne, 1, time.Minute)
	calls := 0
	run := func(ctx context.Context) (*Result, error) {
		calls++
		return nil, errors.New("always fails")
	}

	_, err := s.Run(context.Background(), "a", run)
	if err == nil {
		t.Fatal("expected error")
	}
	// initial run + 1 retry = 2 calls
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}

func TestSupervisorRunRecoversPanic(t *testing.T) {
	s := NewSupervisor(OneForOne, 1, time.Minute)
	calls := 0
	run := func(ctx context.Context) (*Result, error) {
		calls++
		if calls == 1 {
			panic("boom")
		}
		return &Result{Output: "recovered"}, nil
	}

	res, err := s.Run(context.Background(), "a", run)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Output != "recovered" {
		t.Fatalf("expected recovered, got %q", res.Output)
	}
}

func TestSupervisorStartOneForAll(t *testing.T) {
	s := NewSupervisor(OneForAll, 1, time.Minute)

	callsA := 0
	callsB := 0
	done := make(chan struct{})

	s.Register(&SupervisedAgent{
		ID: "a",
		Run: func(ctx context.Context) (*Result, error) {
			callsA++
			if callsA == 1 {
				return nil, errors.New("fail")
			}
			close(done)
			return &Result{Output: "a-ok"}, nil
		},
	})
	s.Register(&SupervisedAgent{
		ID: "b",
		Run: func(ctx context.Context) (*Result, error) {
			callsB++
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		_ = s.Start(ctx)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("timeout waiting for restart")
	}
	cancel()

	if callsA != 2 {
		t.Fatalf("expected a to be called twice, got %d", callsA)
	}
}

func TestRestartPolicyString(t *testing.T) {
	if OneForOne.String() != "one_for_one" {
		t.Fatalf("unexpected one_for_one string: %s", OneForOne.String())
	}
	if OneForAll.String() != "one_for_all" {
		t.Fatalf("unexpected one_for_all string: %s", OneForAll.String())
	}
	if RestForOne.String() != "rest_for_one" {
		t.Fatalf("unexpected rest_for_one string: %s", RestForOne.String())
	}
}
