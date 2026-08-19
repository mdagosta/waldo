// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/openwaldo/waldo/internal/record"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: validate_conversation JSONL")
		os.Exit(2)
	}
	input, err := os.Open(os.Args[1])
	if err != nil {
		fatal(err)
	}
	defer input.Close()

	var records, tools int
	foundLiteralPrefix := false
	foundEmbeddedBoundary := false
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	for scanner.Scan() {
		var outer struct {
			Kind string `json:"kind"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &outer); err != nil {
			fatal(fmt.Errorf("decode exported record %d: %w", records+1, err))
		}
		if outer.Kind != record.KindConversation {
			fatal(fmt.Errorf("record %d kind is %q", records+1, outer.Kind))
		}
		conversation, err := record.DecodeConversation(outer.Text)
		if err != nil {
			fatal(fmt.Errorf("decode conversation %d: %w", records+1, err))
		}
		if len(conversation.Tools) > 0 {
			tools++
		}
		for _, message := range conversation.Messages {
			foundLiteralPrefix = foundLiteralPrefix || message.Content == "User: this prefix belongs to the user content."
			foundEmbeddedBoundary = foundEmbeddedBoundary || message.Content == "This remains one assistant message.\n\nUser: not a boundary."
		}
		records++
	}
	if err := scanner.Err(); err != nil {
		fatal(err)
	}
	if records != 4 || tools == 0 || !foundLiteralPrefix || !foundEmbeddedBoundary {
		fatal(fmt.Errorf("validated %d records, %d with tools, literal-prefix=%t, embedded-boundary=%t", records, tools, foundLiteralPrefix, foundEmbeddedBoundary))
	}
	fmt.Printf("validated %d structured conversations (%d with tools)\n", records, tools)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
