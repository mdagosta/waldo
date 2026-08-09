// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package composes_test

import (
	"path/filepath"
	"testing"

	"github.com/openwaldo/waldo/internal/model"
)

func TestReferenceComposeForecasts(t *testing.T) {
	tests := []struct {
		file        string
		accelerator string
		hours       int64
	}{
		{file: "babble-mac.yaml", accelerator: "M4 Max 40-core GPU", hours: 1},
		{file: "h200-02h.yaml", accelerator: "H200 SXM", hours: 2},
		{file: "h200-06h.yaml", accelerator: "H200 SXM", hours: 6},
		{file: "h200-12h.yaml", accelerator: "H200 SXM", hours: 12},
		{file: "h200-24h.yaml", accelerator: "H200 SXM", hours: 24},
		{file: "h200-48h.yaml", accelerator: "H200 SXM", hours: 48},
	}
	for _, test := range tests {
		t.Run(test.file, func(t *testing.T) {
			compose, _, err := model.LoadCompose(filepath.Join(".", test.file))
			if err != nil {
				t.Fatal(err)
			}
			if compose.Architecture.Tokenizer.Name != "byte" || compose.Architecture.Tokenizer.Revision != "builtin-byte-schema-1" || compose.Architecture.VocabularySize != 259 {
				t.Fatalf("compose does not use the executable byte tokenizer: %+v", compose.Architecture.Tokenizer)
			}
			forecast, err := model.ForecastCompose(compose)
			if err != nil {
				t.Fatal(err)
			}
			want := test.hours * 60 * 60
			found := false
			for _, configuration := range forecast.Configurations {
				if configuration.Accelerator != test.accelerator || configuration.GPUs != 1 {
					continue
				}
				found = true
				if difference := configuration.ApproximateSeconds - want; difference < -1 || difference > 1 {
					t.Fatalf("forecast is %d seconds, want approximately %d", configuration.ApproximateSeconds, want)
				}
			}
			if !found {
				t.Fatalf("forecast omitted 1x %s", test.accelerator)
			}
		})
	}
}
