// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package index

import (
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	SourcePublicDataset        = "public-dataset"
	SourceCommerciallyLicensed = "commercially-licensed"
	SourcePrivateThirdParty    = "private-third-party"
	SourceWebCrawl             = "web-crawl"
	SourceUserData             = "user-data"
	SourceSynthetic            = "synthetic"
	SourceOther                = "other"
)

var modalityNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func validateManifestProvenance(manifest Manifest) error {
	hasModalityFacts := manifest.Rollup != nil && len(manifest.Rollup.Modalities) > 0
	shardModalities := Modalities{}
	if manifest.Rollup != nil {
		if err := validateModalities("rollup", manifest.Rollup.Modalities); err != nil {
			return err
		}
		if len(manifest.Rollup.Modalities) > 0 && modalityTokens(manifest.Rollup.Modalities) != manifest.Rollup.Tokens {
			return fmt.Errorf("rollup modality tokens do not match its token total")
		}
		addModalities(shardModalities, manifest.Rollup.Modalities)
	} else {
		for i, shard := range manifest.Shards {
			if err := validateModalities(fmt.Sprintf("shard %d", i+1), shard.Modalities); err != nil {
				return err
			}
			if len(shard.Modalities) > 0 {
				hasModalityFacts = true
				if shard.Docs <= 0 || shard.Bytes <= 0 {
					return fmt.Errorf("shard %d with modality facts requires positive docs and bytes", i+1)
				}
				if modalityTokens(shard.Modalities) != shard.Tokens {
					return fmt.Errorf("shard %d modality tokens do not match its token total", i+1)
				}
			}
			addModalities(shardModalities, shard.Modalities)
		}
	}

	sourceModalities := Modalities{}
	hasSourceUsage := false
	for i, source := range manifest.Sources {
		if err := validateSourceProvenance(source); err != nil {
			return fmt.Errorf("source %d %q: %w", i+1, source.Name, err)
		}
		if len(source.Usage) > 0 {
			hasSourceUsage = true
		}
		addModalities(sourceModalities, source.Usage)
	}
	if hasModalityFacts != hasSourceUsage {
		return fmt.Errorf("modality facts and per-source usage must be declared together")
	}
	if hasModalityFacts {
		if manifest.Rollup == nil {
			for i, shard := range manifest.Shards {
				if len(shard.Modalities) == 0 {
					return fmt.Errorf("shard %d is missing modality facts", i+1)
				}
			}
		}
		for i, source := range manifest.Sources {
			if len(source.Usage) == 0 {
				return fmt.Errorf("source %d %q is missing usage", i+1, source.Name)
			}
		}
		if !maps.Equal(shardModalities, sourceModalities) {
			return fmt.Errorf("per-source usage does not reconcile with shard modality totals")
		}
	}
	if manifest.Processing != nil {
		return validateProcessing(*manifest.Processing)
	}
	return nil
}

func modalityTokens(modalities Modalities) int64 {
	var tokens int64
	for _, measure := range modalities {
		tokens += measure.Tokens
	}
	return tokens
}

func validateSourceProvenance(source Source) error {
	if err := validateModalities("usage", source.Usage); err != nil {
		return err
	}
	hasNewFacts := len(source.Usage) > 0 || source.Content != nil || source.Acquisition != nil || source.LicenseEvidence != nil || source.CollectedFrom != "" || source.CollectedTo != ""
	category := source.Category
	if category == "public" {
		category = SourcePublicDataset
	}
	if hasNewFacts && category == "" {
		return fmt.Errorf("category is required with provenance facts")
	}
	if category != "" && !validSourceCategory(category) {
		return fmt.Errorf("unsupported category %q", source.Category)
	}
	if err := validatePeriod("acquisition", source.CollectedFrom, source.CollectedTo); err != nil {
		return err
	}
	if source.LicenseEvidence != nil {
		if err := validateLicenseEvidence(*source.LicenseEvidence); err != nil {
			return err
		}
	}
	if source.Content != nil {
		if err := validateContent(*source.Content); err != nil {
			return err
		}
	}
	if source.Acquisition == nil {
		if category == SourceCommerciallyLicensed || category == SourcePrivateThirdParty || category == SourceWebCrawl || category == SourceUserData || category == SourceSynthetic {
			return fmt.Errorf("category %q requires acquisition details", category)
		}
		return nil
	}
	return validateAcquisition(category, *source.Acquisition)
}

// ValidateSourceProvenance validates disclosure facts preserved in a pinned
// source without requiring the original manifest.
func ValidateSourceProvenance(source Source) error {
	return validateSourceProvenance(source)
}

func validSourceCategory(category string) bool {
	_, ok := CanonicalSourceCategory(category)
	return ok
}

// CanonicalSourceCategory maps the schema-1 legacy spelling to the controlled
// vocabulary emitted by new ingestion plans and manifests.
func CanonicalSourceCategory(category string) (string, bool) {
	switch category {
	case "public":
		return SourcePublicDataset, true
	case SourcePublicDataset, SourceCommerciallyLicensed,
		SourcePrivateThirdParty, SourceWebCrawl, SourceUserData,
		SourceSynthetic, SourceOther:
		return category, true
	default:
		return "", false
	}
}

func validateModalities(label string, modalities Modalities) error {
	for modality, measure := range modalities {
		if !modalityNamePattern.MatchString(modality) {
			return fmt.Errorf("%s modality name %q is invalid", label, modality)
		}
		if measure.Samples < 0 || measure.Items < 0 || measure.Tokens < 0 || measure.DurationMS < 0 || measure.ContentBytes < 0 {
			return fmt.Errorf("%s modality %q has a negative measure", label, modality)
		}
		if measure == (ModalityMeasure{}) {
			return fmt.Errorf("%s modality %q has no measures", label, modality)
		}
	}
	return nil
}

// ValidateModalities validates exact per-modality measures preserved outside
// an index manifest, including in OpenWALDO BOMs.
func ValidateModalities(label string, modalities Modalities) error {
	return validateModalities(label, modalities)
}

func addModalities(target Modalities, source Modalities) {
	for modality, value := range source {
		current := target[modality]
		current.Samples += value.Samples
		current.Items += value.Items
		current.Tokens += value.Tokens
		current.DurationMS += value.DurationMS
		current.ContentBytes += value.ContentBytes
		target[modality] = current
	}
}

func validateContent(content Content) error {
	for label, values := range map[string][]string{
		"types": content.Types, "languages": content.Languages,
		"geographies": content.Geographies, "demographics": content.Demographics,
	} {
		if err := validateStrings(label, values, true); err != nil {
			return err
		}
	}
	for label, value := range map[string]string{
		"personal_data":     content.PersonalData,
		"copyrighted":       content.Copyrighted,
		"machine_generated": content.MachineGenerated,
	} {
		if value != "" && value != "yes" && value != "no" && value != "unknown" {
			return fmt.Errorf("content %s must be yes, no, or unknown", label)
		}
	}
	if err := validatePeriod("content", content.From, content.To); err != nil {
		return err
	}
	if content.Selection != "" && strings.TrimSpace(content.Selection) == "" {
		return fmt.Errorf("content selection must not be blank")
	}
	return nil
}

func validateLicenseEvidence(evidence LicenseEvidence) error {
	if strings.TrimSpace(evidence.Declaration) == "" && strings.TrimSpace(evidence.URL) == "" {
		return fmt.Errorf("license_evidence requires declaration or url")
	}
	if evidence.URL == "" {
		return nil
	}
	parsed, err := url.Parse(evidence.URL)
	if err != nil || !parsed.IsAbs() || (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host == "" {
		return fmt.Errorf("license_evidence url must be an absolute URL")
	}
	return nil
}

func validatePeriod(label, from, to string) error {
	fromTime, err := parseEvidenceDate(from, false)
	if err != nil {
		return fmt.Errorf("%s from: %w", label, err)
	}
	toTime, err := parseEvidenceDate(to, true)
	if err != nil {
		return fmt.Errorf("%s to: %w", label, err)
	}
	if from != "" && to != "" && fromTime.After(toTime) {
		return fmt.Errorf("%s from must not be after to", label)
	}
	return nil
}

func parseEvidenceDate(value string, upperBound bool) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, nil
	}
	for _, period := range []struct {
		layout string
		add    func(time.Time) time.Time
	}{
		{"2006-01-02", func(value time.Time) time.Time { return value.AddDate(0, 0, 1) }},
		{"2006-01", func(value time.Time) time.Time { return value.AddDate(0, 1, 0) }},
		{"2006", func(value time.Time) time.Time { return value.AddDate(1, 0, 0) }},
	} {
		if parsed, err := time.Parse(period.layout, value); err == nil {
			if upperBound {
				return period.add(parsed).Add(-time.Nanosecond), nil
			}
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("%q must be an ISO 8601 year, month, date, or RFC3339 timestamp", value)
}

func validateAcquisition(category string, acquisition Acquisition) error {
	if (category == SourceCommerciallyLicensed || category == SourcePrivateThirdParty) && strings.TrimSpace(acquisition.Basis) == "" {
		return fmt.Errorf("category %q requires acquisition basis", category)
	}
	if category == SourceWebCrawl {
		if acquisition.Crawler == nil {
			return fmt.Errorf("web crawl requires crawler details")
		}
		crawler := acquisition.Crawler
		if strings.TrimSpace(crawler.Name) == "" || strings.TrimSpace(crawler.Purpose) == "" || strings.TrimSpace(crawler.Behaviour) == "" {
			return fmt.Errorf("crawler requires name, purpose, and behaviour")
		}
		if err := validateStrings("crawler protocols", crawler.Protocols, true); err != nil {
			return err
		}
		if len(crawler.Protocols) == 0 {
			return fmt.Errorf("crawler requires honoured access or opt-out protocols")
		}
		if len(acquisition.Domains) == 0 {
			return fmt.Errorf("web crawl requires acquired domain measures")
		}
	} else if acquisition.Crawler != nil || len(acquisition.Domains) > 0 {
		return fmt.Errorf("crawler and domain facts require category %q", SourceWebCrawl)
	}
	if category == SourceUserData {
		if acquisition.UserData == nil || strings.TrimSpace(acquisition.UserData.Service) == "" || strings.TrimSpace(acquisition.UserData.Interaction) == "" {
			return fmt.Errorf("user data requires service and interaction")
		}
	} else if acquisition.UserData != nil {
		return fmt.Errorf("user data facts require category %q", SourceUserData)
	}
	if category == SourceSynthetic {
		if acquisition.Synthetic == nil || strings.TrimSpace(acquisition.Synthetic.Model) == "" {
			return fmt.Errorf("synthetic data requires generator model identity")
		}
	} else if acquisition.Synthetic != nil {
		return fmt.Errorf("synthetic facts require category %q", SourceSynthetic)
	}
	previous := ""
	for _, domain := range acquisition.Domains {
		if domain.Domain == "" || domain.Domain != strings.ToLower(domain.Domain) || strings.ContainsAny(domain.Domain, "/: ") {
			return fmt.Errorf("invalid normalized domain %q", domain.Domain)
		}
		if domain.Domain <= previous {
			return fmt.Errorf("domains must be sorted and unique")
		}
		if domain.AcquiredBytes < 0 || domain.RetainedBytes < 0 || domain.AcquiredBytes+domain.RetainedBytes == 0 {
			return fmt.Errorf("domain %q has invalid byte measures", domain.Domain)
		}
		if category == SourceWebCrawl && domain.AcquiredBytes == 0 {
			return fmt.Errorf("web crawl domain %q requires acquired bytes", domain.Domain)
		}
		previous = domain.Domain
	}
	return nil
}

func validateProcessing(processing Processing) error {
	seen := map[string]bool{}
	for _, step := range processing.Steps {
		if strings.TrimSpace(step.Name) == "" || strings.TrimSpace(step.Description) == "" {
			return fmt.Errorf("processing steps require name and description")
		}
		if seen[step.Name] {
			return fmt.Errorf("duplicate processing step %q", step.Name)
		}
		seen[step.Name] = true
	}
	if err := validateStrings("rights reservation measures", processing.RightsReservationMeasures, false); err != nil {
		return err
	}
	return validateStrings("illegal content measures", processing.IllegalContentMeasures, false)
}

// ValidateProcessing validates processing declarations preserved in an
// OpenWALDO BOM.
func ValidateProcessing(processing Processing) error {
	return validateProcessing(processing)
}

func validateStrings(label string, values []string, sorted bool) error {
	seen := map[string]bool{}
	for _, value := range values {
		if strings.TrimSpace(value) == "" || seen[value] {
			return fmt.Errorf("%s must contain non-empty unique values", label)
		}
		seen[value] = true
	}
	if sorted && !slices.IsSorted(values) {
		return fmt.Errorf("%s must be sorted", label)
	}
	return nil
}
