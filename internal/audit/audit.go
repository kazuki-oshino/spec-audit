package audit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kazuki-oshino/spec-audit/internal/config"
	"github.com/kazuki-oshino/spec-audit/internal/ingest"
	"github.com/kazuki-oshino/spec-audit/internal/model"
	"github.com/kazuki-oshino/spec-audit/internal/report"
)

var ErrPartial = errors.New("audit incomplete; partial report saved")

type Codex interface {
	Call(context.Context, string, string, any, any) (json.RawMessage, error)
}
type Jev interface {
	Assess(context.Context, model.Requirement, []model.Block) ([]model.Pair, []json.RawMessage, error)
}
type Runner struct {
	Codex Codex
	Jev   Jev
}

type extractResult struct {
	Requirements []model.Requirement `json:"requirements"`
	ScopeItems   []model.ScopeItem   `json:"scope_items"`
	Dispositions []model.Disposition `json:"block_dispositions"`
}
type reviewResult struct {
	Coverage      string           `json:"coverage"`
	Contradiction bool             `json:"contradiction"`
	Explanation   string           `json:"explanation"`
	Evidence      []model.Evidence `json:"evidence"`
}
type additionsResult struct {
	Findings []model.Finding `json:"findings"`
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}

func runID() string {
	var random [8]byte
	_, _ = rand.Read(random[:])
	return time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(random[:])
}

func blocksFor(documents []model.Document, role string) []model.Block {
	blocks := make([]model.Block, 0)
	for _, doc := range documents {
		if doc.Role == role {
			blocks = append(blocks, doc.Blocks...)
		}
	}
	return blocks
}

func validateEvidence(items []model.Evidence, blocks map[string]model.Block) error {
	for _, item := range items {
		block, exists := blocks[item.BlockID]
		if !exists {
			return fmt.Errorf("unknown evidence block %s", item.BlockID)
		}
		if item.Quote == "" || !strings.Contains(block.Text, item.Quote) {
			return fmt.Errorf("quote not found in %s", item.BlockID)
		}
	}
	return nil
}

func validateExtract(result extractResult, blocks []model.Block) error {
	lookup := make(map[string]model.Block, len(blocks))
	for _, block := range blocks {
		lookup[block.ID] = block
	}
	if len(result.Dispositions) != len(blocks) {
		return errors.New("extraction dispositions are incomplete")
	}
	seen := make(map[string]bool)
	for _, disposition := range result.Dispositions {
		if _, ok := lookup[disposition.BlockID]; !ok || seen[disposition.BlockID] {
			return fmt.Errorf("invalid or duplicate disposition %s", disposition.BlockID)
		}
		switch disposition.Kind {
		case "requirement", "scope", "background", "non_requirement", "unclassified":
		default:
			return fmt.Errorf("invalid disposition kind %q", disposition.Kind)
		}
		seen[disposition.BlockID] = true
	}
	validCitation := func(ids []string, quote string) bool {
		if len(ids) == 0 || quote == "" {
			return false
		}
		found := false
		for _, id := range ids {
			block, ok := lookup[id]
			if !ok {
				return false
			}
			if strings.Contains(block.Text, quote) {
				found = true
			}
		}
		return found
	}
	for _, req := range result.Requirements {
		if req.Statement == "" || !validCitation(req.SourceBlockIDs, req.Quote) {
			return errors.New("invalid extracted requirement citation")
		}
	}
	for _, scope := range result.ScopeItems {
		if scope.Statement == "" || !validCitation(scope.SourceBlockIDs, scope.Quote) {
			return errors.New("invalid extracted scope citation")
		}
		switch scope.Kind {
		case "goal", "non_goal", "assumption", "deferred":
		default:
			return fmt.Errorf("invalid scope kind %q", scope.Kind)
		}
	}
	return nil
}

func chunkBlocks(blocks []model.Block, maxBytes int) ([][]model.Block, error) {
	var chunks [][]model.Block
	var current []model.Block
	for _, block := range blocks {
		encoded, _ := json.Marshal(block)
		if len(encoded) > maxBytes {
			return nil, fmt.Errorf("block %s exceeds %d bytes", block.ID, maxBytes)
		}
		if len(current) > 0 {
			candidate, _ := json.Marshal(append(current, block))
			if len(candidate) > maxBytes {
				chunks = append(chunks, current)
				current = nil
			}
		}
		current = append(current, block)
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks, nil
}

func (r Runner) Run(ctx context.Context, cfg config.Config) (string, error) {
	if r.Codex == nil || r.Jev == nil {
		return "", errors.New("Codex and Jev clients are required")
	}
	outputDir := cfg.OutputDir
	if !filepath.IsAbs(outputDir) {
		outputDir = filepath.Join(cfg.Root, outputDir)
	}
	roles := []struct {
		name     string
		patterns []string
	}{{"baseline", cfg.Baseline}, {"design", cfg.Design}, {"context", cfg.Context}}
	resolved := make(map[string][]string)
	for _, role := range roles {
		if len(role.patterns) == 0 {
			continue
		}
		paths, err := cfg.Resolve(role.patterns)
		if err != nil {
			return "", err
		}
		for _, path := range paths {
			rel, err := filepath.Rel(outputDir, path)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return "", fmt.Errorf("audit input is inside output_dir: %s", path)
			}
		}
		resolved[role.name] = paths
	}
	for _, path := range resolved["baseline"] {
		for _, design := range resolved["design"] {
			if path == design {
				return "", fmt.Errorf("same file used as baseline and design: %s", path)
			}
		}
	}
	runsDir := filepath.Join(outputDir, "runs")
	if err := os.MkdirAll(runsDir, 0700); err != nil {
		return "", err
	}
	dir := filepath.Join(runsDir, runID())
	if err := os.Mkdir(dir, 0700); err != nil {
		return "", err
	}
	requestedCodexModel := cfg.LLM.Model
	if requestedCodexModel == "" {
		requestedCodexModel = "SDK default"
	}
	result := model.Result{Manifest: model.Manifest{RunID: filepath.Base(dir), CreatedAt: time.Now().UTC(), ConfigPath: cfg.Path, RequestedCodexModel: requestedCodexModel, RequestedCodexEffort: cfg.LLM.Effort, RequestedJevModel: cfg.Jev.Model, RuleVersion: 1, BridgeProtocolVersion: 1, Status: "partial"}}
	var logLines []any
	var runErr error
	finish := func() (string, error) {
		if runErr == nil {
			result.Manifest.Status = "complete"
		} else {
			result.Manifest.Warnings = append(result.Manifest.Warnings, runErr.Error())
		}
		if err := writeJSON(filepath.Join(dir, "manifest.json"), result.Manifest); err != nil {
			return dir, err
		}
		if err := writeJSON(filepath.Join(dir, "requirements.json"), result.Requirements); err != nil {
			return dir, err
		}
		if err := writeJSON(filepath.Join(dir, "result.json"), result); err != nil {
			return dir, err
		}
		file, err := os.OpenFile(filepath.Join(dir, "responses.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			return dir, err
		}
		for _, line := range logLines {
			data, err := json.Marshal(line)
			if err == nil {
				_, _ = file.Write(append(data, '\n'))
			}
		}
		if err := file.Close(); err != nil {
			return dir, err
		}
		if err := report.Write(filepath.Join(dir, "report.md"), result); err != nil {
			return dir, err
		}
		if runErr != nil {
			return dir, fmt.Errorf("%w: %v", ErrPartial, runErr)
		}
		return dir, nil
	}
	for _, role := range roles {
		for _, path := range resolved[role.name] {
			doc, err := ingest.Read(role.name, path, len(result.Manifest.Documents)+1, cfg.Root, filepath.Join(dir, "sources"))
			if err != nil {
				runErr = err
				return finish()
			}
			result.Manifest.Documents = append(result.Manifest.Documents, doc)
			for _, ref := range doc.UnresolvedRefs {
				result.Manifest.Warnings = append(result.Manifest.Warnings, doc.Path+": "+ref)
			}
		}
	}
	baseline, design, contextBlocks := blocksFor(result.Manifest.Documents, "baseline"), blocksFor(result.Manifest.Documents, "design"), blocksFor(result.Manifest.Documents, "context")
	baselineMap := make(map[string]model.Block)
	for _, doc := range result.Manifest.Documents {
		for _, block := range doc.Blocks {
			if doc.Role == "baseline" {
				baselineMap[block.ID] = block
			}
		}
	}
	if len(baseline) == 0 || len(design) == 0 {
		runErr = errors.New("baseline or design has no text blocks")
		return finish()
	}
	contextJSON, _ := json.Marshal(contextBlocks)
	if len(contextJSON) > 20*1024 {
		runErr = errors.New("context exceeds 20 KiB; split or reduce it before audit")
		return finish()
	}
	baselineChunks, err := chunkBlocks(baseline, 40*1024)
	if err != nil {
		runErr = err
		return finish()
	}
	for i, chunk := range baselineChunks {
		if err := ctx.Err(); err != nil {
			runErr = err
			return finish()
		}
		var extracted extractResult
		id := fmt.Sprintf("extract-%04d", i+1)
		usage, err := r.Codex.Call(ctx, id, "extract_requirements", map[string]any{"blocks": chunk, "context_blocks": contextBlocks}, &extracted)
		if err != nil {
			runErr = err
			return finish()
		}
		if err := validateExtract(extracted, chunk); err != nil {
			runErr = err
			return finish()
		}
		for _, req := range extracted.Requirements {
			req.ID = fmt.Sprintf("req-%04d", len(result.Requirements)+1)
			result.Requirements = append(result.Requirements, req)
		}
		for _, item := range extracted.ScopeItems {
			item.ID = fmt.Sprintf("scope-%04d", len(result.ScopeItems)+1)
			result.ScopeItems = append(result.ScopeItems, item)
		}
		result.Dispositions = append(result.Dispositions, extracted.Dispositions...)
		logLines = append(logLines, map[string]any{"request_id": id, "task": "extract_requirements", "result": extracted, "usage": usage})
	}
	if len(baseline) > 1 {
		encoded, _ := json.Marshal(baseline)
		if len(encoded) > 120*1024 {
			runErr = errors.New("baseline exceeds 120 KiB; cannot complete conflict review")
			return finish()
		}
		var conflicts additionsResult
		usage, err := r.Codex.Call(ctx, "baseline-conflicts", "review_baseline_conflicts", map[string]any{"baseline_blocks": baseline, "context_blocks": contextBlocks}, &conflicts)
		if err != nil {
			runErr = err
			return finish()
		}
		for _, finding := range conflicts.Findings {
			if finding.Kind != "baseline_conflict" {
				runErr = errors.New("invalid baseline conflict kind")
				return finish()
			}
			if len(finding.Evidence) < 2 {
				runErr = errors.New("baseline conflict requires two cited blocks")
				return finish()
			}
			if finding.Evidence[0].BlockID == finding.Evidence[1].BlockID {
				runErr = errors.New("baseline conflict cites the same block twice")
				return finish()
			}
			if err := validateEvidence(finding.Evidence, baselineMap); err != nil {
				runErr = err
				return finish()
			}
			result.Findings = append(result.Findings, finding)
		}
		logLines = append(logLines, map[string]any{"request_id": "baseline-conflicts", "task": "review_baseline_conflicts", "result": conflicts, "usage": usage})
	}
	for _, requirement := range result.Requirements {
		if err := ctx.Err(); err != nil {
			runErr = err
			return finish()
		}
		pairs, usages, err := r.Jev.Assess(ctx, requirement, design)
		result.Pairs = append(result.Pairs, pairs...)
		for i, usage := range usages {
			logLines = append(logLines, map[string]any{"task": "jev_pairs", "requirement_id": requirement.ID, "batch": i, "usage": usage})
		}
		if err != nil {
			runErr = err
			return finish()
		}
		if len(pairs) != len(design) {
			runErr = fmt.Errorf("Jev pair coverage incomplete for %s", requirement.ID)
			return finish()
		}
	}
	chunks, err := chunkBlocks(design, 40*1024)
	if err != nil {
		runErr = err
		return finish()
	}
	incompleteReview := false
	for _, requirement := range result.Requirements {
		assessment := model.Assessment{RequirementID: requirement.ID, Coverage: "not_found", Status: "complete"}
		var explanations []string
		for i, chunk := range chunks {
			chunkMap := make(map[string]model.Block, len(chunk))
			for _, block := range chunk {
				chunkMap[block.ID] = block
			}
			var reviewed reviewResult
			id := fmt.Sprintf("review-%s-%04d", requirement.ID, i+1)
			usage, err := r.Codex.Call(ctx, id, "review_requirement", map[string]any{"requirement": requirement, "design_blocks": chunk, "context_blocks": contextBlocks}, &reviewed)
			if err != nil {
				runErr = err
				return finish()
			}
			if err := validateEvidence(reviewed.Evidence, chunkMap); err != nil {
				runErr = err
				return finish()
			}
			switch reviewed.Coverage {
			case "covered":
				assessment.Coverage = "covered"
			case "partial":
				if assessment.Coverage != "covered" {
					assessment.Coverage = "partial"
				}
			case "unclear", "deferred":
				if assessment.Coverage == "not_found" {
					assessment.Coverage = "unclear"
				}
			case "not_found":
			default:
				runErr = fmt.Errorf("invalid coverage %q", reviewed.Coverage)
				return finish()
			}
			assessment.Contradiction = assessment.Contradiction || reviewed.Contradiction
			assessment.Evidence = append(assessment.Evidence, reviewed.Evidence...)
			if reviewed.Explanation != "" {
				explanations = append(explanations, reviewed.Explanation)
			}
			logLines = append(logLines, map[string]any{"request_id": id, "task": "review_requirement", "result": reviewed, "usage": usage})
		}
		if len(chunks) > 1 {
			candidateIDs := make(map[string]bool)
			for _, evidence := range assessment.Evidence {
				candidateIDs[evidence.BlockID] = true
			}
			for _, pair := range result.Pairs {
				if pair.RequirementID == requirement.ID && (pair.Related >= 0.65 || pair.Supports >= 0.65 || pair.Contradicts >= 0.65) {
					candidateIDs[pair.DesignBlockID] = true
				}
			}
			var candidates []model.Block
			for _, block := range design {
				if candidateIDs[block.ID] {
					candidates = append(candidates, block)
				}
			}
			encoded, _ := json.Marshal(candidates)
			if len(encoded) > 80*1024 {
				assessment.Coverage = "unclear"
				assessment.Status = "partial"
				incompleteReview = true
			} else if len(candidates) > 0 {
				var combined reviewResult
				id := "combine-" + requirement.ID
				usage, err := r.Codex.Call(ctx, id, "review_requirement", map[string]any{"requirement": requirement, "design_blocks": candidates, "context_blocks": contextBlocks}, &combined)
				if err != nil {
					runErr = err
					return finish()
				}
				candidateMap := make(map[string]model.Block)
				for _, block := range candidates {
					candidateMap[block.ID] = block
				}
				if err := validateEvidence(combined.Evidence, candidateMap); err != nil {
					runErr = err
					return finish()
				}
				if combined.Coverage == "covered" || combined.Coverage == "partial" {
					assessment.Coverage = combined.Coverage
				}
				assessment.Contradiction = assessment.Contradiction || combined.Contradiction
				assessment.Evidence = append(assessment.Evidence, combined.Evidence...)
				if combined.Explanation != "" {
					explanations = append(explanations, combined.Explanation)
				}
				logLines = append(logLines, map[string]any{"request_id": id, "task": "review_requirement", "result": combined, "usage": usage})
			}
		}
		assessment.Explanation = strings.Join(explanations, " ")
		result.Assessments = append(result.Assessments, assessment)
		if assessment.Contradiction {
			result.Findings = append(result.Findings, model.Finding{Kind: "contradiction", RequirementID: requirement.ID, Explanation: assessment.Explanation, Evidence: assessment.Evidence})
		}
	}
	for i, chunk := range chunks {
		chunkMap := make(map[string]model.Block, len(chunk))
		for _, block := range chunk {
			chunkMap[block.ID] = block
		}
		var additions additionsResult
		id := fmt.Sprintf("additions-%04d", i+1)
		payload := map[string]any{"design_blocks": chunk, "baseline_blocks": baseline, "context_blocks": contextBlocks}
		encoded, _ := json.Marshal(payload)
		if len(encoded) > 120*1024 {
			runErr = errors.New("baseline plus design batch exceeds 120 KiB; cannot complete reverse review")
			return finish()
		}
		usage, err := r.Codex.Call(ctx, id, "review_design_additions", payload, &additions)
		if err != nil {
			runErr = err
			return finish()
		}
		for _, finding := range additions.Findings {
			switch finding.Kind {
			case "added_behavior", "contradiction", "clarification_needed":
			default:
				runErr = fmt.Errorf("invalid finding kind %q", finding.Kind)
				return finish()
			}
			if len(finding.Evidence) == 0 {
				runErr = errors.New("design finding has no cited block")
				return finish()
			}
			if err := validateEvidence(finding.Evidence, chunkMap); err != nil {
				runErr = err
				return finish()
			}
			result.Findings = append(result.Findings, finding)
		}
		logLines = append(logLines, map[string]any{"request_id": id, "task": "review_design_additions", "result": additions, "usage": usage})
	}
	sort.Slice(result.Findings, func(i, j int) bool { return result.Findings[i].Kind < result.Findings[j].Kind })
	unresolved := false
	for _, doc := range result.Manifest.Documents {
		if doc.Role != "context" && len(doc.UnresolvedRefs) > 0 {
			unresolved = true
		}
	}
	if unresolved {
		for i := range result.Assessments {
			if result.Assessments[i].Coverage == "not_found" {
				result.Assessments[i].Coverage = "unclear"
				result.Assessments[i].Explanation += " 未収録の参照先があるため未記載と断定できません。"
			}
		}
	}
	if incompleteReview {
		runErr = errors.New("some cross-chunk requirement reviews exceeded the input budget")
	}
	for _, item := range result.Dispositions {
		if item.Kind == "unclassified" {
			runErr = errors.New("some baseline blocks remain unclassified")
			break
		}
	}
	return finish()
}
