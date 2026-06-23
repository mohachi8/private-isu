package main

import (
	"context"
	"sync"
)

// Comments are read on every list page (count + latest 3 per post) and are the
// single largest DB cost after the user cache. The table is ~100k rows / ~10MB,
// so cache it all in memory keyed by post_id (oldest-first), and append on new
// comments / rebuild on initialize.
var (
	commentCacheMu   sync.RWMutex
	commentsByPostID = map[int][]Comment{}
)

// loadAllComments (re)builds the comment cache from the DB.
func loadAllComments(ctx context.Context) error {
	var cs []Comment
	if err := db.SelectContext(ctx, &cs, "SELECT * FROM `comments` ORDER BY `created_at` ASC, `id` ASC"); err != nil {
		return err
	}
	m := make(map[int][]Comment, len(commentsByPostID))
	for _, c := range cs {
		m[c.PostID] = append(m[c.PostID], c)
	}
	commentCacheMu.Lock()
	commentsByPostID = m
	commentCacheMu.Unlock()
	return nil
}

// addComment appends a newly created comment (kept in oldest-first order).
func addComment(c Comment) {
	commentCacheMu.Lock()
	commentsByPostID[c.PostID] = append(commentsByPostID[c.PostID], c)
	commentCacheMu.Unlock()
}

// commentSummary returns the total comment count and a copy of the latest
// `limit` comments (oldest-first). limit <= 0 means all comments. For list
// views (limit=3) this copies at most 3 entries instead of the whole slice.
func commentSummary(postID int, limit int) (count int, latest []Comment) {
	commentCacheMu.RLock()
	src := commentsByPostID[postID]
	count = len(src)
	n := count
	if limit > 0 && limit < n {
		n = limit
	}
	latest = make([]Comment, n)
	copy(latest, src[count-n:])
	commentCacheMu.RUnlock()
	return count, latest
}
