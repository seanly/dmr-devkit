package sqlcompat

import (
	"fmt"
	"strings"
	"sync"
)

// Result holds one evaluation case outcome.
type Result struct {
	ID      string
	Driver  Driver
	Status  Status
	Detail  string
	Blocker bool
}

// Status is PASS, FAIL, or SKIP.
type Status string

const (
	StatusPass Status = "PASS"
	StatusFail Status = "FAIL"
	StatusSkip Status = "SKIP"
)

// Report aggregates evaluation results for impact analysis.
type Report struct {
	mu      sync.Mutex
	Results []Result
}

func (r *Report) Add(id string, driver Driver, status Status, detail string, blocker bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Results = append(r.Results, Result{
		ID: id, Driver: driver, Status: status, Detail: detail, Blocker: blocker,
	})
}

// Summary returns a markdown-friendly table for turso-impact.md.
func (r *Report) Summary() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	type row struct {
		id, modernc, turso, note, level string
	}
	byID := map[string]*row{}
	order := []string{}

	add := func(id string) *row {
		if byID[id] == nil {
			byID[id] = &row{id: id}
			order = append(order, id)
		}
		return byID[id]
	}

	for _, res := range r.Results {
		row := add(res.ID)
		cell := string(res.Status)
		if res.Detail != "" && res.Status == StatusFail {
			cell += " (" + res.Detail + ")"
		}
		switch res.Driver {
		case DriverModernc:
			row.modernc = cell
		case DriverTurso:
			row.turso = cell
			if res.Blocker {
				row.level = "blocker"
			} else if res.Status == StatusFail {
				row.level = "warn"
			} else if row.level == "" {
				row.level = "ok"
			}
			if res.Detail != "" {
				row.note = res.Detail
			}
		}
	}

	var b strings.Builder
	b.WriteString("| 用例 | modernc | tursogo | 差异说明 | 阻塞级别 |\n")
	b.WriteString("|------|---------|---------|---------|----------|\n")
	for _, id := range order {
		row := byID[id]
		if row.modernc == "" {
			row.modernc = "—"
		}
		if row.turso == "" {
			row.turso = "—"
		}
		if row.level == "" {
			row.level = "ok"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
			row.id, row.modernc, row.turso, row.note, row.level)
	}
	return b.String()
}

func (r *Report) BlockerCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, res := range r.Results {
		if res.Driver == DriverTurso && res.Blocker && res.Status == StatusFail {
			n++
		}
	}
	return n
}

func (r *Report) TursoFailCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, res := range r.Results {
		if res.Driver == DriverTurso && res.Status == StatusFail {
			n++
		}
	}
	return n
}
