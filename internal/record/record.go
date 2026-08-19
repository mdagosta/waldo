// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

// Package record defines the OpenWALDO schema-1 interchange record.
package record

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/openwaldo/waldo/internal/canon"
)

const (
	Schema           = 1
	KindPretrain     = "pretrain"
	KindConversation = "conversation"
	LangScoreScale   = 1000
)

// Message is one normalized turn in a tokenizer-neutral conversation.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Context string `json:"context,omitempty"`
}

// Conversation is the canonical logical payload stored by conversation
// shards. Tools is canonical JSON from the source when present; it remains
// structured and is interpreted only by a selected training template.
type Conversation struct {
	Messages []Message       `json:"messages"`
	Tools    json.RawMessage `json:"tools,omitempty"`
}

func (conversation Conversation) Validate() error {
	if len(conversation.Messages) == 0 {
		return errors.New("conversation messages: required")
	}
	hasUser, hasAssistant := false, false
	for position, message := range conversation.Messages {
		if message.Role != "system" && message.Role != "user" && message.Role != "assistant" && message.Role != "tool" {
			return fmt.Errorf("conversation message %d has unsupported role %q", position+1, message.Role)
		}
		if message.Content == "" || !utf8.ValidString(message.Content) {
			return fmt.Errorf("conversation message %d content must be non-empty UTF-8", position+1)
		}
		if !utf8.ValidString(message.Context) {
			return fmt.Errorf("conversation message %d context must be UTF-8", position+1)
		}
		hasUser = hasUser || message.Role == "user"
		hasAssistant = hasAssistant || message.Role == "assistant"
	}
	if !hasUser || !hasAssistant {
		return errors.New("conversation requires at least one user and assistant message")
	}
	if len(conversation.Tools) > 0 && !json.Valid(conversation.Tools) {
		return errors.New("conversation tools must be valid JSON")
	}
	return nil
}

func EncodeConversation(conversation Conversation) (string, error) {
	if err := conversation.Validate(); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(conversation)
	return string(encoded), err
}

func DecodeConversation(encoded string) (Conversation, error) {
	decoder := json.NewDecoder(strings.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var conversation Conversation
	if err := decoder.Decode(&conversation); err != nil {
		return Conversation{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			err = errors.New("conversation payload has trailing JSON")
		}
		return Conversation{}, err
	}
	if err := conversation.Validate(); err != nil {
		return Conversation{}, err
	}
	canonical, err := EncodeConversation(conversation)
	if err != nil {
		return Conversation{}, err
	}
	if canonical != encoded {
		return Conversation{}, errors.New("conversation payload is not canonical JSON")
	}
	return conversation, nil
}

type Record struct {
	SHA256     string
	Kind       string
	Text       string
	Source     string
	SourceName string
	License    string
	LicenseRaw string
	Lang       string
	LangScore  int64
	Date       string
	Tokens     int64
}

func TextHash(text string) string {
	hash := sha256.Sum256([]byte(text))
	return hex.EncodeToString(hash[:])
}

func (r Record) Validate() error {
	if len(r.SHA256) != 64 || !lowerHex(r.SHA256) {
		return fmt.Errorf("record sha256 %q: must be 64 lowercase hex characters", r.SHA256)
	}
	if r.Text == "" {
		return errors.New("record text: required")
	}
	if TextHash(r.Text) != r.SHA256 {
		return fmt.Errorf("record sha256 %s does not match its text", r.SHA256)
	}
	if r.Kind != KindPretrain && r.Kind != KindConversation {
		return fmt.Errorf("unsupported record kind %q", r.Kind)
	}
	if r.Kind == KindConversation {
		if _, err := DecodeConversation(r.Text); err != nil {
			return fmt.Errorf("record conversation: %w", err)
		}
	}
	if r.Source == "" || r.License == "" {
		return errors.New("record source and license are required")
	}
	for name, value := range map[string]string{
		"text": r.Text, "source": r.Source, "source_name": r.SourceName,
		"license": r.License, "license_raw": r.LicenseRaw, "lang": r.Lang, "date": r.Date,
	} {
		if !utf8.ValidString(value) {
			return fmt.Errorf("record %s: invalid UTF-8", name)
		}
	}
	if r.LangScore < 0 || r.LangScore > LangScoreScale {
		return fmt.Errorf("record lang_score %d: must be in 0..%d", r.LangScore, LangScoreScale)
	}
	if r.LangScore != 0 && r.Lang == "" {
		return errors.New("record lang_score is set without lang")
	}
	if r.Tokens < 0 {
		return errors.New("record tokens must not be negative")
	}
	return nil
}

// AppendCanonical appends one newline-terminated canonical JSON record. Meta
// is already canonical JSON in the native shard and is preserved byte-for-byte.
func (r Record) AppendCanonical(dst, meta []byte) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if len(meta) > 0 {
		if !json.Valid(meta) || meta[0] != '{' {
			return nil, errors.New("record meta is not a JSON object")
		}
	}
	dst = append(dst, '{', '"')
	dst = append(dst, `sha256":`...)
	dst = canon.AppendString(dst, r.SHA256)
	dst = append(dst, `,"kind":`...)
	dst = canon.AppendString(dst, r.Kind)
	dst = append(dst, `,"text":`...)
	dst = canon.AppendString(dst, r.Text)
	dst = append(dst, `,"source":`...)
	dst = canon.AppendString(dst, r.Source)
	if r.SourceName != "" {
		dst = append(dst, `,"source_name":`...)
		dst = canon.AppendString(dst, r.SourceName)
	}
	dst = append(dst, `,"license":`...)
	dst = canon.AppendString(dst, r.License)
	if r.LicenseRaw != "" {
		dst = append(dst, `,"license_raw":`...)
		dst = canon.AppendString(dst, r.LicenseRaw)
	}
	if r.Lang != "" {
		dst = append(dst, `,"lang":`...)
		dst = canon.AppendString(dst, r.Lang)
	}
	if r.LangScore != 0 {
		dst = append(dst, `,"lang_score":`...)
		dst = strconv.AppendInt(dst, r.LangScore, 10)
	}
	if r.Date != "" {
		dst = append(dst, `,"date":`...)
		dst = canon.AppendString(dst, r.Date)
	}
	if r.Tokens != 0 {
		dst = append(dst, `,"tokens":`...)
		dst = strconv.AppendInt(dst, r.Tokens, 10)
	}
	if len(meta) > 0 {
		dst = append(dst, `,"meta":`...)
		dst = append(dst, meta...)
	}
	return append(dst, '}', '\n'), nil
}

func lowerHex(value string) bool {
	for i := range len(value) {
		if c := value[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
