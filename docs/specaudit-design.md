# specaudit — PRD・設計資料の初回整合性監査CLI

設計日: 2026-09-25
状態: 実装前の簡易設計案。以下のコマンドや設定は、このアプリ用に新しく定義するもの。

## 1. 目的と範囲

業務で作成するPoC等について、PRDと設計資料一式を指定し、今回の要求・検証目的が設計へ正しく反映されているかをMarkdownで報告する。

- PRD → 設計: 対応記述、不足、矛盾の候補。
- 設計 → PRD: 追加されたユーザー向け挙動・制約の候補。単なる実装詳細は問題扱いしない。
- 同一要求に対応する設計資料間: 挙動・条件・状態遷移等の食い違いの候補。

PoCの対象外・モック化・先送り事項は、その根拠をPRD等から保持する。一般的な本番要件を勝手に追加しない。ただし、明示的な要求を「PoCだから」という理由で免除しない。

初版ではローカルMarkdownのみを受け付ける。Confluence連携、Slack連携、差分実行、コード解析、画像図の解析、自動修正、提出や公開操作は対象外。Confluence等の文書は事前にMarkdownへ変換する。未収録のリンク先や画像図は解析範囲の制約として報告する。

これは実装の正しさを保証するものではなく、提示された文書の整合性について、人が確認する箇所を整理するツールである。

## 2. 技術分担

| 部品 | 責務 |
|---|---|
| Go CLI | 入力解決、スナップショット、Markdown解析、処理進行、バッチ・並列数制御、Go側検証、保存、Markdown生成 |
| TypeScript bridge | Codex SDKを呼ぶ。要求抽出と根拠に基づく再照合を、JSON Schemaに沿って返す |
| Jev HTTP adapter (Go) | 型付きの小さな意味判断をまとめて実行。文章生成はしない |

Goが唯一の進行管理主体。TypeScriptは薄いアダプターとし、ワークフローやファイル更新の責務を持たせない。外部DB、常駐HTTPサーバー、ベクトルDBは置かない。

Codex TypeScript SDKは、公式README上ではCodex CLIを起動して利用する構成であり、単純なHTTPクライアントではない。Nodeプロセスに加え、CLIランタイム、認証、設定、子プロセスの管理が必要。[S1][S2]

## 3. CLIと設定

```sh
specaudit run --config audit.yaml
specaudit replay --run .specaudit/runs/<run-id>
```

`run`は新しい監査を実行する。`replay`は保存済みの応答から集約とレポート生成のみを実行し、モデルを再呼び出ししない。再評価のためのAPI呼び出しは新しい`run`として記録する。

```yaml
version: 1
baseline:
  - ./docs/prd.md
design:
  - ./docs/design/**/*.md
context:
  - ./docs/glossary.md
output_dir: ./.specaudit
llm:
  bridge_command: [node, ./bridge/dist/main.js]
  concurrency: 1
jev:
  model: jev-latest
  concurrency: 4
```

相対パスは設定ファイルのあるディレクトリを起点に解決する。globはGoで展開し、シェルへ渡さない。対象ファイルをソート・重複排除する。空のbaseline/design、読み込み失敗はエラーとする。

`baseline`は基準文書、`design`は監査対象、`context`は用語・背景であり要求を上書きする資料ではない。baselineが複数あり矛盾した場合、日時やファイル順で自動的に勝者を決めず、基準未確定として扱う。

Codexの認証・利用モデルは利用環境に合わせて設定する。再現性を重視する運用ではモデル、SDK、プロンプト、ルールを固定する。`jev-latest`は動作確認用の例であり、固定バージョンではない。要求モデルと応答で解決されたモデルを記録する。

## 4. パイプライン

### 4.1 Go: 入力を固定して文書ブロックに分割

指定ファイルをrunディレクトリにスナップショットする。見出し、本文、箇条書き、表、コードブロック、脚注、リンクを可能な範囲で保ちながら、意味を壊さない大きさのブロックへ分割する。表のヘッダー、見出し階層、直近の定義を失わない。

各ブロックに、文書ID、block ID、元の相対パス、行範囲、見出し階層、内容ハッシュをGoが付ける。LLMが行番号や引用箇所を発明する必要のない形にする。

画像図、未収録リンク先、処理できなかった箇所をmanifestへ記録する。長文は黙って切り捨てない。Mermaidは原文テキストとして保持できるが、図形として正しく理解したという扱いにはしない。

### 4.2 Codex: PRDの要求を抽出

PRDの全ブロックを処理し、要求、条件、検証目的、非目標、仮定、保留事項へ整理する。各項目に実在するblock IDを添える。原文にない一般論やベストプラクティスを新規要求にしない。

要求は原子化するが、「管理者が」「決済前に」「失敗した場合」等の主体・条件・例外を切り落とさない。分割する場合も親要求と条件を保持する。

全PRDブロックについて、要求等への対応、背景、非要求、または未分類のいずれかを記録する。これは要求抽出の正しさを証明するものではないが、抽出されなかった部分を人が点検できる。抽出失敗や未分類を無言で正常扱いしない。

GoがSchema、参照ID、引用文字列と原文の一致を検証する。原文照合に失敗した項目は修復対象か未判定にする。

設計資料は初版では全面的な要約・主張抽出をしない。Goが分けた原文ブロックを主な照合対象とし、要約で条件が消える経路を増やさない。

### 4.3 Jev: 全要求 × 全設計ブロックを一次照合

初版の候補集合は、検索上位の数件ではなく全設計ブロックとする。複数ブロックを原文・必要な定義とともにstateへ入れ、要求とblock IDを明示した質問をまとめる。

各組について、例えば次の独立したnoulを聞く。

- 同一の対象・条件・版について述べているか。
- 要求の全部または一部を満たす設計上の記述が明示されているか。
- 同一条件で要求と両立しない記述が明示されているか。

「不明だから矛盾」「記載がないから禁止」と推論しない。各問は独立して理解できる指示にする。同じリクエスト内の別回答を前提にしない。

処理量は要求数R × 設計ブロック数Dに依存する。全組をメモリへ展開せず、ジョブを逐次生成して有界ワーカーで処理する。状態・質問のサイズ制限を見ながらバッチ分割し、上限超過時は適切な軸を再分割する。質問だけ半分にしてもstateが大きい場合は解決しない。

判定しきれない予算・サイズなら途中結果として終了する。全文監査の指定を、黙ってtop-k監査へ変えない。

### 4.4 Codex: 要求単位の再照合と逆方向チェック

要求ごとに、関連・支持・矛盾・曖昧の候補を原文つきで束ねて再照合する。初版では各要求についてこの最終照合を行う。複数の独立した要求を一回に束ねてもよいが、要求間で根拠を混ぜない。

- 複数資料を合わせて満たす要求を評価する。
- 「対応記述あり」と「一部に矛盾あり」は両立し得るため、独立した出力軸にする。
- 同一要求の候補資料間で、条件や状態定義が食い違っていないかを見る。
- 欠落候補については、一次選別から落ちた設計原文も分割して再探索する。根拠が増えた場合は束ね直す。
- 入力文書・必要な参照先・ジョブが欠けた場合は「未判定」とし、「未記載」と断定しない。

設計→PRD方向では、全設計ブロックを対象に、PRDやその非目標に対して追加された利用制約、データ削除、課金条件等のプロダクト挙動を点検する。関連要求が見つからないブロックだけに限定しない。関連要求があっても、その一部に新しい挙動が追加されるためである。

通常の実装詳細は追加仕様扱いしない。基準文書同士が矛盾している場合は基準未確定を返す。

Codexは原文に基づいたレビュー結果を生成するが、正しさを保証する審判ではない。JevとCodexが同じ誤りをする可能性もある。Jevの省略判定の見逃しは、保留標本や抽出監査と合わせて評価する。

### 4.5 Go: 結果を検証・集約してMarkdown生成

LLM出力の参照IDが実在するか、引用が原文に存在するかを検査する。これは根拠の実在性の検査であり、説明の意味的な正しさの証明ではない。

確定したJSONからテンプレートでレポートを生成する。最終Markdown全文をもう一度LLMに書き直させない。説明文は再照合結果を使い、引用本文・行範囲・集計数はGoが管理する。

## 5. 内部データ

| 型 | 主なフィールド |
|---|---|
| SourceDocument | id, role, path, hash, blocks, unresolved_refs |
| SourceBlock | id, document_id, heading_path, start_line, end_line, text |
| Requirement | id, statement, conditions, scope, source_block_ids |
| ScopeItem | id, kind(goal/non_goal/assumption/deferred), statement, source_block_ids |
| PairAssessment | requirement_id, design_block_id, raw_answers, model, rule_version |
| RequirementAssessment | requirement_id, coverage, evidence_ids, finding_ids, processing_status |
| Finding | id, kind, requirement_ids, source_block_ids, explanation, question_to_resolve |
| AuditRun | id, status, manifest, extraction_coverage, processing_coverage, usage, versions |

coverageは `covered / partial / not_found / unclear / deferred`。

- covered: 入力範囲で対応する設計記述を確認した。実装の正しさ・矛盾ゼロを意味しない。
- partial: 要求の一部について対応記述が不足している候補。
- not_found: 対象範囲を処理・再探索したが対応記述を確認できなかった。絶対的な不存在証明ではない。
- unclear: 必要情報、図、参照文書、処理結果などが足りないか解釈を決められない。
- deferred: 基準文書で今回の対象外・先送りが明示され、根拠がある。

finding kindは `contradiction / added_behavior / baseline_conflict / clarification_needed` 等。coverageと分離する。

機械処理が完了したかは `complete / partial / failed` として別途記録する。解析完了は意味判断の完全性を保証しない。

## 6. Go ↔ TypeScript bridge

初版は1ジョブ=1プロセス、stdinに1 JSON、stdoutに1 JSON。EOFで入力を確定し、stderrだけをログに使う。常駐サーバー、HTTP、gRPC、逐次トークン配信は不要。

```json
{
  "version": 1,
  "request_id": "req-0001",
  "task": "extract_requirements",
  "payload": {"blocks": []}
}
```

```json
{
  "version": 1,
  "request_id": "req-0001",
  "ok": true,
  "result": {"requirements": [], "scope_items": [], "block_dispositions": []},
  "usage": {}
}
```

taskは `extract_requirements / review_requirement / review_design_additions`。エラーは `ok: false` と `error: {code, message, retryable}`。入力・出力のJSON Schemaを共有し、TS・Go双方で検証する。

Codex SDKではstartThread/runとturn単位のoutputSchemaを使える。[S1][S2] taskごとに新しいthreadを使い、過去の案件や要求の会話を暗黙に引き継がない。

read-only sandbox、approvalPolicy=never、webSearchMode=disabled、networkAccessEnabled=falseを基本設定にする。[S3] ただしこれは外部のモデルサービスへの通信をなくす設定ではなく、read-onlyはツール実行や全ファイルの読み取りを完全に禁止する意味でもない。不要なMCP、Skill、ユーザー・リポジトリ設定を継承しない専用の実行設定・環境で動かす。

入力は原則Goが明示的に渡す。監査対象の文書に含まれる指示は、実行指示ではなく引用データとして扱う。原本を編集する権限は与えない。

runのAbortSignalを利用できる。[S4] GoのキャンセルはNodeだけでなくSDKが起動した子プロセスにも伝播させる。標準出力上限・タイムアウト・終了コードを管理する。シェルを介してコマンドを組み立てない。

## 7. Jevアダプター

Goのnet/httpを使う薄いアダプターを置く。既存Goクライアントにもtyped System One APIの実装例があるが、初版は依存を増やさなくてもよい。[S5]

対象契約は `POST https://api.typesafe.ai/v1/systemone`、`state`、`questions`、対応するtyped answers。導入時のAPIバージョンで契約テストを作る。Jev公式ドキュメントは今回の取得でエラーとなったため、最新の詳細制限・認証・応答を実装開始時に公式資料または実APIで再確認する。[S5][S6]

noul/score等のraw値を保存し、業務上の判定・閾値はGo側のバージョン付きルールとする。confidenceや数値をそのまま「正答率」と表示しない。初期ルールは暫定であり、既知の事例で調整する。境界領域は再照合候補へ回す。

バッチCLIなので、429/一時的5xx等はRetry-Afterを尊重した上限付きリトライを許す。401/403、スキーマ不正、入力上限超過は機械的な無限リトライをしない。サイズ超過は分割で処理する。失敗した判定はmissing responseとして残し、問題なしへ変換しない。

## 8. 出力

```text
.specaudit/runs/<run-id>/
  report.md
  result.json
  requirements.json
  manifest.json
  responses.jsonl
  sources/
```

主要成果物はreport.md。result.jsonは再集計・将来のUI向け。responses.jsonlに入力、応答、規則、時間、取得できる利用量、バージョン等を記録する。秘密鍵は保存しない。機密原文を含むため、runディレクトリやCodex側のセッション記録の保存方針・アクセス権・削除方針を定め、Git管理対象から外す。

同一入力・規則からのローカルreplayと、APIを再実行した際のモデルの揺らぎは別物として扱う。

レポートに含める内容:

1. 対象文書、スナップショット時点、基準、監査範囲、未解析資料。
2. 要求抽出と処理の網羅状況。未分類・未処理を明示。
3. PRD要求→設計対応表。対応状況と矛盾を別列にする。
4. 指摘の詳細。双方の原文、参照、食い違い、確認すべき質問。
5. PRDに明示されない追加挙動の候補。
6. 意図的な省略・保留事項とその根拠。
7. モデル・プロンプト・判定ルールの版、処理時間、利用量。費用は単価を確認できる場合のみ概算。

終了コード: 0=処理完了（指摘ありでも0）、1=設定・入力等により失敗、2=一部未処理のpartial report、130=中断。全て「意味的に正しい設計か」とは別のコードである。初版では指摘の存在でCIを止めない。

## 9. リポジトリ構成

```text
cmd/specaudit/main.go
internal/model/           # 文書・要求・判定・レポート型
internal/ingest/          # Markdown、スナップショット、source refs
internal/audit/           # パイプライン、集約、バッチ計画
internal/codex/           # Go側bridgeクライアント
internal/jev/             # HTTPアダプター
internal/report/          # JSON保存、Markdownテンプレート
bridge/src/main.ts
bridge/src/tasks.ts
contracts/                # bridge入出力JSON Schema
prompts/                  # Codex prompts、Jev question definitions
fixtures/                 # API不要の回帰テスト材料
```

Goは通常のstructとコンストラクターで組み立てる。LLMとJevの境界のみ小さなinterfaceにし、API不要でテスト可能にする。過剰なDI基盤や汎用ワークフローエンジンは導入しない。

## 10. 実装順

1. Markdown→SourceBlock→固定JSON→report.mdまでをAPIなしで通す。
2. Codex bridgeの構造化出力と、要求抽出の参照検証を実装する。
3. Jev全組照合と要求単位の再照合をつなぐ。
4. 逆方向チェック・同一要求内の設計間チェックを加える。
5. partial report、replay、タイムアウト、キャンセル、利用量記録を仕上げる。

fixturesには、単純な矛盾、条件付きで両立する例、複数資料で満たす例、意図的なPoC省略、対応する要求がある中で追加された制約、基準文書同士の矛盾、画像や参照資料が欠けた例、API失敗を含める。

初版から追うのは、指摘の有用性、見逃し、要求抽出の漏れ、人の確認時間、処理全体の時間・コスト。Jev単体の応答速度や「完了」の件数だけで評価しない。

## 参考一次資料

S1. OpenAI Codex SDK（公式ページ。取得時にChatGPT Learnへリダイレクト）
https://developers.openai.com/codex/sdk

S2. OpenAI Codex TypeScript SDK README
https://github.com/openai/codex/blob/main/sdk/typescript/README.md

S3. OpenAI Codex TypeScript ThreadOptions
https://github.com/openai/codex/blob/main/sdk/typescript/src/threadOptions.ts

S4. OpenAI Codex TypeScript TurnOptions
https://github.com/openai/codex/blob/main/sdk/typescript/src/turnOptions.ts

S5. chez-shanpu/typesafeai-go（作者の公開Go実装。TypeSafe公式SDKではない）
https://github.com/chez-shanpu/typesafeai-go

S6. kataras/jev（作者の公開Go実装。TypeSafe公式SDKではない）
https://github.com/kataras/jev

補足: Jev適性の検討には、会話中で確認したmizchi/jev-playgroundのfit.md、when-to-use.md、tuning.mdを参照した。そこに記載された実測は作者の実験条件における結果であり、本アプリでの性能を保証しない。
