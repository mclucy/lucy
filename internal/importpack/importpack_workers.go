package importpack

import (
	"context"
	"sync"
)

// materializeWorkers bounds work on independent source files. Entries of a
// streamed container, such as a tar body, stay serial: the next entry cannot
// be read before the current body ends.
const materializeWorkers = 4

// runFileJobs runs count jobs in waves of at most materializeWorkers.
//
// A wave waits for all of its jobs, so every file before the first failing file
// has completed and the returned error is the one a serial run would report.
// Context cancellation stops jobs that have not completed, and the context
// error wins over the failures it caused.
func runFileJobs(
	ctx context.Context,
	count int,
	job func(context.Context, int) error,
) error {
	for start := 0; start < count; start += materializeWorkers {
		end := min(start+materializeWorkers, count)
		errs := make([]error, end-start)
		var workers sync.WaitGroup
		for index := start; index < end; index++ {
			workers.Go(func() {
				errs[index-start] = job(ctx, index)
			})
		}
		workers.Wait()
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, err := range errs {
			if err != nil {
				return err
			}
		}
	}
	return nil
}
