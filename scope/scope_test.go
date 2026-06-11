package scope

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestRootScopeLifecycle(t *testing.T) {
	ctx := context.Background()
	s := Root(ctx, "test-root")

	var ran bool
	s.Go("task", func(scopedCtx context.Context) error {
		ran = true
		return nil
	})

	s.Wait()
	if !ran {
		t.Fatal("task did not run")
	}

	s.Close()
}

func TestScopeCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := Root(ctx, "cancel-test")

	var interrupted bool
	s.Go("blocker", func(scopedCtx context.Context) error {
		<-scopedCtx.Done()
		interrupted = true
		return scopedCtx.Err()
	})

	// Cancel parent context
	cancel()
	s.Wait()

	if !interrupted {
		t.Fatal("task was not interrupted by cancellation")
	}
}

func TestScopeCloseCancelsChildren(t *testing.T) {
	ctx := context.Background()
	s := Root(ctx, "parent")

	child := s.Child("child")
	var childInterrupted bool
	child.Go("blocker", func(scopedCtx context.Context) error {
		<-scopedCtx.Done()
		childInterrupted = true
		return scopedCtx.Err()
	})

	// Close parent should cascade to child
	s.Close()

	if !childInterrupted {
		t.Fatal("child task was not interrupted by parent close")
	}
}

func TestScopeErrors(t *testing.T) {
	ctx := context.Background()
	s := Root(ctx, "error-test")

	s.Go("fail-1", func(scopedCtx context.Context) error {
		return errors.New("failure one")
	})
	s.Go("fail-2", func(scopedCtx context.Context) error {
		return errors.New("failure two")
	})
	s.Go("ok", func(scopedCtx context.Context) error {
		return nil
	})

	s.Wait()
	errs := s.Errors()
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %d", len(errs))
	}

	// Verify error messages are captured
	msgMap := make(map[string]bool)
	for _, e := range errs {
		msgMap[e.Error.Error()] = true
	}
	if !msgMap["failure one"] || !msgMap["failure two"] {
		t.Fatalf("unexpected error contents: %v", errs)
	}
}

func TestScopeAllErrorsRecursive(t *testing.T) {
	ctx := context.Background()
	s := Root(ctx, "root")
	child := s.Child("child")

	s.Go("root-fail", func(scopedCtx context.Context) error {
		return errors.New("root error")
	})
	child.Go("child-fail", func(scopedCtx context.Context) error {
		return errors.New("child error")
	})

	s.Wait()
	all := s.AllErrors()
	if len(all) != 2 {
		t.Fatalf("expected 2 total errors, got %d", len(all))
	}
}

func TestScopeCleanup(t *testing.T) {
	ctx := context.Background()
	s := Root(ctx, "cleanup-test")

	var order []string
	s.OnCleanup(func() { order = append(order, "first") })
	s.OnCleanup(func() { order = append(order, "second") })
	s.OnCleanup(func() { order = append(order, "third") })

	s.Close()

	// LIFO order
	if len(order) != 3 {
		t.Fatalf("expected 3 cleanups, got %d", len(order))
	}
	if order[0] != "third" || order[1] != "second" || order[2] != "first" {
		t.Fatalf("cleanup not LIFO: %v", order)
	}
}

func TestScopeCleanupPanicRecovery(t *testing.T) {
	ctx := context.Background()
	s := Root(ctx, "cleanup-panic-test")

	var ran bool
	s.OnCleanup(func() { panic("intentional") })
	s.OnCleanup(func() { ran = true })

	// Close should not panic and should execute all cleanups
	s.Close()

	if !ran {
		t.Fatal("second cleanup was not executed after first panic")
	}
}

func TestScopeGoPanics(t *testing.T) {
	ctx := context.Background()
	s := Root(ctx, "panic-test")

	var recovered bool
	s.Go("panicker", func(scopedCtx context.Context) error {
		panic("boom")
	})
	s.Go("normal", func(scopedCtx context.Context) error {
		recovered = true
		return nil
	})

	s.Wait()

	if !recovered {
		t.Fatal("normal task was not executed after panicker")
	}

	errs := s.Errors()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error from panic, got %d", len(errs))
	}
	if errs[0].TaskName != "panicker" {
		t.Fatalf("expected panic from panicker, got %s", errs[0].TaskName)
	}
}

func TestScopeWaitTimeout(t *testing.T) {
	ctx := context.Background()
	s := Root(ctx, "timeout-test")

	s.Go("slow", func(scopedCtx context.Context) error {
		time.Sleep(500 * time.Millisecond)
		return nil
	})

	// Should return false when timeout expires
	if s.WaitTimeout(10 * time.Millisecond) {
		t.Fatal("WaitTimeout should return false when tasks are still running")
	}

	// Now wait for real
	s.Wait()
}

func TestScopeCloseWithTimeout(t *testing.T) {
	ctx := context.Background()
	s := Root(ctx, "close-timeout-test")

	s.Go("infinite", func(scopedCtx context.Context) error {
		<-scopedCtx.Done()
		return nil
	})

	// Close with short timeout should not hang
	done := make(chan struct{})
	go func() {
		s.CloseWithTimeout(50 * time.Millisecond)
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(500 * time.Millisecond):
		t.Fatal("CloseWithTimeout hung")
	}
}

func TestScopeRunning(t *testing.T) {
	ctx := context.Background()
	s := Root(ctx, "running-test")

	started := make(chan struct{})
	s.Go("blocker", func(scopedCtx context.Context) error {
		close(started)
		time.Sleep(100 * time.Millisecond)
		return nil
	})

	<-started
	if s.Running() != 1 {
		t.Fatalf("expected 1 running task, got %d", s.Running())
	}

	s.Wait()
	if s.Running() != 0 {
		t.Fatalf("expected 0 running tasks, got %d", s.Running())
	}
}

func TestScopeSpawn(t *testing.T) {
	ctx := context.Background()
	s := Root(ctx, "spawn-test")

	var childRan bool
	s.Spawn("child", func(child *Scope) error {
		childRan = true
		return nil
	})

	s.Wait()
	if !childRan {
		t.Fatal("spawned child scope did not run")
	}
}

func TestGuardConvenience(t *testing.T) {
	ctx := context.Background()
	g := Guard(ctx, "guard-test")
	defer g.Close()

	var ran bool
	g.Go("task", func(scopedCtx context.Context) error {
		ran = true
		return nil
	})

	g.Wait()
	if !ran {
		t.Fatal("guard task did not run")
	}
}

func TestScopeClosedGoNoop(t *testing.T) {
	ctx := context.Background()
	s := Root(ctx, "closed-go-test")
	s.Close()

	var ran bool
	s.Go("after-close", func(scopedCtx context.Context) error {
		ran = true
		return nil
	})

	s.Wait()
	if ran {
		t.Fatal("Go on closed scope should be a no-op")
	}
}

func TestScopeClosedChildNoop(t *testing.T) {
	ctx := context.Background()
	s := Root(ctx, "closed-child-test")
	s.Close()

	child := s.Child("child")
	if child.ctx.Err() == nil {
		t.Fatal("child of closed scope should be cancelled")
	}
}

func TestScopeContextInheritance(t *testing.T) {
	key := struct{}{}
	ctx := context.WithValue(context.Background(), key, "parent-value")
	s := Root(ctx, "inheritance-test")

	var val any
	s.Go("reader", func(scopedCtx context.Context) error {
		val = scopedCtx.Value(key)
		return nil
	})

	s.Wait()
	if val != "parent-value" {
		t.Fatalf("expected parent context value, got %v", val)
	}
}

func TestScopeErrorsAreNotDuplicatedAfterWait(t *testing.T) {
	ctx := context.Background()
	s := Root(ctx, "dedup-test")

	s.Go("fail", func(scopedCtx context.Context) error {
		return fmt.Errorf("single error")
	})

	s.Wait()
	errs1 := s.Errors()
	errs2 := s.Errors()

	if len(errs1) != 1 || len(errs2) != 1 {
		t.Fatalf("errors should not be duplicated: errs1=%d errs2=%d", len(errs1), len(errs2))
	}
}
