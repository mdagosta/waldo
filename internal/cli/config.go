package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/openwaldo/waldo-new/internal/config"
	waldoindex "github.com/openwaldo/waldo-new/internal/index"
)

var configKeys = []string{
	"index",
	"lookaside",
	"lookaside.region",
	"lookaside.workers",
	"lookaside.mirrors",
	"lookaside.cache",
	"lookaside.cache.max-size",
	"lookaside.scratch",
	"ingest.staging",
	"model.root",
	"model.backend",
}

func runConfigShow(context Context, args []string, stdout, _ io.Writer) error {
	if len(args) != 0 {
		return usageError{message: "usage: waldo config show [--json]"}
	}
	configuration, err := config.Load()
	if err != nil {
		return err
	}
	path, err := config.Path()
	if err != nil {
		return err
	}
	values := map[string]any{}
	for _, key := range configKeys {
		value, set, err := configValue(configuration, key)
		if err != nil {
			return err
		}
		if set {
			values[key] = value
		}
	}
	if context.JSON {
		return writeJSON(stdout, struct {
			Path   string         `json:"path"`
			Values map[string]any `json:"values"`
		}{Path: path, Values: values})
	}
	fmt.Fprintf(stdout, "configuration %s\n", path)
	for _, key := range configKeys {
		value, set, err := configValue(configuration, key)
		if err != nil {
			return err
		}
		if !set {
			fmt.Fprintf(stdout, "  %-27s (unset)\n", key)
			continue
		}
		printConfigValue(stdout, key, value)
	}
	return nil
}

func runConfigGet(context Context, args []string, stdout, _ io.Writer) error {
	if len(args) > 1 {
		return usageError{message: "usage: waldo config get [key-or-prefix] [--json]"}
	}
	configuration, err := config.Load()
	if err != nil {
		return err
	}
	selector := ""
	if len(args) == 1 {
		selector = strings.TrimSuffix(args[0], ".")
	}
	matches, err := matchingConfigValues(configuration, selector)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		return usageError{message: unknownConfigKey(selector)}
	}
	exactLeaf := len(matches) == 1 && matches[0].Key == selector
	if context.JSON && exactLeaf {
		return writeJSON(stdout, struct {
			Key   string `json:"key"`
			Value any    `json:"value"`
			Set   bool   `json:"set"`
		}{Key: matches[0].Key, Value: matches[0].Value, Set: matches[0].Set})
	}
	if context.JSON {
		return writeJSON(stdout, struct {
			Matches []configMatch `json:"matches"`
		}{Matches: matches})
	}
	if exactLeaf {
		if !matches[0].Set {
			fmt.Fprintln(stdout, "(unset)")
			return nil
		}
		printConfigValue(stdout, "", matches[0].Value)
		return nil
	}
	for _, match := range matches {
		if !match.Set {
			fmt.Fprintf(stdout, "  %-27s (unset)\n", match.Key)
			continue
		}
		printConfigValue(stdout, match.Key, match.Value)
	}
	return nil
}

type configMatch struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
	Set   bool   `json:"set"`
}

func matchingConfigValues(configuration config.Config, selector string) ([]configMatch, error) {
	matches := []configMatch{}
	for _, key := range configKeys {
		if selector != "" && !strings.HasPrefix(key, selector) {
			continue
		}
		value, set, err := configValue(configuration, key)
		if err != nil {
			return nil, err
		}
		matches = append(matches, configMatch{Key: key, Value: value, Set: set})
	}
	return matches, nil
}

func runConfigSet(context Context, args []string, stdout, _ io.Writer) error {
	if len(args) < 2 {
		return usageError{message: "usage: waldo config set <key> <value...> [--json]"}
	}
	key, values := args[0], args[1:]
	configuration, err := config.Load()
	if err != nil {
		return err
	}
	switch key {
	case "index":
		if len(values) != 1 {
			return oneConfigValue(key)
		}
		target, err := waldoindex.Resolve("", values[0])
		if err != nil {
			return usageError{message: fmt.Sprintf("index must name an existing WALDO index checkout: %v", err)}
		}
		configuration.Index = target.Root
	case "lookaside":
		if len(values) != 1 {
			return oneConfigValue(key)
		}
		publish := config.Publish{}
		if configuration.Lookaside.Publish != nil {
			publish = *configuration.Lookaside.Publish
		}
		publish.URL = values[0]
		if !strings.HasPrefix(strings.TrimSpace(values[0]), "s3://") {
			publish.Region = ""
		}
		configuration.Lookaside.Publish = &publish
	case "lookaside.region":
		if len(values) != 1 {
			return oneConfigValue(key)
		}
		publish, err := configuredPublisher(&configuration, key)
		if err != nil {
			return err
		}
		publish.Region = values[0]
	case "lookaside.workers":
		if len(values) != 1 {
			return oneConfigValue(key)
		}
		workers, err := strconv.Atoi(values[0])
		if err != nil {
			return usageError{message: "lookaside.workers must be an integer in 1..32"}
		}
		publish, err := configuredPublisher(&configuration, key)
		if err != nil {
			return err
		}
		publish.Workers = workers
	case "lookaside.mirrors":
		configuration.Lookaside.Mirrors = append([]string(nil), values...)
	case "lookaside.cache":
		if len(values) != 1 {
			return oneConfigValue(key)
		}
		configuration.Lookaside.Cache, err = filepath.Abs(values[0])
		if err != nil {
			return err
		}
	case "lookaside.cache.max-size":
		if len(values) != 1 {
			return oneConfigValue(key)
		}
		configuration.Lookaside.CacheMaxBytes, err = parseByteSize(values[0])
		if err != nil {
			return usageError{message: "lookaside.cache.max-size must be a positive byte size such as 20GiB or 500MiB"}
		}
	case "lookaside.scratch":
		if len(values) != 1 {
			return oneConfigValue(key)
		}
		configuration.Lookaside.Scratch, err = filepath.Abs(values[0])
		if err != nil {
			return err
		}
	case "ingest.staging":
		if len(values) != 1 {
			return oneConfigValue(key)
		}
		configuration.Ingest.Staging, err = filepath.Abs(values[0])
		if err != nil {
			return err
		}
	case "model.root":
		if len(values) != 1 {
			return oneConfigValue(key)
		}
		configuration.Model.Root, err = filepath.Abs(values[0])
		if err != nil {
			return err
		}
	case "model.backend":
		if len(values) != 1 {
			return oneConfigValue(key)
		}
		if values[0] != "auto" && values[0] != "fake" {
			return usageError{message: "model.backend must be auto or fake"}
		}
		configuration.Model.Backend = values[0]
	default:
		return usageError{message: unknownConfigKey(key)}
	}
	if err := config.Save(configuration); err != nil {
		return err
	}
	return reportConfigMutation(context, stdout, "set", key)
}

func runConfigUnset(context Context, args []string, stdout, _ io.Writer) error {
	if len(args) != 1 {
		return usageError{message: "usage: waldo config unset <key> [--json]"}
	}
	key := args[0]
	configuration, err := config.Load()
	if err != nil {
		return err
	}
	switch key {
	case "index":
		configuration.Index = ""
	case "lookaside":
		configuration.Lookaside.Publish = nil
	case "lookaside.region":
		if configuration.Lookaside.Publish != nil {
			configuration.Lookaside.Publish.Region = ""
		}
	case "lookaside.workers":
		if configuration.Lookaside.Publish != nil {
			configuration.Lookaside.Publish.Workers = 0
		}
	case "lookaside.mirrors":
		configuration.Lookaside.Mirrors = nil
	case "lookaside.cache":
		configuration.Lookaside.Cache = ""
	case "lookaside.cache.max-size":
		configuration.Lookaside.CacheMaxBytes = 0
	case "lookaside.scratch":
		configuration.Lookaside.Scratch = ""
	case "ingest.staging":
		configuration.Ingest.Staging = ""
	case "model.root":
		configuration.Model.Root = ""
	case "model.backend":
		configuration.Model.Backend = ""
	default:
		return usageError{message: unknownConfigKey(key)}
	}
	if err := config.Save(configuration); err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, struct {
			Key    string `json:"key"`
			Status string `json:"status"`
		}{Key: key, Status: "unset"})
	}
	fmt.Fprintf(stdout, "unset %s\n", key)
	return nil
}

func reportConfigMutation(context Context, stdout io.Writer, status, key string) error {
	configuration, err := config.Load()
	if err != nil {
		return err
	}
	value, _, err := configValue(configuration, key)
	if err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, struct {
			Key    string `json:"key"`
			Value  any    `json:"value"`
			Status string `json:"status"`
		}{Key: key, Value: value, Status: status})
	}
	fmt.Fprintf(stdout, "%s %s = ", status, key)
	printConfigValue(stdout, "", value)
	return nil
}

func configValue(configuration config.Config, key string) (any, bool, error) {
	switch key {
	case "index":
		if configuration.Index == "" {
			return nil, false, nil
		}
		return configuration.Index, true, nil
	case "lookaside":
		if configuration.Lookaside.Publish == nil {
			return nil, false, nil
		}
		return configuration.Lookaside.Publish.URL, true, nil
	case "lookaside.region":
		if configuration.Lookaside.Publish == nil || configuration.Lookaside.Publish.Region == "" {
			return nil, false, nil
		}
		return configuration.Lookaside.Publish.Region, true, nil
	case "lookaside.workers":
		if configuration.Lookaside.Publish == nil {
			return nil, false, nil
		}
		return configuration.Lookaside.Publish.Workers, true, nil
	case "lookaside.mirrors":
		if len(configuration.Lookaside.Mirrors) == 0 {
			return nil, false, nil
		}
		return append([]string(nil), configuration.Lookaside.Mirrors...), true, nil
	case "lookaside.cache":
		value, err := config.EffectiveCacheRoot(configuration)
		return value, err == nil, err
	case "lookaside.cache.max-size":
		return config.EffectiveCacheMaxBytes(configuration), true, nil
	case "lookaside.scratch":
		value, err := config.EffectiveScratchRoot(configuration)
		return value, err == nil, err
	case "ingest.staging":
		value, err := config.EffectiveStagingBase(configuration)
		return value, err == nil, err
	case "model.root":
		value, err := config.EffectiveModelRoot(configuration)
		return value, err == nil, err
	case "model.backend":
		return config.EffectiveModelBackend(configuration), true, nil
	default:
		return nil, false, fmt.Errorf("%s", unknownConfigKey(key))
	}
}

func parseByteSize(value string) (int64, error) {
	raw := strings.TrimSpace(value)
	multipliers := []struct {
		suffix     string
		multiplier int64
	}{{"TiB", 1 << 40}, {"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}, {"TB", 1_000_000_000_000}, {"GB", 1_000_000_000}, {"MB", 1_000_000}, {"KB", 1_000}}
	for _, item := range multipliers {
		if strings.HasSuffix(strings.ToUpper(raw), strings.ToUpper(item.suffix)) {
			number := strings.TrimSpace(raw[:len(raw)-len(item.suffix)])
			parsed, err := strconv.ParseInt(number, 10, 64)
			if err != nil || parsed <= 0 || parsed > (1<<63-1)/item.multiplier {
				return 0, fmt.Errorf("invalid size")
			}
			return parsed * item.multiplier, nil
		}
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid size")
	}
	return parsed, nil
}

func configuredPublisher(configuration *config.Config, key string) (*config.Publish, error) {
	if configuration.Lookaside.Publish == nil {
		return nil, usageError{message: fmt.Sprintf("set lookaside before %s", key)}
	}
	return configuration.Lookaside.Publish, nil
}

func oneConfigValue(key string) error {
	return usageError{message: fmt.Sprintf("configuration key %s requires exactly one value", key)}
}

func unknownConfigKey(key string) string {
	return fmt.Sprintf("unknown configuration key %q; keys are %s", key, strings.Join(configKeys, ", "))
}

func printConfigValue(output io.Writer, key string, value any) {
	prefix := ""
	if key != "" {
		prefix = fmt.Sprintf("  %-27s ", key)
	}
	switch typed := value.(type) {
	case []string:
		for index, item := range typed {
			if index == 0 {
				fmt.Fprintf(output, "%s%s\n", prefix, item)
			} else {
				fmt.Fprintf(output, "%s%s\n", strings.Repeat(" ", len(prefix)), item)
			}
		}
	case int64:
		fmt.Fprintf(output, "%s%s\n", prefix, humanBytes(typed))
	default:
		fmt.Fprintf(output, "%s%v\n", prefix, typed)
	}
}
