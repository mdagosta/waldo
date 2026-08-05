package signing

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openwaldo/waldo/internal/config"
)

func TestSignExportRequiresCompleteConfiguration(t *testing.T) {
	directory := t.TempDir()
	if err := SignExport(context.Background(), config.Signing{}, directory, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("unconfigured error = %v", err)
	}
	if err := SignExport(context.Background(), config.Signing{Method: "sigstore-key"}, directory, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "signing.key is unset") {
		t.Fatalf("missing key error = %v", err)
	}
}

func TestSignExportCreatesDetachedBundlesForBothBOMs(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{WALDOBOM, EUBOM} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bin := t.TempDir()
	cosign := filepath.Join(bin, "cosign")
	script := `#!/bin/sh
set -eu
bundle=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--bundle" ]; then
    shift
    bundle=$1
  fi
  shift
done
[ -n "$bundle" ]
printf '{"verified":true}\n' > "$bundle"
`
	if err := os.WriteFile(cosign, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if err := SignExport(context.Background(), config.Signing{Method: "sigstore-keyless"}, directory, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{WALDOBOMBundle, EUBOMBundle} {
		info, err := os.Stat(filepath.Join(directory, name))
		if err != nil || info.Size() == 0 {
			t.Fatalf("bundle %s: info=%v err=%v", name, info, err)
		}
	}
}
