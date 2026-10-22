// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package join

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// SigningNamespace is shared with waldito, so either can verify the other's records.
const SigningNamespace = "waldito"

// Submission is one identity's trained unit for a round.
type Submission struct {
	Kind        string            `json:"kind"`
	Schema      int               `json:"schema"`
	Run         string            `json:"run"`
	Round       int               `json:"round"`
	Stage       string            `json:"stage"`
	Unit        string            `json:"unit"`
	Identity    string            `json:"identity"`
	Base        *string           `json:"base"`
	BaseSHA256  *string           `json:"base_sha256"`
	Model       string            `json:"model"`
	ModelSHA256 string            `json:"model_sha256"`
	BOMSHA256   *string           `json:"bom_sha256"`
	Files       map[string]string `json:"files"`
	Tokens      int64             `json:"tokens"`
	URL         string            `json:"url"`
	CreatedAt   string            `json:"created_at"`
}

// MergeRecord closes a round: the accepted submissions, the dropped units, and the merged weights.
type MergeRecord struct {
	Kind        string            `json:"kind"`
	Schema      int               `json:"schema"`
	Run         string            `json:"run"`
	Round       int               `json:"round"`
	Stage       string            `json:"stage"`
	Identity    string            `json:"identity"`
	Method      string            `json:"method"`
	Base        *string           `json:"base"`
	BaseSHA256  *string           `json:"base_sha256"`
	Accepted    []AcceptedUnit    `json:"accepted"`
	Dropped     []DroppedUnit     `json:"dropped"`
	Evidence    []UnitEvidence    `json:"evidence,omitempty"`
	Model       string            `json:"model"`
	ModelSHA256 string            `json:"model_sha256"`
	Files       map[string]string `json:"files"`
	URL         string            `json:"url"`
	ClosedAt    string            `json:"closed_at"`
}

type AcceptedUnit struct {
	Unit        string `json:"unit"`
	Identity    string `json:"identity"`
	Model       string `json:"model"`
	ModelSHA256 string `json:"model_sha256"`
	Tokens      int64  `json:"tokens"`
}

// UnitEvidence is what the closer measured to judge one unit's replicas: the
// round's starting loss and each replica's held-out loss on the same records.
type UnitEvidence struct {
	Unit                string            `json:"unit"`
	StartLoss           float64           `json:"start_loss"`
	EvaluationSetSHA256 string            `json:"evaluation_set_sha256"`
	Replicas            []ReplicaEvidence `json:"replicas"`
}

type ReplicaEvidence struct {
	Identity    string  `json:"identity"`
	ModelSHA256 string  `json:"model_sha256"`
	HeldoutLoss float64 `json:"heldout_loss"`
}

type DroppedUnit struct {
	Unit       string   `json:"unit"`
	Reason     string   `json:"reason"`
	Identities []string `json:"identities,omitempty"`
}

// writeAllowedSigners lists every plan identity's key for ssh-keygen -Y verify.
func writeAllowedSigners(path string, identities map[string]Identity) error {
	names := make([]string, 0, len(identities))
	for name := range identities {
		names = append(names, name)
	}
	sort.Strings(names)
	var lines strings.Builder
	for _, name := range names {
		fmt.Fprintf(&lines, "%s namespaces=%q %s\n", name, SigningNamespace, identities[name].Key)
	}
	return os.WriteFile(path, []byte(lines.String()), 0o644)
}

// writeSigned writes record as indented JSON beside its ssh signature.
func writeSigned(path string, record any, key string) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return err
	}
	_ = os.Remove(path + ".sig")
	if output, err := exec.Command("ssh-keygen", "-Y", "sign", "-f", key, "-n", SigningNamespace, path).CombinedOutput(); err != nil {
		return fmt.Errorf("sign %s: %w: %s", path, err, bytes.TrimSpace(output))
	}
	return nil
}

// readSigned decodes the record at path if identityOf names a plan identity
// whose key signed it; ok is false for anything unsigned or forged.
func readSigned(path, allowedSigners string, identities map[string]Identity, record any, identityOf func() string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(data, record); err != nil {
		return false, nil
	}
	identity := identityOf()
	if _, ok := identities[identity]; !ok {
		return false, nil
	}
	if _, err := os.Stat(path + ".sig"); err != nil {
		return false, nil
	}
	command := exec.Command("ssh-keygen", "-Y", "verify", "-f", allowedSigners, "-I", identity, "-n", SigningNamespace, "-s", path+".sig")
	command.Stdin = bytes.NewReader(data)
	return command.Run() == nil, nil
}
