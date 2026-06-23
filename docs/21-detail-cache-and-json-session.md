# 21. 詳細ページHTMLキャッシュ / Cookieセッションの脱gob — Phase 17・18

posts 全件メモリ化（[20](./20-posts-cache.md)）後、エンドポイント別 pprof は:
```
POST /        14.7%  (画像書き込み: ほぼ不可避)
GET /         11.6%  (一覧: 既にキャッシュ済)
GET /posts/:id 9.6%  (詳細: 毎回フル描画)  ← Phase 17
GET /@:name    8.8%
```
フラットでは **gob（Cookieセッションのデコード）が約10%** で全リクエストに乗っていた → Phase 18。

## Phase 17: 詳細ページ `/posts/:id` のHTMLキャッシュ

一覧と違い詳細は**全コメント版**の投稿カードを `postIDTmpl` で毎回描画していた。
[17](./17-html-fragment-cache.md) と同じ要領で、全コメント版カードを `detailFragCache`（post_id → CSRF sentinel 入り HTML）にキャッシュし、リクエストはCSRF置換のみ。

- `post_id.html`: `{{template "post.html" .Post}}` → `{{.PostHTML}}`。
- 無効化: コメント追加でその post を `invalidateDetailFragment`、initialize で全消去。

### 併せて: postIndex のトランザクション撤去
読み取りが全てメモリキャッシュになったため、「投稿の可視化」は DB commit ではなく `addPostToCache`（画像書き込みの後に実行）が担う。よって INSERT を囲っていたトランザクションは不要になり、平のINSERTに簡素化。

## Phase 18: Cookie セッションを gob → JSON シリアライズ

`securecookie` の既定 gob シリアライザは **Encode/Decode のたびに型記述子を再コンパイル**する。
セッションは毎リクエスト読むため、これが約10%の CPU。中身は `user_id`(int) / `csrf_token`(str) / `notice`(str) と小さいので、
**文字列キーの JSON にする独自シリアライザ**（`sessionjson.go`）に差し替え。

```go
for _, c := range cs.Codecs {
    if sc, ok := c.(*securecookie.SecureCookie); ok {
        sc.SetSerializer(jsonSessionSerializer{})
    }
}
```

JSON では数値が float64 になるため `getSessionUser` に float64 ケースを追加。
register→login→CSRF付きupload→画像取得のフローでバイト一致を確認。

## 結果

| 時点 | スコア | fail |
|---|---|---|
| Phase 16（posts cache）| 327,806 | 0 |
| Phase 17（詳細キャッシュ + txn撤去）| 330,056 | 0 |
| Phase 18（JSONセッション）| **333,901** | 0 |

ローカルはベンチ同居の CPU 競合で各々控えめだが、いずれも全エンドポイントに効く横断的削減。
