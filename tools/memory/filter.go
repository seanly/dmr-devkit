package memory

import (
	"encoding/json"
	"fmt"
)

// filterPagesByFrontmatter filters pages by exact key-value matches in parsed frontmatter JSON.
func filterPagesByFrontmatter(pages []Page, filter map[string]string) []Page {
	if len(filter) == 0 {
		return pages
	}
	var result []Page
	for _, p := range pages {
		var fm map[string]any
		if err := json.Unmarshal([]byte(p.Frontmatter), &fm); err != nil {
			continue
		}
		match := true
		for k, v := range filter {
			fv, ok := fm[k]
			if !ok {
				match = false
				break
			}
			var sv string
			switch tv := fv.(type) {
			case string:
				sv = tv
			case float64:
				sv = fmt.Sprintf("%g", tv)
			case bool:
				sv = fmt.Sprintf("%t", tv)
			default:
				sv = fmt.Sprintf("%v", tv)
			}
			if sv != v {
				match = false
				break
			}
		}
		if match {
			result = append(result, p)
		}
	}
	return result
}

// applyPagination manually applies limit/offset to a slice of pages.
func applyPagination(pages []Page, limit, offset int) []Page {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(pages) {
		return []Page{}
	}
	end := len(pages)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return pages[offset:end]
}
