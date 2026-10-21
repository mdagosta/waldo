// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/openwaldo/waldo/internal/tokenizer"
)

type wrapper struct {
	Artifact tokenizer.Artifact `json:"artifact"`
	BOSID    int                `json:"bos_id"`
	EOSID    int                `json:"eos_id"`
}

type item struct {
	Form         string `json:"form"`
	Prompt       string `json:"prompt"`
	Continuation string `json:"continuation"`
}

type encoded struct {
	Form         string `json:"form"`
	PromptBytes  int    `json:"prompt_bytes"`
	TargetBytes  int    `json:"target_bytes"`
	PromptTokens []int  `json:"prompt_tokens"`
	TargetTokens []int  `json:"target_tokens"`
}

func main() {
	if len(os.Args) != 3 {
		panic("usage: encode-suite TOKENIZER.json SUITE.json")
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	var artifact wrapper
	if err := json.Unmarshal(data, &artifact); err != nil {
		panic(err)
	}
	codec, err := artifact.Artifact.Codec()
	if err != nil {
		panic(err)
	}
	data, err = os.ReadFile(os.Args[2])
	if err != nil {
		panic(err)
	}
	var suite []item
	if err := json.Unmarshal(data, &suite); err != nil {
		panic(err)
	}
	result := make([]encoded, 0, len(suite))
	for _, one := range suite {
		prompt := append([]int{artifact.BOSID}, codec.Encode(one.Prompt)...)
		target := append(codec.Encode(one.Continuation), artifact.EOSID)
		result = append(result, encoded{
			Form: one.Form, PromptBytes: len([]byte(one.Prompt)), TargetBytes: len([]byte(one.Continuation)),
			PromptTokens: prompt, TargetTokens: target,
		})
	}
	output, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(output))
}
