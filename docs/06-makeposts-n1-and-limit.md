# 06. makePosts の N+1 解消とクエリの LIMIT 化 — Phase 3 / 4

## 何を / なぜ

Phase 2 後の最大ボトルネックは `GET /`（合計約 340 秒, 平均 0.42 秒）と `GET /posts`（約 110 秒）。
どちらも `makePosts` を通り、原因は2つ:

1. **N+1 クエリ**: 投稿ごとに「コメント数 COUNT」「コメント取得」「コメント投稿者の取得（さらにループ）」「投稿者の取得」を個別実行。1ページ表示で約 120 クエリ、ベンチ全体で約 10 万クエリに膨張。
2. **`GET /` が全件取得**: `SELECT ... FROM posts ORDER BY created_at DESC` が **LIMIT なし**で全 1 万件を取得（pt-query-digest で全体の約 46%）。

## どうやって

### (1) makePosts をバッチ取得（2 クエリ）に
投稿ごとのループをやめ、対象 post_id 群に対して **まとめて 2 回だけ**問い合わせる方式に変更（`sqlx.In` で `IN (?)` を展開）。

- コメント: `SELECT * FROM comments WHERE post_id IN (...) ORDER BY created_at DESC` を 1 回。
  - Go 側で post_id ごとにグルーピングし、コメント数も件数から算出（→ COUNT クエリも不要に）。
- ユーザー: 投稿者 + コメント投稿者の id を集合化し `SELECT * FROM users WHERE id IN (...)` を 1 回。Go 側で id→User のマップを作って割り当て。

投稿数によらず **常に 2 クエリ**。約 120 クエリ/ページ → 2 クエリ/ページ。

### (2) 一覧クエリに LIMIT を付与
`GET /`・`GET /posts`・ユーザーページの投稿一覧に `LIMIT` を付け、必要な分だけ取得。

### (3) del_flg フィルタは JOIN ではなく Go 側で
最初は `posts JOIN users WHERE u.del_flg=0 ... LIMIT 20` としたが、**かえって遅化（0.33 秒）**。
EXPLAIN すると、オプティマイザが users を**フルスキャン + filesort**していた:

```
table: u   type: ALL   Extra: Using where; Using temporary; Using filesort
```

そこで JOIN をやめ、`posts ORDER BY created_at DESC LIMIT 60`（= `postsPerPage * 3` のバッファ）で取得し、**del_flg=1 のユーザーの投稿は makePosts 側で除外**する方式に変更。これは元コードと同じ挙動で、EXPLAIN は理想形に:

```
table: posts   type: index   key: idx_created_at   rows: 20   Extra: Backward index scan
```

> なぜバッファ 3 倍? 削除ユーザー（`id % 50 == 0` の約 2%）の投稿を除いても `postsPerPage`(20) 件を確保するため。新着 60 件読んでも created_at インデックスのバックワードスキャンで一瞬。

### (4) interpolateParams=true（おまけ）
DSN に `interpolateParams=true` を追加。クライアント側でパラメータを埋め込み、クエリごとのサーバ側プリペア往復（pt-query-digest の ADMIN PREPARE, 約 7.5 秒）を削減。

## 結果

| 時点 | スコア |
|---|---|
| Phase 2 後 | 25,912 |
| N+1 解消 + LIMIT + interpolateParams（JOIN 版）| 44,145 |
| JOIN をやめてインデックス活用 | **100,458** |

- `GET /` 平均: 0.42 秒 → **0.005 秒**。
- DB の総実行時間: 約 445 秒 → 約 70 秒。
- スコア推移（通算）: 0 → 16,447 → 25,912 → 44,145 → **100,458**（fail 0）。

## 教訓

- **JOIN が常に速いとは限らない**。インデックスで安く取れる範囲を少し広めに読み、絞り込みをアプリ側でやる方が速いことがある。必ず `EXPLAIN` で確認する。
- N+1 は `IN (?)` でまとめ取りし、対応付けはアプリ側のマップで行うのが定石。

## 次のボトルネック

`POST /login`（alp 合計約 94 秒）が浮上。これはパスワードハッシュを **`openssl` の外部プロセス起動**で計算しているため。次フェーズ（Phase 1 の積み残し）で Go ネイティブの `crypto/sha512` に置換する。
