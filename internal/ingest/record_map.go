// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	waldorecord "github.com/openwaldo/waldo/internal/record"
	"github.com/openwaldo/waldo/internal/shard"
	"github.com/parquet-go/parquet-go"
)

var errEmptyMappedRecord = errors.New("required mapped content is empty")
var errLicensePolicy = errors.New("effective license is excluded by input policy")
var errMainContentMapping = errors.New("main_content classification failed")

type recordAccessor interface {
	Values(string) ([]string, error)
}

func StreamMappedRecordBatches(ctx context.Context, plan Plan, consume func(TextBatch) error) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	batch := TextBatch{}
	flush := func() error {
		if len(batch.Rows) == 0 && batch.RejectedDocs == 0 {
			return nil
		}
		if err := consume(batch); err != nil {
			return err
		}
		batch = TextBatch{}
		return nil
	}
	emit := func(row shard.TextRow) error {
		size := int64(len(row.Text))
		if len(batch.Rows) > 0 && batch.LogicalBytes+size > plan.Writer.AdapterBatchBytes {
			if err := flush(); err != nil {
				return err
			}
		}
		batch.Rows = append(batch.Rows, row)
		batch.LogicalBytes += size
		if batch.LogicalBytes >= plan.Writer.AdapterBatchBytes {
			return flush()
		}
		return nil
	}
	reject := func(reason string) error {
		batch.RejectedDocs++
		if batch.Rejections == nil {
			batch.Rejections = make(map[string]int64)
		}
		batch.Rejections[reason]++
		return nil
	}
	for _, input := range plan.Inputs {
		if !input.Profile.recordProfile() {
			return fmt.Errorf("input %s has no record profile", input.Artifact.Path)
		}
		var err error
		switch input.Adapter {
		case "json":
			err = streamJSONObject(ctx, plan, input, emit, reject)
		case "jsonl":
			err = streamMappedJSONL(ctx, plan, input, emit, reject)
		case "parquet":
			err = streamMappedParquet(ctx, plan, input, emit, reject)
		default:
			err = fmt.Errorf("unsupported record container %q", input.Adapter)
		}
		if err != nil {
			return fmt.Errorf("adapt %s: %w", input.Artifact.Path, err)
		}
	}
	return flush()
}

func streamJSONObject(ctx context.Context, plan Plan, input PlanInput, emit func(shard.TextRow) error, reject func(string) error) error {
	file, verified, err := openVerifiedInput(ctx, input.Artifact)
	if err != nil {
		return err
	}
	defer file.Close()
	rejectVerified := func(reason string) error {
		if err := reject(reason); err != nil {
			return err
		}
		return unchangedInput(file, verified)
	}
	decoder := json.NewDecoder(io.LimitReader(&contextReader{ctx: ctx, reader: file}, plan.MemoryBytes/2+1))
	decoder.UseNumber()
	var raw any
	if err := decoder.Decode(&raw); err != nil {
		if plan.RecipeEvidence != nil {
			return rejectVerified(RejectionMalformed)
		}
		return fmt.Errorf("decode JSON object: %w", err)
	}
	object, ok := raw.(map[string]any)
	if !ok {
		if plan.RecipeEvidence != nil {
			return rejectVerified(RejectionMapping)
		}
		if _, array := raw.([]any); array {
			return fmt.Errorf("top-level JSON arrays are not supported; JSON input is exactly one object per file (use a future explicit json-array container)")
		}
		return fmt.Errorf("top-level JSON value must be an object")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if plan.RecipeEvidence != nil {
			return rejectVerified(RejectionMalformed)
		}
		if err == nil {
			return fmt.Errorf("JSON input contains more than one top-level value")
		}
		return err
	}
	row, err := mapJSONCanonicalRecord(object, plan, input, "sha256:"+input.Artifact.SHA256)
	if err != nil {
		if errors.Is(err, errMainContentMapping) {
			return err
		}
		if errors.Is(err, errLicensePolicy) {
			return rejectVerified(RejectionLicense)
		}
		if errors.Is(err, errEmptyMappedRecord) && (input.Profile.OnEmpty == "skip" || plan.RecipeEvidence != nil) {
			if err := reject(RejectionEmpty); err != nil {
				return err
			}
			return unchangedInput(file, verified)
		}
		if plan.RecipeEvidence != nil {
			return rejectVerified(RejectionMapping)
		}
		return err
	}
	if err := emit(row); err != nil {
		return err
	}
	return unchangedInput(file, verified)
}

func streamMappedJSONL(ctx context.Context, plan Plan, input PlanInput, emit func(shard.TextRow) error, reject func(string) error) error {
	file, verified, err := openVerifiedInput(ctx, input.Artifact)
	if err != nil {
		return err
	}
	defer file.Close()
	reader, err := openDecompressed(&contextReader{ctx: ctx, reader: file}, input.Artifact.Compression)
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), int(plan.Writer.RecordMaximumBytes+1<<20))
	line := int64(0)
	candidates := int64(0)
	for scanner.Scan() {
		line++
		data := bytes.TrimSpace(scanner.Bytes())
		if len(data) == 0 {
			continue
		}
		candidates++
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		var raw any
		if err := decoder.Decode(&raw); err != nil {
			if plan.RecipeEvidence != nil {
				if err := reject(RejectionMalformed); err != nil {
					_ = reader.Close()
					return err
				}
				continue
			}
			_ = reader.Close()
			return fmt.Errorf("line %d: %w", line, err)
		}
		object, ok := raw.(map[string]any)
		if !ok {
			if plan.RecipeEvidence != nil {
				if err := reject(RejectionMapping); err != nil {
					_ = reader.Close()
					return err
				}
				continue
			}
			_ = reader.Close()
			return fmt.Errorf("line %d must be one JSON object", line)
		}
		row, err := mapJSONCanonicalRecord(object, plan, input, fmt.Sprintf("sha256:%s#line=%d", input.Artifact.SHA256, line))
		if err != nil {
			if errors.Is(err, errMainContentMapping) {
				_ = reader.Close()
				return fmt.Errorf("line %d: %w", line, err)
			}
			if errors.Is(err, errLicensePolicy) {
				if err := reject(RejectionLicense); err != nil {
					_ = reader.Close()
					return err
				}
				continue
			}
			if errors.Is(err, errEmptyMappedRecord) && (input.Profile.OnEmpty == "skip" || plan.RecipeEvidence != nil) {
				if err := reject(RejectionEmpty); err != nil {
					_ = reader.Close()
					return err
				}
				continue
			}
			if plan.RecipeEvidence != nil {
				if err := reject(RejectionMapping); err != nil {
					_ = reader.Close()
					return err
				}
				continue
			}
			_ = reader.Close()
			return fmt.Errorf("line %d: %w", line, err)
		}
		if err := emit(row); err != nil {
			_ = reader.Close()
			return err
		}
	}
	scanErr := scanner.Err()
	closeErr := reader.Close()
	if scanErr != nil {
		return scanErr
	}
	if closeErr != nil {
		return closeErr
	}
	if candidates == 0 {
		return fmt.Errorf("JSONL input contains no records")
	}
	return unchangedInput(file, verified)
}

func streamMappedParquet(ctx context.Context, plan Plan, input PlanInput, emit func(shard.TextRow) error, reject func(string) error) error {
	file, verified, err := openVerifiedInput(ctx, input.Artifact)
	if err != nil {
		return err
	}
	defer file.Close()
	parquetFile, err := parquet.OpenFile(file, input.Artifact.Bytes)
	if err != nil {
		return err
	}
	compiled, err := compileParquetRecord(parquetFile.Schema(), input.Profile)
	if err != nil {
		return err
	}
	rowGroups := parquetFile.RowGroups()
	if len(rowGroups) == 0 || parquetFile.NumRows() == 0 {
		return fmt.Errorf("Parquet input contains no records")
	}
	var group parquet.RowGroup = rowGroups[0]
	if len(rowGroups) > 1 {
		group = parquet.MultiRowGroup(rowGroups...)
	}
	rows := group.Rows()
	defer rows.Close()
	buffer := make([]parquet.Row, 1)
	position := int64(0)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		count, readErr := rows.ReadRows(buffer)
		if count > 0 {
			position++
			row, err := mapCanonicalRecord(compiled.accessor(buffer[0]), plan, input, fmt.Sprintf("sha256:%s#row=%d", input.Artifact.SHA256, position))
			if err != nil {
				if errors.Is(err, errMainContentMapping) {
					return fmt.Errorf("row %d: %w", position, err)
				}
				if errors.Is(err, errLicensePolicy) {
					if err := reject(RejectionLicense); err != nil {
						return err
					}
					continue
				}
				if errors.Is(err, errEmptyMappedRecord) && (input.Profile.OnEmpty == "skip" || plan.RecipeEvidence != nil) {
					if err := reject(RejectionEmpty); err != nil {
						return err
					}
					continue
				}
				if plan.RecipeEvidence != nil {
					if err := reject(RejectionMapping); err != nil {
						return err
					}
					continue
				}
				return fmt.Errorf("row %d: %w", position, err)
			}
			if err := emit(row); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if position == 0 {
		return fmt.Errorf("Parquet input contains no records")
	}
	return unchangedInput(file, verified)
}

func mapCanonicalRecord(record recordAccessor, plan Plan, input PlanInput, fallbackSource string) (shard.TextRow, error) {
	mapText := func(paths []string) (string, error) {
		parts := make([]string, 0, len(paths))
		for _, path := range paths {
			values, err := record.Values(path)
			if err != nil {
				return "", err
			}
			for _, value := range values {
				if strings.TrimSpace(value) != "" {
					parts = append(parts, value)
				}
			}
		}
		return strings.Join(parts, "\n\n"), nil
	}
	text, err := mapText(input.Profile.Fields.Text)
	if err != nil {
		return shard.TextRow{}, err
	}
	if text == "" && len(input.Profile.Fields.TextFallback) > 0 {
		text, err = mapText(input.Profile.Fields.TextFallback)
		if err != nil {
			return shard.TextRow{}, err
		}
	}
	if text == "" {
		return shard.TextRow{}, fmt.Errorf("%w: mapped text fields are empty or absent", errEmptyMappedRecord)
	}
	var meta *string
	if input.Profile.Type == ProfileDialoguePair {
		contextText, err := optionalScalar(record, input.Profile.Fields.Context)
		if err != nil {
			return shard.TextRow{}, err
		}
		if strings.TrimSpace(contextText) != "" {
			text += "\n\n" + contextText
		}
		response, err := optionalScalar(record, input.Profile.Fields.Response)
		if err != nil {
			return shard.TextRow{}, err
		}
		if strings.TrimSpace(response) == "" {
			return shard.TextRow{}, fmt.Errorf("%w: mapped response field is empty or absent", errEmptyMappedRecord)
		}
		text = renderDialogue(text, response)
		meta = dialogueMeta(2)
	} else if input.Profile.Type == ProfileRecordMap {
		mapped, err := mappedRecordMeta(record, input.Profile.Fields.Meta)
		if err != nil {
			return shard.TextRow{}, err
		}
		meta = mapped
	} else {
		return shard.TextRow{}, fmt.Errorf("profile %q cannot be mapped from scalar fields", input.Profile.Type)
	}
	return canonicalMappedRow(record, plan, input, fallbackSource, text, meta)
}

func mappedRecordMeta(record recordAccessor, fields map[string]string) (*string, error) {
	if len(fields) == 0 {
		return nil, nil
	}
	metadata := map[string]any{}
	for name, path := range fields {
		values, err := record.Values(path)
		if err != nil {
			return nil, err
		}
		switch len(values) {
		case 0:
			continue
		case 1:
			metadata[name] = values[0]
		default:
			metadata[name] = values
		}
	}
	if len(metadata) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	value := string(encoded)
	return &value, nil
}

func mapJSONCanonicalRecord(object map[string]any, plan Plan, input PlanInput, fallbackSource string) (shard.TextRow, error) {
	if input.Profile.Type != ProfileRankedConversationTree {
		return mapCanonicalRecord(jsonRecord{object}, plan, input, fallbackSource)
	}
	text, turns, err := renderRankedTree(object, input.Profile.Tree)
	if err != nil {
		return shard.TextRow{}, err
	}
	return canonicalMappedRow(jsonRecord{object}, plan, input, fallbackSource, text, dialogueMeta(turns))
}

func canonicalMappedRow(record recordAccessor, plan Plan, input PlanInput, fallbackSource, text string, meta *string) (shard.TextRow, error) {
	if input.Profile.NUL == "space" {
		text = strings.ReplaceAll(text, "\x00", " ")
	}
	if int64(len(text)) > plan.Writer.RecordMaximumBytes || !utf8.ValidString(text) || strings.IndexByte(text, 0) >= 0 {
		return shard.TextRow{}, fmt.Errorf("mapped text is not bounded NUL-free UTF-8")
	}
	sourcePath := input.Profile.Fields.Source
	if sourcePath == "" {
		sourcePath = input.Profile.Fields.ID
	}
	source, err := optionalScalar(record, sourcePath)
	if err != nil {
		return shard.TextRow{}, err
	}
	if source == "" {
		source = fallbackSource
	}
	language, err := optionalScalar(record, input.Profile.Fields.Language)
	if err != nil {
		return shard.TextRow{}, err
	}
	date, err := optionalScalar(record, input.Profile.Fields.Date)
	if err != nil {
		return shard.TextRow{}, err
	}
	license, rawLicense, err := optionalLicense(record, input.Profile.Fields.License)
	if err != nil {
		return shard.TextRow{}, err
	}
	projectSource, effective, err := plan.sourceFor(input)
	if err != nil {
		return shard.TextRow{}, err
	}
	if license != "" {
		effective = license
	}
	if !input.Profile.LicensePolicy.Allows(effective) {
		return shard.TextRow{}, fmt.Errorf("%w: %s", errLicensePolicy, effective)
	}
	mainContent, err := mappedMainContent(record, input.Profile.MainContent)
	if err != nil {
		return shard.TextRow{}, err
	}
	sourceName := projectSource.Name
	hash := sha256.Sum256([]byte(text))
	return shard.TextRow{
		ContentSHA256: hash, Text: text, Source: source, SourceName: &sourceName,
		License: effective, LicenseRaw: rawLicense, Language: stringPointer(language), Date: stringPointer(date), Meta: meta, MainContent: mainContent,
	}, nil
}

func mappedMainContent(record recordAccessor, condition map[string]any) (bool, error) {
	if len(condition) == 0 {
		return true, nil
	}
	for path, raw := range condition {
		expected, _ := mainContentScalar(raw)
		values, err := record.Values(path)
		if err != nil {
			return false, fmt.Errorf("%w: %v", errMainContentMapping, err)
		}
		if len(values) == 0 {
			return false, fmt.Errorf("%w: field %q is absent", errMainContentMapping, path)
		}
		if len(values) != 1 {
			return false, fmt.Errorf("%w: field %q must be scalar", errMainContentMapping, path)
		}
		return values[0] == expected, nil
	}
	return true, nil
}

func mainContentScalar(value any) (string, bool) {
	switch value := value.(type) {
	case string:
		return value, true
	case bool:
		return strconv.FormatBool(value), true
	case json.Number:
		return value.String(), true
	case int:
		return strconv.Itoa(value), true
	case int64:
		return strconv.FormatInt(value, 10), true
	case uint64:
		return strconv.FormatUint(value, 10), true
	case float64:
		return strconv.FormatFloat(value, 'g', -1, 64), true
	default:
		return "", false
	}
}

func optionalLicense(record recordAccessor, path string) (string, *string, error) {
	if path == "" {
		return "", nil, nil
	}
	values, err := record.Values(path)
	if err != nil {
		return "", nil, err
	}
	if len(values) == 0 {
		return "", nil, nil
	}
	effective := waldorecord.NormalizeLicenseSet(values)
	if effective == "" {
		return "", nil, nil
	}
	raw := values[0]
	if len(values) > 1 {
		encoded, err := json.Marshal(values)
		if err != nil {
			return "", nil, err
		}
		raw = string(encoded)
	}
	return effective, &raw, nil
}

func renderDialogue(user, assistant string) string {
	return "User: " + user + "\n\nAssistant: " + assistant + "\n"
}

func dialogueMeta(turns int) *string {
	value := fmt.Sprintf(`{"format":"dialogue-flattened","turns":%d}`, turns)
	return &value
}

func renderRankedTree(object map[string]any, tree ConversationTree) (string, int, error) {
	var current any = object
	if tree.Root != "" {
		value, err := jsonPathValue(object, tree.Root)
		if err != nil {
			return "", 0, err
		}
		current = value
	}
	var output strings.Builder
	turns := 0
	for current != nil {
		node, ok := current.(map[string]any)
		if !ok {
			return "", 0, fmt.Errorf("conversation node %d is not an object", turns+1)
		}
		bodyValues, err := (jsonRecord{node}).Values(tree.Text)
		if err != nil || len(bodyValues) != 1 || strings.TrimSpace(bodyValues[0]) == "" {
			return "", 0, fmt.Errorf("conversation node %d has no scalar text", turns+1)
		}
		assistant := turns%2 == 1
		if tree.Role != "" && tree.AssistantRole != "" {
			role, err := optionalScalar(jsonRecord{node}, tree.Role)
			if err != nil {
				return "", 0, err
			}
			if (role == tree.AssistantRole) != assistant {
				return "", 0, fmt.Errorf("conversation node %d role %q does not alternate as expected", turns+1, role)
			}
		}
		label := "User"
		if assistant {
			label = "Assistant"
		}
		fmt.Fprintf(&output, "%s: %s\n\n", label, strings.TrimSpace(bodyValues[0]))
		turns++
		repliesValue, exists, err := optionalJSONPathValue(node, tree.Replies)
		if err != nil {
			return "", 0, err
		}
		if !exists || repliesValue == nil {
			break
		}
		replies, ok := repliesValue.([]any)
		if !ok {
			return "", 0, fmt.Errorf("conversation node %d replies are not an array", turns)
		}
		if len(replies) == 0 {
			break
		}
		best := -1
		bestRank := int64(0)
		hasRankedBest := false
		for position, reply := range replies {
			child, ok := reply.(map[string]any)
			if !ok {
				return "", 0, fmt.Errorf("conversation reply %d is not an object", position+1)
			}
			rankText, err := optionalScalar(jsonRecord{child}, tree.Rank)
			if err != nil {
				return "", 0, fmt.Errorf("conversation reply %d has no scalar rank", position+1)
			}
			if rankText == "" {
				if tree.MissingRank != "source-order" {
					return "", 0, fmt.Errorf("conversation reply %d has no scalar rank", position+1)
				}
				if best < 0 {
					best = position
				}
				continue
			}
			rank, err := strconv.ParseInt(rankText, 10, 64)
			if err != nil {
				return "", 0, fmt.Errorf("conversation reply %d rank %q is not an integer", position+1, rankText)
			}
			if !hasRankedBest || rank < bestRank {
				best, bestRank = position, rank
				hasRankedBest = true
			}
		}
		current = replies[best]
	}
	if turns == 0 {
		return "", 0, fmt.Errorf("conversation tree contains no turns")
	}
	return strings.TrimRight(output.String(), "\n") + "\n", turns, nil
}

func jsonPathValue(object map[string]any, path string) (any, error) {
	value, exists, err := optionalJSONPathValue(object, path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("field %q is absent", path)
	}
	return value, nil
}

func optionalJSONPathValue(object map[string]any, path string) (any, bool, error) {
	var current any = object
	for _, segment := range strings.Split(path, ".") {
		next, ok := current.(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("field %q traverses a non-object", path)
		}
		value, exists := next[segment]
		if !exists {
			return nil, false, nil
		}
		current = value
	}
	return current, true, nil
}

func optionalScalar(record recordAccessor, path string) (string, error) {
	if path == "" {
		return "", nil
	}
	values, err := record.Values(path)
	if err != nil {
		return "", err
	}
	if len(values) > 1 {
		return "", fmt.Errorf("field %q expands to multiple values but must be scalar", path)
	}
	if len(values) == 1 {
		return values[0], nil
	}
	return "", nil
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

type jsonRecord struct{ value map[string]any }

func (record jsonRecord) Values(path string) ([]string, error) {
	segments := strings.Split(path, ".")
	values := []any{record.value}
	for _, raw := range segments {
		expand := strings.HasSuffix(raw, "[]")
		name := strings.TrimSuffix(raw, "[]")
		next := []any{}
		for _, value := range values {
			object, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("field %q traverses a non-object", path)
			}
			child, exists := object[name]
			if !exists || child == nil {
				continue
			}
			if expand {
				array, ok := child.([]any)
				if !ok {
					return nil, fmt.Errorf("field %q declares [] but value is not an array", path)
				}
				next = append(next, array...)
			} else {
				next = append(next, child)
			}
		}
		values = next
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		scalar, ok := jsonScalar(value)
		if !ok {
			return nil, fmt.Errorf("field %q resolves to a non-scalar value", path)
		}
		result = append(result, scalar)
	}
	return result, nil
}

func jsonScalar(value any) (string, bool) {
	switch value := value.(type) {
	case string:
		return value, true
	case json.Number:
		return value.String(), true
	case bool:
		return strconv.FormatBool(value), true
	case nil:
		return "", true
	default:
		return "", false
	}
}

type parquetField struct {
	path     string
	column   int
	repeated bool
}

type parquetRecord struct{ fields map[string]parquetField }

func compileParquetRecord(schema *parquet.Schema, profile InputProfile) (parquetRecord, error) {
	result := parquetRecord{fields: map[string]parquetField{}}
	for _, path := range profile.paths() {
		if path == "" {
			continue
		}
		clean := strings.ReplaceAll(path, "[]", "")
		leaf, ok := schema.Lookup(strings.Split(clean, ".")...)
		if !ok {
			return parquetRecord{}, fmt.Errorf("mapped field %q is absent or non-scalar", path)
		}
		repeated := strings.Contains(path, "[]")
		if repeated != (leaf.MaxRepetitionLevel > 0) {
			return parquetRecord{}, fmt.Errorf("mapped field %q repetition does not match its [] declaration", path)
		}
		result.fields[path] = parquetField{path: path, column: leaf.ColumnIndex, repeated: repeated}
	}
	return result, nil
}

func (record parquetRecord) accessor(row parquet.Row) recordAccessor {
	values := map[string][]string{}
	row.Range(func(column int, columnValues []parquet.Value) bool {
		for path, field := range record.fields {
			if field.column != column {
				continue
			}
			for _, value := range columnValues {
				if !value.IsNull() {
					values[path] = append(values[path], parquetScalar(value))
				}
			}
		}
		return true
	})
	return parquetValues(values)
}

type parquetValues map[string][]string

func (values parquetValues) Values(path string) ([]string, error) {
	return values[path], nil
}

func parquetScalar(value parquet.Value) string {
	switch value.Kind() {
	case parquet.Boolean:
		return strconv.FormatBool(value.Boolean())
	case parquet.Int32:
		return strconv.FormatInt(int64(value.Int32()), 10)
	case parquet.Int64:
		return strconv.FormatInt(value.Int64(), 10)
	case parquet.Float:
		return strconv.FormatFloat(float64(value.Float()), 'g', -1, 32)
	case parquet.Double:
		return strconv.FormatFloat(value.Double(), 'g', -1, 64)
	case parquet.ByteArray, parquet.FixedLenByteArray:
		return string(value.ByteArray())
	default:
		return value.String()
	}
}

func openVerifiedInput(ctx context.Context, artifact Artifact) (*os.File, os.FileInfo, error) {
	file, err := os.Open(artifact.Path)
	if err != nil {
		return nil, nil, err
	}
	verified, err := verifyPlannedArtifact(ctx, file, artifact)
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		return nil, nil, err
	}
	return file, verified, nil
}

func unchangedInput(file *os.File, verified os.FileInfo) error {
	after, err := file.Stat()
	if err != nil {
		return err
	}
	if after.Size() != verified.Size() || !after.ModTime().Equal(verified.ModTime()) {
		return fmt.Errorf("artifact changed while it was being converted")
	}
	return nil
}
