# private-isu 改善作業ドキュメント

このディレクトリは、ISUCON 練習用アプリ **private-isu** をパフォーマンス改善していく過程の作業記録です。
「何を / なぜ / どうやって / 結果」が後から追えるように、初心者でも読めるレベルで日本語でまとめています。

- 対象アプリ: private-isu（GitHub の元ネタは [catatsuy/private-isu](https://github.com/catatsuy/private-isu)）
- 計測対象の実装: **Go 実装**（`webapp/golang/app.go`）
- 作業環境: AWS EC2 上（このリポジトリは EC2 上で直接編集している）
- フォーク管理: https://github.com/mohachi8/private-isu の `main` ブランチ

---

## 目次

| ドキュメント | 内容 |
|---|---|
| [01-setup-github-and-environment.md](./01-setup-github-and-environment.md) | GitHub のセットアップと実行環境の全体像 |
| [02-measurement-stack.md](./02-measurement-stack.md) | 計測スタック（alp / pt-query-digest / スロークエリログ）= 三種の神器 |
| [03-indexes.md](./03-indexes.md) | インデックス追加によるクイックウィン（最初のスコア改善） |
| [04-profiling-pprof-jaeger.md](./04-profiling-pprof-jaeger.md) | Go の詳細プロファイリング（pprof）と分散トレーシング（Jaeger / OpenTelemetry）|
| [05-static-image-serving.md](./05-static-image-serving.md) | 画像の静的配信化（nginx で /image/* を返す）= Phase 2 |
| [06-makeposts-n1-and-limit.md](./06-makeposts-n1-and-limit.md) | makePosts の N+1 解消・クエリ LIMIT 化・interpolateParams = Phase 3/4 |
| [07-native-password-hash.md](./07-native-password-hash.md) | パスワードハッシュを Go ネイティブ化（openssl 起動を廃止）= Phase 1 |
| [08-static-assets-nginx.md](./08-static-assets-nginx.md) | 静的ファイル（css/js/favicon）を nginx 直配信 = Phase 5 |

---

## アプリ構成（全体像）

```
[ベンチマーカー] --HTTP--> nginx(:80) --proxy--> Go アプリ(:8080)
                                                    |
                                  +-----------------+------------------+
                                  |                                    |
                            MySQL 8.0                          memcached(セッション)
```

- **nginx**: ポート 80 で受けて `http://localhost:8080`（Go アプリ）へリバースプロキシ（`infra/nginx/sites-available/isucon.conf`）。
- **Go アプリ**: `webapp/golang/app.go`。`http.ListenAndServe(":8080", ...)` で起動。systemd の `isu-go.service` で管理。
- **MySQL 8.0**: アプリのデータ（posts / comments / users など）を保持。画像も `posts.imgdata`（BLOB）に格納されている。
- **memcached**: ログインセッションの保存に使用（`gorilla-sessions-memcache`）。
- 環境変数: `/home/isucon/env.sh`（DB ユーザー名 / パスワード / DB 名）。

### Ruby → Go への切り替え（実施済み）
初期状態では Ruby 実装が動いていたため、Go 実装で計測できるよう以下で切り替え済み。

```bash
sudo systemctl stop isu-ruby
sudo systemctl disable isu-ruby
sudo systemctl enable isu-go
sudo systemctl start isu-go
```

---

## 基本の作業ループ

改善は次のサイクルを回して進めます。

1. **コードや設定を変更する**（`webapp/golang/*.go`、`infra/` 配下など）
2. **ビルド & デプロイ**: `webapp/golang` で `make`（= `go build -o app`）→ `sudo systemctl restart isu-go`
3. **ベンチマーク実行**（約 1 分）:
   ```bash
   cd benchmarker && ./bin/benchmarker -t http://localhost -u ./userdata
   ```
4. **計測結果を解析**（alp / pt-query-digest）してボトルネックを特定
5. 1 に戻る

> 上記の手順は `bin/` 配下のスクリプトでワンコマンド化しています（後述）。

### スコア計算式
ベンチマーカーのスコアは概ね次の式です。

```
スコア = 成功GET × 1 + 成功POST × 2 + 画像投稿 × 5
        − (サーバエラー × 10 + リクエスト失敗 × 20)
```

**失格条件**:
- `GET /initialize` が 10 秒を超える
- 必須の DOM 要素が欠落している（レスポンス HTML が壊れている）

つまり「速く・大量に・エラーなく」捌くほど高得点。失敗はペナルティが重いので、まず安定して捌けることが重要です。

---

## 進捗スコア表

| 時点 | スコア | 備考 |
|---|---|---|
| 初期（Go, 改善前） | **0** | タイムアウト多発で点が積み上がらない |
| Phase 1: インデックス追加後 | **16,447** | fail 0（失敗ゼロで安定）|
| Phase 2: 画像静的配信後（ウォーム）| **25,912** | `/image/*` を nginx が直接配信 |
| Phase 3/4: N+1解消・LIMIT・interpolateParams | **100,458** | DB総実行時間 445s→70s |
| Phase 1: パスワードハッシュ Go ネイティブ化 | **128,763** | openssl 外部起動を廃止 |
| Phase 5: 静的ファイル nginx 直配信 | **141,992** | css/js/favicon をアプリ未経由に |

GET / の応答時間: **約 1.5 秒 → 約 0.07 秒**（インデックス追加の効果）。
`/image/*` 応答時間合計: **約 154 秒 → 約 13 秒**（nginx 静的配信の効果）。

---

## 主要な意思決定

### 1. EC2 上で作業する（ローカルクローンではなく）
- **理由**: ベンチマーク実行・サービス再起動・実機での計測は EC2 上でしか行えない。改善ループ（変更→ベンチ→計測）が回るのは EC2 だけ。
- ローカル環境は「閲覧」「バックアップ」用途と割り切る。

### 2. git worktree は使わない
- **理由**: MySQL / nginx / Go アプリは単一の共有インフラで、ポートも固定（80 / 8080 / 3306）。ベンチマークも逐次実行が必須なので、worktree で作業ツリーを並列化してもベンチを同時に回せず、並列化の利点が薄い。
- 代わりに、**互いに競合しない独立作業（ドキュメント作成など）にサブエージェントを使う**方針。

### 3. インフラ設定とスクリプトをリポジトリで管理する
- nginx / MySQL の設定（`infra/`）と運用スクリプト（`bin/`）を Git 管理下に置く。
- **理由**: 再現性（誰でも同じ状態を再構築できる）と差分管理（何をいつ変えたかが履歴で追える）。

---

## bin/ スクリプトの使い方

`bin/` には改善ループを回すための運用スクリプトを置いています。すべて EC2 上で実行します。

| スクリプト | 役割 | 使い方 |
|---|---|---|
| `bin/deploy.sh` | Go アプリをビルドして再起動 | `bin/deploy.sh` |
| `bin/bench.sh` | ログを回転させてベンチマーク実行 | `bin/bench.sh [TARGET]`（既定 `http://localhost`）|
| `bin/alp.sh` | nginx アクセスログを alp で解析 | `bin/alp.sh` |
| `bin/slowlog.sh` | MySQL スロークエリログを pt-query-digest で解析 | `bin/slowlog.sh` |
| `bin/apply-infra.sh` | `infra/` の設定をシステムへ反映＆再起動 | `bin/apply-infra.sh` |

### 典型的な 1 サイクル

```bash
# 1) コードを編集したら、ビルド & 再起動
bin/deploy.sh

# 2) ログを初期化してベンチマークを実行（約1分）
bin/bench.sh

# 3) ボトルネックを解析
bin/alp.sh       # どのエンドポイントが遅い/重いか（応答時間合計順）
bin/slowlog.sh   # どのクエリが遅い/回数が多いか
```

`infra/` 配下（nginx / MySQL 設定）を変更したときだけ、追加で次を実行します。

```bash
bin/apply-infra.sh   # infra/ の設定を /etc/... へ配置してサービス再起動
```

各スクリプトの詳細は [02-measurement-stack.md](./02-measurement-stack.md) を参照。

---

## 今後の予定（次のボトルネック）

計測で判明済みの主なボトルネックと改善方針。

- **GET / の `SELECT posts`**: LIMIT なしで全 1 万件を取得し、1 回あたり約 2 万行を走査。pt-query-digest で全体の **約 46%** を占める最大の負荷源。
- **画像配信が DB の BLOB をアプリ経由で返している**: alp 集計で `/image/*` の応答時間合計が **約 154 秒**。nginx の静的配信に切り出す予定。
- **ユーザー取得の N+1**: `SELECT * FROM users WHERE id = ?` が約 **4.3 万回** 実行されている（投稿・コメントごとに 1 回ずつ）。
- **プリペアドステートメントのオーバーヘッド**: DSN に `interpolateParams=true` を追加して往復を削減できる。

### フェーズ計画

| フェーズ | 内容 | 状態 |
|---|---|---|
| インデックス追加 | comments/posts への索引（[03](./03-indexes.md)）| ✅ 完了 |
| Phase 2 | 画像の静的配信化（[05](./05-static-image-serving.md)）| ✅ 完了 |
| Phase 3 | makePosts の N+1 解消（[06](./06-makeposts-n1-and-limit.md)）| ✅ 完了 |
| Phase 4 | クエリ LIMIT・interpolateParams（[06](./06-makeposts-n1-and-limit.md)）| ✅ 完了 |
| Phase 1 | パスワードハッシュ Go ネイティブ化（[07](./07-native-password-hash.md)）| ✅ 完了 |

### まだ伸ばせそうな箇所（今後の候補）

- **静的ファイル（css/js/favicon）を nginx で直接配信**: 現在は Go の `FileServer` 経由（`location /` → アプリ）。alp で各ファイル ~21 秒。nginx の `location` で直接返せば削減可能。
- **`GET /` のさらなる最適化**: 現在アプリが CPU 首位（41%）。テンプレートのプリコンパイル（毎回 `ParseFiles` している）や、コメント数のキャッシュなど。
- **memcached 活用**: 投稿一覧やコメント数のキャッシュ。
- **MySQL 設定チューニング**: `innodb_buffer_pool_size` など。
