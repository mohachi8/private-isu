# 03. インデックス追加によるクイックウィン

最初の改善として、**MySQL のインデックスを追加**しました。コードはほぼ触らず、DB に索引を足すだけで効く「費用対効果の高い一手（クイックウィン）」です。
結果として **スコア 0（タイムアウト多発）→ 16,447（fail 0）** という大きな改善になりました。

---

## なぜインデックスなのか

### 状況
- 初期状態の Go 実装はベンチ中に**タイムアウトが多発**し、スコアが積み上がらず **0** でした。
- 計測（pt-query-digest / alp）すると、トップページ `GET /` が**約 1.5 秒**かかっており、その中で `comments` テーブルへのアクセスが大量に発生していました。

### 原因: `comments` にインデックスが無く、毎回フルスキャン
トップページの描画は `makePosts`（`webapp/golang/app.go`）が担っており、**投稿 1 件ごとに `comments` テーブルへ「コメント件数（COUNT）」と「最新コメント取得」を問い合わせ**ています。

```go
// app.go（抜粋・要約）
// 投稿ごとにコメント件数を数える
db.GetContext(ctx, &commentCount,
    "SELECT COUNT(*) FROM `comments` WHERE `post_id` = ?", p.ID)

// 投稿ごとに最新コメントを取得（created_at 降順、最大3件）
query := "SELECT * FROM `comments` WHERE `post_id` = ? ORDER BY `created_at` DESC"
if !allComments { query += " LIMIT 3" }
db.SelectContext(ctx, &comments, query, p.ID)
```

`comments` は**約 10 万行**あるのに `post_id` にインデックスがありませんでした。
そのため、これらのクエリは**毎回テーブル全体（10 万行）を走査（フルスキャン）**します。投稿の数だけこれが繰り返されるので、トップページが致命的に遅くなり、タイムアウトしていました。

> インデックスは本の「索引」と同じです。索引が無ければ「`post_id` が一致する行」を探すのに全ページをめくる必要がありますが、索引があれば一発でたどり着けます。

---

## 何をしたか（追加したインデックス）

`infra/mysql/migrations/001_add_indexes.sql` で 4 本のインデックスを追加しました。

```sql
-- comments を post_id で引く（件数 + 最新3件を created_at 順で取得）
ALTER TABLE comments ADD INDEX idx_post_id_created_at (post_id, created_at);

-- ユーザーページでコメント数を user_id で数える
ALTER TABLE comments ADD INDEX idx_user_id (user_id);

-- ユーザーごとの投稿一覧を created_at 順で出す
ALTER TABLE posts ADD INDEX idx_user_id_created_at (user_id, created_at);

-- トップページで全投稿を created_at 順に並べる
ALTER TABLE posts ADD INDEX idx_created_at (created_at);
```

### 各インデックスの狙い

| インデックス | 効くクエリ |
|---|---|
| `comments(post_id, created_at)` | `WHERE post_id = ? ORDER BY created_at DESC LIMIT 3` と `COUNT(*) WHERE post_id = ?`。**複合インデックス**なので「絞り込み（post_id）」と「並べ替え（created_at）」の両方を 1 本でカバー |
| `comments(user_id)` | ユーザーページの `COUNT(*) ... WHERE user_id = ?` |
| `posts(user_id, created_at)` | ユーザーページの `WHERE user_id = ? ORDER BY created_at DESC` |
| `posts(created_at)` | トップページ等の `ORDER BY created_at DESC` |

### `post_id, created_at` を複合にした理由
クエリは「`post_id` で絞り込んでから `created_at` で並べ替え」をします。`(post_id, created_at)` の複合インデックスにすると、**絞り込んだ結果がすでに created_at 順に並んでいる**ため、MySQL は追加のソート（filesort）をせずに最新 3 件をそのまま取れます。

---

## どうやって適用したか

インデックスはマイグレーション SQL として `infra/` に置き、実 DB へ流して適用します。

```bash
mysql -u isuconp -pisuconp isuconp < infra/mysql/migrations/001_add_indexes.sql
```

> **重要**: これらのインデックスは `GET /initialize`（ベンチ開始時の初期化）では再作成されません。`/initialize` は行を DELETE するだけでスキーマは変えないため、**一度適用すればベンチ実行をまたいで残り続けます**（SQL ファイル冒頭のコメントにも明記）。

---

## 結果

| 指標 | 改善前 | 改善後 |
|---|---|---|
| スコア | **0**（タイムアウト多発）| **16,447**（fail 0）|
| `GET /` 応答時間 | 約 **1.5 秒** | 約 **0.07 秒** |

- フルスキャンが消えたことで `GET /` が一気に高速化し、タイムアウトが解消。
- 失敗（fail）が **0** になり、スコアがそのまま積み上がるようになりました。

---

## 学び

- まず計測して「どのクエリが・なぜ遅いか」を特定してから手を打つ（推測で索引を貼らない）。
- **N+1 的に何度も呼ばれるクエリ**にインデックスが無いと、テーブルが大きいほど致命的。
- 「絞り込み列 + 並べ替え列」の**複合インデックス**は、WHERE と ORDER BY の両方を一度に効かせられる強力な手。

---

## 次の一手へ

インデックスでタイムアウトは解消しましたが、計測ではまだ大きなボトルネックが残っています。

- `GET /` の `SELECT posts`（LIMIT 無しで全件取得）が pt-query-digest 全体の約 46%
- 画像配信が DB の BLOB をアプリ経由で返している（alp で合計約 154 秒）
- ユーザー取得の N+1（`SELECT users` が約 4.3 万回）

詳細と計画は [README.md](./README.md) の「今後の予定」を参照してください。
