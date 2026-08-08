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

func TestIngestExclusionWarningIsProminent(t *testing.T) {
	plan := ingest.Plan{Inputs: []ingest.PlanInput{{Profile: ingest.InputProfile{
		Type: ingest.ProfileXMLRecord, XML: ingest.XMLMapping{OnMalformed: "skip"},
	}}}}
	var output bytes.Buffer
	emitIngestExclusionWarning(&output, ingest.AssemblyResult{RejectedDocs: 7}, plan)
	message := output.String()
	if !strings.Contains(message, "WARNING: WALDO EXCLUDED 7 RECORDS DURING INGESTION (MALFORMED XML)") || !strings.Contains(message, "NOT PRESENT IN THE PUBLISHED SHARDS") {
		t.Fatalf("warning = %q", message)
	}
}
