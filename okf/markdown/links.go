package markdown

import (
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// Link represents a single markdown link found in a document.
type Link struct {
	Text string
	URL  string
}

// ExtractLinks finds all markdown links in the given text using goldmark AST.
// It returns relative links (not absolute URLs like https://...).
func ExtractLinks(src string) []Link {
	source := text.NewReader([]byte(src))
	parser := goldmark.DefaultParser()
	root := parser.Parse(source)

	var links []Link
	ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		link, ok := n.(*ast.Link)
		if !ok {
			return ast.WalkContinue, nil
		}
		url := string(link.Destination)
		url = strings.TrimSpace(url)
		if strings.HasPrefix(url, "http://") ||
			strings.HasPrefix(url, "https://") ||
			strings.HasPrefix(url, "#") {
			return ast.WalkContinue, nil
		}
		// Extract link text
		var textParts []string
		for child := link.FirstChild(); child != nil; child = child.NextSibling() {
			if t, ok := child.(*ast.Text); ok {
				textParts = append(textParts, string(t.Segment.Value(source.Source())))
			}
		}
		links = append(links, Link{
			Text: strings.Join(textParts, ""),
			URL:  url,
		})
		return ast.WalkContinue, nil
	})
	return links
}
