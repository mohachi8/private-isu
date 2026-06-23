# 01. GitHub のセットアップと実行環境

このドキュメントでは「なぜ EC2 上で Git 管理を始めたのか」と「どうやって GitHub に push できる状態にしたのか」、そして実行環境の全体像をまとめます。

---

## なぜ GitHub 管理するのか

ISUCON の改善作業は「設定やコードを少しずつ変えて、ベンチで効果を測る」の繰り返しです。
変更履歴を残さないと、

- どの変更でスコアが上がった / 下がったのか分からない
- 壊したときに前の状態へ戻せない
- 別マシン / 別メンバーへ同じ環境を再現できない

という問題が起きます。そこで **EC2 上のリポジトリを Git 管理し、GitHub（フォーク）へ push** できる状態を整えました。

---

## 実行環境の全体像

| 要素 | 内容 |
|---|---|
| 場所 | AWS EC2 インスタンス上 |
| 作業ユーザー | `isucon` |
| アプリ本体 | `/home/isucon/private_isu/webapp/golang/app.go`（Go 実装）|
| Web サーバ | nginx（ポート 80）→ Go アプリ（ポート 8080）へリバースプロキシ |
| DB | MySQL 8.0 |
| セッション | memcached |
| プロセス管理 | systemd（`isu-go.service`）|
| 環境変数 | `/home/isucon/env.sh`（`ISUCONP_DB_USER` / `ISUCONP_DB_PASSWORD` / `ISUCONP_DB_NAME`）|

### Ruby から Go への切り替え（実施済み）
private-isu は複数言語の実装を同梱しています。初期は Ruby 実装が稼働していたため、計測対象を Go 実装へ切り替えました。

```bash
sudo systemctl stop isu-ruby
sudo systemctl disable isu-ruby   # 起動時に Ruby が立ち上がらないようにする
sudo systemctl enable isu-go      # 起動時に Go を立ち上げる
sudo systemctl start isu-go
```

---

## 作業手順の記録

### 1. リポジトリ所有者の修正

EC2 上の `.git` ディレクトリが **root 所有**になっていて、`isucon` ユーザーでは Git 操作ができませんでした。所有者を `isucon` へ変更して解決。

```bash
sudo chown -R isucon:isucon /home/isucon/private_isu/.git
```

### 2. `.gitignore` の作成

private-isu には**ベンチマーク用の画像データ（約 1.2GB）**やビルド成果物・依存パッケージが含まれており、そのまま GitHub に上げると巨大かつ無意味です。ルートに `.gitignore` を作成して以下を除外しました。

- ベンチマーク用データ: `benchmarker/userdata/img/`、`img.zip`、`dump.sql(.bz2)` など（約 1.2GB）
- ビルド成果物 / 依存:
  - Go: `webapp/golang/app`（ビルド済みバイナリ）
  - Node: `node_modules/`、`dist/`
  - Python: `.venv/`、`__pycache__/`
  - PHP: `vendor/`
  - Ruby: `vendor/`、`.bundle/`
- その他: `.DS_Store`、`*.log`、`*.test`

この状態で **初回コミットは 113 ファイル**になりました（巨大データを除いたアプリ本体と設定のみ）。

### 3. GitHub CLI（gh）の導入と認証

push のたびにトークンを手入力するのは面倒なので、GitHub 公式の CLI を導入して認証を一度だけ済ませました。

```bash
# 公式 apt リポジトリから gh CLI 2.95.0 を導入
# （導入後）アカウント mohachi8 で対話ログイン
gh auth login

# 以降、git の push/pull で gh の認証情報を使う（トークン手入力が不要に）
gh auth setup-git
```

さらに、新しいブランチを push するときに `--set-upstream` を毎回打たなくて済むよう設定。

```bash
git config --global push.autoSetupRemote true
```

---

## 主要な意思決定（この章に関係するもの）

### EC2 上で作業する（ローカルクローンではない）
- ベンチマーク実行・サービス再起動・実機計測は **EC2 でしか行えない**。改善ループが回るのは EC2 だけなので、編集も EC2 上で行う。
- ローカルは閲覧 / バックアップ用途。

### git worktree は使わない
- MySQL / nginx / Go アプリは単一の共有インフラ・固定ポートで動作し、ベンチは逐次実行が必須。worktree で作業ツリーを分けても並列にベンチを回せないため利点が薄い。
- 代わりに、競合しない独立作業（このドキュメント作成など）にサブエージェントを活用する。

---

## まとめ

- root 所有だった `.git` を `isucon` 所有へ修正し、Git 操作可能に。
- 巨大データ / 生成物を除外する `.gitignore` を作成（初回 113 ファイル）。
- gh CLI で認証し、`gh auth setup-git` でトークン手入力を不要化。
- 計測対象は Go 実装。EC2 上で改善ループを回す方針を確立。
