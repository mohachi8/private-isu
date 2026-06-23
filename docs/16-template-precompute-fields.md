# 16. テンプレートの reflection 呼び出しを事前計算で排除 — Phase 12

## 何を / なぜ

pprof を取り直すと、コメント/ユーザーを DB から排除した後の **アプリ CPU の約 56% が `html/template.Execute`** に集中。
さらにその内訳で **`reflect.Value.Call`（テンプレート内の関数/メソッド呼び出し）が ~30%** を占めていた。

犯人は post.html の2種類の reflection 呼び出し（投稿ごとに実行）:
- `{{imageURL .}}` … Go 関数を reflection 経由で呼ぶ
- `{{.CreatedAt.Format "..."}}` … `time.Time.Format` メソッドを reflection 経由で呼ぶ（2 箇所）

`html/template` は関数もメソッドも `reflect.Value.Call` で呼ぶため、投稿数 × 呼び出し数だけ高コストな reflection が走っていた。

## どうやって

呼び出し結果を **`makePosts` で事前計算し、`Post` 構造体のフィールドに格納**。テンプレートは「呼び出し」ではなく「フィールド参照」に変更。

```go
type Post struct {
    ...
    ImageURL     string  // 追加
    CreatedAtISO string  // 追加
}

// makePosts 内
p.ImageURL = imageURL(p)
p.CreatedAtISO = p.CreatedAt.Format(ISO8601Format)
```

```html
<!-- post.html -->
<img src="{{.ImageURL}}" class="isu-image">
<div ... data-created-at="{{.CreatedAtISO}}">
<time class="timeago" datetime="{{.CreatedAtISO}}"></time>
```

フィールド参照（`evalField`）も reflection だが、`reflect.Value.Call`（関数呼び出し）よりはるかに安い。

## 結果

| 時点 | スコア | fail |
|---|---|---|
| Phase 11（コメントキャッシュ）| 246,398 | 0 |
| テンプレート事前計算 | **250,204** | 0 |

- テンプレート内の関数/メソッド呼び出し（`reflect.Value.Call`）を排除。
- ローカルはベンチ同居の CPU 競合で伸びが小さく見えるが、**テンプレート描画はアプリ CPU の最大要素**なので実環境ではより効く。

## 補足：残るテンプレートコスト

`html/template` は各フィールド参照を reflection で行い、HTML エスケープも毎回走る。これ以上削るには
「レンダリング結果（投稿カードの HTML 断片やインデックスページ）のキャッシュ」が必要だが、
CSRF トークンやログイン状態が絡むため実装が重い。費用対効果を見て判断する領域。
