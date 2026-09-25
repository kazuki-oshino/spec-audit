# specaudit

PRDとMarkdown設計資料を照合し、要求の反映、不足、矛盾、追加挙動の候補をMarkdownで報告するCLIです。判定は人が確認するための材料で、実装や文書の正しさを保証しません。

## 準備

- Go 1.24以降、Node.js 18以降、npm、`just`
- Codex SDKが利用できる認証状態
- `TYPESAFE_API_KEY` 環境変数（Jev用）

```sh
npm ci
just check
just build
```

認証情報は設定ファイルに書かず、実行環境から渡してください。監査対象の原文、応答、レポートは出力先の`.specaudit/`に保存されます。このディレクトリはGitから除外しています。

## 監査

公開可能なダミー文書を使う設定例は[examples/sample/audit.yaml](examples/sample/audit.yaml)です。実際の文書ではパスを対象のMarkdownに変更します。

```sh
just run examples/sample/audit.yaml
just replay examples/sample/.specaudit/runs/<run-id>
```

`just run`はbridgeとCLIをビルドしてから監査します。結果の`report.md`のパスを標準出力に表示します。終了コードは0が処理完了、1が設定・入力エラー、2がpartial report、130が中断です。指摘の有無だけでは終了コードは変わりません。

設定ファイルの相対パスは設定ファイルのあるディレクトリから解決します。`baseline`と`design`にはローカルMarkdownのファイルまたはglobを指定します。`context`は用語などの参考資料です。Codexは`gpt-6-sol`・reasoning `medium`に固定しています。Jevモデルは例では`jev-latest`で、再現性が必要な場合は固定版を指定します。

設計と既知の制約は[docs/specaudit-design.md](docs/specaudit-design.md)を参照してください。現時点のテストはモックを用いた通し確認であり、実モデルの監査精度は未評価です。
