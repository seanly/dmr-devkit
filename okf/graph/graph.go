package graph

import (
	"fmt"
	"strings"

	"github.com/seanly/dmr-devkit/okf/frontmatter"
	"github.com/seanly/dmr-devkit/okf/bundle"
)

// NodeType categorizes graph nodes.
type NodeType string

const (
	NodeTable     NodeType = "table"
	NodeColumn    NodeType = "column"
	NodeGuideline NodeType = "guideline"
	NodeAttested  NodeType = "attested_computation"
	NodeConcept   NodeType = "concept"
)

// Node is a vertex in the knowledge graph.
type Node struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Type    NodeType `json:"type"`
	Trust   string   `json:"trust"`
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
}

// Edge is a directed relationship between two nodes.
type Edge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Label  string `json:"label"`
}

// Graph is the in-memory knowledge graph derived from a Bundle.
type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// NewGraph builds a knowledge graph from a Bundle.
func NewGraph(b *bundle.Bundle) *Graph {
	g := &Graph{}

	// Add concept nodes
	for id, c := range b.Concepts {
		label := id
		if c.Meta != nil && c.Meta.Title != "" {
			label = c.Meta.Title
		}
		nt := NodeConcept
		if c.Meta != nil {
			switch {
			case strings.EqualFold(c.Meta.Type, "table"):
				nt = NodeTable
			case strings.EqualFold(c.Meta.Type, "guideline"):
				nt = NodeGuideline
			case frontmatter.IsAttestedComputation(c.Meta.Type):
				nt = NodeAttested
			}
		}
		trust := "unverified"
		if c.Meta != nil {
			trust = string(c.Meta.DeriveTrustTier())
		}
		g.Nodes = append(g.Nodes, Node{
			ID:      id,
			Label:   label,
			Type:    nt,
			Trust:   trust,
			Title:   c.Meta.Title,
			Summary: c.Summary(),
		})

		// Edges from markdown links
		for _, target := range c.Links {
			if b.Get(target) != nil {
				g.Edges = append(g.Edges, Edge{
					Source: id,
					Target: target,
					Label:  "refers_to",
				})
			}
		}
	}

	return g
}

// Stats returns basic graph statistics.
func (g *Graph) Stats() (nodes, edges int) {
	return len(g.Nodes), len(g.Edges)
}

// Neighbors returns outgoing neighbor IDs for a given node.
func (g *Graph) Neighbors(id string) []string {
	var out []string
	for _, e := range g.Edges {
		if e.Source == id {
			out = append(out, e.Target)
		}
	}
	return out
}

// Backlinks returns incoming neighbor IDs for a given node — i.e. the concepts
// that reference (link to) the given id. This is the inverse of Neighbors and
// supports OKF's reverse-traversal / impact-analysis capability.
func (g *Graph) Backlinks(id string) []string {
	var out []string
	for _, e := range g.Edges {
		if e.Target == id {
			out = append(out, e.Source)
		}
	}
	return out
}

// DOT generates a Graphviz DOT representation (optional debug output).
func (g *Graph) DOT() string {
	var sb strings.Builder
	sb.WriteString("digraph okf {\n")
	for _, n := range g.Nodes {
		shape := "ellipse"
		switch n.Type {
		case NodeTable:
			shape = "box"
		case NodeGuideline:
			shape = "diamond"
		case NodeAttested:
			shape = "hexagon"
		}
		sb.WriteString(fmt.Sprintf("  \"%s\" [label=\"%s\" shape=%s];\n", n.ID, n.Label, shape))
	}
	for _, e := range g.Edges {
		sb.WriteString(fmt.Sprintf("  \"%s\" -> \"%s\" [label=\"%s\"];\n", e.Source, e.Target, e.Label))
	}
	sb.WriteString("}\n")
	return sb.String()
}
