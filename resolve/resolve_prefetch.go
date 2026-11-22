package resolve

import (
	"context"
	"slices"
	"sync"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/upstream"
)

// prefetchLimit bounds the candidate listings and artifact analyses that run
// beside the search. The search itself stays sequential, so this only decides
// how much upstream work may overlap.
const prefetchLimit = 8

// listingKey identifies one candidate listing. A listing answers the same
// builds whatever selectors a search applies, so it does not name them.
type listingKey struct {
	registry  catalog.Registry
	project   string
	minecraft string
	ecosystem catalog.Ecosystem
}

func listingKeyFor(key projectKey, minecraft string) listingKey {
	return listingKey{
		catalogueRegistry(key.provider),
		key.project,
		minecraft,
		key.ecosystem,
	}
}

// CandidateCache shares candidate listings across related searches. It only
// keeps listings that succeeded, because a failed listing may answer
// differently after a transient upstream error.
type CandidateCache struct {
	mu      sync.Mutex
	entries map[listingKey][]upstream.Candidate
}

// NewCandidateCache constructs an empty listing cache.
func NewCandidateCache() *CandidateCache {
	return &CandidateCache{
		entries: make(map[listingKey][]upstream.Candidate),
	}
}

func (c *CandidateCache) contains(key listingKey) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.entries[key]
	return ok
}

func (c *CandidateCache) lookup(key listingKey) ([]upstream.Candidate, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	list, ok := c.entries[key]
	return slices.Clone(list), ok
}

func (c *CandidateCache) store(key listingKey, list []upstream.Candidate) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = slices.Clone(list)
}

// analysisResult is the outcome of one candidate analysis. A fatal error ends
// the enclosing branch; a descriptor error rejects only the candidate.
type analysisResult struct {
	modules    []catalog.DescriptorModule
	hashes     map[string]string
	fatal      error
	descriptor error
}

type analysisFuture struct {
	done chan struct{}
	// started marks the owner that actually performs the read, so the search
	// can take over a queued speculative read instead of waiting behind it.
	started bool
	result  analysisResult
}

type listingFuture struct {
	done chan struct{}
	list []upstream.Candidate
	err  error
}

// prefetcher overlaps upstream work with the sequential search. It starts
// candidate listings and analyses in the background, and the search consumes
// them in its own deterministic order. Unused work is cancelled; an error of
// unused work is dropped, so attribution stays identical to a serial search.
type prefetcher struct {
	ctx         context.Context
	cancel      context.CancelFunc
	source      CandidateSource
	materialize func(*catalog.Artifact) (artifact.MaterializedFile, error)
	cache       *AnalysisCache
	shared      *CandidateCache
	target      Target
	slots       chan struct{}

	mu       sync.Mutex
	listings map[listingKey]*listingFuture
	analyses map[analysisKey]*analysisFuture

	wg sync.WaitGroup
}

func newPrefetcher(
	ctx context.Context,
	request PackagesRequest,
	cache *AnalysisCache,
	shared *CandidateCache,
) *prefetcher {
	ctx, cancel := context.WithCancel(ctx)
	return &prefetcher{
		ctx:         ctx,
		cancel:      cancel,
		source:      request.Candidates,
		materialize: request.Materialize,
		cache:       cache,
		shared:      shared,
		target:      request.Target,
		slots:       make(chan struct{}, prefetchLimit),
		listings:    make(map[listingKey]*listingFuture),
		analyses:    make(map[analysisKey]*analysisFuture),
	}
}

func (p *prefetcher) close() {
	p.cancel()
	p.wg.Wait()
}

func (p *prefetcher) acquire() bool {
	select {
	case p.slots <- struct{}{}:
		return true
	case <-p.ctx.Done():
		return false
	}
}

func (p *prefetcher) release() {
	<-p.slots
}

// prefetchRequests starts the listings and pinned analyses the search will
// need for requests it has just learned about. skipLocked reports requests a
// lock pin settles without listing alternatives.
func (p *prefetcher) prefetchRequests(
	requests []projectRequest,
	previous lockIndex,
	pinsPreferred bool,
) {
	for _, r := range requests {
		if pinsPreferred {
			prior := previous.byProject[r.key]
			if pin, found, err := pinAccepts(prior, r); err == nil && found {
				p.startAnalysis(pin, r.key.ecosystem)
				continue
			}
		}
		p.startListing(listingKeyFor(r.key, string(p.target.Minecraft)))
	}
}

// startListing registers one listing before it starts, so the search can
// consume it whether it asks first or last.
func (p *prefetcher) startListing(key listingKey) {
	if p.source == nil || p.shared.contains(key) {
		return
	}
	p.mu.Lock()
	if _, ok := p.listings[key]; ok {
		p.mu.Unlock()
		return
	}
	future := &listingFuture{done: make(chan struct{})}
	p.listings[key] = future
	p.mu.Unlock()
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		if !p.acquire() {
			future.err = p.ctx.Err()
			close(future.done)
			return
		}
		defer p.release()
		list, err := p.source(
			p.ctx,
			key.registry,
			key.project,
			key.minecraft,
			key.ecosystem,
		)
		future.list, future.err = list, err
		if err == nil && len(list) > 0 {
			p.shared.store(key, list)
			// Register the analysis before the listing completes, so the
			// search joins it instead of reading the artifact again.
			p.startAnalysis(list[0], key.ecosystem)
		}
		close(future.done)
	}()
}

// list returns the candidate listing for key, waiting for a speculative one
// when it is already running.
func (p *prefetcher) list(
	ctx context.Context,
	key projectKey,
) ([]upstream.Candidate, error) {
	listing := listingKeyFor(key, string(p.target.Minecraft))
	if list, ok := p.shared.lookup(listing); ok {
		return list, nil
	}
	p.startListing(listing)
	p.mu.Lock()
	future := p.listings[listing]
	p.mu.Unlock()
	if future == nil {
		// Another search published the listing between the two lookups.
		if list, ok := p.shared.lookup(listing); ok {
			return list, nil
		}
		return p.source(
			ctx,
			listing.registry,
			listing.project,
			listing.minecraft,
			listing.ecosystem,
		)
	}
	select {
	case <-future.done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if future.err != nil {
		return nil, future.err
	}
	return slices.Clone(future.list), nil
}

// startAnalysis registers one artifact analysis and runs it in the background.
// A cached entry needs no work, so a pinned candidate whose lock records its
// modules stays untouched.
func (p *prefetcher) startAnalysis(
	candidate upstream.Candidate,
	ecosystem catalog.Ecosystem,
) {
	if p.materialize == nil || p.cache == nil {
		return
	}
	loader := p.target.DescriptorLoader(ecosystem)
	key := analysisKeyFor(candidate, loader, p.target.Minecraft)
	if p.cache.has(key, candidate.Artifact.Hashes) {
		return
	}
	p.mu.Lock()
	if _, ok := p.analyses[key]; ok {
		p.mu.Unlock()
		return
	}
	future := &analysisFuture{done: make(chan struct{})}
	p.analyses[key] = future
	p.mu.Unlock()
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		if !p.acquire() {
			p.mu.Lock()
			if !future.started {
				future.started = true
				future.result = analysisResult{fatal: p.ctx.Err()}
				close(future.done)
			}
			p.mu.Unlock()
			return
		}
		defer p.release()
		p.mu.Lock()
		if future.started {
			// The search took the read over; it publishes the result.
			p.mu.Unlock()
			return
		}
		future.started = true
		p.mu.Unlock()
		result := p.analyzeSerial(p.ctx, candidate, loader, key)
		p.mu.Lock()
		future.result = result
		close(future.done)
		p.mu.Unlock()
	}()
}

// analyze returns the analysis of one candidate. It joins a speculative read
// when one is running, and reads synchronously otherwise.
func (p *prefetcher) analyze(
	ctx context.Context,
	candidate upstream.Candidate,
	ecosystem catalog.Ecosystem,
) analysisResult {
	loader := p.target.DescriptorLoader(ecosystem)
	key := analysisKeyFor(candidate, loader, p.target.Minecraft)
	if entry, ok := p.cache.get(key, candidate.Artifact.Hashes); ok {
		return analysisResult{modules: entry.modules, hashes: entry.hashes}
	}
	p.mu.Lock()
	future := p.analyses[key]
	if future != nil && !future.started {
		// A queued speculative read has not started, so the search performs
		// it now instead of waiting behind unrelated artifacts.
		future.started = true
		p.mu.Unlock()
		result := p.analyzeSerial(ctx, candidate, loader, key)
		p.mu.Lock()
		future.result = result
		close(future.done)
		p.mu.Unlock()
		return result
	}
	p.mu.Unlock()
	if future == nil {
		return p.analyzeSerial(ctx, candidate, loader, key)
	}
	select {
	case <-future.done:
	case <-ctx.Done():
		return analysisResult{fatal: ctx.Err()}
	}
	result := future.result
	if result.fatal == nil && result.descriptor == nil &&
		!matchesAnalysisHashes(result.hashes, candidate.Artifact.Hashes) {
		// The speculative read addressed bytes the caller did not ask for.
		return p.analyzeSerial(ctx, candidate, loader, key)
	}
	return result
}

// analyzeSerial performs one candidate analysis: reuse verified bytes when the
// cache already holds them, otherwise materialize and read the descriptor.
func (p *prefetcher) analyzeSerial(
	ctx context.Context,
	candidate upstream.Candidate,
	loader catalog.Ecosystem,
	key analysisKey,
) analysisResult {
	a := candidate.Artifact
	if entry, ok := p.cache.get(key, a.Hashes); ok {
		return analysisResult{modules: entry.modules, hashes: entry.hashes}
	}
	if err := ctx.Err(); err != nil {
		return analysisResult{fatal: err}
	}
	materialized, err := p.materialize(&a)
	if err != nil {
		return analysisResult{fatal: err}
	}
	a.Hashes = materialized.Hashes
	if entry, ok := p.cache.get(key, a.Hashes); ok {
		return analysisResult{modules: entry.modules, hashes: entry.hashes}
	}
	modules, err := artifact.ReadModulesFor(
		ctx,
		materialized.Path,
		loader,
		string(p.target.Minecraft),
	)
	if err != nil {
		return analysisResult{descriptor: err, hashes: a.Hashes}
	}
	p.cache.put(key, analysisEntry{modules: modules, hashes: a.Hashes})
	return analysisResult{modules: modules, hashes: a.Hashes}
}

// pinAccepts reports whether the locked file answers the request without
// listing alternatives. The search and the prefetcher share this test, so the
// prefetcher never lists a project the search settles from the lock.
func pinAccepts(
	prior *document.Package,
	request projectRequest,
) (upstream.Candidate, bool, error) {
	pin, found := lockedCandidate(prior)
	if !found {
		return upstream.Candidate{}, false, nil
	}
	decision, err := decideCandidate(pin, request)
	if err != nil {
		return upstream.Candidate{}, false, err
	}
	if !decision.eligible {
		return upstream.Candidate{}, false, nil
	}
	if len(request.releaseHints) != 0 &&
		request.releaseHints[0].releaseID != pin.ReleaseID {
		return upstream.Candidate{}, false, nil
	}
	return pin, true, nil
}
