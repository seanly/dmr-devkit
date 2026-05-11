package workflow

import (
	"context"
	"fmt"
	"iter"
	"strings"
	"sync"
)

const startNode = "START"
const endNode = "END"

// RouterFunc inspects the output of a node and returns the route key that
// determines which downstream edge to follow.
type RouterFunc func(ctx context.Context, wctx *Context, input any) (route string, output any, err error)

// routerNode wraps a RouterFunc so it can live in the Nodes map.
type routerNode struct {
	name string
	fn   RouterFunc
}

func (r routerNode) Name() string { return r.name }

func (r routerNode) Run(ctx context.Context, wctx *Context, input any) (any, error) {
	key, out, err := r.fn(ctx, wctx, input)
	if err != nil {
		return nil, err
	}
	// Encode route key into output so the graph executor can read it.
	return &routerOutput{route: key, payload: out}, nil
}

type routerOutput struct {
	route   string
	payload any
}

// Graph executes nodes connected by explicit edges.  It supports sequential
// chains, branches via RouterFunc, parallel starts, and nested Workflows.
type Graph struct {
	Name  string
	Nodes map[string]Node
	// Edges define the connectivity. Each edge is From->To.
	// "START" is the reserved entry node name.
	// "END"   is the reserved terminal node name.
	Edges []Edge
}

// Edge defines a directed connection between two nodes.
type Edge struct {
	From string
	To   string
}

// AddNode registers a node in the graph.
func (g *Graph) AddNode(name string, n Node) {
	if g.Nodes == nil {
		g.Nodes = make(map[string]Node)
	}
	g.Nodes[name] = n
}

// AddEdge registers a directed edge.
func (g *Graph) AddEdge(from, to string) {
	g.Edges = append(g.Edges, Edge{From: from, To: to})
}

// AddRouter registers a router node and its outgoing conditional edges.
// The branches map is routeKey -> targetNodeName.
func (g *Graph) AddRouter(name string, router RouterFunc, branches map[string]string) {
	g.AddNode(name, routerNode{name: name, fn: router})
	for routeKey, target := range branches {
		g.Edges = append(g.Edges, Edge{From: name + "::" + routeKey, To: target})
	}
}

// AddConditionalEdges is a convenience wrapper around AddRouter that creates
// an internal router node and automatically wires from -> router.
//
//	// Before (manual): g.AddEdge("classify", "route"); g.AddRouter("route", fn, branches)
//	// After (sugar):  g.AddConditionalEdges("classify", fn, branches)
func (g *Graph) AddConditionalEdges(from string, router RouterFunc, branches map[string]string) {
	routerName := from + "::__router"
	g.AddEdge(from, routerName)
	g.AddRouter(routerName, router, branches)
}

// --- Pre-built routers ---

// ExactMatchRouter returns a RouterFunc that matches the string input against
// cases[route]. The first matching route is returned.
func ExactMatchRouter(cases map[string]string) RouterFunc {
	return func(_ context.Context, _ *Context, input any) (string, any, error) {
		s, _ := input.(string)
		for route, match := range cases {
			if s == match {
				return route, input, nil
			}
		}
		return "", input, fmt.Errorf("exact match failed for %q", s)
	}
}

// ContainsRouter returns a RouterFunc that matches when input contains
// cases[route] as a substring. The first matching route is returned.
func ContainsRouter(cases map[string]string) RouterFunc {
	return func(_ context.Context, _ *Context, input any) (string, any, error) {
		s, _ := input.(string)
		for route, match := range cases {
			if strings.Contains(s, match) {
				return route, input, nil
			}
		}
		return "", input, fmt.Errorf("substring match failed for %q", s)
	}
}

// PrefixRouter returns a RouterFunc that matches when input starts with
// cases[route]. The first matching route is returned.
func PrefixRouter(cases map[string]string) RouterFunc {
	return func(_ context.Context, _ *Context, input any) (string, any, error) {
		s, _ := input.(string)
		for route, match := range cases {
			if strings.HasPrefix(s, match) {
				return route, input, nil
			}
		}
		return "", input, fmt.Errorf("prefix match failed for %q", s)
	}
}

// Default wraps a RouterFunc so that when it fails, defaultRoute is used
// instead of returning an error.
func Default(router RouterFunc, defaultRoute string) RouterFunc {
	return func(ctx context.Context, wctx *Context, input any) (string, any, error) {
		route, out, err := router(ctx, wctx, input)
		if err != nil {
			return defaultRoute, out, nil
		}
		return route, out, nil
	}
}

// Run executes the graph starting from the START node(s) (synchronous facade).
func (g *Graph) Run(ctx context.Context, wctx *Context, input any) (any, error) {
	return runEventsFacade(g, ctx, wctx, input)
}

// RunEvents emits an event stream as the graph executes.
func (g *Graph) RunEvents(ctx context.Context, wctx *Context, input any) iter.Seq2[*Event, error] {
	return func(yield func(*Event, error) bool) {
		if wctx == nil {
			wctx = NewContext()
		}

		adj := make(map[string][]string)
		for _, e := range g.Edges {
			adj[e.From] = append(adj[e.From], e.To)
		}

		starts := adj[startNode]
		if len(starts) == 0 {
			_ = yield(nil, fmt.Errorf("workflow %q: no edges from START", g.Name))
			return
		}

		if !yield(newEvent(EventTypeWorkflowStart, g.Name, "", wctx.Step), nil) {
			return
		}

		var result *Result
		var err error
		if len(starts) == 1 && !isParallelChain(adj, starts[0]) {
			result, err = g.runSequentialEvents(ctx, wctx, adj, starts[0], input, yield)
		} else {
			result, err = g.runWithJoinEvents(ctx, wctx, adj, starts, input, yield)
		}

		endEv := newEvent(EventTypeWorkflowEnd, g.Name, "", wctx.Step)
		if result != nil {
			endEv.Result = result
		}
		if err != nil {
			if endEv.Result == nil {
				endEv.Result = &Result{Error: err}
			} else {
				endEv.Result.Error = err
			}
		}
		_ = yield(endEv, nil)
	}
}

// runSequentialEvents walks a single chain and yields events.
func (g *Graph) runSequentialEvents(ctx context.Context, wctx *Context, adj map[string][]string, start string, input any, yield func(*Event, error) bool) (*Result, error) {
	current := input
	nodeName := start
	steps := 0

	for nodeName != "" && nodeName != endNode {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("workflow %q: context cancelled at node %q: %w", g.Name, nodeName, err)
		}

		n, ok := g.Nodes[nodeName]
		if !ok {
			return nil, fmt.Errorf("workflow: node %q not found", nodeName)
		}

		if skipped, out := resumeSkip(wctx, n); skipped {
			ev := newEvent(EventTypeNodeSkip, g.Name, n.Name(), wctx.Step)
			ev.Output = out
			if !yield(ev, nil) {
				return nil, fmt.Errorf("workflow %q: consumer stopped", g.Name)
			}
			current = out
			wctx.Step++
			nodeName = nextNodeName(adj, nodeName, nil)
			continue
		}

		if !yield(newEvent(EventTypeNodeStart, g.Name, n.Name(), wctx.Step), nil) {
			return nil, fmt.Errorf("workflow %q: consumer stopped", g.Name)
		}

		out, err := runNode(ctx, wctx, wctx.Step, n, current)
		steps++

		endEv := newEvent(EventTypeNodeEnd, g.Name, n.Name(), wctx.Step)
		endEv.Output = out
		if err != nil {
			endEv.Error = err.Error()
		}
		if !yield(endEv, nil) {
			return nil, fmt.Errorf("workflow %q: consumer stopped", g.Name)
		}

		if err != nil {
			if ie, ok := err.(*InterruptError); ok {
				_ = yield(newEvent(EventTypeInterrupt, g.Name, n.Name(), wctx.Step), nil)
				return &Result{Output: ie.Value, Error: ie, Steps: steps}, ie
			}
			return &Result{Output: current, Error: err, Steps: steps}, err
		}

		if ro, ok := out.(*routerOutput); ok {
			current = ro.payload
			routeKey := nodeName + "::" + ro.route
			nextList := adj[routeKey]
			if len(nextList) == 0 {
				nextList = adj[nodeName]
			}
			if len(nextList) == 0 {
				return &Result{Output: current, Error: fmt.Errorf("no route %q from node %q", ro.route, nodeName), Steps: steps},
					fmt.Errorf("workflow %q: no route %q from node %q", g.Name, ro.route, nodeName)
			}
			nodeName = nextList[0]
			continue
		}

		current = out
		nextList := adj[nodeName]
		if len(nextList) == 0 {
			break
		}
		if len(nextList) > 1 {
			return g.runFanOutEvents(ctx, wctx, adj, nodeName, nextList, current, yield)
		}
		nodeName = nextList[0]
	}

	return &Result{Output: current, Steps: steps}, nil
}

// nextNodeName resolves the next node name from adjacency.
func nextNodeName(adj map[string][]string, from string, routerOut *routerOutput) string {
	nextList := adj[from]
	if routerOut != nil {
		routeKey := from + "::" + routerOut.route
		if r := adj[routeKey]; len(r) > 0 {
			nextList = r
		}
	}
	if len(nextList) == 0 {
		return ""
	}
	return nextList[0]
}

// runSequential walks a single chain (including router branches) until END.
// Used internally by runWithJoin for branch execution in goroutines.
func (g *Graph) runSequential(ctx context.Context, wctx *Context, adj map[string][]string, start string, input any) (*Result, error) {
	current := input
	nodeName := start
	steps := 0

	for nodeName != "" && nodeName != endNode {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("workflow %q: context cancelled at node %q: %w", g.Name, nodeName, err)
		}

		out, err := runNodeNamed(ctx, wctx, wctx.Step, g.Nodes, nodeName, current)
		steps++
		if err != nil {
			return &Result{Output: current, Error: err, Steps: steps}, err
		}

		if ro, ok := out.(*routerOutput); ok {
			current = ro.payload
			routeKey := nodeName + "::" + ro.route
			nextList := adj[routeKey]
			if len(nextList) == 0 {
				nextList = adj[nodeName]
			}
			if len(nextList) == 0 {
				return &Result{Output: current, Error: fmt.Errorf("no route %q from node %q", ro.route, nodeName), Steps: steps},
					fmt.Errorf("workflow %q: no route %q from node %q", g.Name, ro.route, nodeName)
			}
			if len(nextList) > 1 {
				nodeName = nextList[0]
				continue
			}
			nodeName = nextList[0]
			continue
		}

		current = out
		nextList := adj[nodeName]
		if len(nextList) == 0 {
			break
		}
		if len(nextList) > 1 {
			return g.runWithJoin(ctx, wctx, adj, nodeName, nextList, current)
		}
		nodeName = nextList[0]
	}

	return &Result{Output: current, Steps: steps}, nil
}

// runWithJoin executes branches in parallel and returns their results.
// Used internally by runSequential when a non-router node fans out.
func (g *Graph) runWithJoin(ctx context.Context, wctx *Context, adj map[string][]string, from string, targets []string, input any) (*Result, error) {
	type branchResult struct {
		name string
		out  any
		err  error
	}

	results := make([]branchResult, len(targets))
	var wg sync.WaitGroup

	for i, start := range targets {
		wg.Add(1)
		go func(idx int, name string) {
			defer wg.Done()
			fork := wctx.WithMetadata(nil)
			fork.Step = wctx.Step + idx
			res, err := g.runSequential(ctx, fork, adj, name, input)
			if err != nil {
				results[idx] = branchResult{name: name, err: err}
				return
			}
			results[idx] = branchResult{name: name, out: res.Output}
		}(i, start)
	}

	wg.Wait()

	outputs := make([]any, len(targets))
	steps := 0
	var errs []error
	for i, r := range results {
		outputs[i] = r.out
		steps++
		if r.err != nil {
			errs = append(errs, fmt.Errorf("branch %q: %w", r.name, r.err))
		}
	}

	joinTarget := g.findJoinTarget(targets, adj)
	if joinTarget != "" && len(errs) == 0 {
		joined := map[string]any{"branches": outputs}
		res, err := g.runSequential(ctx, wctx, adj, joinTarget, joined)
		if err != nil {
			errs = append(errs, err)
		} else {
			steps += res.Steps
			outputs = []any{res.Output}
		}
	}

	if len(errs) > 0 {
		return &Result{Output: outputs, Error: errs[0], Steps: steps}, errs[0]
	}
	return &Result{Output: outputs, Steps: steps}, nil
}

// runWithJoinEvents handles parallel START branches and any fan-out that
// requires a join barrier. Events are yielded from the main goroutine.
func (g *Graph) runWithJoinEvents(ctx context.Context, wctx *Context, adj map[string][]string, starts []string, input any, yield func(*Event, error) bool) (*Result, error) {
	type branchResult struct {
		name string
		out  any
		err  error
	}

	results := make([]branchResult, len(starts))
	var wg sync.WaitGroup

	for i, start := range starts {
		wg.Add(1)
		go func(idx int, name string) {
			defer wg.Done()
			fork := wctx.WithMetadata(nil)
			fork.Step = wctx.Step + idx
			res, err := g.runSequential(ctx, fork, adj, name, input)
			if err != nil {
				results[idx] = branchResult{name: name, err: err}
				return
			}
			results[idx] = branchResult{name: name, out: res.Output}
		}(i, start)
	}

	wg.Wait()

	outputs := make([]any, len(starts))
	steps := 0
	var errs []error
	for i, r := range results {
		outputs[i] = r.out
		if r.err != nil {
			errs = append(errs, fmt.Errorf("branch %q: %w", r.name, r.err))
		}
	}

	joinTarget := g.findJoinTarget(starts, adj)
	if joinTarget != "" && len(errs) == 0 {
		joined := map[string]any{"branches": outputs}
		res, err := g.runSequentialEvents(ctx, wctx, adj, joinTarget, joined, yield)
		if err != nil {
			errs = append(errs, err)
		} else {
			steps += res.Steps
			outputs = []any{res.Output}
		}
	}

	if len(errs) > 0 {
		return &Result{Output: outputs, Error: errs[0], Steps: steps}, errs[0]
	}
	return &Result{Output: outputs, Steps: steps}, nil
}

// runFanOutEvents executes multiple successors in parallel and then attempts
// to continue past a join point.
func (g *Graph) runFanOutEvents(ctx context.Context, wctx *Context, adj map[string][]string, from string, targets []string, input any, yield func(*Event, error) bool) (*Result, error) {
	return g.runWithJoinEvents(ctx, wctx, adj, targets, input, yield)
}

// isParallelChain checks whether a node fans out to multiple branches.
func isParallelChain(adj map[string][]string, start string) bool {
	seen := make(map[string]bool)
	var walk func(string) bool
	walk = func(n string) bool {
		if seen[n] {
			return false
		}
		seen[n] = true
		next := adj[n]
		if len(next) > 1 {
			return true
		}
		for _, c := range next {
			if walk(c) {
				return true
			}
		}
		return false
	}
	return walk(start)
}

// findJoinTarget looks for a node that is the unique common successor of
// all given branch names.  This is a simple heuristic; complex DAGs may
// require explicit JoinNode support in future.
func (g *Graph) findJoinTarget(branchNames []string, adj map[string][]string) string {
	if len(branchNames) == 0 {
		return ""
	}
	// Collect successors reachable from each branch.
	reach := make([]map[string]bool, len(branchNames))
	for i, start := range branchNames {
		reach[i] = reachable(adj, start)
	}
	// Find nodes common to all.
	for candidate := range reach[0] {
		common := true
		for i := 1; i < len(reach); i++ {
			if !reach[i][candidate] {
				common = false
				break
			}
		}
		if common && candidate != endNode {
			return candidate
		}
	}
	return ""
}

func reachable(adj map[string][]string, start string) map[string]bool {
	seen := make(map[string]bool)
	var dfs func(string)
	dfs = func(n string) {
		if seen[n] {
			return
		}
		seen[n] = true
		for _, c := range adj[n] {
			dfs(c)
		}
	}
	dfs(start)
	return seen
}
