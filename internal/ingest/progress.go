// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"context"
	"sync"
)

// ProgressEvent is a stable, structured account of ingestion work. Callers
// may render it for humans or serialize it without coupling the pipeline to a
// terminal.
type ProgressEvent struct {
	Phase          string `json:"phase"`
	Status         string `json:"status"`
	Input          string `json:"input,omitempty"`
	Adapter        string `json:"adapter,omitempty"`
	Shard          string `json:"shard,omitempty"`
	Remote         string `json:"remote,omitempty"`
	Sequence       int    `json:"sequence,omitempty"`
	Worker         int    `json:"worker,omitempty"`
	Bytes          int64  `json:"bytes,omitempty"`
	TotalBytes     int64  `json:"total_bytes,omitempty"`
	Files          int64  `json:"files,omitempty"`
	TotalFiles     int64  `json:"total_files,omitempty"`
	Docs           int64  `json:"docs,omitempty"`
	Tokens         int64  `json:"tokens,omitempty"`
	ReclaimedBytes int64  `json:"reclaimed_bytes,omitempty"`
	Message        string `json:"message,omitempty"`
}

type ProgressSink func(ProgressEvent)

type progressKey struct{}

type progressEmitter struct {
	mu   sync.Mutex
	sink ProgressSink
}

func WithProgress(ctx context.Context, sink ProgressSink) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, progressKey{}, &progressEmitter{sink: sink})
}

func emitProgress(ctx context.Context, event ProgressEvent) {
	if emitter, ok := ctx.Value(progressKey{}).(*progressEmitter); ok && emitter != nil {
		emitter.mu.Lock()
		defer emitter.mu.Unlock()
		emitter.sink(event)
	}
}

func proportionalProgress(total, current, count int64) int64 {
	if total <= 0 || current <= 0 || count <= 0 {
		return 0
	}
	if current >= count {
		return total
	}
	return int64(float64(total) * (float64(current) / float64(count)))
}
