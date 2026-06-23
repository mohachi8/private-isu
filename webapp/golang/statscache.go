package main

import (
	"context"
	"sync"
)

// Per-user aggregates for the /@user page, kept in memory to avoid the
// COUNT/SELECT queries that page issued. Rebuilt on startup/initialize and
// updated incrementally on new post / new comment.
var (
	statsMu            sync.RWMutex
	postIDsByUser      = map[int][]int{}
	commentCountByUser = map[int]int{} // comments authored by the user
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

	// comments authored per user, derived from the already-loaded comment cache
	ccu := map[int]int{}
	commentCacheMu.RLock()
	for _, cs := range commentsByPostID {
		for i := range cs {
			ccu[cs[i].UserID]++
		}
	}
	commentCacheMu.RUnlock()

	statsMu.Lock()
	postIDsByUser = pbu
	commentCountByUser = ccu
	statsMu.Unlock()
	return nil
}

func statAddPost(userID, postID int) {
	statsMu.Lock()
	postIDsByUser[userID] = append(postIDsByUser[userID], postID)
	statsMu.Unlock()
}

func statAddComment(authorID int) {
	statsMu.Lock()
	commentCountByUser[authorID]++
	statsMu.Unlock()
}

// userStats returns post count, comments-authored count, and comments-received
// count for a user, all from memory.
func userStats(uid int) (postCount, commentCount, commentedCount int) {
	statsMu.RLock()
	pids := make([]int, len(postIDsByUser[uid]))
	copy(pids, postIDsByUser[uid])
	commentCount = commentCountByUser[uid]
	statsMu.RUnlock()

	postCount = len(pids)
	commentCacheMu.RLock()
	for _, pid := range pids {
		commentedCount += len(commentsByPostID[pid])
	}
	commentCacheMu.RUnlock()
	return postCount, commentCount, commentedCount
}
