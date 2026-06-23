# 15. コメント全件キャッシュ & 画像アップロードの競合修正 — Phase 11

## A. コメントの全件インメモリキャッシュ

### 何を / なぜ
ユーザーキャッシュ（[14](./14-user-cache.md)）後、pt-query-digest の DB 実行時間は **`SELECT comments` が 75.2%**（16,583 回）に集中。
`makePosts` が一覧表示のたびにコメント（件数＋最新3件）を取得していた。コメントは ~100k 行 / ~10MB でメモリに載る。

### どうやって（`webapp/golang/commentcache.go`）
- 起動時・`/initialize` ごとに `SELECT * FROM comments ORDER BY created_at` を 1 回だけ実行し、`post_id → []Comment`（古い順）を構築。
- `makePosts` を **DB クエリゼロ**に変更：コメントもユーザーもキャッシュから取得（件数 = `len`、最新3件 = 末尾3件）。
- `postComment`：INSERT 後に `addComment` でキャッシュへ追記（`created_at` は `time.Now()` で近似）。

これで `makePosts` の 2 大クエリ（comments + users）が **完全に消滅**し、一覧ページの DB アクセスは「posts の SELECT 1 本」だけになった。

## B. 画像アップロードの競合バグ修正（fail を 0 に）

### 症状
スループット向上後、ごく稀に `静的ファイルが正しくありません (GET /image/NNNNN.png)`（アップロード画像の内容不一致）が発生。単発アップロードのバイト比較は一致するのに、高負荷時のみ失敗 = **並行時のレース**。

### 原因
`postIndex` が **INSERT（=投稿が GET / 等に即可視化）した後に画像ファイルを書いて**いた。
画像実体は DB から排除済み（[13](./13-remove-imgdata-blob-and-disk.md)）なので、INSERT 〜 ファイル書き込みの**隙間**に別の並行リクエストが
「新着投稿を一覧で見つけて画像を GET」すると、ファイル未生成で不一致になる（DB フォールバックも無い）。

### 修正
**トランザクションで順序を保証**：
```
BEGIN → INSERT posts → writeImageFile（ファイル生成）→ COMMIT（ここで初めて投稿が可視化）
```
他コネクションは COMMIT まで投稿を見られないため、**可視化時点で必ず画像ファイルが存在**する。
（補助的に nginx `/image/` の `open_file_cache off`、`writeImageFile` の temp+rename 化も実施。）

## 結果

| 時点 | スコア | fail |
|---|---|---|
| Phase 10（ユーザーキャッシュ）| 201,324 | 0 |
| コメントキャッシュ（race 顕在化）| ~243,950 | 1〜3 |
| **+ アップロード競合修正** | **246,398** | **0（安定）** |

- `makePosts` の DB クエリが 2 本 → 0 本。`SELECT comments`（DB 時間の 75%）が消滅。
- 一覧系ページの DB アクセスは posts の SELECT のみ。

## 学び

- キャッシュ化で「DB に存在＝即可視」と「ファイル/キャッシュの準備完了」の**順序**がズレると競合バグになる。
  外部状態（ファイル）を伴う場合は、**可視化（commit / cache 追加）を最後に**行う。
- フレーキーなベンチ失敗は、単発の正常動作確認では捕まらない。**並行・高負荷でしか出ない**ことを念頭に。
