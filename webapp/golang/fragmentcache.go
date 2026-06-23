package main

import (
	"bytes"
	"html/template"
	"strings"
	"sync"
)

// Rendering the post list (post.html per post, with reflection + HTML escaping)
// is the dominant app-CPU cost. Each post's fragment is identical across users
// except for the per-session CSRF token in its comment form, so we render each
// fragment once with a CSRF placeholder, cache it by post id, and substitute the
// real token per request with a single string replace.

const csrfSentinel = "__CSRF_TOKEN_SENTINEL__"

// renders a single post (list view: latest 3 comments). No funcmap needed —
// imageURL/timestamp are precomputed fields on Post.
var postFragmentTmpl = template.Must(template.New("post.html").ParseFiles(getTemplPath("post.html")))

var (
	fragMu    sync.RWMutex
	fragCache = map[int]string{}
)

func postFragment(p Post) string {
	fragMu.RLock()
	s, ok := fragCache[p.ID]
	fragMu.RUnlock()
	if ok {
		return s
	}

	p.CSRFToken = csrfSentinel
	var buf bytes.Buffer
	if err := postFragmentTmpl.Execute(&buf, p); err != nil {
		return buf.String() // don't cache a bad render
	}
	s = buf.String()

	fragMu.Lock()
	fragCache[p.ID] = s
	fragMu.Unlock()
	return s
}

func invalidatePostFragment(postID int) {
	fragMu.Lock()
	delete(fragCache, postID)
	fragMu.Unlock()
}

func clearPostFragments() {
	fragMu.Lock()
	fragCache = map[int]string{}
	fragMu.Unlock()
}

// renderPostList builds the <div class="isu-posts">…</div> block (same wrapper
// as posts.html) from cached fragments, substituting the CSRF token once.
func renderPostList(posts []Post, csrfToken string) template.HTML {
	var b strings.Builder
	b.WriteString(`<div class="isu-posts">`)
	for i := range posts {
		b.WriteString(postFragment(posts[i]))
	}
	b.WriteString(`</div>`)
	return template.HTML(strings.ReplaceAll(b.String(), csrfSentinel, csrfToken))
}

// --- detail page (/posts/:id) fragment cache: full comments, not just 3 ---

var (
	detailFragMu    sync.RWMutex
	detailFragCache = map[int]string{}
)

func postDetailFragment(p Post) string {
	detailFragMu.RLock()
	s, ok := detailFragCache[p.ID]
	detailFragMu.RUnlock()
	if ok {
		return s
	}
	p.CSRFToken = csrfSentinel
	var buf bytes.Buffer
	if err := postFragmentTmpl.Execute(&buf, p); err != nil {
		return buf.String()
	}
	s = buf.String()
	detailFragMu.Lock()
	detailFragCache[p.ID] = s
	detailFragMu.Unlock()
	return s
}

func invalidateDetailFragment(postID int) {
	detailFragMu.Lock()
	delete(detailFragCache, postID)
	detailFragMu.Unlock()
}

func clearDetailFragments() {
	detailFragMu.Lock()
	detailFragCache = map[int]string{}
	detailFragMu.Unlock()
}

func renderPostDetail(p Post, csrfToken string) template.HTML {
	return template.HTML(strings.ReplaceAll(postDetailFragment(p), csrfSentinel, csrfToken))
}
