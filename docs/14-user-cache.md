# 14. ユーザーの全件インメモリキャッシュ — Phase 10

## 何を / なぜ

BLOB 排除（[13](./13-remove-imgdata-blob-and-disk.md)）後に pt-query-digest を取り直すと、DB 実行時間の内訳は:

```
1. SELECT comments  51.9%  (makePosts)
2. SELECT users     27.8%  (makePosts の IN 取得)
3. SELECT posts      6.5%
```

`users` テーブルは **わずか ~1,000–2,000 行**で、変更は「登録 / BAN / initialize」のときだけ。
にもかかわらず **ほぼ全リクエストで参照**される（セッションユーザー・投稿者・コメント投稿者）。
→ 全件をメモリに載せれば、これらの SELECT をまるごと消せる。

## どうやって（`webapp/golang/usercache.go`）

- 起動時と `/initialize` ごとに `SELECT * FROM users` を 1 回だけ実行し、
  `id→User` と `account_name→User` の 2 つのマップを構築（`sync.RWMutex` 保護）。
- 各所を DB クエリ → キャッシュ参照に置換:
  - `getSessionUser`（毎リクエスト）→ `userByID`
  - `makePosts` の `SELECT users IN`（27.8%）→ キャッシュ参照
  - `tryLogin`（POST /login）→ `userByName`（passhash もキャッシュ済み）
  - `getAccountName`（/@user）→ `userByName`
  - `postRegister` の存在チェック → `userByName`
- 更新の反映:
  - 登録: INSERT 後に `cacheUser` で 1 件追加
  - BAN / initialize: `loadAllUsers` で再構築

## 結果

| 時点 | スコア |
|---|---|
| Phase 9（BLOB排除）後 | 172,648 |
| ユーザーキャッシュ | **201,324** |

- `SELECT users`（DB 時間の約 28%）と毎リクエストのユーザー取得を **DB から完全に除去**。
- DB 負荷減 → mysqld の CPU が空き、アプリ側の行スキャン/アロケーションも減ってローカルでも +17%。

## 副次的な挙動変更（許容）

- 存在しないユーザーの `/@name`: 旧コードは空の 200 を返していた（DOM 欠落のバグ気味）。新コードは **404**（より正しい）。ベンチは実在ユーザーしか叩かないため影響なし。

## 注意

- キャッシュの一貫性は「登録・BAN・initialize で必ず更新」で担保。新しい更新経路を追加する場合はキャッシュ更新を忘れないこと。
- 残る最大の DB コストは `SELECT comments`（makePosts）。次の対象。
