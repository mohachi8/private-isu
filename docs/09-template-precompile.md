# 09. テンプレートのプリコンパイル — Phase 6

## 何を / なぜ

各ハンドラが**リクエストごとに** `template.ParseFiles(...)` を呼び、HTML テンプレートをディスクから読み込んでパースしていた。

```go
// 変更前: GET / のたびに 4 ファイルを読み込み & パース
template.Must(template.New("layout.html").Funcs(fmap).ParseFiles(
    getTemplPath("layout.html"), getTemplPath("index.html"),
    getTemplPath("posts.html"), getTemplPath("post.html"),
)).Execute(w, data)
```

テンプレートは起動後に変化しないので、毎回パースするのは無駄。`html/template` はパース後の値を**並行 Execute して安全**なので、起動時に1回だけパースして使い回せる。

## どうやって

`webapp/golang/templates.go` を新設し、全テンプレートをパッケージ変数として起動時にパース。

```go
var tmplFuncs = template.FuncMap{"imageURL": imageURL}
var (
    indexTmpl   = template.Must(template.New("layout.html").Funcs(tmplFuncs).ParseFiles(...))
    accountTmpl = ...
    postsTmpl   = template.Must(template.New("posts.html").Funcs(tmplFuncs).ParseFiles(...))
    postIDTmpl  = ...
    loginTmpl, registerTmpl, bannedTmpl = ...
)
```

各ハンドラは `indexTmpl.Execute(w, data)` のように使い回すだけに変更。app.go から `html/template` の import も不要になり削除。

> パース失敗時は起動時に panic（fail fast）。テンプレートは既知の正しいものなので問題なし。

## 結果

| 時点 | スコア |
|---|---|
| Phase 5（静的ファイル）後 | 141,992 |
| テンプレートのプリコンパイル | **149,974** |

リクエストごとのファイル I/O とパースが消え、テンプレート描画が軽くなった。
