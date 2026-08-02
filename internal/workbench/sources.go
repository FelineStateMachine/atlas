package workbench

// The source registry, as data.
//
// A source card says what a volume owes the people whose work it carries: the
// licence, the attribution, the id space its numbers live in, and -- where a
// crawler exists for it -- what a fetch may be pointed at. Those facts are the
// generate lane's own: they ride each source's registry entry
// (internal/generate/sources, doc.Provenance) and are copied verbatim into
// every document the source emits.
//
// The workbench may not import the generate lane (issue #5 §3.2), so the
// entries reach it as **data, handed over by whoever mounted the handler**.
// `atlas workbench` is that whoever, and cmd/ is the one place allowed to wire
// every lane: it reads the sources' own Describe() and the crawl registry's own
// Usage(), and hands the result to [New].
//
// The alternative shapes were a subcommand emitting the registry as JSON for
// the workbench to shell out for, and a curation file restating it. Both were
// refused for the same reason: they put a second copy of a licence somewhere,
// either on a wire or in a file, and a licence that exists twice is a licence
// that can be wrong in one of them. Wiring passes the registry entry itself,
// once, at construction -- the lane matrix stays intact, no subprocess stands
// between a page and a fact, and a source added to the lane appears on the card
// wall with no second edit anywhere.

// Source is one capture source as its registry entry describes it.
type Source struct {
	// Name is the registry name -- the slug a merge ledger line and this card
	// agree on.
	Name string
	// Label is the same source spelled for a person.
	Label string
	// License and Attribution are what a volume owes the people whose work it
	// carries. Either may be empty: a source that imposes no terms says so by
	// saying nothing, and the card says that plainly rather than inventing a
	// licence.
	License     string
	Attribution string
	// IDSpace is "native" or "derived": whether the source's captures carried
	// their own numbers or the translator minted them from stable names.
	IDSpace string
	// Crawlable reports that a crawler is registered under this source's name,
	// so the operations page may offer a fetch. The game sources' endpoints are
	// somebody else's editorial work and their crawlers are kept complete
	// rather than run from here, which is exactly what this being false means.
	Crawlable bool
	// TargetHint is the crawler's own line about what a target means, printed
	// on the form that asks for one.
	TargetHint string
	// Pair marks a source addressed as two slugs joined by a slash, which is
	// the one thing target validation has to be told.
	Pair bool
}

// sourceByName finds one registered source.
func sourceByName(sources []Source, name string) (Source, bool) {
	for _, source := range sources {
		if source.Name == name {
			return source, true
		}
	}
	return Source{}, false
}
