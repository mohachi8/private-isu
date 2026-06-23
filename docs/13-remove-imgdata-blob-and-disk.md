# 13. 根本対策：画像BLOBをDBから排除 & ディスク満杯の解消 — Phase 9

計測ツールの数値を追う前に、**土台（ストレージ）に2つの根本問題**が見つかった。

## 発見した根本問題

### A. ルートFSが96%満杯（残 622MB）
- `/var/lib/mysql` 4.4GB のうち、**無効化前の孤立 binlog が 2.5GB**（[10](./10-mysql-tuning.md) で `disable-log-bin` にした後も旧ファイルが残存）。
- スロークエリログ（`long_query_time=0` で全クエリ記録）が 1 回 ~550MB。
- ディスクが満杯だと **MySQL が書き込み失敗 → エラー → スコア低下**の温床。これは計測の数値以前の問題。

### B. posts テーブルが 1.5GB（画像BLOB同居）
```
posts:    data 1,492 MB  (10,296 行)  ← ほぼ imgdata BLOB
comments: data    10 MB  (100,411 行)
users:    data   0.4 MB
```
- buffer pool は 1GB。**posts 単体すら載らない** → キャッシュミス・ディスクI/O。
- 画像は既に nginx/ディスク配信（[05](./05-static-image-serving.md)）なのに、DB に重複保管され続けていた。

## どうやって

### 1. ディスク解放
- 孤立 binlog 2.5GB を削除（`log_bin=0` で MySQL 未使用の孤立ファイル。レギュレーション・採点に無関係であることを確認のうえ実施）。
- → 96% → 79%（残 3.1GB）。

### 2. 画像を完全にディスクへ（DBから排除）
- `bin/materialize-images.sh`: 全シード画像（id ≤ 10000）をディスクへ事前展開（`/image/<id>.<ext>` を並列 GET → getImage がディスク書き出し）。10,284 枚を確認。
- `getImage`: **DB参照を廃止し、ディスクのファイルを返すだけ**に変更（nginx の try_files がヒットしない miss 時のみ呼ばれる）。
- `postIndex`: INSERT から `imgdata` を除外し、画像はディスクにのみ保存。
- `getPostsID` / `getImage`: 旧 `SELECT *`（BLOB込み）を必要列のみに変更。
- **`ALTER TABLE posts DROP COLUMN imgdata`**（復旧は `/home/isucon/backup/mysqldump.sql.bz2` から可能なことを確認のうえ実施）。

## 結果

| 指標 | 変更前 | 変更後 |
|---|---|---|
| posts.ibd 実ファイル | 1.5 GB | **7.0 MB** |
| 全テーブル合計 | ~1.5 GB | **~18 MB**（buffer pool に完全収容）|
| ルートFS 空き | 622 MB | **4.6 GB** |
| スコア（ローカル）| ~166k | ~172k |

> ローカルのスコア伸びが小さいのは、**ベンチマーカーが同居して CPU を奪う**ため（[12](./12-infra-tuning-keepalive-pool.md)）。
> 実環境（ベンチ別マシン）では全テーブルが常時メモリに乗る効果がより大きく出るはず。

## なぜこれが「根本的」か

- これまでの改善（インデックス・N+1・LIMIT）は「クエリ1本の速さ」。今回は **DB が抱えるデータ量そのもの**を 1.5GB → 18MB に削減し、
  「全データが常時メモリにある」状態を作った。土台が変わるので、今後の全クエリが速くなる。
- 計測は数値を出す前に「**飽和しているリソース（CPU/メモリ/ディスク）**」を俯瞰するのが重要、という好例。`df` と テーブルサイズ確認は最初に見るべきだった。

## リスク/復旧メモ

- DB は git 管理外。`imgdata` 列 DROP は git で戻せないが、`/home/isucon/backup/mysqldump.sql.bz2`（フルダンプ 1.2GB）から復元可能。
- DB を復元した場合は、再度 `bin/materialize-images.sh` を実行して画像をディスク展開すること。
