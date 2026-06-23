# 12. インフラ層の徹底チューニング（keepalive / DBプール / カーネル）— Phase 8

アルゴリズムではなく、**接続の扱い**というインフラ層に大きな無駄が残っていた。

## 何が問題だったか（計測で実証）

ベンチ実行中に `ss` で TCP 接続状態を観察すると、大量の `TIME_WAIT` が発生していた。

| 接続先 | 改善前 TIME_WAIT |
|---|---|
| アプリ :8080（nginx→app）| 約 2,100 |
| MySQL :3306（app→db）| 約 880 |
| 合計 | 約 3,200 |

原因は2つ:

1. **nginx → アプリが毎回新規 TCP 接続**。`proxy_pass` は既定で HTTP/1.0・接続使い捨て。リクエストごとに 3 ウェイハンドシェイク + `TIME_WAIT` が発生。
2. **アプリ → MySQL も毎回新規接続**。Go の `database/sql` は既定 `MaxIdleConns=2` のため、並行リクエスト下では接続を開いては閉じる。

エフェメラルポートは `32768–60999`（約 28,000 個）しかなく、`TIME_WAIT` が 60 秒残るため、高負荷では**ポート枯渇**の危険すらある。

## どうやって

### 1. nginx → アプリの keepalive（`infra/nginx/sites-available/isucon.conf`）
```nginx
upstream app {
  server 127.0.0.1:8080;
  keepalive 128;            # アイドル接続を最大128本プール
  keepalive_requests 100000;
}
location / {
  proxy_http_version 1.1;   # keepalive には 1.1 が必須
  proxy_set_header Connection "";
  proxy_pass http://app;
}
```

### 2. DB コネクションプール（`webapp/golang/app.go`）
```go
db.SetMaxOpenConns(100)   // MySQL max_connections(151) より十分小さく
db.SetMaxIdleConns(100)   // アイドル接続を保持して使い回す
db.SetConnMaxLifetime(0)  // 寿命無制限（ベンチ用途）
```

### 3. nginx 本体（`infra/nginx/nginx.conf`）
```nginx
worker_rlimit_nofile 65535;
events { worker_connections 8192; multi_accept on; }
http {
  tcp_nodelay on;
  open_file_cache max=100000 inactive=60s;   # 画像は ~10万 hit/run
  open_file_cache_valid 60s;
  open_file_cache_min_uses 1;
  open_file_cache_errors on;
}
```

### 4. カーネル（`infra/sysctl/99-isucon.conf`）
```ini
net.core.somaxconn          = 32768
net.core.netdev_max_backlog = 8192
net.ipv4.tcp_max_syn_backlog = 8192
net.ipv4.tcp_tw_reuse       = 1
```

`bin/apply-infra.sh` で nginx / MySQL / sysctl をまとめて反映するようにした。

## 結果

### 接続 TIME_WAIT（ベンチ中）
| 接続先 | 改善前 | 改善後 |
|---|---|---|
| アプリ :8080 | 約 2,100 | **203** |
| MySQL :3306 | 約 880 | **70** |

nginx→app は ESTAB が 36 本前後で安定し、接続を使い回せている。

### スコア
| 時点 | スコア |
|---|---|
| 計測セッション時点 | 155,417 |
| インフラチューニング後 | **166,173** |

## 学び

- **アルゴリズムを変えなくても、接続の使い回し（keepalive / コネクションプール）だけで大きく改善**することがある。
- ボトルネックは CPU やクエリだけでなく、**TCP 接続の生成コスト・ポート枯渇**にも潜む。`ss` で接続状態を見るのが有効。
