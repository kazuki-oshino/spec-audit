package ingest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadKeepsHeadingAndLineRange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "prd.md")
	text := "# 目的\n\n利用者は検索できる。\n\n## 対象外\n\n決済は今回扱わない。\n"
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	doc, err := Read("baseline", path, 1, root, filepath.Join(root, "snapshots"))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Blocks) != 4 {
		t.Fatalf("blocks=%d", len(doc.Blocks))
	}
	if doc.Blocks[1].StartLine != 3 || doc.Blocks[1].EndLine != 3 || doc.Blocks[1].HeadingPath[0] != "目的" {
		t.Fatalf("unexpected block: %+v", doc.Blocks[1])
	}
	if _, err := os.Stat(filepath.Join(root, "snapshots", "doc-0001.md")); err != nil {
		t.Fatal(err)
	}
}
