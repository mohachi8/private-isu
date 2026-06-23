# 11. 計測セッション（4 ツール実践）と次の打ち手

スコアが頭打ちに近づいたので、4 つの計測ツールを実際に回して現状のボトルネックを再特定した記録。

## 使った 4 ツールと「何が見えるか」

| ツール | 粒度 | コマンド |
|---|---|---|
| alp | エンドポイント単位 | `bin/alp.sh` |
| pt-query-digest | SQL クエリ単位 | `bin/slowlog.sh` |
| pprof | Go 関数単位（CPU）| `go tool pprof -top app cpu.pprof` |
| Jaeger | 1 リクエストの内訳（HTTP→各SQL）| `ENABLE_TRACING=1` + UI/API |

## 結果（スコア 152,889 時点）

### alp（応答時間合計順）
```
GET /            sum 173.6s  avg 0.032s  (5423)
GET /posts/\d+   sum  86.2s  avg 0.028s  (3110)
GET /posts       sum  75.9s  avg 0.051s  (1480)
POST /login      sum  41.2s  avg 0.013s  (3175)
GET /image/...   sum  10.0s  avg 0.000s  (104844)  ← nginx 配信で激安
```

### pt-query-digest（実行時間順）
```
1. SELECT comments        34.2%  (makePosts のコメント取得)
2. SELECT posts WHERE id  21.5%  (getPostsID/getImage。SELECT * で imgdata BLOB まで取得)
3. SELECT users           18.2%  (makePosts のユーザー取得)
4. INSERT posts            9.5%
```

### pprof（CPU 内訳）
- DB スキャン（`sqlx.SelectContext` / `scanAll`）≈ **36%**
- **テンプレート描画（`html/template.Execute`）≈ 27%**
- `getIndex` + `makePosts` が支配的

### Jaeger（GET / の 1 リクエスト）
- 合計 ≈ 2.3 ms、内訳は **SQL 3 クエリ**（投稿一覧 + コメント IN + ユーザー IN）。
- N+1 が解消され、1 ページが 3 クエリで描けていることをトレースで確認。

## ここから分かる次の打ち手（候補）

1. **`getPostsID` / `getImage` の `SELECT *` をやめる**（pt #2, 21%）
   投稿詳細ページは画像を `<img src=/image/..>` で読むため `imgdata`（約 80KB の BLOB）は不要。
   メタデータ列だけ取得すれば 21% の DB コストが大きく減る見込み。**最も確実な次の一手**。
2. **コメント取得（pt #1, 34%）の軽量化**
   コメント数・最新3件を memcached にキャッシュ、または posts にコメント数を非正規化。
3. **テンプレート描画（CPU 27%）**
   投稿カードの部分 HTML をキャッシュする等。効果は大きいが実装が重い。

## メモ

- pprof は `curl -o cpu.pprof http://localhost:6060/debug/pprof/profile?seconds=25` でファイル取得 → `go tool pprof -top app cpu.pprof` が安定（直接 URL 指定はシンボル化で固まることがある）。
- Jaeger のトレースは計測したいときだけ有効化し、**採点前に必ずドロップインを外す**（オーバーヘッド回避）。
- 計測時点の確定スコア（トレース OFF・素の状態）: **155,417 / fail 0**。
