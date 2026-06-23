package main

import (
	"context"
	"sync"
)

// The users table is tiny (~2k rows) and changes rarely (register / ban /
// initialize), but it is read on almost every request (session user, post
// authors, comment authors). Cache it entirely in memory to remove those
// queries from the hot path.
var (
	userCacheMu sync.RWMutex
	usersByID   = map[int]User{}
	usersByName = map[string]User{}
)

// loadAllUsers (re)builds the cache from the DB. Called at startup and after
// every /initialize (which resets del_flg and deletes users with id > 1000).
func loadAllUsers(ctx context.Context) error {
	var us []User
	if err := db.SelectContext(ctx, &us, "SELECT * FROM `users`"); err != nil {
		return err
	}
	byID := make(map[int]User, len(us))
	byName := make(map[string]User, len(us))
	for _, u := range us {
		byID[u.ID] = u
		byName[u.AccountName] = u
	}
	userCacheMu.Lock()
	usersByID = byID
	usersByName = byName
	userCacheMu.Unlock()
	return nil
}

// cacheUser inserts/updates a single user (used on register and ban).
func cacheUser(u User) {
	userCacheMu.Lock()
	usersByID[u.ID] = u
	usersByName[u.AccountName] = u
	userCacheMu.Unlock()
}

func userByID(id int) (User, bool) {
	userCacheMu.RLock()
	u, ok := usersByID[id]
	userCacheMu.RUnlock()
	return u, ok
}

func userByName(name string) (User, bool) {
	userCacheMu.RLock()
	u, ok := usersByName[name]
	userCacheMu.RUnlock()
	return u, ok
}
