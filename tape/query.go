package tape

// TapeQuery is a chainable query builder for tape entries.
type TapeQuery struct {
	tape  string
	store TapeStore
	opts  FetchOpts
}

func NewQuery(tape string, store TapeStore) *TapeQuery {
	return &TapeQuery{tape: tape, store: store}
}

func (q *TapeQuery) AfterAnchor(name string) *TapeQuery {
	q.opts.AfterAnchor = name
	return q
}

func (q *TapeQuery) LastAnchor() *TapeQuery {
	q.opts.LastAnchor = true
	return q
}

func (q *TapeQuery) BetweenAnchors(start, end string) *TapeQuery {
	q.opts.BetweenAnchors = [2]string{start, end}
	return q
}

func (q *TapeQuery) BetweenDates(start, end string) *TapeQuery {
	q.opts.StartDate = start
	q.opts.EndDate = end
	return q
}

func (q *TapeQuery) Query(text string) *TapeQuery {
	q.opts.TextQuery = text
	return q
}

func (q *TapeQuery) Kinds(kinds ...string) *TapeQuery {
	q.opts.Kinds = kinds
	return q
}

func (q *TapeQuery) Limit(n int) *TapeQuery {
	q.opts.Limit = n
	return q
}

func (q *TapeQuery) All() ([]TapeEntry, error) {
	return q.store.FetchAll(q.tape, &q.opts)
}
