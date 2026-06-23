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

// commentsForPost returns a copy of a post's comments (oldest-first) so callers
// can read safely without holding the lock.
func commentsForPost(postID int) []Comment {
	commentCacheMu.RLock()
	src := commentsByPostID[postID]
	out := make([]Comment, len(src))
	copy(out, src)
	commentCacheMu.RUnlock()
	return out
}
