# 17. 投稿HTML断片のキャッシュ（CSRFはプレースホルダ置換）— Phase 13

## 何を / なぜ

pprof で残るアプリ CPU の最大要素は **`html/template` の描画（~50%）**。一覧ページは投稿ごとに
post.html を reflection + HTML エスケープで描画していた。

post.html はコメントフォームの `{{.CSRFToken}}`（セッション毎）以外、**全ユーザーで同一**。
→ 断片を1度だけ描画してキャッシュし、CSRF だけ後から差し替えればテンプレートエンジンを回避できる。

## どうやって（`webapp/golang/fragmentcache.go`）

1. `postFragment(p)`: post.html を **CSRF をプレースホルダ（sentinel）にして描画**し、`post_id → HTML文字列` でキャッシュ。
2. `renderPostList(posts, csrf)`: キャッシュ断片を連結し `<div class="isu-posts">…</div>` で包み、
   **`strings.ReplaceAll(sentinel, csrf)` 一発**で実トークンに置換 → `template.HTML` で返す。
3. テンプレート変更: index.html / user.html の `{{template "posts.html" .Posts}}` を `{{.PostsHTML}}` に。
   index/account テンプレートから posts.html/post.html を外し（layout＋本体のみ）、断片は専用テンプレートで描画。
4. 一覧系ハンドラ（getIndex / getAccountName / getPosts）は `renderPostList` の結果を流し込むだけ。
   - getPostsID（詳細・全コメント）は従来通りフル描画（断片キャッシュは最新3件用なので不使用）。

### キャッシュ無効化
- `postComment`: 対象投稿の断片を `invalidatePostFragment`（件数＋最新3件が変わる）。
- `getInitialize`: `clearPostFragments`（コメント全リセット）。
- BAN は断片に影響しない（コメントは著者の del_flg に関係なく表示。BAN ユーザーの投稿は makePosts が一覧から除外＝断片を使わない）。

## 結果

| 時点 | スコア | fail |
|---|---|---|
| Phase 12（テンプレ事前計算）| 250,204 | 0 |
| HTML 断片キャッシュ | **270,090** | 0 |

- 一覧ページの投稿描画が「reflection＋エスケープ × 投稿数」→「連結＋1回の置換」に。
- ローカルはベンチ同居の CPU 競合で +8% だが、テンプレ描画はアプリ CPU の最大要素なので**実環境ではより効く**見込み。

## 正当性の確認
- index に `isu-posts` ラッパー・20 投稿・CSRF 差し替え（sentinel 漏れ 0）・/posts ページネーション 20 件を確認。ベンチ fail 0。

## 注意 / リスク
- 断片の不変条件は「投稿内容＋最新3コメント＋件数」。コメント追加以外で表示が変わる経路を足す場合は無効化を追加すること。
- CSRF は sentinel 置換方式。sentinel 文字列が本文に出現しないことが前提（ランダム性の高い固定文字列を使用）。
