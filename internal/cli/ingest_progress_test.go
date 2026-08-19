// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/openwaldo/waldo/internal/ingest"
)

func TestIngestProgressReporterCombinesKnownAndDiscoveredTotals(t *testing.T) {
	var output bytes.Buffer
	report := ingestProgressReporter(&output, false)
	report(ingest.ProgressEvent{Phase: "ingest", Status: "started", TotalFiles: 2, TotalBytes: 1024})
	report(ingest.ProgressEvent{Phase: "ingest", Status: "records", Bytes: 256, TotalBytes: 1024, Docs: 3, Tokens: 17})
	report(ingest.ProgressEvent{Phase: "shard", Status: "creating", Sequence: 1})
	report(ingest.ProgressEvent{Phase: "shard", Status: "ready", Sequence: 1, Shard: "1234567890abcdef", Bytes: 384, Docs: 2, Tokens: 11})
	report(ingest.ProgressEvent{Phase: "ingest", Status: "progress", Files: 1, TotalFiles: 2, Bytes: 512, TotalBytes: 1024})
	report(ingest.ProgressEvent{Phase: "ingest", Status: "completed", Files: 2, TotalFiles: 2, Bytes: 1024, TotalBytes: 1024, Docs: 3, Tokens: 17})
	text := output.String()
	for _, expected := range []string{
		"0/2 files", "256 B/1.0 KiB", "3 docs  17 tokens", "0 output shards",
		"creating OpenWALDO Parquet file 1",
		"created  OpenWALDO Parquet file 1  1234567890ab  384 B  2 docs  11 tokens",
		"1/2 files", "1 output shards", "ingest complete  2/2 files",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("progress output lacks %q:\n%s", expected, text)
		}
	}
}

func TestIngestProgressReporterJSONIncludesCounts(t *testing.T) {
	var output bytes.Buffer
	ingestProgressReporter(&output, true)(ingest.ProgressEvent{Phase: "ingest", Status: "records", Docs: 3, Tokens: 17})
	if text := output.String(); !strings.Contains(text, `"docs":3`) || !strings.Contains(text, `"tokens":17`) {
		t.Fatalf("JSON progress = %q", text)
	}
}
