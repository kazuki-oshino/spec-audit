package audit

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kazuki-oshino/spec-audit/internal/config"
	"github.com/kazuki-oshino/spec-audit/internal/model"
)

type fakeCodex struct{}

func (fakeCodex) Call(_ context.Context, _, task string, payload any, into any) (json.RawMessage, error) {
	data, _ := json.Marshal(payload)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if string(fields["context_blocks"]) != "[]" {
		return nil, errors.New("context_blocks must be an empty array when no context is configured")
	}
	var input struct {
		Blocks []model.Block `json:"blocks"`
		Design []model.Block `json:"design_blocks"`
	}
	_ = json.Unmarshal(data, &input)
	var output any
	switch task {
	case "extract_requirements":
		output = map[string]any{"requirements": []map[string]any{{"statement": "利用者は検索できる", "conditions": "", "source_block_ids": []string{input.Blocks[0].ID}, "quote": "利用者は検索できる"}}, "scope_items": []any{}, "block_dispositions": []map[string]string{{"block_id": input.Blocks[0].ID, "kind": "requirement"}}}
	case "review_requirement":
		output = map[string]any{"coverage": "covered", "contradiction": false, "explanation": "設計に検索機能がある", "evidence": []map[string]string{{"block_id": input.Design[0].ID, "quote": "検索機能"}}}
	case "review_design_additions":
		output = map[string]any{"findings": []any{}}
	}
	encoded, _ := json.Marshal(output)
	return nil, json.Unmarshal(encoded, into)
}

type fakeJev struct{}

func (fakeJev) Assess(_ context.Context, requirement model.Requirement, blocks []model.Block) ([]model.Pair, []json.RawMessage, error) {
	pairs := make([]model.Pair, len(blocks))
	for i, block := range blocks {
		pairs[i] = model.Pair{RequirementID: requirement.ID, DesignBlockID: block.ID, Supports: 0.9, Model: "fake"}
	}
	return pairs, nil, nil
}

func TestRunWritesReportAndReplayData(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "prd.md"), []byte("利用者は検索できる。\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "design.md"), []byte("検索機能を提供する。\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Version: 1, Baseline: []string{"prd.md"}, Design: []string{"design.md"}, OutputDir: ".specaudit", Root: root, Path: filepath.Join(root, "audit.yaml")}
	cfg.LLM.Model = config.CodexModel
	cfg.LLM.Effort = config.CodexEffort
	dir, err := (Runner{Codex: fakeCodex{}, Jev: fakeJev{}}).Run(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "covered") {
		t.Fatalf("report: %s", data)
	}
	var result model.Result
	encoded, err := os.ReadFile(filepath.Join(dir, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	if result.Manifest.Status != "complete" || len(result.Pairs) != 1 || len(result.Assessments) != 1 {
		t.Fatalf("result=%+v", result)
	}
	if result.Manifest.RequestedCodexModel != "gpt-6-sol" || result.Manifest.RequestedCodexEffort != "medium" {
		t.Fatalf("Codex settings missing from manifest: %+v", result.Manifest)
	}
}

func TestValidateRejectsInventedQuote(t *testing.T) {
	block := model.Block{ID: "b1", Text: "原文"}
	err := validateExtract(extractResult{Requirements: []model.Requirement{{Statement: "存在しない", Quote: "架空", SourceBlockIDs: []string{"b1"}}}, Dispositions: []model.Disposition{{BlockID: "b1", Kind: "requirement"}}}, []model.Block{block})
	if err == nil {
		t.Fatal("invented quote accepted")
	}
}
