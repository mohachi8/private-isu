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
| [09-template-precompile.md](./09-template-precompile.md) | テンプレートのプリコンパイル（起動時に1回パース）= Phase 6 |
| [10-mysql-tuning.md](./10-mysql-tuning.md) | MySQL 設定チューニング（buffer pool / fsync / binlog）= Phase 7 |
| [11-measurement-session.md](./11-measurement-session.md) | 計測ツール4種（alp/pt-query-digest/pprof/Jaeger）の実践と次の打ち手 |
| [12-infra-tuning-keepalive-pool.md](./12-infra-tuning-keepalive-pool.md) | インフラ層チューニング（keepalive / DBプール / カーネル）= Phase 8 |
| [13-remove-imgdata-blob-and-disk.md](./13-remove-imgdata-blob-and-disk.md) | 根本対策：画像BLOBをDBから排除・ディスク満杯解消 = Phase 9 |
| [14-user-cache.md](./14-user-cache.md) | ユーザーの全件インメモリキャッシュ = Phase 10 |
| [15-comment-cache-and-upload-race.md](./15-comment-cache-and-upload-race.md) | コメント全件キャッシュ & 画像アップロード競合修正 = Phase 11 |
| [16-template-precompute-fields.md](./16-template-precompute-fields.md) | テンプレートの reflection 呼び出しを事前計算で排除 = Phase 12 |
| [17-html-fragment-cache.md](./17-html-fragment-cache.md) | 投稿HTML断片のキャッシュ（CSRFはプレースホルダ置換）= Phase 13 |
| [18-pprof-labels-and-user-stats.md](./18-pprof-labels-and-user-stats.md) | pprofエンドポイントラベル & ユーザーページ集計のメモリ化 = Phase 14 |
| [19-cookie-sessions.md](./19-cookie-sessions.md) | セッションを memcached から署名付き Cookie へ = Phase 15 |
| [20-posts-cache.md](./20-posts-cache.md) | posts メタデータの全件メモリ化 = Phase 16 |
| [21-detail-cache-and-json-session.md](./21-detail-cache-and-json-session.md) | 詳細ページHTMLキャッシュ / Cookieセッション脱gob = Phase 17・18 |
| [22-scoring-mode-ab.md](./22-scoring-mode-ab.md) | 計測モード/本番スコアモードの切替（最終A/B）= Phase 19 |

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
| Phase 6: テンプレートのプリコンパイル | **149,974** | 毎回の ParseFiles を廃止 |
| Phase 7: MySQL 設定チューニング | **152,889** | buffer pool 1G / fsync 緩和 / binlog 無効 |
| 計測セッション（確定値）| **155,417** | 4ツールでボトルネック再特定（[11](./11-measurement-session.md)）|
| Phase 8: インフラ層チューニング | **166,173** | keepalive/DBプールで接続churn激減 |
| Phase 9: 画像BLOBをDBから排除 | **172,648** | posts 1.5GB→7MB（全DBがメモリに収容）/ ローカルはCPU頭打ち |
| Phase 10: ユーザー全件キャッシュ | **201,324** | SELECT users(28%)と毎回のユーザー取得を排除 |
| Phase 11: コメント全件キャッシュ + アップロード競合修正 | **246,398** | makePostsのDBクエリ2→0本 / fail 0で安定 |
| Phase 12: テンプレート reflection 呼び出し排除 | **250,204** | imageURL/Formatを事前計算しフィールド参照に |
| Phase 13: 投稿HTML断片キャッシュ | **270,090** | 一覧描画を連結+CSRF置換に（CPU最大要素を削減）|
| Phase 14: ユーザー集計メモリ化 + pprofラベル | **276,948** | /@user のDB集計を0本に・エンドポイント別計測 |
| Phase 15: セッションをCookie化 | **287,588** | 全リクエストのmemcached往復を排除 |
| Phase 16: posts全件メモリ化 | **327,806** | 読み取りパスが完全にインメモリ化（DBは書き込みのみ）|
| Phase 17: 詳細HTMLキャッシュ + txn撤去 | **330,056** | /posts/:id をキャッシュ・POST /簡素化 |
| Phase 18: Cookieセッション脱gob(JSON化) | **333,901** | 全リクエストのgob再コンパイルを排除 |
| Phase 19: 本番スコアモード(ログ/ラベルOFF) | **341,482** | 計測オーバーヘッドを落として最終計測 |

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

## 完了したフェーズ一覧

| フェーズ | 内容 | doc |
|---|---|---|
| インデックス追加 | comments/posts への索引 | [03](./03-indexes.md) |
| Phase 2 | 画像の静的配信化（nginx）| [05](./05-static-image-serving.md) |
| Phase 3/4 | makePosts の N+1 解消・LIMIT・interpolateParams | [06](./06-makeposts-n1-and-limit.md) |
| Phase 1 | パスワードハッシュ Go ネイティブ化 | [07](./07-native-password-hash.md) |
| Phase 5 | 静的ファイル nginx 直配信 | [08](./08-static-assets-nginx.md) |
| Phase 6 | テンプレートのプリコンパイル | [09](./09-template-precompile.md) |
| Phase 7 | MySQL 設定チューニング | [10](./10-mysql-tuning.md) |
| Phase 8 | インフラ層（keepalive / DBプール / カーネル）| [12](./12-infra-tuning-keepalive-pool.md) |
| Phase 9 | 画像BLOBをDBから排除（posts 1.5GB→7MB）| [13](./13-remove-imgdata-blob-and-disk.md) |
| Phase 10 | ユーザー全件インメモリキャッシュ | [14](./14-user-cache.md) |
| Phase 11 | コメント全件キャッシュ + アップロード競合修正 | [15](./15-comment-cache-and-upload-race.md) |
| Phase 12 | テンプレート reflection 呼び出し排除 | [16](./16-template-precompute-fields.md) |

## 計測上の注意（重要）

**ローカル（このEC2）はベンチマーカーが同一2コア上で動くため CPU を奪い合い、スコアが頭打ち・ばらつく**（[12](./12-infra-tuning-keepalive-pool.md)）。
正確なスコアは **ベンチマーカーを別マシンで動かす本番計測環境**で測ること。ローカルは相対指標（pt-query-digest / pprof の内訳）でボトルネック特定に使う。

## まだ伸ばせそうな箇所（今後の候補）

現状、DB は `makePosts` で 0 クエリ（user/comment はキャッシュ）。残るアプリ CPU の最大要素は **`html/template` の描画（~50%）**。

- **レンダリング結果のキャッシュ**（最有力・要設計）: インデックスの投稿リストや投稿カードの HTML 断片をキャッシュ。CSRF トークン・ログイン状態の扱いが論点。
- **投稿一覧（newest posts）のメモリ化**: `GET /` の posts SELECT もキャッシュ可能（新規投稿時のみ更新）。
- **レスポンス圧縮 / ペイロード削減**。
