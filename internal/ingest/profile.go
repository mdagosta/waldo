package ingest

import (
	"fmt"
	"regexp"
	"strings"
)

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
	Type   string           `json:"type" yaml:"type"`
	Fields ProfileFields    `json:"fields,omitempty" yaml:"fields,omitempty"`
	Tree   ConversationTree `json:"tree,omitempty" yaml:"tree,omitempty"`
	Bounds TextBounds       `json:"bounds,omitempty" yaml:"bounds,omitempty"`
	XML    XMLMapping       `json:"xml,omitempty" yaml:"xml,omitempty"`
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

type ConversationTree struct {
	Root          string `json:"root,omitempty" yaml:"root,omitempty"`
	Replies       string `json:"replies,omitempty" yaml:"replies,omitempty"`
	Text          string `json:"text,omitempty" yaml:"text,omitempty"`
	Rank          string `json:"rank,omitempty" yaml:"rank,omitempty"`
	Role          string `json:"role,omitempty" yaml:"role,omitempty"`
	AssistantRole string `json:"assistant_role,omitempty" yaml:"assistant_role,omitempty"`
}

func (profile InputProfile) Validate() error {
	switch profile.Type {
	case "":
		if !profile.Fields.empty() || profile.Tree != (ConversationTree{}) || profile.Bounds != (TextBounds{}) || !profile.XML.empty() {
			return fmt.Errorf("input profile fields require a type")
		}
		return nil
	case ProfileRecordMap:
		if len(profile.Fields.Text) == 0 {
			return fmt.Errorf("record-map requires fields.text")
		}
		if profile.Fields.Context != "" || profile.Fields.Response != "" || profile.Tree != (ConversationTree{}) || profile.Bounds != (TextBounds{}) || !profile.XML.empty() {
			return fmt.Errorf("record-map accepts text, id, date, language, and license fields only")
		}
	case ProfileDialoguePair:
		if len(profile.Fields.Text) == 0 || profile.Fields.Response == "" {
			return fmt.Errorf("dialogue-pair requires fields.text and fields.response")
		}
		if profile.Tree != (ConversationTree{}) || profile.Bounds != (TextBounds{}) || !profile.XML.empty() {
			return fmt.Errorf("dialogue-pair does not accept tree fields")
		}
	case ProfileRankedConversationTree:
		if profile.Tree.Replies == "" || profile.Tree.Text == "" || profile.Tree.Rank == "" {
			return fmt.Errorf("ranked-conversation-tree requires tree.replies, tree.text, and tree.rank")
		}
		if len(profile.Fields.Text) > 0 || profile.Fields.Context != "" || profile.Fields.Response != "" || profile.Bounds != (TextBounds{}) || !profile.XML.empty() {
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
	parts := strings.Split(strings.TrimPrefix(selector, "/"), "/")
	for position, part := range parts {
		if part == "" { // The empty segment in // is the descendant axis.
			continue
		}
		if strings.HasPrefix(part, "@") {
			if position != len(parts)-1 || len(part) == 1 {
				return fmt.Errorf("invalid XML selector %q", selector)
			}
			continue
		}
		if strings.ContainsAny(part, "[]@") {
			return fmt.Errorf("invalid XML selector %q", selector)
		}
	}
	return nil
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
