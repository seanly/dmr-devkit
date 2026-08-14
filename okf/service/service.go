// Package service provides the shared OKF consumer operations used by both the
// MCP server (pkg/mcp) and the playground (internal/playground). All methods
// return map[string]any with snake_case keys so the two surfaces produce
// identical, SPEC-aligned JSON regardless of whether the underlying structs
// carry json tags.
package service

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/seanly/dmr-devkit/okf/frontmatter"
	"github.com/seanly/dmr-devkit/okf/bundle"
	"github.com/seanly/dmr-devkit/okf/graph"
	"github.com/seanly/dmr-devkit/okf/search"
	"github.com/seanly/dmr-devkit/okf/vcs"
)

// Service wraps a loaded bundle and its derived graph, exposing the consumer
// operations shared by the MCP server and the playground.
//
// Concurrency: a read/write mutex guards both the in-memory Bundle and the
// cached Graph. Mutation methods (Create/Update/Delete) take the write lock
// and rebuild the graph; read methods take the read lock. This is required
// because the playground serves GET /api/graph concurrently with agent-driven
// writes, and concurrent map writes panic in Go.
type Service struct {
	b  *bundle.Bundle
	g  *graph.Graph
	mu sync.RWMutex
	w  *bundle.Writer
	// idx is an optional persistent FTS5 index. When nil, SearchConcepts
	// falls back to the in-memory Bundle.Search scan.
	idx *search.Index
	// vcs is an optional Git versioning handle. When nil, the Commit/Revert/
	// Restore/GitLog/GitDiff methods report "git versioning disabled".
	vcs *vcs.Git
}

// New builds a Service over the given bundle, with a Writer bound to the
// bundle root. No FTS index is attached; SearchConcepts uses the in-memory
// scan. Use NewWithIndex or OpenIndexForBundle to opt into persistent FTS.
func New(b *bundle.Bundle) *Service {
	return NewWithWriter(b, bundle.NewWriter(b.RootPath))
}

// NewWithWriter builds a Service with an explicit Writer (for tests or when
// the bundle root differs from the writer target). No FTS index is attached.
func NewWithWriter(b *bundle.Bundle, w *bundle.Writer) *Service {
	return &Service{b: b, g: graph.NewGraph(b), w: w}
}

// NewWithIndex builds a Service with an explicit Writer and a persistent FTS5
// index. When idx is non-nil, SearchConcepts queries FTS first and the
// mutation methods keep the index in sync under the write lock.
func NewWithIndex(b *bundle.Bundle, w *bundle.Writer, idx *search.Index) *Service {
	return &Service{b: b, g: graph.NewGraph(b), w: w, idx: idx}
}

// NewWithVCS builds a Service with an explicit Writer, an optional FTS5 index,
// and an optional Git versioning handle. nil idx/vcs disable those features.
func NewWithVCS(b *bundle.Bundle, w *bundle.Writer, idx *search.Index, v *vcs.Git) *Service {
	return &Service{b: b, g: graph.NewGraph(b), w: w, idx: idx, vcs: v}
}

// OpenIndexForBundle opens (or creates) a persistent FTS5 index at
// <bundleRoot>/.okf/index.db and reconciles it against the bundle's current
// concepts. Returns the index, or an error. The caller is responsible for
// Close (typically via Service.Close once the index is attached).
func OpenIndexForBundle(b *bundle.Bundle) (*search.Index, error) {
	dbPath := filepath.Join(b.RootPath, ".okf", "index.db")
	return search.Open(dbPath, b.Concepts)
}

// Close releases the FTS index if one is attached. It is safe to call on a
// Service built with New/NewWithWriter (no-op).
func (s *Service) Close() error {
	if s.idx != nil {
		err := s.idx.Close()
		s.idx = nil
		return err
	}
	return nil
}

// Bundle exposes the underlying bundle (used by playground helpers).
func (s *Service) Bundle() *bundle.Bundle { return s.b }

// VCS exposes the optional Git versioning handle. Returns nil when versioning
// is disabled; the playground uses this to decide whether to register git tools.
func (s *Service) VCS() *vcs.Git { return s.vcs }

// Graph exposes the underlying graph (used by playground helpers). Takes the
// read lock so the returned pointer cannot be swapped mid-call by a mutation.
func (s *Service) Graph() *graph.Graph {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.g
}

// --- shaping helpers ---

// conceptSummary returns the listing projection of a concept.
func conceptSummary(c *bundle.Concept) map[string]any {
	status := "stable"
	typ := ""
	title := ""
	if c.Meta != nil {
		typ = c.Meta.Type
		title = c.Meta.Title
		if c.Meta.Status != "" {
			status = c.Meta.Status
		}
	}
	return map[string]any{
		"id":         c.ID,
		"type":       typ,
		"title":      title,
		"trust_tier": string(c.TrustTier()),
		"status":     status,
	}
}

// conceptDetail returns the full disclosure projection of a concept
// (frontmatter + body + links).
func conceptDetail(c *bundle.Concept) map[string]any {
	out := map[string]any{
		"id":         c.ID,
		"trust_tier": string(c.TrustTier()),
		"body":       c.Body,
		"links":      c.Links,
		"summary":    c.Summary(),
	}
	if c.Meta != nil {
		m := c.Meta
		out["type"] = m.Type
		out["title"] = m.Title
		out["description"] = m.Description
		out["resource"] = m.Resource
		out["tags"] = m.Tags
		out["status"] = m.Status
		if m.StaleAfter != nil {
			out["stale_after"] = *m.StaleAfter
		}
		if m.Generated != nil {
			out["generated"] = map[string]any{
				"by": m.Generated.By,
				"at": m.Generated.At,
			}
		}
		if len(m.Verified) > 0 {
			verified := make([]map[string]any, 0, len(m.Verified))
			for _, v := range m.Verified {
				verified = append(verified, map[string]any{"by": v.By, "at": v.At})
			}
			out["verified"] = verified
		}
		if len(m.Sources) > 0 {
			out["sources"] = m.Sources
		}
		if m.OKFVersion != "" {
			out["okf_version"] = m.OKFVersion
		}
	}
	return out
}

// metaTypeMatches reports whether a concept's type matches the requested filter
// in a SPEC-tolerant way: it accepts both "Attested Computation" and
// "attested_computation" spellings (and any case variant) for the attested type.
func metaTypeMatches(actual, requested string) bool {
	if frontmatter.IsAttestedComputation(actual) && frontmatter.IsAttestedComputation(requested) {
		return true
	}
	return strings.EqualFold(actual, requested)
}

// --- consumer operations (the 8 MCP tools) ---

// ListConcepts lists concepts, optionally filtered by type and/or tag.
func (s *Service) ListConcepts(typeFilter, tag string) []map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []map[string]any
	for _, c := range s.b.Concepts {
		if typeFilter != "" {
			if c.Meta == nil || !metaTypeMatches(c.Meta.Type, typeFilter) {
				continue
			}
		}
		if tag != "" {
			hasTag := false
			if c.Meta != nil {
				for _, t := range c.Meta.Tags {
					if t == tag {
						hasTag = true
						break
					}
				}
			}
			if !hasTag {
				continue
			}
		}
		out = append(out, conceptSummary(c))
	}
	return out
}

// SearchConcepts returns concepts matching the query, ranked by relevance.
//
// When a persistent FTS5 index is attached, the query runs against it and
// results are ranked by bm25 (title > type > description > tags > body).
// The trigram tokenizer cannot match tokens shorter than 3 Unicode codepoints;
// in that case (or on any FTS error) it falls back to the in-memory
// Bundle.Search substring scan, preserving the prior behavior for short
// queries. With no index attached it always uses the in-memory scan.
func (s *Service) SearchConcepts(query string) []map[string]any {
	if s.idx != nil {
		hits, err := s.idx.Search(query, 0)
		if err == nil {
			s.mu.RLock()
			defer s.mu.RUnlock()
			out := make([]map[string]any, 0, len(hits))
			for _, h := range hits {
				if c := s.b.Get(h.ID); c != nil {
					out = append(out, conceptSummary(c))
				}
			}
			return out
		}
		if !errors.Is(err, search.ErrQueryTooShort) {
			slog.Warn("FTS search failed, falling back to in-memory scan", "error", err)
		}
		// fall through to in-memory scan
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	results := s.b.Search(query)
	out := make([]map[string]any, 0, len(results))
	for _, c := range results {
		out = append(out, conceptSummary(c))
	}
	return out
}

// GetConcept returns the full detail of a single concept by ID.
func (s *Service) GetConcept(id string) (map[string]any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := s.b.Get(id)
	if c == nil {
		return nil, fmt.Errorf("concept not found: %s", id)
	}
	return conceptDetail(c), nil
}

// GetIndex returns the navigation entries of an index.md. With an empty path it
// parses the root index; otherwise the index of the given directory.
func (s *Service) GetIndex(path string) ([]map[string]any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var entries []bundle.IndexEntry
	if path == "" || path == "." {
		entries = s.b.ParseIndex()
	} else {
		entries = s.b.ParseIndexAt(path)
	}
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]any{
			"id":          e.ID,
			"title":       e.Title,
			"link":        e.Link,
			"level":       e.Level,
			"description": e.Description,
		})
	}
	return out, nil
}

// GetNeighbors returns the concepts directly referenced by the given concept
// (outgoing edges / forward traversal).
func (s *Service) GetNeighbors(id string) ([]map[string]any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.b.Get(id) == nil {
		return nil, fmt.Errorf("concept not found: %s", id)
	}
	neighbors := s.g.Neighbors(id)
	out := make([]map[string]any, 0, len(neighbors))
	for _, nid := range neighbors {
		if c := s.b.Get(nid); c != nil {
			out = append(out, conceptSummary(c))
		}
	}
	return out, nil
}

// GetBacklinks returns the concepts that reference the given concept
// (incoming edges / reverse traversal, a.k.a. impact analysis).
func (s *Service) GetBacklinks(id string) ([]map[string]any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.b.Get(id) == nil {
		return nil, fmt.Errorf("concept not found: %s", id)
	}
	backlinks := s.g.Backlinks(id)
	out := make([]map[string]any, 0, len(backlinks))
	for _, sid := range backlinks {
		if c := s.b.Get(sid); c != nil {
			out = append(out, conceptSummary(c))
		}
	}
	return out, nil
}

// CheckStale returns concepts that are stale (past stale_after) or deprecated.
func (s *Service) CheckStale() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stale, deprecated := s.b.StaleAndDeprecated()
	staleOut := make([]map[string]any, 0, len(stale))
	for _, c := range stale {
		staleOut = append(staleOut, conceptSummary(c))
	}
	depOut := make([]map[string]any, 0, len(deprecated))
	for _, c := range deprecated {
		depOut = append(depOut, conceptSummary(c))
	}
	return map[string]any{
		"stale":      staleOut,
		"deprecated": depOut,
	}
}

// GetTrusted returns concepts filtered by trust tier. An empty tier defaults to
// human-reviewed (the OKF default consumer trust level).
func (s *Service) GetTrusted(tier string) ([]map[string]any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := frontmatter.ParseTrustTier(tier)
	if !ok {
		return nil, fmt.Errorf("unknown trust tier %q (want human-reviewed|machine-confirmed|unverified)", tier)
	}
	results := s.b.FilterByTrustTier(t)
	out := make([]map[string]any, 0, len(results))
	for _, c := range results {
		out = append(out, conceptSummary(c))
	}
	return out, nil
}

// --- development / validation operations (playground-only, shared here) ---

// ListTypes returns counts of concepts grouped by type.
func (s *Service) ListTypes() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	counts := make(map[string]int)
	for _, c := range s.b.Concepts {
		if c.Meta != nil {
			counts[c.Meta.Type]++
		} else {
			counts["(missing)"]++
		}
	}
	return counts
}

// GetGraph returns the full knowledge graph (nodes + edges).
func (s *Service) GetGraph() *graph.Graph {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.g
}

// ValidateBundle runs OKF compliance validation and returns the issues.
func (s *Service) ValidateBundle() []bundle.Issue {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return bundle.NewValidator().Validate(s.b)
}

// BundleStats returns aggregate bundle statistics.
func (s *Service) BundleStats() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, edgeCount := s.g.Stats()

	trustCounts := make(map[string]int)
	typeCounts := make(map[string]int)
	linkCount := 0
	for _, c := range s.b.Concepts {
		trustCounts[string(c.TrustTier())]++
		if c.Meta != nil {
			typeCounts[c.Meta.Type]++
		}
		linkCount += len(c.Links)
	}
	return map[string]any{
		"concepts":    len(s.b.Concepts),
		"types":       typeCounts,
		"trust_tiers": trustCounts,
		"links":       linkCount,
		"graph_edges": edgeCount,
		"has_index":   s.b.HasIndex(),
	}
}

// GetGraphSummary returns a compact graph overview.
func (s *Service) GetGraphSummary() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	nodes, edges := s.g.Stats()
	typeCount := make(map[string]int)
	for _, n := range s.g.Nodes {
		typeCount[string(n.Type)]++
	}
	return map[string]any{
		"total_nodes":    nodes,
		"total_edges":    edges,
		"type_breakdown": typeCount,
	}
}

// --- mutation operations (producer-side, shared by playground) ---
//
// These methods write a concept .md to disk (via bundle.Writer) and update the
// in-memory Bundle + rebuild the cached Graph under the write lock. They
// return the concept's detail projection (same shape as GetConcept) so tool
// callers see the result of their write.

// CreateConcept writes a new concept (or overwrites an existing one). meta
// must carry a non-empty type (OKF §4.1).
func (s *Service) CreateConcept(id string, meta *frontmatter.Meta, body string) (map[string]any, error) {
	if err := meta.ValidateRequired(); err != nil {
		return nil, fmt.Errorf("create %s: %w", id, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.w.WriteConcept(id, meta, body); err != nil {
		return nil, err
	}
	s.setConceptInMemory(id, meta, body)
	s.syncUpsert(s.b.Get(id))
	s.g = graph.NewGraph(s.b)
	return conceptDetail(s.b.Get(id)), nil
}

// UpdateConcept replaces a concept's body, preserving its frontmatter.
func (s *Service) UpdateConcept(id, body string) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.b.Get(id)
	if c == nil {
		return nil, fmt.Errorf("update %s: concept not found", id)
	}
	if err := s.w.WriteConcept(id, c.Meta, body); err != nil {
		return nil, err
	}
	s.setConceptInMemory(id, c.Meta, body)
	s.syncUpsert(s.b.Get(id))
	s.g = graph.NewGraph(s.b)
	return conceptDetail(s.b.Get(id)), nil
}

// SetFrontmatter sets a single frontmatter field on an existing concept. The
// supported field names are the OKF core frontmatter keys; value must be a
// string except for tags ([]string) and stale_after (string date).
func (s *Service) SetFrontmatter(id, field string, value any) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.b.Get(id)
	if c == nil {
		return nil, fmt.Errorf("set_frontmatter %s: concept not found", id)
	}
	meta := *c.Meta // shallow copy; slices are reassigned below, not mutated
	if err := applyMetaField(&meta, field, value); err != nil {
		return nil, fmt.Errorf("set_frontmatter %s: %w", id, err)
	}
	if err := s.w.WriteConcept(id, &meta, c.Body); err != nil {
		return nil, err
	}
	s.setConceptInMemory(id, &meta, c.Body)
	s.syncUpsert(s.b.Get(id))
	s.g = graph.NewGraph(s.b)
	return conceptDetail(s.b.Get(id)), nil
}

// AppendToConcept appends a section to the concept's body and rewrites the
// document, preserving frontmatter.
func (s *Service) AppendToConcept(id, heading, content string) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.b.Get(id)
	if c == nil {
		return nil, fmt.Errorf("append %s: concept not found", id)
	}
	var body string
	if heading != "" {
		body = c.Body + "\n\n## " + heading + "\n\n" + content
	} else {
		body = c.Body + "\n\n" + content
	}
	body = strings.TrimSpace(body)
	if err := s.w.WriteConcept(id, c.Meta, body); err != nil {
		return nil, err
	}
	s.setConceptInMemory(id, c.Meta, body)
	s.syncUpsert(s.b.Get(id))
	s.g = graph.NewGraph(s.b)
	return conceptDetail(s.b.Get(id)), nil
}

// DeleteConcept removes a concept file and drops it from the in-memory Bundle.
func (s *Service) DeleteConcept(id string) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.b.Get(id) == nil {
		return nil, fmt.Errorf("delete %s: concept not found", id)
	}
	if err := s.w.Delete(id); err != nil {
		return nil, err
	}
	delete(s.b.Concepts, id)
	s.syncRemove(id)
	s.g = graph.NewGraph(s.b)
	return map[string]any{"id": id, "deleted": true}, nil
}

// --- git versioning operations (optional; gated by vcs != nil) ---
//
// These expose the bundle's Git history to the playground agent. Commit/Revert/
// Restore take the write lock (they mutate disk and must serialize with other
// writes); GitLog/GitDiff are read-only git operations and need no lock.
// Revert/Restore resync the in-memory Bundle + Graph + FTS index after git has
// rewritten files, so reads stay consistent with disk.

var errVCSDisabled = errors.New("git versioning disabled (enable with --git or OKF_GIT=1)")

// Commit stages all pending changes and creates a commit if the tree is dirty.
// Returns committed=false when there was nothing to commit.
func (s *Service) Commit(msg string) (map[string]any, error) {
	if s.vcs == nil {
		return nil, errVCSDisabled
	}
	if msg == "" {
		msg = "manual commit"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	committed, err := s.vcs.CommitIfDirty(msg)
	if err != nil {
		return nil, err
	}
	return map[string]any{"committed": committed, "message": msg}, nil
}

// GitLog returns commit history, optionally filtered to a concept id.
func (s *Service) GitLog(conceptID string, limit int) ([]vcs.Commit, error) {
	if s.vcs == nil {
		return nil, errVCSDisabled
	}
	path := ""
	if conceptID != "" {
		path = conceptID + ".md"
	}
	if limit <= 0 {
		limit = 50
	}
	return s.vcs.Log(path, limit)
}

// GitDiff returns a textual diff. conceptID restricts to one concept; ref empty
// means uncommitted working-tree changes, otherwise the diff introduced by ref.
func (s *Service) GitDiff(conceptID, ref string) (string, error) {
	if s.vcs == nil {
		return "", errVCSDisabled
	}
	path := ""
	if conceptID != "" {
		path = conceptID + ".md"
	}
	return s.vcs.Diff(path, ref)
}

// Revert creates a new commit undoing the given commit, then resyncs memory.
func (s *Service) Revert(ref string) (map[string]any, error) {
	if s.vcs == nil {
		return nil, errVCSDisabled
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.vcs.Revert(ref); err != nil {
		return nil, err
	}
	files, _ := s.vcs.ChangedFiles(ref)
	ids := conceptIDsFromPaths(files)
	s.reloadFromDisk(ids)
	s.g = graph.NewGraph(s.b)
	return map[string]any{"reverted": ref, "affected": ids}, nil
}

// Restore restores a single concept to its state at ref, then resyncs memory.
func (s *Service) Restore(conceptID, ref string) (map[string]any, error) {
	if s.vcs == nil {
		return nil, errVCSDisabled
	}
	if conceptID == "" {
		return nil, fmt.Errorf("concept id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.vcs.Restore(conceptID+".md", ref); err != nil {
		return nil, err
	}
	s.reloadFromDisk([]string{conceptID})
	s.g = graph.NewGraph(s.b)
	return conceptDetail(s.b.Get(conceptID)), nil
}

// conceptIDsFromPaths converts bundle-relative file paths (e.g.
// "tables/orders.md") to concept ids, skipping reserved files (index.md,
// log.md) and non-markdown files.
func conceptIDsFromPaths(paths []string) []string {
	var ids []string
	for _, p := range paths {
		if !strings.HasSuffix(p, ".md") {
			continue
		}
		base := p
		if i := strings.LastIndex(p, "/"); i >= 0 {
			base = p[i+1:]
		}
		if base == "index.md" || base == "log.md" {
			continue
		}
		ids = append(ids, strings.TrimSuffix(p, ".md"))
	}
	return ids
}

// reloadFromDisk re-reads the given concept ids from disk and updates the
// in-memory Bundle + FTS index. If a file no longer exists (git reverted a
// creation), the concept is dropped from memory and the index. Caller must hold
// the write lock.
func (s *Service) reloadFromDisk(ids []string) {
	for _, id := range ids {
		abs := filepath.Join(s.b.RootPath, filepath.FromSlash(id+".md"))
		data, err := os.ReadFile(abs)
		if err != nil {
			// File gone: drop from memory + index.
			if c := s.b.Get(id); c != nil {
				s.syncRemove(id)
			}
			delete(s.b.Concepts, id)
			continue
		}
		res, err := frontmatter.Extract(string(data))
		if err != nil {
			slog.Warn("vcs reload: unparseable frontmatter, skipping",
				"id", id, "error", err)
			continue
		}
		s.setConceptInMemory(id, res.Meta, res.Body)
		s.syncUpsert(s.b.Get(id))
	}
}

// setConceptInMemory inserts or replaces the in-memory Concept for id,
// recomputing its link targets from the body. Caller must hold the write lock.
func (s *Service) setConceptInMemory(id string, meta *frontmatter.Meta, body string) {
	rel := id + ".md"
	s.b.Concepts[id] = &bundle.Concept{
		ID:       id,
		Meta:     meta,
		Body:     body,
		Links:    bundle.ResolveLinks(rel, body),
		FilePath: filepath.Join(s.b.RootPath, filepath.FromSlash(rel)),
	}
}

// syncUpsert updates the FTS index for a just-written concept. The caller must
// hold the write lock (the mutation has already updated disk and memory). A
// failure is logged but not returned: the markdown file is the source of truth
// and the next Reconcile on startup will self-heal the index.
func (s *Service) syncUpsert(c *bundle.Concept) {
	if s.idx == nil || c == nil {
		return
	}
	if err := s.idx.Upsert(c); err != nil {
		slog.Warn("FTS upsert failed; index will self-heal on next reconcile",
			"id", c.ID, "error", err)
	}
}

// syncRemove drops a concept from the FTS index after deletion. The caller must
// hold the write lock. Failures are logged, not returned (same self-heal rule).
func (s *Service) syncRemove(id string) {
	if s.idx == nil {
		return
	}
	if err := s.idx.Remove(id); err != nil {
		slog.Warn("FTS remove failed; index will self-heal on next reconcile",
			"id", id, "error", err)
	}
}

// applyMetaField sets a single field on meta by name. Supported: type, title,
// description, resource, status, stale_after (YYYY-MM-DD string), tags
// ([]string). Unknown fields error rather than silently dropping.
func applyMetaField(meta *frontmatter.Meta, field string, value any) error {
	switch field {
	case "type", "title", "description", "resource", "status":
		s, _ := value.(string)
		switch field {
		case "type":
			meta.Type = s
		case "title":
			meta.Title = s
		case "description":
			meta.Description = s
		case "resource":
			meta.Resource = s
		case "status":
			meta.Status = s
		}
	case "stale_after":
		s, _ := value.(string)
		if s == "" {
			meta.StaleAfter = nil
		} else {
			v := s
			meta.StaleAfter = &v
		}
	case "tags":
		tags, err := toStringSlice(value)
		if err != nil {
			return err
		}
		meta.Tags = tags
	default:
		return fmt.Errorf("unsupported field %q (allowed: type, title, description, resource, status, stale_after, tags)", field)
	}
	return nil
}

func toStringSlice(value any) ([]string, error) {
	switch v := value.(type) {
	case []string:
		return v, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			s, _ := x.(string)
			out = append(out, s)
		}
		return out, nil
	case nil:
		return nil, nil
	default:
		return nil, fmt.Errorf("expected []string or []any, got %T", value)
	}
}
