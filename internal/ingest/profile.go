package ingest

import (
	"fmt"
	"strings"
)

const (
	ProfileRecordMap              = "record-map"
	ProfileDialoguePair           = "dialogue-pair"
	ProfileRankedConversationTree = "ranked-conversation-tree"
	ProfileGutenbergText          = "gutenberg-text"
	ProfileJATSXML                = "jats-xml"
)

// InputProfile describes how one physical input record becomes canonical
// text. The container remains a separately detected fact: JSON is one object,
// JSONL is one object per line, and Parquet is one row per record.
type InputProfile struct {
	Type   string           `json:"type" yaml:"type"`
	Fields ProfileFields    `json:"fields,omitempty" yaml:"fields,omitempty"`
	Tree   ConversationTree `json:"tree,omitempty" yaml:"tree,omitempty"`
}

type ProfileFields struct {
	Text     []string `json:"text,omitempty" yaml:"text,omitempty"`
	ID       string   `json:"id,omitempty" yaml:"id,omitempty"`
	Date     string   `json:"date,omitempty" yaml:"date,omitempty"`
	Language string   `json:"language,omitempty" yaml:"language,omitempty"`
	License  string   `json:"license,omitempty" yaml:"license,omitempty"`
	Context  string   `json:"context,omitempty" yaml:"context,omitempty"`
	Response string   `json:"response,omitempty" yaml:"response,omitempty"`
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
		if !profile.Fields.empty() || profile.Tree != (ConversationTree{}) {
			return fmt.Errorf("input profile fields require a type")
		}
		return nil
	case ProfileRecordMap:
		if len(profile.Fields.Text) == 0 {
			return fmt.Errorf("record-map requires fields.text")
		}
		if profile.Fields.Context != "" || profile.Fields.Response != "" || profile.Tree != (ConversationTree{}) {
			return fmt.Errorf("record-map accepts text, id, date, language, and license fields only")
		}
	case ProfileDialoguePair:
		if len(profile.Fields.Text) == 0 || profile.Fields.Response == "" {
			return fmt.Errorf("dialogue-pair requires fields.text and fields.response")
		}
		if profile.Tree != (ConversationTree{}) {
			return fmt.Errorf("dialogue-pair does not accept tree fields")
		}
	case ProfileRankedConversationTree:
		if profile.Tree.Replies == "" || profile.Tree.Text == "" || profile.Tree.Rank == "" {
			return fmt.Errorf("ranked-conversation-tree requires tree.replies, tree.text, and tree.rank")
		}
		if len(profile.Fields.Text) > 0 || profile.Fields.Context != "" || profile.Fields.Response != "" {
			return fmt.Errorf("ranked-conversation-tree text comes from the tree mapping")
		}
	case ProfileGutenbergText, ProfileJATSXML:
		if !profile.Fields.empty() || profile.Tree != (ConversationTree{}) {
			return fmt.Errorf("%s does not accept record fields", profile.Type)
		}
	default:
		return fmt.Errorf("unsupported input profile %q", profile.Type)
	}
	for _, path := range profile.paths() {
		if err := validateFieldPath(path); err != nil {
			return err
		}
	}
	return nil
}

func (fields ProfileFields) empty() bool {
	return len(fields.Text) == 0 && fields.ID == "" && fields.Date == "" && fields.Language == "" &&
		fields.License == "" && fields.Context == "" && fields.Response == ""
}

func (profile InputProfile) paths() []string {
	paths := append([]string(nil), profile.Fields.Text...)
	paths = append(paths, profile.Fields.ID, profile.Fields.Date, profile.Fields.Language,
		profile.Fields.License, profile.Fields.Context, profile.Fields.Response,
		profile.Tree.Root, profile.Tree.Replies, profile.Tree.Text, profile.Tree.Rank,
		profile.Tree.Role)
	return paths
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
