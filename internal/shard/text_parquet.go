// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package shard

import (
	"fmt"
	"io"

	kzstd "github.com/klauspost/compress/zstd"
	"github.com/parquet-go/parquet-go"
	parquetzstd "github.com/parquet-go/parquet-go/compress/zstd"
)

const (
	TextRecordSchema        = 2
	FormerTextRecordSchema  = 1
	TextWriterRecipe        = "parquet-go/0.30.1/zstd-6/page-1m/rg-64m/v9-privacy-redaction"
	FormerMainContentRecipe = "parquet-go/0.30.1/zstd-6/page-1m/rg-64m/v8-main-content"
	FormerAssessmentRecipe  = "parquet-go/0.30.1/zstd-6/page-1m/rg-64m/v7-content-assessment"
	FormerTextBOMRecipe     = "parquet-go/0.30.1/zstd-6/page-1m/rg-64m/v5-bom"
	FormerTextRecipe        = "parquet-go/0.30.1/zstd-6/page-1m/rg-64m/v4"
	EmailDetector           = "waldo/email-address-v1"
	RepetitionDetector      = "waldo/gopher-ngram-repetition-v1"
	BoilerplateDetector     = "waldo/gopher-structural-duplication-v1"
	PrivacyRedactionPolicy  = "waldo/privacy-redaction-v1"
	TextRowGroupBytes       = 64 << 20
	TextPageBytes           = 1 << 20
)

// TextRow is the canonical tokenizer-neutral pretraining row for record schema
// 2. Pointer fields are true Parquet nulls; empty strings and zero values are
// not overloaded to mean absence.
type TextRow struct {
	ContentSHA256             [32]byte `parquet:"content_sha256"`
	Text                      string   `parquet:"text"`
	Source                    string   `parquet:"source"`
	SourceName                *string  `parquet:"source_name,dict"`
	License                   string   `parquet:"license,dict"`
	LicenseRaw                *string  `parquet:"license_raw,dict"`
	Language                  *string  `parquet:"language,dict"`
	LanguageScore             *int32   `parquet:"language_score"`
	Date                      *string  `parquet:"date,dict"`
	TokenCount                *int64   `parquet:"token_count"`
	Meta                      *string  `parquet:"meta"`
	EmailAddresses            bool     `parquet:"email_addresses"`
	RepetitiveContent         bool     `parquet:"repetitive_content"`
	BoilerplateContent        bool     `parquet:"boilerplate_content"`
	MainContent               bool     `parquet:"main_content"`
	RedactedEmailAddresses    int64    `parquet:"redacted_email_addresses"`
	RedactedIPAddresses       int64    `parquet:"redacted_ip_addresses"`
	RedactedPhoneNumbers      int64    `parquet:"redacted_phone_numbers"`
	RemovedMailRoutingHeaders int64    `parquet:"removed_mail_routing_headers"`
	RedactedCredentials       int64    `parquet:"redacted_credentials"`
}

// textRowV3 preserves the schema-2 physical contract before automatic
// privacy redaction evidence was added.
type textRowV3 struct {
	ContentSHA256      [32]byte `parquet:"content_sha256"`
	Text               string   `parquet:"text"`
	Source             string   `parquet:"source"`
	SourceName         *string  `parquet:"source_name,dict"`
	License            string   `parquet:"license,dict"`
	LicenseRaw         *string  `parquet:"license_raw,dict"`
	Language           *string  `parquet:"language,dict"`
	LanguageScore      *int32   `parquet:"language_score"`
	Date               *string  `parquet:"date,dict"`
	TokenCount         *int64   `parquet:"token_count"`
	Meta               *string  `parquet:"meta"`
	EmailAddresses     bool     `parquet:"email_addresses"`
	RepetitiveContent  bool     `parquet:"repetitive_content"`
	BoilerplateContent bool     `parquet:"boilerplate_content"`
	MainContent        bool     `parquet:"main_content"`
}

// textRowV2 preserves the initial schema-2 physical contract. Before
// main_content existed, every retained row was implicitly main content.
type textRowV2 struct {
	ContentSHA256      [32]byte `parquet:"content_sha256"`
	Text               string   `parquet:"text"`
	Source             string   `parquet:"source"`
	SourceName         *string  `parquet:"source_name,dict"`
	License            string   `parquet:"license,dict"`
	LicenseRaw         *string  `parquet:"license_raw,dict"`
	Language           *string  `parquet:"language,dict"`
	LanguageScore      *int32   `parquet:"language_score"`
	Date               *string  `parquet:"date,dict"`
	TokenCount         *int64   `parquet:"token_count"`
	Meta               *string  `parquet:"meta"`
	EmailAddresses     bool     `parquet:"email_addresses"`
	RepetitiveContent  bool     `parquet:"repetitive_content"`
	BoilerplateContent bool     `parquet:"boilerplate_content"`
}

// textRowV1 preserves the established schema-1 physical contract for reads.
type textRowV1 struct {
	ContentSHA256 [32]byte `parquet:"content_sha256"`
	Text          string   `parquet:"text"`
	Source        string   `parquet:"source"`
	SourceName    *string  `parquet:"source_name,dict"`
	License       string   `parquet:"license,dict"`
	LicenseRaw    *string  `parquet:"license_raw,dict"`
	Language      *string  `parquet:"language,dict"`
	LanguageScore *int32   `parquet:"language_score"`
	Date          *string  `parquet:"date,dict"`
	TokenCount    *int64   `parquet:"token_count"`
	Meta          *string  `parquet:"meta"`
}

// NewTextParquetWriter returns the canonical schema-2 writer. Callers control
// row-group boundaries explicitly with Flush and write directly to the final
// temporary-file stream; this function never introduces a whole-shard buffer.
func NewTextParquetWriter(output io.Writer) *parquet.GenericWriter[TextRow] {
	writer := parquet.NewGenericWriter[TextRow](output, proposedTextWriterOptions()...)
	writer.SetKeyValueMetadata("waldo.object", "shard")
	writer.SetKeyValueMetadata("waldo.record_schema", fmt.Sprint(TextRecordSchema))
	writer.SetKeyValueMetadata("waldo.recipe", TextWriterRecipe)
	return writer
}

func proposedTextWriterOptions() []parquet.WriterOption {
	return textWriterOptionsForLevel(6)
}

func textWriterOptionsForLevel(level int) []parquet.WriterOption {
	return []parquet.WriterOption{
		parquet.Compression(&parquetzstd.Codec{
			Level:       kzstd.EncoderLevelFromZstd(level),
			Concurrency: 1,
		}),
		parquet.PageBufferSize(TextPageBytes),
		parquet.WriteBufferSize(32 << 10),
		parquet.DataPageVersion(2),
		parquet.SkipPageBounds("text"),
		parquet.SkipPageBounds("source"),
		parquet.SkipPageBounds("meta"),
		parquet.SkipPageStatistics("text"),
		parquet.SkipPageStatistics("source"),
		parquet.SkipPageStatistics("meta"),
	}
}

func formerTextWriterOptions() []parquet.WriterOption {
	return []parquet.WriterOption{
		parquet.Compression(&parquetzstd.Codec{
			Level:       parquetzstd.SpeedBestCompression,
			Concurrency: 1,
		}),
		parquet.PageBufferSize(4 << 20),
		parquet.WriteBufferSize(32 << 10),
		parquet.DataPageVersion(2),
	}
}
