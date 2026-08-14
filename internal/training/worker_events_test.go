// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package training

import (
	"regexp"
	"strings"
	"testing"
)

func TestEmbeddedWorkerEmitsOnlyRecognizedEventKinds(t *testing.T) {
	kindPattern := regexp.MustCompile(`"kind":\s*"([a-z_]+)"`)
	matches := kindPattern.FindAllStringSubmatch(string(pyTorchWorker), -1)
	if len(matches) == 0 {
		t.Fatal("no event kinds found in embedded worker; pattern or worker changed")
	}
	seen := map[string]bool{}
	for _, match := range matches {
		kind := match[1]
		if seen[kind] {
			continue
		}
		seen[kind] = true
		if err := (Event{Kind: kind}).Validate(); err != nil && strings.Contains(err.Error(), "unsupported worker event kind") {
			t.Errorf("embedded worker emits event kind %q that the Go driver rejects: %v", kind, err)
		}
	}
}

func TestEmbeddedWorkerEmitsOnlyRecognizedFrameKinds(t *testing.T) {
	framePattern := regexp.MustCompile(`emit\(\s*"([a-z_]+)"`)
	matches := framePattern.FindAllStringSubmatch(string(pyTorchWorker), -1)
	if len(matches) == 0 {
		t.Fatal("no emit frame kinds found in embedded worker; pattern or worker changed")
	}
	seen := map[string]bool{}
	for _, match := range matches {
		kind := match[1]
		if seen[kind] {
			continue
		}
		seen[kind] = true
		frame := WorkerOutputFrame{Kind: kind, Schema: WorkerProtocolSchema, Error: "x"}
		if err := frame.Validate(); err != nil && strings.Contains(err.Error(), "unsupported worker output kind") {
			t.Errorf("embedded worker emits frame kind %q that the Go driver rejects: %v", kind, err)
		}
	}
}
