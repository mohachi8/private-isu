# 07. パスワードハッシュを Go ネイティブ化 — Phase 1（積み残し）

## 何を / なぜ

ログイン・登録時のパスワードハッシュ計算が、**`openssl` コマンドを外部プロセスとして起動**していた。

```go
// 変更前: ログイン1回ごとに bash + openssl + sed のプロセスを起動
out, _ := exec.CommandContext(ctx, "/bin/bash", "-c",
    `printf "%s" `+escapeshellarg(src)+` | openssl dgst -sha512 | sed 's/^.*= //'`).Output()
```

`calculatePasshash` は内部で `digest` を 2 回呼ぶ（ソルト計算 + 本体）ため、**ログイン1回でプロセス起動が複数回**発生。
Phase 3/4 後の計測で `POST /login` が alp 2 位（合計約 94 秒, 平均 0.056 秒）に浮上していた。

## どうやって

Go 標準の `crypto/sha512` に置換。アルゴリズム（SHA-512 の小文字 16 進）は同一なので、**DB 内の既存ハッシュはそのまま有効**。

```go
func digest(ctx context.Context, src string) string {
    sum := sha512.Sum512([]byte(src))
    return hex.EncodeToString(sum[:])
}
```

不要になった `os/exec` の import と `escapeshellarg` を削除。

### 出力一致の検証

置換前にコマンドで等価性を確認（`openssl` と `sha512sum` が一致 = Go の `crypto/sha512` も同一出力）。

```
openssl:   3384babd...d1f170
sha512sum: 3384babd...d1f170   → MATCH
```

ベンチでもログイン成功（fail 0）で、既存ハッシュと一致することを確認。

## 結果

| 時点 | スコア |
|---|---|
| Phase 3/4 後 | 100,458 |
| パスワードハッシュ Go ネイティブ化 | **128,763** |

- `POST /login` 平均: 0.056 秒 → 0.013 秒（合計 94 秒 → 37 秒）。
- CPU 使用率: アプリ 41% / MySQL 27% へ（プロセス起動のオーバーヘッドが消え、負荷が分散）。

## 教訓

- **リクエストごとの外部プロセス起動は高コスト**。同じ計算は言語標準ライブラリで内製化する。
- ハッシュ等は「アルゴリズムを変えない（既存データと互換）」ことが必須。置換前にバイト一致を必ず検証する。
