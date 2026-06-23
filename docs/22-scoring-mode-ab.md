# 22. 計測モード / 本番スコアモードの切替（最終A/B）— Phase 19

アプリが十分速くなると、**計測用のオーバーヘッド（ログ・pprofラベル）自体**がスコアに効いてくる。
計測時は ON、本番スコア計測時は OFF に切り替えられるようにした。

## 変更

- **pprof ラベルを env ゲート化**: `PPROF_LABELS=1` のときだけミドルウェアを有効化（既定 OFF）。
- **切替スクリプト**:
  - `bin/scoring-mode.sh`: slow query log OFF / nginx access_log OFF / pprof labels OFF（本番スコア用）
  - `bin/analysis-mode.sh`: 上記をすべて ON に戻す（調査用）

## A/B 結果（60s ベンチ）

| 構成 | スコア |
|---|---|
| 分析モード（labels ON, logs ON）| 333,901 |
| labels OFF, logs ON | 336,817 |
| **本番モード（labels OFF, logs OFF）** | **341,482** |

ログ・ラベルを切るだけで合計 **約 +2.3%**。`long_query_time=0` の全クエリ記録と毎リクエストのアクセスログ I/O、
pprof ラベル付与のコストが、この速度域では無視できなくなっていた。

## 運用

- 普段（ボトルネック調査）は `bin/analysis-mode.sh` で計測 ON。
- **公式スコア計測の直前に `bin/scoring-mode.sh` を実行**して計測オーバーヘッドを落とす。
- リポジトリの既定（infra/nginx.conf 等）は分析モード相当（access_log ltsv, slow log on）。
