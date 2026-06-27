package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// RestartPolicy decides how a Supervisor reacts when one supervised agent fails.
type RestartPolicy int

const (
	// OneForOne restarts only the failed agent.
	OneForOne RestartPolicy = iota
	// OneForAll restarts all supervised agents.
	OneForAll
	// RestForOne restarts the failed agent and all agents that depend on it.
	RestForOne
)

// String returns the canonical policy name.
func (p RestartPolicy) String() string {
	switch p {
	case OneForOne:
		return "one_for_one"
	case OneForAll:
		return "one_for_all"
	case RestForOne:
		return "rest_for_one"
	default:
		return fmt.Sprintf("RestartPolicy(%d)", p)
	}
}

// SupervisedRun is the unit of work executed under a Supervisor.
// It returns the agent result and any error. Panics are caught by the Supervisor.
type SupervisedRun func(ctx context.Context) (*Result, error)

// SupervisedAgent describes an agent monitored by a Supervisor.
type SupervisedAgent struct {
	ID        string
	Run       SupervisedRun
	DependsOn []string // for RestForOne: IDs of upstream agents
}

// FailureReport is emitted on the Supervisor's failure channel whenever an
// agent fails and the Supervisor makes a restart decision.
type FailureReport struct {
	ID        string
	Err       error
	Restarted bool
	Permanent bool
	Policy    RestartPolicy
}

// Supervisor provides Erlang/OTP-style process supervision for Agent runs.
// It runs agents in isolated goroutines, catches panics, enforces a restart
// budget, and can restart dependents according to the configured policy.
//
// When an Agent run is restarted, the caller's Run function is responsible for
// recovering state (e.g. via tape.TapeController.ReplayExec).
type Supervisor struct {
	policy      RestartPolicy
	maxRestarts int
	window      time.Duration
	agents      []*SupervisedAgent

	mu       sync.Mutex
	running  map[string]context.CancelFunc
	restarts map[string][]time.Time
	failures chan FailureReport
	wg       sync.WaitGroup
}

// NewSupervisor creates a Supervisor with the given policy and restart budget.
// maxRestarts is the maximum number of restarts allowed within window for each
// agent. A zero window disables the budget (unlimited restarts).
func NewSupervisor(policy RestartPolicy, maxRestarts int, window time.Duration) *Supervisor {
	if maxRestarts < 0 {
		maxRestarts = 0
	}
	return &Supervisor{
		policy:      policy,
		maxRestarts: maxRestarts,
		window:      window,
		running:     make(map[string]context.CancelFunc),
		restarts:    make(map[string][]time.Time),
		failures:    make(chan FailureReport, 64),
	}
}

// Register adds an agent to be supervised. Must be called before Start.
func (s *Supervisor) Register(a *SupervisedAgent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agents = append(s.agents, a)
}

// Failures returns the channel of failure reports. It is closed when the
// Supervisor stops.
func (s *Supervisor) Failures() <-chan FailureReport {
	return s.failures
}

// Start runs all registered agents under supervision until the context is
// cancelled or all agents fail permanently.
func (s *Supervisor) Start(ctx context.Context) error {
	s.mu.Lock()
	agents := make([]*SupervisedAgent, len(s.agents))
	copy(agents, s.agents)
	s.mu.Unlock()

	if len(agents) == 0 {
		return nil
	}

	for _, a := range agents {
		a := a
		s.wg.Add(1)
		go s.supervise(ctx, a)
	}

	s.wg.Wait()
	close(s.failures)
	return ctx.Err()
}

// Run executes a single supervised run. It catches panics and retries according
// to the Supervisor's policy and restart budget.
func (s *Supervisor) Run(ctx context.Context, id string, run SupervisedRun) (*Result, error) {
	for {
		res, err, retry := s.runOnce(ctx, id, run)
		if !retry {
			return res, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
}

func (s *Supervisor) runOnce(ctx context.Context, id string, run SupervisedRun) (*Result, error, bool) {
	resCh := make(chan struct {
		res *Result
		err error
	}, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				resCh <- struct {
					res *Result
					err error
				}{nil, fmt.Errorf("panic: %v", r)}
			}
		}()
		res, err := run(ctx)
		resCh <- struct {
			res *Result
			err error
		}{res, err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err(), false
	case out := <-resCh:
		if out.err == nil {
			return out.res, nil, false
		}
		retry := s.handleFailure(id, out.err)
		return out.res, out.err, retry
	}
}

func (s *Supervisor) supervise(ctx context.Context, a *SupervisedAgent) {
	defer s.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		res, err, retry := s.runOnce(ctx, a.ID, a.Run)
		if !retry {
			if err != nil {
				s.reportFailure(a.ID, err, false, true)
			}
			return
		}
		_ = res
	}
}

func (s *Supervisor) handleFailure(id string, err error) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.maxRestarts > 0 && s.window > 0 {
		now := time.Now()
		cutoff := now.Add(-s.window)
		times := s.restarts[id]
		kept := times[:0]
		for _, t := range times {
			if t.After(cutoff) {
				kept = append(kept, t)
			}
		}
		s.restarts[id] = kept

		if len(kept) >= s.maxRestarts {
			s.reportFailureLocked(id, err, false, true)
			return false
		}
		s.restarts[id] = append(s.restarts[id], now)
	}

	s.reportFailureLocked(id, err, true, false)

	switch s.policy {
	case OneForAll:
		s.restartAllLocked(id)
	case RestForOne:
		s.restartRestLocked(id)
	}

	if s.policy != OneForOne {
		slog.Info("supervisor: applied restart policy",
			"policy", s.policy.String(),
			"failed_id", id,
		)
	}

	return true
}

func (s *Supervisor) restartAllLocked(exceptID string) {
	for id, cancel := range s.running {
		if id == exceptID {
			continue
		}
		cancel()
	}
}

func (s *Supervisor) restartRestLocked(failedID string) {
	dependents := s.dependentsOf(failedID)
	for id, cancel := range s.running {
		if id == failedID {
			continue
		}
		if _, ok := dependents[id]; ok {
			cancel()
		}
	}
}

func (s *Supervisor) dependentsOf(root string) map[string]struct{} {
	deps := make(map[string]struct{})
	queue := []string{root}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, a := range s.agents {
			for _, d := range a.DependsOn {
				if d == cur {
					if _, ok := deps[a.ID]; !ok {
						deps[a.ID] = struct{}{}
						queue = append(queue, a.ID)
					}
				}
			}
		}
	}
	return deps
}

func (s *Supervisor) reportFailure(id string, err error, restarted, permanent bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reportFailureLocked(id, err, restarted, permanent)
}

func (s *Supervisor) reportFailureLocked(id string, err error, restarted, permanent bool) {
	select {
	case s.failures <- FailureReport{
		ID:        id,
		Err:       err,
		Restarted: restarted,
		Permanent: permanent,
		Policy:    s.policy,
	}:
	default:
		slog.Warn("supervisor: failure report dropped (channel full)", "id", id)
	}
}

// AgentRun returns a SupervisedRun that invokes agent.RunWithOptsAndTools.
// It is a convenience helper for wiring an *Agent into a Supervisor.
func AgentRun(a *Agent, tapeName, prompt string, allowedTools *[]string, contextJSON string) SupervisedRun {
	return func(ctx context.Context) (*Result, error) {
		return a.RunWithOptsAndTools(ctx, tapeName, prompt, 0, 0, allowedTools, contextJSON)
	}
}
