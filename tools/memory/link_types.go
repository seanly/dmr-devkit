package memory

// Suggested link_type values for memoryLink / memoryUnlink. The backend stores any
// non-empty string; use these (and similar) so queries and the graph stay interpretable.
const (
	LinkMentions   = "mentions"    // A references B in prose (default if unsure)
	LinkWorksAt    = "works_at"    // person → company
	LinkInvestedIn = "invested_in" // investor → company
	LinkFounded    = "founded"     // person → company
	LinkAdvises    = "advises"     // person → person or company
	LinkAttended   = "attended"    // person → meeting
	LinkSource     = "source"      // page → external or cited page
)

// LinkTypesForTools is appended to tool descriptions (keyword-only; no auto inference).
const LinkTypesForTools = "Suggested link_type: mentions, works_at, invested_in, founded, advises, attended, source. " +
	"Outgoing: this page points at another; incoming: others point here (see memoryLinks direction). " +
	"Same (from, to, link_type) is unique: repeat put is ignored."
