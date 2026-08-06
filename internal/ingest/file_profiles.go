package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/openwaldo/waldo/internal/record"
	"github.com/openwaldo/waldo/internal/shard"
)

func StreamProfiledFileBatches(ctx context.Context, plan Plan, consume func(TextBatch) error) error {
	for _, input := range plan.Inputs {
		file, verified, err := openVerifiedInput(ctx, input.Artifact)
		if err != nil {
			return err
		}
		limited := io.LimitReader(&contextReader{ctx: ctx, reader: file}, plan.Writer.RecordMaximumBytes+1)
		data, err := io.ReadAll(limited)
		if err == nil && int64(len(data)) > plan.Writer.RecordMaximumBytes {
			err = fmt.Errorf("input exceeds the %d-byte maximum record size", plan.Writer.RecordMaximumBytes)
		}
		if err == nil {
			err = unchangedInput(file, verified)
		}
		closeErr := file.Close()
		if err != nil {
			return fmt.Errorf("adapt %s: %w", input.Artifact.Path, err)
		}
		if closeErr != nil {
			return closeErr
		}
		var row shard.TextRow
		switch input.Profile.Type {
		case ProfileBoundedText:
			row, err = mapBoundedText(plan, input, data)
		case ProfileXMLRecord:
			row, err = mapXMLRecord(plan, input, data)
		default:
			err = fmt.Errorf("unsupported whole-file profile %q", input.Profile.Type)
		}
		if err != nil {
			return fmt.Errorf("adapt %s: %w", input.Artifact.Path, err)
		}
		if err := consume(TextBatch{Rows: []shard.TextRow{row}, LogicalBytes: int64(len(row.Text))}); err != nil {
			return err
		}
	}
	return nil
}

func mapBoundedText(plan Plan, input PlanInput, data []byte) (shard.TextRow, error) {
	startPattern := regexp.MustCompile(input.Profile.Bounds.StartPattern)
	endPattern := regexp.MustCompile(input.Profile.Bounds.EndPattern)
	start := startPattern.FindIndex(data)
	if start == nil {
		return shard.TextRow{}, fmt.Errorf("start boundary did not match")
	}
	endRelative := endPattern.FindIndex(data[start[1]:])
	if endRelative == nil {
		return shard.TextRow{}, fmt.Errorf("end boundary did not match after the start boundary")
	}
	text := strings.TrimSpace(string(data[start[1]:start[1]+endRelative[0]])) + "\n"
	return profiledFileRow(plan, input, text, "", "", "", "", nil)
}

type xmlNode struct {
	Name     xml.Name
	Attrs    map[xml.Name]string
	Text     strings.Builder
	Children []*xmlNode
}

func mapXMLRecord(plan Plan, input PlanInput, data []byte) (shard.TextRow, error) {
	root, err := parseXMLTree(strings.NewReader(string(data)))
	if err != nil {
		return shard.TextRow{}, err
	}
	profile := input.Profile
	parts := make([]string, 0, len(profile.Fields.Text))
	for _, selector := range profile.Fields.Text {
		values := xmlSelectorValues(root, selector, profile.XML.Exclude)
		if len(values) > 0 {
			parts = append(parts, strings.Join(values, "\n\n"))
		}
	}
	if len(parts) == 0 {
		return shard.TextRow{}, fmt.Errorf("mapped XML text fields are empty or absent")
	}
	scalar := func(selector string) (string, error) {
		if selector == "" {
			return "", nil
		}
		values := xmlSelectorValues(root, selector, profile.XML.Exclude)
		if len(values) > 1 {
			return "", fmt.Errorf("XML selector %q produced multiple scalar values", selector)
		}
		if len(values) == 0 {
			return "", nil
		}
		return values[0], nil
	}
	sourceSelector := profile.Fields.Source
	if sourceSelector == "" {
		sourceSelector = profile.Fields.ID
	}
	source, err := scalar(sourceSelector)
	if err != nil {
		return shard.TextRow{}, err
	}
	if source != "" {
		source = profile.XML.SourcePrefix + source
	}
	date, err := scalar(profile.Fields.Date)
	if err != nil {
		return shard.TextRow{}, err
	}
	language, err := scalar(profile.Fields.Language)
	if err != nil {
		return shard.TextRow{}, err
	}
	license, err := scalar(profile.Fields.License)
	if err != nil {
		return shard.TextRow{}, err
	}
	metadata := map[string]string{}
	for name, selector := range profile.Fields.Meta {
		value, err := scalar(selector)
		if err != nil {
			return shard.TextRow{}, err
		}
		if value != "" {
			metadata[name] = value
		}
	}
	var meta *string
	if len(metadata) > 0 {
		encoded, _ := json.Marshal(metadata)
		value := string(encoded)
		meta = &value
	}
	return profiledFileRow(plan, input, strings.Join(parts, "\n\n")+"\n", source, date, language, license, meta)
}

func parseXMLTree(reader io.Reader) (*xmlNode, error) {
	decoder := xml.NewDecoder(reader)
	decoder.Strict = false
	decoder.Entity = xml.HTMLEntity
	container := &xmlNode{}
	stack := []*xmlNode{container}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch token := token.(type) {
		case xml.StartElement:
			node := &xmlNode{Name: token.Name, Attrs: map[xml.Name]string{}}
			for _, attribute := range token.Attr {
				node.Attrs[attribute.Name] = attribute.Value
			}
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, node)
			stack = append(stack, node)
		case xml.EndElement:
			if len(stack) <= 1 {
				return nil, fmt.Errorf("unexpected XML end element %q", token.Name.Local)
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			stack[len(stack)-1].Text.Write([]byte(token))
		}
	}
	if len(container.Children) != 1 {
		return nil, fmt.Errorf("XML input must contain exactly one root element")
	}
	return container.Children[0], nil
}

type xpathStep struct {
	name       string
	descendant bool
}

type xmlMatch struct {
	node *xmlNode
	path []*xmlNode
}

func xmlSelectorValues(root *xmlNode, selector string, excludes []string) []string {
	steps, attribute := parseXPath(selector)
	var matches []xmlMatch
	var walk func(*xmlNode, []*xmlNode)
	walk = func(node *xmlNode, path []*xmlNode) {
		path = append(path, node)
		if xpathMatches(path, steps) {
			copied := append([]*xmlNode(nil), path...)
			matches = append(matches, xmlMatch{node: node, path: copied})
		}
		for _, child := range node.Children {
			walk(child, path)
		}
	}
	walk(root, nil)
	values := make([]string, 0, len(matches))
	for _, match := range matches {
		var value string
		if attribute != "" {
			for name, candidate := range match.node.Attrs {
				if xmlNameMatches(name, attribute) {
					value = candidate
					break
				}
			}
		} else {
			value = xmlNodeText(match.node, match.path, excludes)
		}
		value = strings.Join(strings.Fields(value), " ")
		if value != "" {
			values = append(values, value)
		}
	}
	return values
}

func xmlNodeText(node *xmlNode, path []*xmlNode, excludes []string) string {
	if xmlExcluded(path, excludes) {
		return ""
	}
	parts := []string{node.Text.String()}
	for _, child := range node.Children {
		parts = append(parts, xmlNodeText(child, append(path, child), excludes))
	}
	return strings.Join(parts, " ")
}

func xmlExcluded(path []*xmlNode, excludes []string) bool {
	for _, exclude := range excludes {
		steps, attribute := parseXPath(exclude)
		if attribute == "" && xpathMatches(path, steps) {
			return true
		}
	}
	return false
}

func parseXPath(selector string) ([]xpathStep, string) {
	parts := strings.Split(strings.TrimPrefix(selector, "/"), "/")
	steps := make([]xpathStep, 0, len(parts))
	descendant := false
	attribute := ""
	for _, part := range parts {
		if part == "" {
			descendant = true
			continue
		}
		if strings.HasPrefix(part, "@") {
			attribute = strings.TrimPrefix(part, "@")
			break
		}
		steps = append(steps, xpathStep{name: part, descendant: descendant})
		descendant = false
	}
	return steps, attribute
}

func xpathMatches(path []*xmlNode, steps []xpathStep) bool {
	var match func(int, int) bool
	match = func(pathPosition, stepPosition int) bool {
		if stepPosition == len(steps) {
			return pathPosition == len(path)
		}
		step := steps[stepPosition]
		if step.descendant {
			for position := pathPosition; position < len(path); position++ {
				if xmlNameMatches(path[position].Name, step.name) && match(position+1, stepPosition+1) {
					return true
				}
			}
			return false
		}
		return pathPosition < len(path) && xmlNameMatches(path[pathPosition].Name, step.name) && match(pathPosition+1, stepPosition+1)
	}
	return match(0, 0)
}

func xmlNameMatches(name xml.Name, selector string) bool {
	if selector == "*" {
		return true
	}
	if strings.HasPrefix(selector, "{") {
		end := strings.IndexByte(selector, '}')
		return end > 1 && name.Space == selector[1:end] && name.Local == selector[end+1:]
	}
	if colon := strings.IndexByte(selector, ':'); colon >= 0 {
		selector = selector[colon+1:]
	}
	return name.Local == selector
}

func profiledFileRow(plan Plan, input PlanInput, text, source, date, language, rawLicense string, meta *string) (shard.TextRow, error) {
	if strings.TrimSpace(text) == "" || !utf8.ValidString(text) || strings.IndexByte(text, 0) >= 0 {
		return shard.TextRow{}, fmt.Errorf("extracted text is not nonempty NUL-free UTF-8")
	}
	if source == "" {
		source = "sha256:" + input.Artifact.SHA256
	}
	license := plan.License
	var licenseRaw *string
	if rawLicense != "" {
		license = record.NormalizeLicense(rawLicense)
		licenseRaw = &rawLicense
	}
	sourceName := plan.Source.Name
	digest := sha256.Sum256([]byte(text))
	return shard.TextRow{
		ContentSHA256: digest, Text: text, Source: source, SourceName: &sourceName,
		License: license, LicenseRaw: licenseRaw, Language: stringPointer(language), Date: stringPointer(date), Meta: meta,
	}, nil
}
