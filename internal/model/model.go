package model

import "time"

type Block struct {
	ID          string   `json:"id"`
	DocumentID  string   `json:"document_id"`
	Path        string   `json:"path"`
	HeadingPath []string `json:"heading_path,omitempty"`
	StartLine   int      `json:"start_line"`
	EndLine     int      `json:"end_line"`
	Text        string   `json:"text"`
}

type Document struct {
	ID             string   `json:"id"`
	Role           string   `json:"role"`
	Path           string   `json:"path"`
	SHA256         string   `json:"sha256"`
	Blocks         []Block  `json:"blocks"`
	UnresolvedRefs []string `json:"unresolved_refs,omitempty"`
}

type Requirement struct {
	ID             string   `json:"id"`
	Statement      string   `json:"statement"`
	Conditions     string   `json:"conditions"`
	SourceBlockIDs []string `json:"source_block_ids"`
	Quote          string   `json:"quote"`
}

type ScopeItem struct {
	ID             string   `json:"id"`
	Kind           string   `json:"kind"`
	Statement      string   `json:"statement"`
	SourceBlockIDs []string `json:"source_block_ids"`
	Quote          string   `json:"quote"`
}

type Disposition struct {
	BlockID string `json:"block_id"`
	Kind    string `json:"kind"`
}

type Pair struct {
	RequirementID string  `json:"requirement_id"`
	DesignBlockID string  `json:"design_block_id"`
	Related       float64 `json:"related"`
	Supports      float64 `json:"supports"`
	Contradicts   float64 `json:"contradicts"`
	Model         string  `json:"model"`
}

type Evidence struct {
	BlockID string `json:"block_id"`
	Quote   string `json:"quote"`
}

type Assessment struct {
	RequirementID string     `json:"requirement_id"`
	Coverage      string     `json:"coverage"`
	Contradiction bool       `json:"contradiction"`
	Explanation   string     `json:"explanation"`
	Evidence      []Evidence `json:"evidence"`
	Status        string     `json:"status"`
}

type Finding struct {
	Kind          string     `json:"kind"`
	Explanation   string     `json:"explanation"`
	Question      string     `json:"question_to_resolve"`
	RequirementID string     `json:"requirement_id,omitempty"`
	Evidence      []Evidence `json:"evidence"`
}

type Manifest struct {
	RunID                 string     `json:"run_id"`
	CreatedAt             time.Time  `json:"created_at"`
	ConfigPath            string     `json:"config_path"`
	RequestedCodexModel   string     `json:"requested_codex_model"`
	RequestedCodexEffort  string     `json:"requested_codex_effort"`
	RequestedJevModel     string     `json:"requested_jev_model"`
	RuleVersion           int        `json:"rule_version"`
	BridgeProtocolVersion int        `json:"bridge_protocol_version"`
	Documents             []Document `json:"documents"`
	Warnings              []string   `json:"warnings,omitempty"`
	Status                string     `json:"status"`
}

type Result struct {
	Manifest     Manifest      `json:"manifest"`
	Requirements []Requirement `json:"requirements"`
	ScopeItems   []ScopeItem   `json:"scope_items"`
	Dispositions []Disposition `json:"block_dispositions"`
	Pairs        []Pair        `json:"pairs"`
	Assessments  []Assessment  `json:"assessments"`
	Findings     []Finding     `json:"findings"`
}
