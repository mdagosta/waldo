package corpus

import (
	"context"
	"fmt"
	"sync"

	"github.com/openwaldo/waldo/internal/lookaside"
)

type Availability struct {
	Objects int   `json:"objects"`
	Bytes   int64 `json:"bytes"`
}

type AvailabilityProgress struct {
	Current int
	Total   int
	Shard   ShardPin
	Probe   lookaside.ProbeResult
}

// CheckAvailability checks every canonical shard URL with bounded concurrency.
// It proves reachability and declared size, not content identity.
func CheckAvailability(ctx context.Context, bom BOM, cache *lookaside.Cache, workers int, progress func(AvailabilityProgress)) (Availability, error) {
	if cache == nil {
		return Availability{}, fmt.Errorf("lookaside client is required")
	}
	if err := bom.Validate(); err != nil {
		return Availability{}, err
	}
	if workers < 1 {
		workers = 8
	}
	if workers > len(bom.Shards) {
		workers = len(bom.Shards)
	}
	if workers == 0 {
		return Availability{}, nil
	}
	type job struct {
		position int
		shard    ShardPin
	}
	type result struct {
		job
		probe lookaside.ProbeResult
		err   error
	}
	execution, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan job)
	results := make(chan result)
	go func() {
		defer close(jobs)
		for position, shard := range bom.Shards {
			select {
			case jobs <- job{position: position, shard: shard}:
			case <-execution.Done():
				return
			}
		}
	}()
	var group sync.WaitGroup
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			for work := range jobs {
				probe, err := cache.Probe(execution, work.shard.URL, work.shard.Bytes)
				select {
				case results <- result{job: work, probe: probe, err: err}:
				case <-execution.Done():
					return
				}
			}
		}()
	}
	go func() {
		group.Wait()
		close(results)
	}()
	var availability Availability
	for checked := range results {
		if checked.err != nil {
			cancel()
			return availability, fmt.Errorf("%s shard %s: %w", checked.shard.Manifest, shortHash(checked.shard.SHA256), checked.err)
		}
		availability.Objects++
		availability.Bytes += checked.probe.Bytes
		if progress != nil {
			progress(AvailabilityProgress{Current: availability.Objects, Total: len(bom.Shards), Shard: checked.shard, Probe: checked.probe})
		}
	}
	if err := ctx.Err(); err != nil {
		return availability, err
	}
	return availability, nil
}
