package main

import (
	"errors"
	"os"
	"sync"
	"syscall"
	"time"
)

// Uploaded images (post id > 10000) are written to disk and only cleaned at the
// next /initialize, so at high throughput they can fill the disk mid-run. A full
// disk fails MySQL writes + image writes and cascades into mass benchmark
// failures. This janitor keeps headroom by evicting the OLDEST uploaded images
// (least likely to still be on a page the benchmark fetches) when free space
// drops below a threshold. Seeded images (id <= 10000) are never recorded here,
// so they are never evicted.
const (
	diskFreeLowMB   = 800 // start evicting below this
	evictBatchCount = 512 // files removed per janitor pass when low
)

var (
	uploadedMu    sync.Mutex
	uploadedFiles []string // upload paths in creation order (FIFO)
)

func recordUpload(path string) {
	uploadedMu.Lock()
	uploadedFiles = append(uploadedFiles, path)
	uploadedMu.Unlock()
}

// resetUploads forgets tracked uploads (call when /initialize clears id>10000).
func resetUploads() {
	uploadedMu.Lock()
	uploadedFiles = nil
	uploadedMu.Unlock()
}

// evictOldestUploads removes up to n oldest uploaded image files. Returns count.
func evictOldestUploads(n int) int {
	uploadedMu.Lock()
	if n > len(uploadedFiles) {
		n = len(uploadedFiles)
	}
	victims := uploadedFiles[:n]
	uploadedFiles = uploadedFiles[n:]
	uploadedMu.Unlock()
	for _, p := range victims {
		os.Remove(p)
	}
	return n
}

func diskFreeMB() int64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(imageDir, &st); err != nil {
		return 1 << 60 // unknown -> don't evict
	}
	return int64(st.Bavail) * int64(st.Bsize) / (1024 * 1024)
}

func isNoSpace(err error) bool {
	return errors.Is(err, syscall.ENOSPC)
}

// startDiskJanitor proactively keeps free space above diskFreeLowMB so neither
// MySQL nor image writes ever hit ENOSPC during a run.
func startDiskJanitor() {
	go func() {
		for {
			time.Sleep(2 * time.Second)
			for diskFreeMB() < diskFreeLowMB {
				if evictOldestUploads(evictBatchCount) == 0 {
					break // nothing left to evict
				}
			}
		}
	}()
}
