package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/openwaldo/waldo-new/internal/model"
)

func TestApproximateDurationUsesHoursUntilOneHundred(t *testing.T) {
	for _, test := range []struct {
		seconds int64
		want    string
	}{
		{seconds: 30 * 60, want: "under 1 hour"},
		{seconds: 60 * 60, want: "1 hour"},
		{seconds: 99 * 60 * 60, want: "99 hours"},
		{seconds: 100 * 60 * 60, want: "4 days"},
		{seconds: 12 * 24 * 60 * 60, want: "12 days"},
	} {
		if got := approximateDuration(test.seconds); got != test.want {
			t.Errorf("approximateDuration(%d) = %q, want %q", test.seconds, got, test.want)
		}
	}
}

func TestWriteModelForecastUsesApprovedCompactColumns(t *testing.T) {
	report := model.ResourceForecast{Configurations: []model.HardwareConfiguration{
		{Manufacturer: "Apple", Accelerator: "M4 Max 40-core GPU", GPUs: 1, MemoryPerGPUBytes: 128 << 30, ApproximateSeconds: 48 * 24 * 60 * 60},
		{Manufacturer: "NVIDIA", Accelerator: "H100 SXM", GPUs: 8, MemoryPerGPUBytes: 80 << 30, ApproximateSeconds: 44 * 60 * 60},
	}}
	var output bytes.Buffer
	writeModelForecast(&output, report)
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("output = %q", output.String())
	}
	for _, want := range []string{"MFR", "ACCELERATOR", "GPUS", "MEMORY/GPU", "APPROX. TIME", "Apple", "128 GB", "48 days", "NVIDIA", "80 GB", "44 hours"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("output missing %q:\n%s", want, output.String())
		}
	}
	for _, unwanted := range []string{"BACKEND", "FIT", "~", "unified"} {
		if strings.Contains(output.String(), unwanted) {
			t.Errorf("output unexpectedly contains %q:\n%s", unwanted, output.String())
		}
	}
}
