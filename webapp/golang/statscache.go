package main

import (
	"context"
	"sync"
)

// Per-user aggregates for the /@user page, kept in memory to avoid the
// COUNT/SELECT queries that page issued. Rebuilt on startup/initialize and
// updated incrementally on new post / new comment.
var (
	statsMu              sync.RWMutex
	postIDsByUser        = map[int][]int{}
	commentCountByUser   = map[int]int{} // comments authored by the user
	commentedCountByUser = map[int]int{} // comments received on the user's posts
)

func loadStats(ctx context.Context) error {
	type pidRow struct {
		ID     int `db:"id"`
		UserID int `db:"user_id"`
	}
	var rows []pidRow
	if err := db.SelectContext(ctx, &rows, "SELECT `id`, `user_id` FROM `posts`"); err != nil {
		return err
	}
	pbu := make(map[int][]int, len(rows)/4+1)
	for _, r := range rows {
		pbu[r.UserID] = append(pbu[r.UserID], r.ID)
	}

	// comments authored per user + comments received per user (their posts'
	// comment counts), derived from the already-loaded comment cache.
	ccu := map[int]int{}
	cbu := map[int]int{}
	commentCacheMu.RLock()
	for _, cs := range commentsByPostID {
		for i := range cs {
			ccu[cs[i].UserID]++
		}
	}
	for uid, pids := range pbu {
		s := 0
		for _, pid := range pids {
			s += len(commentsByPostID[pid])
		}
		cbu[uid] = s
	}
	commentCacheMu.RUnlock()

	statsMu.Lock()
	postIDsByUser = pbu
	commentCountByUser = ccu
	commentedCountByUser = cbu
	statsMu.Unlock()
	return nil
}

func statAddPost(userID, postID int) {
	statsMu.Lock()
	postIDsByUser[userID] = append(postIDsByUser[userID], postID)
	statsMu.Unlock()
}

// statAddComment records a new comment: +1 authored for the commenter, and +1
// received for the post's author (looked up before locking to avoid nesting).
func statAddComment(commenterID, postID int) {
	postAuthor := 0
	if p, ok := postByIDCache(postID); ok {
		postAuthor = p.UserID
	}
	statsMu.Lock()
	commentCountByUser[commenterID]++
	if postAuthor != 0 {
		commentedCountByUser[postAuthor]++
	}
	statsMu.Unlock()
}

// userStats returns post / comments-authored / comments-received counts, O(1).
func userStats(uid int) (postCount, commentCount, commentedCount int) {
	statsMu.RLock()
	postCount = len(postIDsByUser[uid])
	commentCount = commentCountByUser[uid]
	commentedCount = commentedCountByUser[uid]
	statsMu.RUnlock()
	return postCount, commentCount, commentedCount
}
