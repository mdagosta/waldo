package cli

import (
	"fmt"
	"io"

	"github.com/openwaldo/waldo-new/internal/config"
	"github.com/openwaldo/waldo-new/internal/lookaside"
)

func runLookasideStatus(context Context, args []string, stdout, _ io.Writer) error {
	if len(args) != 0 {
		return usageError{message: "usage: waldo lookaside status [--json]"}
	}
	cache, err := lookaside.DefaultCache()
	if err != nil {
		return err
	}
	stats, err := cache.Stats()
	if err != nil {
		return err
	}
	configuration, err := config.Load()
	if err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, struct {
			Root    string          `json:"root"`
			Mirrors []string        `json:"mirrors"`
			Publish *config.Publish `json:"publish,omitempty"`
			Stats   lookaside.Stats `json:"stats"`
		}{Root: cache.Root(), Mirrors: cache.Mirrors(), Publish: configuration.Lookaside.Publish, Stats: stats})
	}
	fmt.Fprintf(stdout, "lookaside scratch  %s\n", cache.Root())
	fmt.Fprintf(stdout, "  objects        %s\n", humanInteger(stats.Objects))
	fmt.Fprintf(stdout, "  bytes          %s\n", humanBytes(stats.Bytes))
	if stats.Other > 0 {
		fmt.Fprintf(stdout, "  other files    %s\n", humanInteger(stats.Other))
	}
	if len(cache.Mirrors()) == 0 {
		fmt.Fprintln(stdout, "  mirrors        (none)")
	} else {
		for i, mirror := range cache.Mirrors() {
			label := ""
			if i == 0 {
				label = "mirrors"
			}
			fmt.Fprintf(stdout, "  %-13s  %s\n", label, mirror)
		}
	}
	if configuration.Lookaside.Publish == nil {
		fmt.Fprintln(stdout, "  publish        (none)")
	} else {
		publish := configuration.Lookaside.Publish
		fmt.Fprintf(stdout, "  publish        %s (%d workers)\n", publish.URL, publish.Workers)
	}
	return nil
}

func runLookasideVerify(context Context, args []string, stdout, _ io.Writer) error {
	if len(args) != 0 {
		return usageError{message: "usage: waldo lookaside verify [--json]"}
	}
	cache, err := lookaside.DefaultCache()
	if err != nil {
		return err
	}
	result, err := cache.Scrub()
	if err != nil {
		return err
	}
	if context.JSON {
		if err := writeJSON(stdout, struct {
			Root   string                `json:"root"`
			Result lookaside.ScrubResult `json:"result"`
		}{Root: cache.Root(), Result: result}); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(stdout, "scrubbed %s: %s verified, %s corrupt, %s total\n",
			cache.Root(), humanInteger(result.Verified), humanInteger(int64(len(result.Corrupt))), humanBytes(result.Bytes))
		for _, issue := range result.Corrupt {
			fmt.Fprintf(stdout, "  CORRUPT %s: %s\n", issue.Path, issue.Error)
		}
	}
	if len(result.Corrupt) > 0 {
		return fmt.Errorf("lookaside scratch contains %d corrupt object(s)", len(result.Corrupt))
	}
	return nil
}
