# 10. MySQL 設定チューニング — Phase 7

## 何を / なぜ

MySQL がほぼ初期設定のままだった。

- `innodb_buffer_pool_size = 128M`（初期値）: データ／インデックスのキャッシュ領域。小さいとディスク I/O が増える。
- `innodb_flush_log_at_trx_commit = 1`: コミットごとに redo ログを fsync（最も安全だが遅い）。
- `log_bin = 1`: バイナリログ有効（レプリケーション用。ベンチでは不要なのに書き込みコストが乗る）。
- `innodb_flush_method = fsync`: InnoDB と OS の二重バッファリングが起きうる。

搭載 RAM は 3.7GB。画像は nginx がディスク配信するため、DB が抱える BLOB はほぼ参照されず、
キャッシュすべき作業セットは posts/comments/users の**メタデータ中心**。

## どうやって（`infra/mysql/conf.d/zz-isucon.cnf`）

```ini
innodb_buffer_pool_size        = 1G      # 作業セットをメモリに載せる
innodb_flush_log_at_trx_commit = 2       # fsync を毎コミット→約1秒ごとに緩和
innodb_flush_method            = O_DIRECT # 二重バッファ回避
innodb_redo_log_capacity       = 256M    # チェックポイント flush を減らす
disable-log-bin                          # バイナリログ無効化（書き込みコスト削減）
```

> いずれも**耐障害性をわずかに犠牲に**して速度を取る設定。ISUCON のようなベンチ用途では定石だが、
> 本番では要件に応じて見直すべき（特に `flush_log_at_trx_commit` と `disable-log-bin`）。

## 結果

| 時点 | スコア |
|---|---|
| Phase 6（テンプレート）後 | 149,974 |
| MySQL 設定チューニング | **152,889** |

伸びが小さいのは、これまでの改善（インデックス・N+1 解消・LIMIT）で**作業セットが既に小さく**、
128MB でもほぼ収まっていたため。それでも将来のデータ増や書き込み負荷に対する余裕として有効。

## 学び

- チューニングは「現状のボトルネックが何か」で効果が大きく変わる。ここでは既に DB 負荷が小さく、
  バッファプール拡大の伸びしろは限定的だった → **次は計測でボトルネックを再特定する**のが正しい順序。
