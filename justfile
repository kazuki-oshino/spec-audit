default:
  just --list

# Goソースを整形
fmt:
  go fmt ./...

# Goテストを実行
test-go:
  go test ./...

# Codex bridgeをビルド
build-bridge:
  npm run build:bridge

# Codex bridgeのテストを実行
test-bridge:
  npm run test:bridge

# すべてのテストを実行
test: test-go test-bridge

# ローカルCLIとbridgeをビルド
build: build-bridge
  go build -o specaudit ./cmd/specaudit

# 設定ファイルを指定して監査を実行
run config="audit.yaml": build
  ./specaudit run --config "{{config}}"

# 保存済み結果からレポートを再生成
replay run_dir:
  go run ./cmd/specaudit replay --run "{{run_dir}}"

# 差分の空白エラーを確認
check: test
  git diff --check
