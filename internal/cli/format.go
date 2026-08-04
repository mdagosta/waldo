package cli

import (
	"fmt"
	"strconv"
)

func humanInteger(value int64) string {
	raw := strconv.FormatInt(value, 10)
	start := 0
	if len(raw) > 0 && raw[0] == '-' {
		start = 1
	}
	for i := len(raw) - 3; i > start; i -= 3 {
		raw = raw[:i] + "," + raw[i:]
	}
	return raw
}

func humanCount(value int64) string {
	if value < 1_000 {
		return fmt.Sprintf("%d", value)
	}
	units := []struct {
		value  float64
		suffix string
	}{
		{1e12, "T"},
		{1e9, "B"},
		{1e6, "M"},
		{1e3, "K"},
	}
	for _, unit := range units {
		if float64(value) >= unit.value {
			return fmt.Sprintf("%.1f%s", float64(value)/unit.value, unit.suffix)
		}
	}
	return fmt.Sprintf("%d", value)
}

func humanBytes(value int64) string {
	if value < 1024 {
		return fmt.Sprintf("%d B", value)
	}
	units := []struct {
		value  float64
		suffix string
	}{
		{1 << 40, "TiB"},
		{1 << 30, "GiB"},
		{1 << 20, "MiB"},
		{1 << 10, "KiB"},
	}
	for _, unit := range units {
		if float64(value) >= unit.value {
			return fmt.Sprintf("%.1f %s", float64(value)/unit.value, unit.suffix)
		}
	}
	return fmt.Sprintf("%d B", value)
}
