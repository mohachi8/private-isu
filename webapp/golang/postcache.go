package main

import (
	"context"
	"sort"
	"sync"
	"time"
)

// posts metadata (no BLOB) is small, so cache it all in memory and serve every
// list/detail query from RAM. Stored ascending by (created_at, id); the newest
// posts are at the end so appends are O(1). Readers copy the slices they need.
var (
	postCacheMu sync.RWMutex
	postsAsc    []Post       // created_at ASC, id ASC
	postByID    = map[int]Post{}
	postsByUser = map[int][]Post{} // per user, ASC
)

func loadAllPostsCache(ctx context.Context) error {
	var ps []Post
	if err := db.SelectContext(ctx, &ps,
		"SELECT `id`, `user_id`, `body`, `mime`, `created_at` FROM `posts` ORDER BY `created_at` ASC, `id` ASC"); err != nil {
		return err
	}
	byID := make(map[int]Post, len(ps))
	byUser := make(map[int][]Post)
	for _, p := range ps {
		byID[p.ID] = p
		byUser[p.UserID] = append(byUser[p.UserID], p)
	}
	postCacheMu.Lock()
	postsAsc = ps
	postByID = byID
	postsByUser = byUser
	postCacheMu.Unlock()
	return nil
}

// addPostToCache records a newly created post. created_at is truncated to the
// second to match MySQL TIMESTAMP precision so max_created_at pagination is
// consistent with the displayed/served timestamps.
func addPostToCache(p Post) {
	p.CreatedAt = p.CreatedAt.Truncate(time.Second)
	postCacheMu.Lock()
	postsAsc = append(postsAsc, p)
	postByID[p.ID] = p
	postsByUser[p.UserID] = append(postsByUser[p.UserID], p)
	postCacheMu.Unlock()
}

func postByIDCache(id int) (Post, bool) {
	postCacheMu.RLock()
	p, ok := postByID[id]
	postCacheMu.RUnlock()
	return p, ok
}

// newestPosts returns up to `limit` newest posts (created_at DESC, id DESC).
func newestPosts(limit int) []Post {
	postCacheMu.RLock()
	defer postCacheMu.RUnlock()
	out := make([]Post, 0, limit)
	for i := len(postsAsc) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, postsAsc[i])
	}
	return out
}

// postsBefore returns up to `limit` newest posts with created_at <= t
// (created_at DESC, id DESC), matching the original WHERE created_at <= ? query.
func postsBefore(t time.Time, limit int) []Post {
	postCacheMu.RLock()
	defer postCacheMu.RUnlock()
	// first index whose created_at > t; everything before it is <= t
	hi := sort.Search(len(postsAsc), func(i int) bool { return postsAsc[i].CreatedAt.After(t) })
	out := make([]Post, 0, limit)
	for i := hi - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, postsAsc[i])
	}
	return out
}

// userPosts returns up to `limit` newest posts by a user (created_at DESC).
func userPosts(uid, limit int) []Post {
	postCacheMu.RLock()
	defer postCacheMu.RUnlock()
	src := postsByUser[uid]
	out := make([]Post, 0, limit)
	for i := len(src) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, src[i])
	}
	return out
}
