package ingest

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const inputProfileMaximum = 1 << 20

const (
	ProfileRecordMap              = "record-map"
	ProfileDialoguePair           = "dialogue-pair"
	ProfileRankedConversationTree = "ranked-conversation-tree"
	ProfileBoundedText            = "bounded-text"
	ProfileXMLRecord              = "xml-record"
)

// InputProfile describes how one physical input record becomes canonical
// text. The container remains a separately detected fact: JSON is one object,
// JSONL is one object per line, and Parquet is one row per record.
type InputProfile struct {
	Type    string           `json:"type" yaml:"type"`
	OnEmpty string           `json:"on_empty,omitempty" yaml:"on_empty,omitempty"`
	Fields  ProfileFields    `json:"fields,omitempty" yaml:"fields,omitempty"`
	Tree    ConversationTree `json:"tree,omitempty" yaml:"tree,omitempty"`
	Bounds  TextBounds       `json:"bounds,omitempty" yaml:"bounds,omitempty"`
	XML     XMLMapping       `json:"xml,omitempty" yaml:"xml,omitempty"`
}

type ProfileFields struct {
	Text     []string          `json:"text,omitempty" yaml:"text,omitempty"`
	ID       string            `json:"id,omitempty" yaml:"id,omitempty"`
	Date     string            `json:"date,omitempty" yaml:"date,omitempty"`
	Language string            `json:"language,omitempty" yaml:"language,omitempty"`
	License  string            `json:"license,omitempty" yaml:"license,omitempty"`
	Source   string            `json:"source,omitempty" yaml:"source,omitempty"`
	Context  string            `json:"context,omitempty" yaml:"context,omitempty"`
	Response string            `json:"response,omitempty" yaml:"response,omitempty"`
	Meta     map[string]string `json:"meta,omitempty" yaml:"meta,omitempty"`
}

type TextBounds struct {
	StartPattern string `json:"start_pattern,omitempty" yaml:"start_pattern,omitempty"`
	EndPattern   string `json:"end_pattern,omitempty" yaml:"end_pattern,omitempty"`
}

type XMLMapping struct {
	Exclude      []string `json:"exclude,omitempty" yaml:"exclude,omitempty"`
	SourcePrefix string   `json:"source_prefix,omitempty" yaml:"source_prefix,omitempty"`
}

// LoadInputProfile reads one strict standalone YAML or JSON profile for direct
// local ingestion. Recipes embed the same InputProfile shape under `input`.
func LoadInputProfile(path string) (InputProfile, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return InputProfile{}, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > inputProfileMaximum {
		return InputProfile{}, fmt.Errorf("input profile must be a regular non-symlink file no larger than %d bytes", inputProfileMaximum)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return InputProfile{}, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var profile InputProfile
	if err := decoder.Decode(&profile); err != nil {
		return InputProfile{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple YAML documents are not allowed")
		}
		return InputProfile{}, err
	}
	if err := profile.Validate(); err != nil {
		return InputProfile{}, err
	}
	if profile.Type == "" {
		return InputProfile{}, fmt.Errorf("input profile type is required")
	}
	return profile, nil
}

type ConversationTree struct {
	Root          string `json:"root,omitempty" yaml:"root,omitempty"`
	Replies       string `json:"replies,omitempty" yaml:"replies,omitempty"`
	Text          string `json:"text,omitempty" yaml:"text,omitempty"`
	Rank          string `json:"rank,omitempty" yaml:"rank,omitempty"`
	Role          string `json:"role,omitempty" yaml:"role,omitempty"`
	AssistantRole string `json:"assistant_role,omitempty" yaml:"assistant_role,omitempty"`
}

func (profile InputProfile) Validate() error {
	if profile.OnEmpty != "" && profile.OnEmpty != "error" && profile.OnEmpty != "skip" {
		return fmt.Errorf("on_empty must be error or skip")
	}
	if profile.OnEmpty != "" && profile.Type != ProfileRecordMap && profile.Type != ProfileDialoguePair {
		return fmt.Errorf("on_empty is supported only for record-map and dialogue-pair")
	}
	switch profile.Type {
	case "":
		if profile.OnEmpty != "" || !profile.Fields.empty() || profile.Tree != (ConversationTree{}) || profile.Bounds != (TextBounds{}) || !profile.XML.empty() {
			return fmt.Errorf("input profile fields require a type")
		}
		return nil
	case ProfileRecordMap:
		if len(profile.Fields.Text) == 0 {
			return fmt.Errorf("record-map requires fields.text")
		}
		if profile.Fields.Context != "" || profile.Fields.Response != "" || profile.Fields.Source != "" || len(profile.Fields.Meta) > 0 || profile.Tree != (ConversationTree{}) || profile.Bounds != (TextBounds{}) || !profile.XML.empty() {
			return fmt.Errorf("record-map accepts text, id, date, language, and license fields only")
		}
	case ProfileDialoguePair:
		if len(profile.Fields.Text) == 0 || profile.Fields.Response == "" {
			return fmt.Errorf("dialogue-pair requires fields.text and fields.response")
		}
		if profile.Fields.Source != "" || len(profile.Fields.Meta) > 0 || profile.Tree != (ConversationTree{}) || profile.Bounds != (TextBounds{}) || !profile.XML.empty() {
			return fmt.Errorf("dialogue-pair does not accept tree fields")
		}
	case ProfileRankedConversationTree:
		if profile.Tree.Replies == "" || profile.Tree.Text == "" || profile.Tree.Rank == "" {
			return fmt.Errorf("ranked-conversation-tree requires tree.replies, tree.text, and tree.rank")
		}
		if len(profile.Fields.Text) > 0 || profile.Fields.Context != "" || profile.Fields.Response != "" || profile.Fields.Source != "" || len(profile.Fields.Meta) > 0 || profile.Bounds != (TextBounds{}) || !profile.XML.empty() {
			return fmt.Errorf("ranked-conversation-tree text comes from the tree mapping")
		}
	case ProfileBoundedText:
		if profile.Bounds.StartPattern == "" || profile.Bounds.EndPattern == "" {
			return fmt.Errorf("bounded-text requires bounds.start_pattern and bounds.end_pattern")
		}
		if _, err := regexp.Compile(profile.Bounds.StartPattern); err != nil {
			return fmt.Errorf("invalid bounds.start_pattern: %w", err)
		}
		if _, err := regexp.Compile(profile.Bounds.EndPattern); err != nil {
			return fmt.Errorf("invalid bounds.end_pattern: %w", err)
		}
		if !profile.Fields.empty() || profile.Tree != (ConversationTree{}) || !profile.XML.empty() {
			return fmt.Errorf("bounded-text accepts bounds only")
		}
	case ProfileXMLRecord:
		if len(profile.Fields.Text) == 0 {
			return fmt.Errorf("xml-record requires fields.text")
		}
		if profile.Fields.Context != "" || profile.Fields.Response != "" || profile.Tree != (ConversationTree{}) || profile.Bounds != (TextBounds{}) {
			return fmt.Errorf("xml-record accepts text, id, date, language, license, source, and meta fields only")
		}
		for _, selector := range profile.xmlSelectors() {
			if err := validateXMLSelector(selector); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported input profile %q", profile.Type)
	}
	if profile.Type != ProfileXMLRecord {
		for _, path := range profile.paths() {
			if err := validateFieldPath(path); err != nil {
				return err
			}
		}
	}
	return nil
}

func (fields ProfileFields) empty() bool {
	return len(fields.Text) == 0 && fields.ID == "" && fields.Date == "" && fields.Language == "" &&
		fields.License == "" && fields.Source == "" && fields.Context == "" && fields.Response == "" && len(fields.Meta) == 0
}

func (mapping XMLMapping) empty() bool {
	return len(mapping.Exclude) == 0 && mapping.SourcePrefix == ""
}

func (profile InputProfile) paths() []string {
	paths := append([]string(nil), profile.Fields.Text...)
	paths = append(paths, profile.Fields.ID, profile.Fields.Date, profile.Fields.Language,
		profile.Fields.License, profile.Fields.Context, profile.Fields.Response,
		profile.Tree.Root, profile.Tree.Replies, profile.Tree.Text, profile.Tree.Rank,
		profile.Tree.Role)
	return paths
}

func (profile InputProfile) xmlSelectors() []string {
	selectors := append([]string(nil), profile.Fields.Text...)
	selectors = append(selectors, profile.Fields.ID, profile.Fields.Date, profile.Fields.Language,
		profile.Fields.License, profile.Fields.Source)
	for _, selector := range profile.Fields.Meta {
		selectors = append(selectors, selector)
	}
	selectors = append(selectors, profile.XML.Exclude...)
	return selectors
}

func validateXMLSelector(selector string) error {
	if selector == "" {
		return nil
	}
	if !strings.HasPrefix(selector, "/") || selector == "/" || strings.HasSuffix(selector, "/") {
		return fmt.Errorf("XML selector %q must be an absolute XPath", selector)
	}
	parts, err := splitXPath(selector)
	if err != nil {
		return err
	}
	for position, part := range parts {
		if part == "" { // The empty segment in // is the descendant axis.
			if position == len(parts)-1 || (position > 0 && parts[position-1] == "") {
				return fmt.Errorf("invalid XML selector %q", selector)
			}
			continue
		}
		if strings.HasPrefix(part, "@") {
			if position != len(parts)-1 || !validXMLName(strings.TrimPrefix(part, "@")) {
				return fmt.Errorf("invalid XML selector %q", selector)
			}
			continue
		}
		if !validXMLName(part) {
			return fmt.Errorf("invalid XML selector %q", selector)
		}
	}
	return nil
}

var xmlQName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*(?::[A-Za-z_][A-Za-z0-9_.-]*)?$`)

func validXMLName(value string) bool {
	if value == "*" {
		return true
	}
	if strings.HasPrefix(value, "{") {
		end := strings.LastIndexByte(value, '}')
		return end > 1 && end < len(value)-1 && xmlQName.MatchString(value[end+1:])
	}
	return xmlQName.MatchString(value)
}

func splitXPath(selector string) ([]string, error) {
	var parts []string
	start := 1
	braces := 0
	for position := 1; position < len(selector); position++ {
		switch selector[position] {
		case '{':
			braces++
		case '}':
			braces--
			if braces < 0 {
				return nil, fmt.Errorf("invalid XML selector %q", selector)
			}
		case '/':
			if braces == 0 {
				parts = append(parts, selector[start:position])
				start = position + 1
			}
		}
	}
	if braces != 0 {
		return nil, fmt.Errorf("invalid XML selector %q", selector)
	}
	return append(parts, selector[start:]), nil
}

func validateFieldPath(path string) error {
	if path == "" {
		return nil
	}
	for _, segment := range strings.Split(path, ".") {
		name := strings.TrimSuffix(segment, "[]")
		if name == "" || strings.ContainsAny(name, "[]") {
			return fmt.Errorf("invalid declarative field path %q", path)
		}
	}
	return nil
}

func (profile InputProfile) recordProfile() bool {
	return profile.Type == ProfileRecordMap || profile.Type == ProfileDialoguePair || profile.Type == ProfileRankedConversationTree
}
