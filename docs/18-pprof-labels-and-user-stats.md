# 18. pprofエンドポイントラベル & ユーザーページ集計のメモリ化 — Phase 14

## A. pprof エンドポイントラベル（客観性）

アプリ全体の CPU 集計では「どのエンドポイントが `template.Execute` を食うか」が分からない。
`runtime/pprof.Do` でリクエストごとに `endpoint` ラベルを付与するミドルウェアを追加。

```go
r.Use(pprofLabelMiddleware) // pprof.Do(ctx, pprof.Labels("endpoint", method+path), ...)
```

> 注: chi の `r.Use` ミドルウェアはルーティング前に走るため `RoutePattern()` は空。`r.URL.Path` を
> 正規化（`/posts/:id`, `/@:name`, `/image/:id`）してラベルにした。

確認:
```bash
go tool pprof -tags cpu.pprof   # endpoint 別の CPU 内訳
```

### 計測結果（エンドポイント別 CPU）
```
GET /            21.3%
POST /           12.8%   (画像投稿: ファイル書き込みが主)
GET /@:name      10.2%   (ユーザーページ: DB集計4本)  ← B で対処
GET /posts/:id    8.7%
POST /login       3.9%
GET /posts        2.9%
```

## B. ユーザーページ集計のメモリ化（`statscache.go`）

`/@user` は `postCount` / `commentCount(本人のコメント)` / `commentedCount(本人投稿への被コメント)` を
**3〜4本の COUNT/SELECT で DB から**取っていた。これらをメモリ化:

- `postIDsByUser map[int][]int`、`commentCountByUser map[int]int` を起動時/initialize に構築。
- `userStats(uid)`:
  - `postCount` = `len(postIDsByUser[uid])`
  - `commentCount` = `commentCountByUser[uid]`
  - `commentedCount` = ユーザーの各投稿の `len(commentsByPostID[pid])` の合計（コメントキャッシュから）
- 更新: 投稿時 `statAddPost`、コメント時 `statAddComment`、initialize で再構築。

`/@user` の集計クエリ（最大4本）を **0 本**に（表示用の posts 一覧 SELECT のみ残す）。

### 正当性
`/@mary` の表示値（12 / 101 / 124）が DB の正解値と完全一致を確認。

## 結果

| 時点 | スコア | fail |
|---|---|---|
| Phase 13（断片キャッシュ）| 270,090 | 0 |
| pprofラベル + ユーザー集計メモリ化 | **276,948** | 0 |

> pprof ラベルは毎リクエストで `pprof.Do`（小さなオーバーヘッド）が走るが、スコアは上昇したので維持。
> 最終スコア計測で切りたい場合は env でゲートする余地あり。
