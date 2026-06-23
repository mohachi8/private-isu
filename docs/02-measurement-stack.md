# 02. 計測スタック（三種の神器）

「推測するな、計測せよ」が ISUCON の鉄則です。やみくもにコードをいじっても、ボトルネックを外していれば効果はありません。
そこでまず **どこが遅いかを数字で見える化する**仕組みを整えました。これを ISUCON では俗に「三種の神器」と呼びます。

| ツール | 何を見るか | 対象ログ |
|---|---|---|
| **alp** | どの URL（エンドポイント）が遅い / 重いか | nginx アクセスログ |
| **pt-query-digest** | どの SQL が遅い / 回数が多いか | MySQL スロークエリログ |
| **MySQL スロークエリログ** | 上記の解析対象となる生データ | （ログ出力設定）|

---

## 1. alp（nginx アクセスログ解析）

- バージョン: **alp 1.0.21**
- 役割: nginx のアクセスログを集計し、エンドポイントごとに「リクエスト数 / 平均・最大・合計応答時間 / p99」などを表で出す。
- **「応答時間の合計（sum）が大きいエンドポイント」= 全体で最も時間を食っている = 改善の優先度が高い**、という見方をする。

### nginx ログを LTSV 形式に変更
alp で解析しやすいよう、nginx のアクセスログを **LTSV（Labeled Tab-Separated Values）形式**に変更しました（`infra/nginx/nginx.conf`）。

LTSV は `key:value` をタブ区切りで並べる形式で、ツールがフィールドを確実にパースできます。出力している主なフィールド:

| フィールド | 意味 |
|---|---|
| `time` | リクエスト時刻（ISO8601）|
| `method` / `uri` / `status` | メソッド / URI / HTTP ステータス |
| `reqtime` | nginx から見たリクエスト全体の処理時間 |
| `apptime` | 上流（Go アプリ）の応答時間 |
| `size` / `ua` / `vhost` | レスポンスサイズ / UA / ホスト |

### 使い方（`bin/alp.sh`）

```bash
sudo alp ltsv --file /var/log/nginx/access.log \
  --sort sum -r \
  -m '/posts/\d+,/image/\d+\.\w+,/@\w+' \
  -o count,method,uri,min,avg,max,sum,p99
```

- `--sort sum -r`: 応答時間の**合計の降順**で並べる（一番効くところが上に来る）。
- `-m '...'`: 動的な ID を含む URL を**正規表現でグルーピング**。`/posts/123` や `/posts/456` を `/posts/\d+` として 1 行にまとめ、集計を意味のある単位にする。
- `-o ...`: 出力する列の指定。

---

## 2. pt-query-digest（MySQL スロークエリ解析）

- バージョン: **pt-query-digest 3.2.1**（Percona Toolkit）
- 役割: MySQL のスロークエリログを集計し、「クエリの種類ごとに、合計実行時間・実行回数・平均時間・走査行数」などをランキング表示する。
- **クエリは値（`WHERE id = 123`）を正規化してまとめてくれる**ので、「この形のクエリが全体の何 % の時間を占めているか」が分かる。

### 使い方（`bin/slowlog.sh`）

```bash
LOG=/var/log/mysql/mysql-slow.log
if command -v pt-query-digest >/dev/null 2>&1; then
  sudo pt-query-digest "$LOG" | head -80
else
  # 未インストール時のフォールバック
  sudo mysqldumpslow -s t "$LOG" | head -60
fi
```

pt-query-digest が入っていない環境でも止まらないよう、`mysqldumpslow` にフォールバックするようにしています。

---

## 3. MySQL スロークエリログの有効化

pt-query-digest が解析する「生データ」を出すための設定です（`infra/mysql/conf.d/zz-isucon.cnf`）。

```ini
[mysqld]
slow_query_log      = 1
slow_query_log_file = /var/log/mysql/mysql-slow.log
# long_query_time = 0 で「すべてのクエリ」を記録する
long_query_time     = 0
```

- ファイル名を `zz-` で始めているのは、`/etc/mysql/conf.d/` の中で**辞書順で最後に読み込まれ、既定値を確実に上書きする**ため。
- **`long_query_time = 0`** がポイント。これで「遅いクエリだけ」ではなく**全クエリ**が記録されます。ベンチ中の全クエリを取りこぼさず解析するための設定です（ログ量は増えるので、通常運用なら `0.1` 等に上げる、とコメントで明記）。

---

## ベンチマークとログ回転（`bin/bench.sh`）

解析は「直近 1 回のベンチ分」だけを見たいので、ベンチ前にログを空にしてから実行します。

```bash
# 解析対象を今回の実行分だけにするためログを 0 バイトに切り詰める
sudo truncate -s 0 /var/log/nginx/access.log
sudo truncate -s 0 /var/log/mysql/mysql-slow.log

# ベンチマーク本体（約1分）
cd /home/isucon/private_isu/benchmarker
./bin/benchmarker -t http://localhost -u ./userdata
```

実行後は `bin/alp.sh` と `bin/slowlog.sh` で解析、という流れです。

---

## デプロイ（`bin/deploy.sh`）

コードを変えたらビルドして再起動。ヘルスチェックまで自動で行います。

```bash
export PATH="$PATH:/home/isucon/.local/go/bin"
cd /home/isucon/private_isu/webapp/golang
go build -o app                       # = make
sudo systemctl restart isu-go
systemctl is-active isu-go            # 起動できたか確認
curl -s -o /dev/null -w "health: HTTP %{http_code} %{time_total}s\n" http://localhost/
```

---

## インフラ設定の反映（`bin/apply-infra.sh`）

`infra/` 配下の設定ファイルは**リポジトリが正（source of truth）**です。編集したらこのスクリプトで実システムへ反映します。

```bash
REPO=/home/isucon/private_isu

# nginx 設定を配置 → 構文チェック → リロード
sudo cp "$REPO/infra/nginx/nginx.conf" /etc/nginx/nginx.conf
sudo cp "$REPO/infra/nginx/sites-available/isucon.conf" /etc/nginx/sites-available/isucon.conf
sudo ln -sf /etc/nginx/sites-available/isucon.conf /etc/nginx/sites-enabled/isucon.conf
sudo nginx -t
sudo systemctl reload nginx

# MySQL 設定を配置 → 再起動
sudo cp "$REPO/infra/mysql/conf.d/zz-isucon.cnf" /etc/mysql/conf.d/zz-isucon.cnf
sudo systemctl restart mysql
```

`nginx -t`（構文チェック）を挟んでから reload しているので、設定ミスで nginx が落ちる事故を防げます。

---

## なぜリポジトリ管理するのか（狙い）

`infra/`（nginx / MySQL 設定）と `bin/`（運用スクリプト）を Git 管理下に置く狙いは 2 つ。

1. **再現性**: `apply-infra.sh` を実行すれば誰でも・どの環境でも同じ状態を再現できる。サーバの `/etc/...` を直接いじると「何をしたか」が失われるが、リポジトリにあれば失われない。
2. **差分管理**: 「いつ・何を・なぜ変えたか」が Git 履歴に残り、スコアの変化と紐付けて振り返れる。

---

## まとめ

- **alp**（URL 視点）と **pt-query-digest**（SQL 視点）の両面でボトルネックを数値化。
- そのために nginx ログを **LTSV 化**、MySQL は **`long_query_time = 0`** で全クエリ記録。
- ベンチ前に**ログ回転**して直近の 1 回分だけを解析。
- 設定・スクリプトは**リポジトリを正**として `apply-infra.sh` で反映（再現性・差分管理）。
