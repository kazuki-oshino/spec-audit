package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kazuki-oshino/spec-audit/internal/model"
)

var headingRE = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)
var imageRE = regexp.MustCompile(`!\[[^]]*\]\(([^)]+)\)`)
var linkRE = regexp.MustCompile(`(?m)\[[^]]+\]\((https?://[^)]+)\)`)

func Read(role, path string, id int, root, snapshotDir string) (model.Document, error) {
	var doc model.Document
	data, err := os.ReadFile(path)
	if err != nil {
		return doc, err
	}
	if !strings.EqualFold(filepath.Ext(path), ".md") {
		return doc, fmt.Errorf("not Markdown: %s", path)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return doc, err
	}
	doc.ID = fmt.Sprintf("doc-%04d", id)
	doc.Role = role
	doc.Path = filepath.ToSlash(rel)
	hash := sha256.Sum256(data)
	doc.SHA256 = hex.EncodeToString(hash[:])
	if err := os.MkdirAll(snapshotDir, 0700); err != nil {
		return doc, err
	}
	if err := os.WriteFile(filepath.Join(snapshotDir, doc.ID+".md"), data, 0600); err != nil {
		return doc, err
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	var headings []string
	var pending []string
	start := 0
	insideCode := false
	flush := func(end int) {
		if len(pending) == 0 {
			return
		}
		body := strings.TrimSpace(strings.Join(pending, "\n"))
		if body != "" {
			block := model.Block{ID: fmt.Sprintf("%s-b%04d", doc.ID, len(doc.Blocks)+1), DocumentID: doc.ID, Path: doc.Path, HeadingPath: append([]string(nil), headings...), StartLine: start, EndLine: end, Text: body}
			doc.Blocks = append(doc.Blocks, block)
		}
		pending = nil
	}
	for i, line := range lines {
		lineNo := i + 1
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			insideCode = !insideCode
		}
		if !insideCode {
			if match := headingRE.FindStringSubmatch(line); match != nil {
				flush(lineNo - 1)
				depth := len(match[1])
				if depth <= len(headings) {
					headings = headings[:depth-1]
				}
				for len(headings) < depth-1 {
					headings = append(headings, "")
				}
				headings = append(headings, match[2])
				pending, start = []string{line}, lineNo
				flush(lineNo)
				continue
			}
			if trimmed == "" {
				flush(lineNo - 1)
				continue
			}
		}
		if len(pending) == 0 {
			start = lineNo
		}
		pending = append(pending, line)
	}
	flush(len(lines))
	for _, m := range imageRE.FindAllStringSubmatch(text, -1) {
		doc.UnresolvedRefs = append(doc.UnresolvedRefs, "画像: "+m[1])
	}
	for _, m := range linkRE.FindAllStringSubmatch(text, -1) {
		doc.UnresolvedRefs = append(doc.UnresolvedRefs, "外部リンク: "+m[1])
	}
	return doc, nil
}
