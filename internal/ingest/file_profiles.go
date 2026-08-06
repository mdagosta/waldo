package ingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/openwaldo/waldo/internal/shard"
)

var (
	gutenbergStart = regexp.MustCompile(`(?mi)^\*\*\*\s*START OF (?:THE|THIS) PROJECT GUTENBERG EBOOK[^\n]*\*\*\*\s*$`)
	gutenbergEnd   = regexp.MustCompile(`(?mi)^\*\*\*\s*END OF (?:THE|THIS) PROJECT GUTENBERG EBOOK[^\n]*\*\*\*\s*$`)
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
		switch input.Adapter {
		case ProfileGutenbergText:
			row, err = mapGutenberg(plan, input, data)
		case ProfileJATSXML:
			row, err = mapJATS(plan, input, data)
		default:
			err = fmt.Errorf("unsupported whole-file profile %q", input.Adapter)
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

func mapGutenberg(plan Plan, input PlanInput, data []byte) (shard.TextRow, error) {
	text := string(data)
	start := gutenbergStart.FindStringIndex(text)
	end := gutenbergEnd.FindStringIndex(text)
	if start == nil || end == nil {
		return shard.TextRow{}, fmt.Errorf("Project Gutenberg START and END markers are required")
	}
	if end[0] <= start[1] {
		return shard.TextRow{}, fmt.Errorf("Project Gutenberg END marker precedes the START marker")
	}
	return profiledFileRow(plan, input, strings.TrimSpace(text[start[1]:end[0]])+"\n", "", "", "", nil)
}

type jatsArticle struct {
	Title, Abstract, Body string
	DOI, Date, Journal    string
	License               string
}

func mapJATS(plan Plan, input PlanInput, data []byte) (shard.TextRow, error) {
	article, err := parseJATS(bytes.NewReader(data))
	if err != nil {
		return shard.TextRow{}, err
	}
	parts := []string{}
	for _, part := range []string{article.Title, article.Abstract, article.Body} {
		if strings.TrimSpace(part) != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return shard.TextRow{}, fmt.Errorf("JATS article contains no title, abstract, or body text")
	}
	var meta *string
	if article.Journal != "" {
		encoded, _ := json.Marshal(map[string]string{"journal": article.Journal})
		value := string(encoded)
		meta = &value
	}
	source := ""
	if article.DOI != "" {
		source = "https://doi.org/" + article.DOI
	}
	return profiledFileRow(plan, input, strings.Join(parts, "\n\n")+"\n", source, article.Date, article.License, meta)
}

func profiledFileRow(plan Plan, input PlanInput, text, source, date, rawLicense string, meta *string) (shard.TextRow, error) {
	if strings.TrimSpace(text) == "" || !utf8.ValidString(text) || strings.IndexByte(text, 0) >= 0 {
		return shard.TextRow{}, fmt.Errorf("extracted text is not nonempty NUL-free UTF-8")
	}
	if source == "" {
		source = "sha256:" + input.Artifact.SHA256
	}
	license := plan.License
	var licenseRaw *string
	if rawLicense != "" {
		license = rawLicense
		licenseRaw = &rawLicense
	}
	sourceName := plan.Source.Name
	digest := sha256.Sum256([]byte(text))
	return shard.TextRow{
		ContentSHA256: digest, Text: text, Source: source, SourceName: &sourceName,
		License: license, LicenseRaw: licenseRaw, Date: stringPointer(date), Meta: meta,
	}, nil
}

var jatsSkipped = map[string]bool{
	"sub-article": true, "table-wrap": true, "fig": true, "disp-formula": true,
	"inline-formula": true, "tex-math": true, "math": true, "supplementary-material": true,
	"ref-list": true, "object-id": true, "graphic": true, "media": true,
}

func parseJATS(reader io.Reader) (jatsArticle, error) {
	decoder := xml.NewDecoder(reader)
	decoder.Strict = false
	decoder.Entity = xml.HTMLEntity
	var article jatsArticle
	var stack []string
	var title, abstract, body, license strings.Builder
	var skip int
	var articleIDType, publicationType string
	var year, month, day string
	in := func(name string) bool {
		for _, element := range stack {
			if element == name {
				return true
			}
		}
		return false
	}
	top := func() string {
		if len(stack) == 0 {
			return ""
		}
		return stack[len(stack)-1]
	}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return jatsArticle{}, err
		}
		switch token := token.(type) {
		case xml.StartElement:
			stack = append(stack, token.Name.Local)
			if skip > 0 {
				skip++
				continue
			}
			if jatsSkipped[token.Name.Local] {
				skip = 1
				continue
			}
			switch token.Name.Local {
			case "license":
				article.License = xmlAttribute(token, "href")
			case "ext-link":
				if article.License == "" && in("license") {
					article.License = xmlAttribute(token, "href")
				}
			case "article-id":
				articleIDType = xmlAttribute(token, "pub-id-type")
			case "pub-date":
				publicationType, year, month, day = xmlAttribute(token, "pub-type"), "", "", ""
			}
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			if skip > 0 {
				skip--
				continue
			}
			switch token.Name.Local {
			case "p", "title", "sec":
				if in("body") {
					body.WriteString("\x00")
				} else if in("abstract") {
					abstract.WriteString("\x00")
				}
			case "pub-date":
				if (publicationType == "epub" || article.Date == "") && strings.TrimSpace(year) != "" {
					article.Date = strings.TrimSpace(year)
					if strings.TrimSpace(month) != "" {
						article.Date += "-" + padDate(month)
					}
					if strings.TrimSpace(day) != "" {
						article.Date += "-" + padDate(day)
					}
				}
			case "article-id":
				articleIDType = ""
			}
		case xml.CharData:
			if skip > 0 {
				continue
			}
			text := string(token)
			switch {
			case in("license"):
				license.WriteString(text)
			case in("body"):
				body.WriteString(text)
			case in("article-meta") && in("abstract"):
				abstract.WriteString(text)
			case in("article-meta") && in("title-group") && in("article-title"):
				title.WriteString(text)
			case in("article-meta") && top() == "article-id" && articleIDType == "doi" && article.DOI == "":
				article.DOI = strings.TrimSpace(text)
			case in("journal-title") && article.Journal == "" && strings.TrimSpace(text) != "":
				article.Journal = strings.TrimSpace(text)
			case in("article-meta") && in("pub-date"):
				switch top() {
				case "year":
					year += text
				case "month":
					month += text
				case "day":
					day += text
				}
			}
		}
	}
	article.Title, article.Abstract, article.Body = cleanJATS(title.String()), cleanJATS(abstract.String()), cleanJATS(body.String())
	if article.License == "" {
		article.License = cleanJATS(license.String())
	}
	return article, nil
}

var jatsParagraphs = regexp.MustCompile(`\s*\x00[\x00\s]*`)

func cleanJATS(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	return strings.TrimSpace(jatsParagraphs.ReplaceAllString(value, "\n\n"))
}

func xmlAttribute(element xml.StartElement, name string) string {
	for _, attribute := range element.Attr {
		if attribute.Name.Local == name {
			return attribute.Value
		}
	}
	return ""
}

func padDate(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == 1 {
		return "0" + value
	}
	return value
}
