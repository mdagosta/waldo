// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package shard

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"github.com/parquet-go/parquet-go"
)

const (
	BOMMetadataKey = "waldo.bom"
	BOMSchema      = 1
)

// BOM is the self-contained builder attestation carried in Parquet footer
// metadata. The enclosing lookaside SHA-256 supplies object identity; a shard
// cannot include its own object hash without a circular dependency.
type BOM struct {
	Kind         string     `json:"kind"`
	Schema       int        `json:"schema"`
	Subject      string     `json:"subject"`
	PlanSHA256   string     `json:"plan_sha256"`
	RecordSchema int        `json:"record_schema"`
	WriterRecipe string     `json:"writer_recipe"`
	Tokenizer    string     `json:"tokenizer"`
	Records      int64      `json:"records"`
	Tokens       int64      `json:"tokens"`
	ContentBytes int64      `json:"content_bytes"`
	Licenses     []string   `json:"licenses"`
	Validation   Validation `json:"validation"`
}

type Validation struct {
	CanonicalRecords  bool `json:"canonical_records"`
	ContentHashes     bool `json:"content_hashes"`
	TokenCounts       bool `json:"token_counts"`
	ExactLicenseDedup bool `json:"exact_license_partition_dedup"`
}

// Attestation describes the provenance evidence discoverable in one shard.
// Embedded BOMs are hashed independently so higher-level BOMs can pin the
// evidence without confusing it with the enclosing Parquet object hash.
type Attestation struct {
	Status       string `json:"status"`
	WriterRecipe string `json:"writer_recipe,omitempty"`
	BOMSHA256    string `json:"bom_sha256,omitempty"`
	BOM          *BOM   `json:"bom,omitempty"`
}

func NewBOM(planSHA256, tokenizer string, records, tokens, contentBytes int64, licenses []string) BOM {
	licenses = append([]string(nil), licenses...)
	slices.Sort(licenses)
	return BOM{
		Kind: "openwaldo-bom", Schema: BOMSchema, Subject: "shard",
		PlanSHA256: planSHA256, RecordSchema: TextRecordSchema,
		WriterRecipe: TextWriterRecipe, Tokenizer: tokenizer,
		Records: records, Tokens: tokens, ContentBytes: contentBytes, Licenses: licenses,
		Validation: Validation{CanonicalRecords: true, ContentHashes: true, TokenCounts: true, ExactLicenseDedup: true},
	}
}

func (bom BOM) Validate() error {
	if bom.Kind != "openwaldo-bom" || bom.Schema != BOMSchema || bom.Subject != "shard" {
		return fmt.Errorf("unsupported shard BOM identity %q/%d/%q", bom.Kind, bom.Schema, bom.Subject)
	}
	if !validDigest(bom.PlanSHA256) {
		return fmt.Errorf("shard BOM plan_sha256 must be 64 lowercase hexadecimal characters")
	}
	if bom.RecordSchema != TextRecordSchema || bom.WriterRecipe != TextWriterRecipe || bom.Tokenizer == "" {
		return fmt.Errorf("shard BOM has unsupported record, writer, or tokenizer identity")
	}
	if bom.Records <= 0 || bom.Tokens < 0 || bom.ContentBytes <= 0 || len(bom.Licenses) == 0 || !slices.IsSorted(bom.Licenses) {
		return fmt.Errorf("shard BOM has invalid totals or licenses")
	}
	for position, license := range bom.Licenses {
		if license == "" || position > 0 && license == bom.Licenses[position-1] {
			return fmt.Errorf("shard BOM has an empty or duplicate license")
		}
	}
	if !bom.Validation.CanonicalRecords || !bom.Validation.ContentHashes || !bom.Validation.TokenCounts || !bom.Validation.ExactLicenseDedup {
		return fmt.Errorf("shard BOM does not attest complete ingest validation")
	}
	return nil
}

func EncodeBOM(bom BOM) (string, error) {
	if err := bom.Validate(); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(bom)
	return string(encoded), err
}

func ReadBOM(file *parquet.File) (BOM, error) {
	encoded, ok := file.Lookup(BOMMetadataKey)
	if !ok {
		return BOM{}, fmt.Errorf("shard has no embedded OpenWALDO BOM")
	}
	decoder := json.NewDecoder(bytes.NewBufferString(encoded))
	decoder.DisallowUnknownFields()
	var bom BOM
	if err := decoder.Decode(&bom); err != nil {
		return BOM{}, fmt.Errorf("decode embedded shard BOM: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return BOM{}, fmt.Errorf("decode embedded shard BOM: %w", err)
	}
	if err := bom.Validate(); err != nil {
		return BOM{}, err
	}
	return bom, nil
}

func InspectAttestation(path string) (Attestation, error) {
	file, parquetFile, _, err := openShard(path)
	if err != nil {
		return Attestation{}, err
	}
	defer file.Close()
	recipe, _ := parquetFile.Lookup("waldo.recipe")
	switch recipe {
	case TextWriterRecipe:
		if _, err := verifyAttestedOne(path); err != nil {
			return Attestation{}, err
		}
		bom, err := ReadBOM(parquetFile)
		if err != nil {
			return Attestation{}, err
		}
		encoded, err := json.Marshal(bom)
		if err != nil {
			return Attestation{}, err
		}
		digest := sha256.Sum256(encoded)
		return Attestation{Status: "embedded", WriterRecipe: recipe, BOMSHA256: hex.EncodeToString(digest[:]), BOM: &bom}, nil
	case FormerTextRecipe:
		if _, err := verifyAttestedOne(path); err != nil {
			return Attestation{}, err
		}
		return Attestation{Status: "implicit-v4", WriterRecipe: recipe}, nil
	default:
		return Attestation{Status: "unattested", WriterRecipe: recipe}, nil
	}
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}
