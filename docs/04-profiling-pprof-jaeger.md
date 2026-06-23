# 04. 詳細プロファイリング（pprof）と分散トレーシング（Jaeger / OpenTelemetry）

alp と pt-query-digest（[02](./02-measurement-stack.md)）は「どのエンドポイント / どのクエリが重いか」を教えてくれます。
ですが、**1リクエストの中で時間がどこに溶けているか**（CPU・メモリ・関数単位、DB待ちかロジックか）まではわかりません。
そこを埋めるために、Go アプリへ次の2つを導入しました。

| ツール | 何が見える | 常時ON? | オーバーヘッド |
|---|---|---|---|
| **pprof** | CPU / メモリ / goroutine など Go 内部のプロファイル | ✅ 常時ON（localhost のみ）| ほぼゼロ |
| **Jaeger + OpenTelemetry** | 1リクエストの分散トレース（HTTP→各DBクエリの内訳）| ⛔ 既定OFF（`ENABLE_TRACING=1` で有効）| あり（採点時はOFF）|

実装は `webapp/golang/tracing.go` に分離し、`app.go` への変更を最小化しています。

---

## なぜこの2つなのか / なぜトレースは既定OFFなのか

- **pprof**: Go 標準の `net/http/pprof` を読み込むだけで使え、オーバーヘッドが無視できるので常時ONにしています。CPU を食っている関数や、無駄なアロケーションを特定するのに最適。
- **Jaeger（トレーシング）**: 「このリクエストは users の SELECT を20回呼んでいる」といった **N+1 の可視化**に強力。ただし全リクエストにスパン生成・送信のコストが乗るため、**ベンチ採点時はOFF**にします（計測したいときだけON）。

---

## pprof の使い方

アプリ起動時に `localhost:6060` でプロファイル用サーバが立ち上がります（外部公開しない安全なポート）。

```bash
# CPU プロファイル（30秒間サンプリング）。別ターミナルでベンチを走らせながら取得すると有効。
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30

# ヒープ（メモリ）プロファイル
go tool pprof http://localhost:6060/debug/pprof/heap

# goroutine 一覧
curl http://localhost:6060/debug/pprof/goroutine?debug=1
```

`go tool pprof` のプロンプトで `top`（重い関数順）、`list 関数名`（行単位の内訳）、`web`（グラフ。要 graphviz）が便利です。

> `go` は `/home/isucon/.local/go/bin` にあります。PATH に無ければ `export PATH=$PATH:/home/isucon/.local/go/bin`。

---

## Jaeger（分散トレーシング）の使い方

### 構成

```
Go アプリ (ENABLE_TRACING=1) --OTLP/HTTP(:4318)--> Jaeger all-in-one --UI(:16686)--> ブラウザ
```

- Jaeger は all-in-one バイナリを **systemd サービス**（`infra/systemd/jaeger.service`）として常駐。ストレージはメモリ（再起動で消える＝ベンチ用途に十分）。
- ポート: UI `16686` / OTLP gRPC `4317` / OTLP HTTP `4318`。
- Go 側の計装:
  - HTTP: `otelhttp` でルータをラップ（エンドポイント単位のスパン）。
  - DB: `otelsql` で MySQL ドライバをラップ（クエリ単位のスパンが HTTP スパンの下にネストされる）。
  - いずれも `ENABLE_TRACING=1` のときだけ有効。OFF時は素の `sqlx` で接続し、オーバーヘッドゼロ。

### トレースを有効化する

systemd のドロップインを使います（`infra/systemd/isu-go-tracing.conf`）。

```bash
# 有効化
sudo mkdir -p /etc/systemd/system/isu-go.service.d
sudo cp infra/systemd/isu-go-tracing.conf /etc/systemd/system/isu-go.service.d/tracing.conf
sudo systemctl daemon-reload && sudo systemctl restart isu-go

# 無効化（採点・本番計測の前に必ず戻す）
sudo rm /etc/systemd/system/isu-go.service.d/tracing.conf
sudo systemctl daemon-reload && sudo systemctl restart isu-go
```

### Jaeger UI を見る（EC2 なので SSH トンネル経由）

EC2 のポートを直接公開せず、手元の PC から SSH トンネルで覗きます。

```bash
# 手元の PC で実行
ssh -L 16686:localhost:16686 <ec2-host>
# → ブラウザで http://localhost:16686 を開く
```

UI で Service に `private-isu-go` を選び、`GET /` のトレースを開くと、HTTP スパンの下に各 SQL スパンが並びます。
ここで「users の SELECT が何度も繰り返されている（N+1）」といった構造がひと目で分かります。

### 動作確認の結果

導入後に `ENABLE_TRACING=1` で起動し `/` と `/posts/1` に負荷をかけたところ、Jaeger に `private-isu-go` サービスとトレースが記録されることを確認済みです。

---

## まとめ（計測ツールの使い分け）

| 知りたいこと | 使うツール |
|---|---|
| どのエンドポイントが重い？ | alp（`bin/alp.sh`）|
| どのSQLが重い/多い？ | pt-query-digest（`bin/slowlog.sh`）|
| 1リクエストの内訳（どのSQLが何回？N+1か？）| Jaeger（`ENABLE_TRACING=1`）|
| CPU/メモリを食う Go の関数は？ | pprof（`localhost:6060`）|

採点・スコア計測の前には **トレーシングを必ず OFF** に戻すこと（オーバーヘッド回避）。pprof は常時ONのままで問題ありません。
