# 20. posts メタデータの全件メモリ化 — Phase 16

## 何を / なぜ

ユーザー/コメントキャッシュ後もホットパスに残っていた DB アクセスは「posts の SELECT」だけだった:
- `GET /`: `posts ORDER BY created_at DESC LIMIT ?`
- `GET /posts`: `posts WHERE created_at <= ? ...`
- `GET /@name`: `posts WHERE user_id = ? ...`
- `GET /posts/:id`: `posts WHERE id = ?`

BLOB 削除後（[13](./13-remove-imgdata-blob-and-disk.md)）の posts は数 MB なので、全件メモリ化して読み取りを RAM で完結させる。

## どうやって（`webapp/golang/postcache.go`）

- 起動時/initialize に `SELECT id,user_id,body,mime,created_at FROM posts ORDER BY created_at ASC, id ASC` を構築:
  - `postsAsc []Post`（created_at 昇順。**新着は末尾**なので追加が O(1)）
  - `postByID map[int]Post`
  - `postsByUser map[int][]Post`
- 取得 API（すべてコピーを返す）:
  - `newestPosts(limit)`: 末尾から limit 件（created_at DESC, id DESC）
  - `postsBefore(t, limit)`: `created_at <= t` を二分探索（`sort.Search`）して DESC で limit 件
  - `userPosts(uid, limit)`: ユーザーの新着 limit 件
  - `postByIDCache(id)`
- 新規投稿: `addPostToCache`。**画像ファイル生成 → DB commit → cache 追加** の順を厳守（cache 追加で初めて一覧に出る＝画像が必ず先に存在）。

### ページネーションの正確性（重要）
- 順序は `created_at DESC, id DESC` で統一。これは MySQL の `idx_created_at` バックワードスキャン（created_at, id の複合）と一致するので、`max_created_at` ページングが元実装と同じ切れ方になる。
- 新規投稿の `created_at` は **秒に切り詰め**て保持（MySQL TIMESTAMP は秒精度）。表示用 `CreatedAtISO`（秒）と二分探索のキーが一致し、境界ズレ（自分の投稿が次ページで消える等）を防ぐ。
- 検証: page1 末尾の created_at で `/posts?max_created_at=` を叩くと page2 が 20 件返り、境界の `<=` も元実装どおり。

## 結果

| 時点 | スコア | fail |
|---|---|---|
| Phase 15（Cookieセッション）| 287,588 | 0 |
| posts 全件メモリ化 | **327,806** | 0 |

- 一覧/詳細の **読み取りパスが完全にインメモリ化**（users / comments / posts / stats / HTML 断片すべて）。DB は INSERT（投稿/コメント/登録/BAN）のみ。
- +14%。
