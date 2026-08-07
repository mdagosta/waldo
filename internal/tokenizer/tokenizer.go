// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

// Package tokenizer provides the reference token estimates recorded in index
// manifests. Counts are planning metadata: they never affect canonical row or
// object identity.
package tokenizer

import (
	"fmt"
	"sync"

	"github.com/pkoukk/tiktoken-go"
	tiktokenloader "github.com/pkoukk/tiktoken-go-loader"
)

// Default is the reference tokenizer used for manifest token estimates.
const Default = "tiktoken/cl100k_base"

type Counter interface {
	Name() string
	Count(string) int
}

var (
	initialize sync.Once
	mutex      sync.Mutex
	loaded     = map[string]Counter{}
)

func Get(name string) (Counter, error) {
	initializeLoader()
	mutex.Lock()
	defer mutex.Unlock()
	if counter, ok := loaded[name]; ok {
		return counter, nil
	}
	counter, err := New(name)
	if err != nil {
		return nil, err
	}
	loaded[name] = counter
	return counter, nil
}

// New returns an independent counter. Callers that count concurrently should
// use one counter per worker because the underlying encoder's regular
// expression state is not documented as safe for concurrent use.
func New(name string) (Counter, error) {
	initializeLoader()
	return newCounter(name)
}

func initializeLoader() {
	initialize.Do(func() {
		// Loading vocabulary data must never require network access during an
		// ingestion run.
		tiktoken.SetBpeLoader(tiktokenloader.NewOfflineLoader())
	})
}

func newCounter(name string) (Counter, error) {
	const prefix = "tiktoken/"
	if len(name) <= len(prefix) || name[:len(prefix)] != prefix {
		return nil, fmt.Errorf("unknown tokenizer %q (supported: tiktoken/<encoding>)", name)
	}
	encoding, err := tiktoken.GetEncoding(name[len(prefix):])
	if err != nil {
		return nil, fmt.Errorf("tokenizer %q: %w", name, err)
	}
	counter := &tiktokenCounter{name: name, encoding: encoding}
	return counter, nil
}

type tiktokenCounter struct {
	name     string
	encoding *tiktoken.Tiktoken
}

func (counter *tiktokenCounter) Name() string { return counter.name }

func (counter *tiktokenCounter) Count(text string) int {
	return len(counter.encoding.Encode(text, nil, nil))
}
