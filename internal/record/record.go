// Package record defines the OpenWALDO schema-1 interchange record.
package record

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"unicode/utf8"

	"github.com/openwaldo/waldo-new/internal/canon"
)

const (
	KindPretrain   = "pretrain"
	LangScoreScale = 1000
)

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
	if r.Kind != KindPretrain {
		return fmt.Errorf("record kind %q: schema 1 defines only %q", r.Kind, KindPretrain)
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
