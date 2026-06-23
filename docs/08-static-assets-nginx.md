# 08. 静的ファイル（css/js/img/favicon）を nginx 直配信 — Phase 5

## 何を / なぜ

`css/style.css`・`js/main.js`・`js/timeago.min.js`・`favicon.ico` は、nginx の `location /` 経由で
**Go アプリの `FileServer`（`r.Mount("/", ...)`）が返して**いた。alp 計測で各ファイル約 21 秒、合計で約 85 秒をアプリが消費。

これらは中身の変わらない静的ファイルなので、画像と同様 **nginx が直接返す**べき。

## どうやって

nginx に静的アセット用の `location` を追加（`infra/nginx/sites-available/isucon.conf`）。

```nginx
location ~* ^/(css|js|img)/ {
  expires 1d;
}
location = /favicon.ico {
  expires 1d;
}
```

`root` は server ブロックで `/home/isucon/private_isu/webapp/public/` を指しているため、
`/css/style.css` → `.../public/css/style.css` のようにそのままディスクから配信される。

## 結果

| 時点 | スコア |
|---|---|
| Phase 1（ハッシュ）後 | 128,763 |
| 静的ファイル nginx 直配信 | **141,992** |

- css/js/favicon のリクエストはアクセスログで `apptime:-`（アプリ未経由）になり、`reqtime:0.000`。
- アプリの CPU をテンプレート描画など本質的な処理に回せるようになった。
