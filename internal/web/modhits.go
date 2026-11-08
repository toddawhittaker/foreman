package web

import (
	"strconv"

	"github.com/toddawhittaker/foreman/internal/modportal"
)

// hitsPerPage is how many search results one page shows, so the plan page's
// Install button stays a short scroll above its "Add another mod" results
// (SPEC 9.1).
const hitsPerPage = 10

// ModHits is one page of a mod search's results (SPEC 9.1).
type ModHits struct {
	Rows []modportal.Hit
	// Total counts every match; Page is 1-based, of Pages.
	Total, Page, Pages int
}

// pageOfHits cuts page out of every match, moving a page past either end to
// the nearest one.
func pageOfHits(all []modportal.Hit, page int) ModHits {
	pages := max(1, (len(all)+hitsPerPage-1)/hitsPerPage)
	page = min(max(page, 1), pages)
	start := (page - 1) * hitsPerPage
	end := min(start+hitsPerPage, len(all))
	return ModHits{Rows: all[start:end], Total: len(all), Page: page, Pages: pages}
}

// First and Last number the page's first and last rows among all matches.
func (h ModHits) First() int { return (h.Page-1)*hitsPerPage + 1 }
func (h ModHits) Last() int  { return (h.Page-1)*hitsPerPage + len(h.Rows) }
func (h ModHits) Prev() int  { return h.Page - 1 }
func (h ModHits) Next() int  { return h.Page + 1 }

// readPage reads a page number; anything else is the first page.
func readPage(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 1
	}
	return n
}
