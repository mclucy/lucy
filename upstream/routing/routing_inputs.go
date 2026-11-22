package routing

import (
	"context"
	"net/url"
	"sync"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
)

const (
	// inputWorkers bounds acquisitions in flight for one server product. The
	// set is a few dozen small files, so a wider pool adds contention without
	// shortening the wait.
	inputWorkers = 8
	// hostWorkers bounds requests in flight to one upstream host, so a wide
	// input set does not open one connection per file to the same server.
	hostWorkers = 4
)

// acquireInputs acquires every input that is not embedded and records the
// digests each acquisition proved. Results keep input order. The first failure
// in input order is returned after every started acquisition ends.
func acquireInputs(
	ctx context.Context,
	inputs []catalog.Artifact,
	acquire func(*catalog.Artifact) (artifact.MaterializedFile, error),
) error {
	downloadable := make([]int, 0, len(inputs))
	for i := range inputs {
		if inputs[i].Embedded == "" {
			downloadable = append(downloadable, i)
		}
	}
	errs := runBounded(
		ctx,
		len(downloadable),
		func(position int) string {
			return hostFromURL(inputs[downloadable[position]].URL)
		},
		func(position int) error {
			index := downloadable[position]
			materialized, err := acquire(&inputs[index])
			if err != nil {
				return err
			}
			inputs[index].Hashes = materialized.Hashes
			return nil
		},
	)
	if err := firstError(errs); err != nil {
		return err
	}
	return ctx.Err()
}

// firstError reports the failure with the lowest input index.
func firstError(errs []error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// runBounded calls visit for every index below count with at most inputWorkers
// visits in flight, each waiting for a slot on the host its index names. A
// failure stops further visits without interrupting those in flight. Every
// visit keeps its result at its index, so a caller reports the first failure in
// input order.
func runBounded(
	ctx context.Context,
	count int,
	host func(int) string,
	visit func(int) error,
) []error {
	errs := make([]error, count)
	workers := min(inputWorkers, count)
	if workers == 0 {
		return errs
	}
	gates := newHostGates(hostWorkers)
	var (
		mu     sync.Mutex
		next   int
		failed bool
		group  sync.WaitGroup
	)
	worker := func() {
		for {
			mu.Lock()
			if failed || next >= count {
				mu.Unlock()
				return
			}
			index := next
			next++
			mu.Unlock()
			gate := host(index)
			if err := gates.acquire(ctx, gate); err != nil {
				mu.Lock()
				errs[index], failed = err, true
				mu.Unlock()
				return
			}
			err := visit(index)
			gates.release(gate)
			if err == nil {
				continue
			}
			mu.Lock()
			errs[index], failed = err, true
			mu.Unlock()
			return
		}
	}
	for range workers {
		group.Go(worker)
	}
	group.Wait()
	return errs
}

// hostGates bounds visits in flight per upstream host.
type hostGates struct {
	limit int
	mu    sync.Mutex
	slots map[string]chan struct{}
}

func newHostGates(limit int) *hostGates {
	return &hostGates{limit: limit, slots: make(map[string]chan struct{})}
}

func (g *hostGates) acquire(ctx context.Context, host string) error {
	g.mu.Lock()
	slots, ok := g.slots[host]
	if !ok {
		slots = make(chan struct{}, g.limit)
		g.slots[host] = slots
	}
	g.mu.Unlock()
	select {
	case slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *hostGates) release(host string) {
	g.mu.Lock()
	slots := g.slots[host]
	g.mu.Unlock()
	<-slots
}

func hostFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Host
}
