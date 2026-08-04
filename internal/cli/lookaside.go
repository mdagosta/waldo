package cli

import (
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/openwaldo/waldo-new/internal/config"
	"github.com/openwaldo/waldo-new/internal/lookaside"
)

func runLookasideConfigure(context Context, args []string, stdout, _ io.Writer) error {
	configuration, err := config.Load()
	if err != nil {
		return err
	}
	changed := false
	for i := 0; i < len(args); i++ {
		argument := args[i]
		switch {
		case argument == "--scratch":
			value, next, err := optionValue(args, i, "--scratch")
			if err != nil {
				return err
			}
			configuration.Lookaside.Scratch, i, changed = value, next, true
		case strings.HasPrefix(argument, "--scratch="):
			configuration.Lookaside.Scratch, changed = strings.TrimPrefix(argument, "--scratch="), true
		case argument == "--default-scratch":
			configuration.Lookaside.Scratch, changed = "", true
		case argument == "--mirror":
			value, next, err := optionValue(args, i, "--mirror")
			if err != nil {
				return err
			}
			configuration.Lookaside.Mirrors, i, changed = append(configuration.Lookaside.Mirrors, value), next, true
		case strings.HasPrefix(argument, "--mirror="):
			configuration.Lookaside.Mirrors, changed = append(configuration.Lookaside.Mirrors, strings.TrimPrefix(argument, "--mirror=")), true
		case argument == "--remove-mirror":
			value, next, err := optionValue(args, i, "--remove-mirror")
			if err != nil {
				return err
			}
			configuration.Lookaside.Mirrors, i, changed = removeString(configuration.Lookaside.Mirrors, strings.TrimRight(value, "/")), next, true
		case strings.HasPrefix(argument, "--remove-mirror="):
			configuration.Lookaside.Mirrors, changed = removeString(configuration.Lookaside.Mirrors, strings.TrimRight(strings.TrimPrefix(argument, "--remove-mirror="), "/")), true
		case argument == "--clear-mirrors":
			configuration.Lookaside.Mirrors, changed = nil, true
		case argument == "--publish":
			value, next, err := optionValue(args, i, "--publish")
			if err != nil {
				return err
			}
			publish := ensurePublish(&configuration)
			publish.URL, i, changed = value, next, true
		case strings.HasPrefix(argument, "--publish="):
			ensurePublish(&configuration).URL, changed = strings.TrimPrefix(argument, "--publish="), true
		case argument == "--publish-local":
			value, next, err := optionValue(args, i, "--publish-local")
			if err != nil {
				return err
			}
			absolute, err := filepath.Abs(value)
			if err != nil {
				return err
			}
			previous := ensurePublish(&configuration)
			configuration.Lookaside.Publish = &config.Publish{
				URL:     (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}).String(),
				Workers: previous.Workers,
			}
			i, changed = next, true
		case argument == "--publish-region":
			value, next, err := optionValue(args, i, "--publish-region")
			if err != nil {
				return err
			}
			ensurePublish(&configuration).Region, i, changed = value, next, true
		case argument == "--upload-workers":
			value, next, err := optionValue(args, i, "--upload-workers")
			if err != nil {
				return err
			}
			workers, err := strconv.Atoi(value)
			if err != nil {
				return usageError{message: "--upload-workers needs an integer"}
			}
			ensurePublish(&configuration).Workers, i, changed = workers, next, true
		case argument == "--clear-publish":
			configuration.Lookaside.Publish, changed = nil, true
		default:
			return usageError{message: fmt.Sprintf("unknown lookaside configure option %q", argument)}
		}
	}
	if !changed {
		return usageError{message: "lookaside configure requires a scratch, mirror, or publish option"}
	}
	if err := config.Save(configuration); err != nil {
		return err
	}
	// Reload to report normalized mirrors and the effective scratch path.
	configuration, err = config.Load()
	if err != nil {
		return err
	}
	scratchRoot, err := config.EffectiveScratchRoot(configuration)
	if err != nil {
		return err
	}
	configPath, err := config.Path()
	if err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, struct {
			Path    string          `json:"path"`
			Scratch string          `json:"scratch"`
			Mirrors []string        `json:"mirrors"`
			Publish *config.Publish `json:"publish,omitempty"`
		}{Path: configPath, Scratch: scratchRoot, Mirrors: configuration.Lookaside.Mirrors, Publish: configuration.Lookaside.Publish})
	}
	fmt.Fprintf(stdout, "configured lookaside in %s\n", configPath)
	fmt.Fprintf(stdout, "  scratch  %s\n", scratchRoot)
	if len(configuration.Lookaside.Mirrors) == 0 {
		fmt.Fprintln(stdout, "  mirrors  (none)")
	} else {
		for i, mirror := range configuration.Lookaside.Mirrors {
			label := ""
			if i == 0 {
				label = "mirrors"
			}
			fmt.Fprintf(stdout, "  %-7s  %s\n", label, mirror)
		}
	}
	if configuration.Lookaside.Publish == nil {
		fmt.Fprintln(stdout, "  publish  (none)")
	} else {
		publish := configuration.Lookaside.Publish
		fmt.Fprintf(stdout, "  publish  %s (%d workers)\n", publish.URL, publish.Workers)
	}
	return nil
}

func ensurePublish(configuration *config.Config) *config.Publish {
	if configuration.Lookaside.Publish == nil {
		configuration.Lookaside.Publish = &config.Publish{}
	}
	return configuration.Lookaside.Publish
}

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

func removeString(values []string, remove string) []string {
	return slices.DeleteFunc(values, func(value string) bool { return strings.TrimRight(value, "/") == remove })
}
