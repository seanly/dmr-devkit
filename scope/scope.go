// Package scope provides structured concurrency for DMR.
//
// A Scope forms a tree: each child scope inherits cancellation from its parent,
// and when a scope is closed, all descendants are recursively cancelled and cleaned up.
//
// This replaces ad-hoc goroutine + sync.WaitGroup patterns with a unified lifecycle model.
package scope

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Scope is an execution domain with structured lifecycle.
type Scope struct {
	id       string
	parent   *Scope
	children map[string]*Scope
	ctx      context.Context
	cancel   context.CancelFunc

	mu        sync.RWMutex
	running   map[string]*Task // keyed by taskID
	errors    []ScopedError
	onCleanup []func()
	closed    bool
}

// Task represents a running unit of work within a scope.
type Task struct {
	ID       string
	Name     string
	Start    time.Time
	End      time.Time
	Error    error
	Canceled bool
}

// ScopedError attaches task identity to an error.
type ScopedError struct {
	TaskID string
	TaskName string
	Error  error
}

// Root creates a root scope from a context.
func Root(ctx context.Context, id string) *Scope {
	ctx, cancel := context.WithCancel(ctx)
	return &Scope{
		id:      id,
		ctx:     ctx,
		cancel:  cancel,
		children: make(map[string]*Scope),
		running:  make(map[string]*Task),
	}
}

// Child creates a child scope. Closing the parent will close all children.
func (s *Scope) Child(id string) *Scope {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		// Return a no-op scope so callers don't panic
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // immediately cancelled
		return &Scope{id: id, parent: s, ctx: ctx, cancel: cancel, closed: true}
	}

	childCtx, childCancel := context.WithCancel(s.ctx)
	child := &Scope{
		id:       id,
		parent:   s,
		ctx:      childCtx,
		cancel:   childCancel,
		children: make(map[string]*Scope),
		running:  make(map[string]*Task),
	}
	s.children[id] = child
	return child
}

// Ctx returns the scope's context.
func (s *Scope) Ctx() context.Context { return s.ctx }

// ID returns the scope identifier.
func (s *Scope) ID() string { return s.id }

// Go launches a function in a new goroutine tracked by this scope.
// The scope waits for all Go'd functions before closing.
func (s *Scope) Go(taskID string, fn func(ctx context.Context) error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		slog.Warn("scope.Go called on closed scope", "scope", s.id, "task", taskID)
		return
	}
	task := &Task{ID: taskID, Name: taskID, Start: time.Now()}
	s.running[taskID] = task
	s.mu.Unlock()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.mu.Lock()
				task.Error = fmt.Errorf("panic: %v", r)
				task.End = time.Now()
				s.errors = append(s.errors, ScopedError{TaskID: taskID, TaskName: task.Name, Error: task.Error})
				delete(s.running, taskID)
				s.mu.Unlock()
				slog.Error("scope task panic", "scope", s.id, "task", taskID, "panic", r)
			}
		}()

		err := fn(s.ctx)

		s.mu.Lock()
		task.Error = err
		task.End = time.Now()
		if s.ctx.Err() != nil {
			task.Canceled = true
		}
		if err != nil {
			s.errors = append(s.errors, ScopedError{TaskID: taskID, TaskName: task.Name, Error: err})
		}
		delete(s.running, taskID)
		s.mu.Unlock()
	}()
}

// Spawn launches a child scope to run fn. The parent automatically
// tracks the child and propagates cancellation.
func (s *Scope) Spawn(childID string, fn func(child *Scope) error) {
	child := s.Child(childID)
	s.Go("spawn:"+childID, func(ctx context.Context) error {
		defer child.Close()
		return fn(child)
	})
}

// OnCleanup registers a function to run when the scope closes.
// Cleanup runs in LIFO order, and panics in one cleanup do not stop others.
func (s *Scope) OnCleanup(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		// Already closed; run immediately in recovery-safe way
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("cleanup panic (late)", "scope", s.id, "panic", r)
				}
			}()
			fn()
		}()
		return
	}
	s.onCleanup = append(s.onCleanup, fn)
}

// Wait blocks until all tasks in this scope complete.
// It waits for goroutines to finish even after cancellation so that
// cleanup and error collection are deterministic.
func (s *Scope) Wait() {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		s.mu.RLock()
		running := len(s.running)
		s.mu.RUnlock()
		if running == 0 {
			return
		}
		<-ticker.C
	}
}

// WaitTimeout blocks until all tasks complete or timeout expires.
// Returns true if all finished, false on timeout.
func (s *Scope) WaitTimeout(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		s.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// Errors returns all errors collected from tasks in this scope (not recursive).
func (s *Scope) Errors() []ScopedError {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ScopedError, len(s.errors))
	copy(out, s.errors)
	return out
}

// AllErrors recursively collects errors from this scope and all descendants.
func (s *Scope) AllErrors() []ScopedError {
	var out []ScopedError
	s.walkErrors(&out)
	return out
}

func (s *Scope) walkErrors(out *[]ScopedError) {
	s.mu.RLock()
	*out = append(*out, s.errors...)
	children := make([]*Scope, 0, len(s.children))
	for _, c := range s.children {
		children = append(children, c)
	}
	s.mu.RUnlock()
	for _, c := range children {
		c.walkErrors(out)
	}
}

// Running returns the number of active tasks in this scope (not recursive).
func (s *Scope) Running() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.running)
}

// Close cancels the scope, waits for tasks, and runs cleanup hooks.
// It is safe to call multiple times.
func (s *Scope) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true

	// Cancel this scope (and therefore all children via context chain)
	if s.cancel != nil {
		s.cancel()
	}

	// Copy cleanup list
	cleanups := make([]func(), len(s.onCleanup))
	copy(cleanups, s.onCleanup)
	s.onCleanup = nil

	// Copy children for recursive close
	childList := make([]*Scope, 0, len(s.children))
	for _, c := range s.children {
		childList = append(childList, c)
	}
	s.mu.Unlock()

	// Wait for own tasks
	s.Wait()

	// Recursively close children (they're already cancelled by context)
	for _, c := range childList {
		c.Close()
	}

	// Run cleanups in LIFO order with panic recovery
	for i := len(cleanups) - 1; i >= 0; i-- {
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("scope cleanup panic", "scope", s.id, "panic", r)
				}
			}()
			cleanups[i]()
		}()
	}
}

// CloseWithTimeout calls Close but returns after timeout even if tasks haven't finished.
func (s *Scope) CloseWithTimeout(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		s.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		slog.Warn("scope close timed out", "scope", s.id, "timeout", timeout)
	}
}

// Guard is a convenience constructor for a resource-guard pattern.
// Usage:
//
//	guard := scope.Guard(ctx, "myFunc")
//	defer guard.Close()
//	guard.OnCleanup(func() { db.Close() })
//	guard.Go("worker", func(ctx context.Context) error { ... })
func Guard(ctx context.Context, id string) *Scope {
	return Root(ctx, id)
}
