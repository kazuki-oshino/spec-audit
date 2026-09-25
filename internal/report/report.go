package report

import (
	"fmt"
	"os"
	"strings"

	"github.com/kazuki-oshino/spec-audit/internal/model"
)

func table(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.TrimSpace(value)
}

func Render(result model.Result) string {
	var b strings.Builder
	blockMap := make(map[string]model.Block)
	for _, doc := range result.Manifest.Documents {
		for _, block := range doc.Blocks {
			blockMap[block.ID] = block
		}
	}
	fmt.Fprintf(&b, "# specaudit 監査レポート\n\n実行ID: `%s`  \n日時: %s  \n処理状態: **%s**\n\n", result.Manifest.RunID, result.Manifest.CreatedAt.Format("2006-01-02 15:04:05 UTC"), result.Manifest.Status)
	fmt.Fprintf(&b, "要求したモデル: Codex `%s`（reasoning `%s`）、Jev `%s`。判定ルール: v%d、bridge契約: v%d。\n\n", result.Manifest.RequestedCodexModel, result.Manifest.RequestedCodexEffort, result.Manifest.RequestedJevModel, result.Manifest.RuleVersion, result.Manifest.BridgeProtocolVersion)
	b.WriteString("## 対象文書\n\n| 役割 | 文書 | ブロック数 | SHA-256 |\n|---|---|---:|---|\n")
	for _, doc := range result.Manifest.Documents {
		fmt.Fprintf(&b, "| %s | `%s` | %d | `%s` |\n", doc.Role, table(doc.Path), len(doc.Blocks), doc.SHA256)
	}
	b.WriteString("\n## 処理範囲\n\n")
	fmt.Fprintf(&b, "基準ブロック: %d、抽出した要求: %d、一次照合した組: %d、最終照合した要求: %d。\n\n", countBlocks(result.Manifest.Documents, "baseline"), len(result.Requirements), len(result.Pairs), len(result.Assessments))
	if len(result.Manifest.Warnings) > 0 {
		b.WriteString("### 未解析・警告\n\n")
		for _, warning := range result.Manifest.Warnings {
			fmt.Fprintf(&b, "- %s\n", warning)
		}
		b.WriteString("\n")
	}
	var unclassified []string
	for _, item := range result.Dispositions {
		if item.Kind == "unclassified" {
			unclassified = append(unclassified, item.BlockID)
		}
	}
	if len(unclassified) > 0 {
		fmt.Fprintf(&b, "未分類の基準ブロック: `%s`\n\n", strings.Join(unclassified, "`, `"))
	}
	b.WriteString("## PRD要求と設計の対応\n\n| ID | 要求 | 対応 | 矛盾候補 | 根拠 |\n|---|---|---|---|---|\n")
	assessments := make(map[string]model.Assessment)
	for _, a := range result.Assessments {
		assessments[a.RequirementID] = a
	}
	for _, req := range result.Requirements {
		a, ok := assessments[req.ID]
		coverage := "未処理"
		contradiction := "未処理"
		if ok {
			coverage = a.Coverage
			if a.Contradiction {
				contradiction = "あり"
			} else {
				contradiction = "なし"
			}
		}
		var refs []string
		for _, evidence := range a.Evidence {
			refs = append(refs, evidence.BlockID)
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s |\n", req.ID, table(req.Statement), coverage, contradiction, table(strings.Join(refs, ", ")))
	}
	b.WriteString("\n### 要求の原文\n\n")
	for _, req := range result.Requirements {
		fmt.Fprintf(&b, "- `%s` %s\n", req.ID, req.Statement)
		for _, id := range req.SourceBlockIDs {
			if block, ok := blockMap[id]; ok {
				fmt.Fprintf(&b, "  - `%s`（`%s:%d-%d`）: %s\n", id, block.Path, block.StartLine, block.EndLine, req.Quote)
			}
		}
	}
	b.WriteString("\n### 要求ごとの判定理由\n\n")
	for _, assessment := range result.Assessments {
		fmt.Fprintf(&b, "- `%s` [%s]: %s\n", assessment.RequirementID, assessment.Coverage, assessment.Explanation)
	}
	b.WriteString("\n## 指摘\n\n")
	if len(result.Findings) == 0 {
		b.WriteString("指摘はありません。\n\n")
	}
	for i, finding := range result.Findings {
		fmt.Fprintf(&b, "### %d. %s\n\n%s\n\n", i+1, finding.Kind, finding.Explanation)
		if finding.RequirementID != "" {
			fmt.Fprintf(&b, "対象要求: `%s`  \n", finding.RequirementID)
		}
		for _, evidence := range finding.Evidence {
			if block, ok := blockMap[evidence.BlockID]; ok {
				fmt.Fprintf(&b, "根拠 `%s`（`%s:%d-%d`）:\n\n", evidence.BlockID, block.Path, block.StartLine, block.EndLine)
			}
			for _, line := range strings.Split(evidence.Quote, "\n") {
				fmt.Fprintf(&b, "> %s\n", line)
			}
			b.WriteString("\n")
		}
		if finding.Question != "" {
			fmt.Fprintf(&b, "確認事項: %s\n\n", finding.Question)
		}
	}
	b.WriteString("## 意図的な対象外・仮定\n\n")
	if len(result.ScopeItems) == 0 {
		b.WriteString("抽出なし。\n\n")
	}
	for _, item := range result.ScopeItems {
		fmt.Fprintf(&b, "- `%s` [%s] %s（根拠: `%s`）\n", item.ID, item.Kind, item.Statement, strings.Join(item.SourceBlockIDs, "`, `"))
	}
	b.WriteString("\nこのレポートは提示された文書の整合性を点検する材料です。実装の正しさや指摘の網羅性を保証しません。\n")
	return b.String()
}

func countBlocks(documents []model.Document, role string) int {
	n := 0
	for _, doc := range documents {
		if doc.Role == role {
			n += len(doc.Blocks)
		}
	}
	return n
}

func Write(path string, result model.Result) error {
	return os.WriteFile(path, []byte(Render(result)), 0600)
}
