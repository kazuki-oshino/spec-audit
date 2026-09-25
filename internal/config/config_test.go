package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRecursiveGlobSortedAndDeduplicated(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "design", "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"design/a.md", "design/nested/b.md"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("# dummy\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := Config{Root: root}
	paths, err := cfg.Resolve([]string{"design/**/*.md", "design/a.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != filepath.Join(root, "design", "a.md") || paths[1] != filepath.Join(root, "design", "nested", "b.md") {
		t.Fatalf("paths=%v", paths)
	}
}

func TestLoadFixesCodexModelAndEffort(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "audit.yaml")
	base := "version: 1\nbaseline: [prd.md]\ndesign: [design.md]\n"
	if err := os.WriteFile(path, []byte(base), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.Model != CodexModel || cfg.LLM.Effort != CodexEffort {
		t.Fatalf("model=%s effort=%s", cfg.LLM.Model, cfg.LLM.Effort)
	}
	if err := os.WriteFile(path, []byte(base+"llm:\n  model: gpt-6-luna\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("non-fixed model was accepted")
	}
	if err := os.WriteFile(path, []byte(base+"llm:\n  effort: high\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("non-fixed effort was accepted")
	}
}
