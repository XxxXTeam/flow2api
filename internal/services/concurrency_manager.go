package services

import (
	"sync"

	"flow2api/internal/models"
)

// ConcurrencyManager manages concurrent generation limits
type ConcurrencyManager struct {
	imageSlots map[int64]int
	videoSlots map[int64]int
	limits     map[int64]struct {
		imageLimit int
		videoLimit int
	}
	mu sync.RWMutex
}

// NewConcurrencyManager creates a new concurrency manager
func NewConcurrencyManager() *ConcurrencyManager {
	return &ConcurrencyManager{
		imageSlots: make(map[int64]int),
		videoSlots: make(map[int64]int),
		limits: make(map[int64]struct {
			imageLimit int
			videoLimit int
		}),
	}
}

// Initialize sets up concurrency limits for tokens
func (cm *ConcurrencyManager) Initialize(tokens []*models.Token) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for _, token := range tokens {
		cm.limits[token.ID] = struct {
			imageLimit int
			videoLimit int
		}{
			imageLimit: token.ImageConcurrency,
			videoLimit: token.VideoConcurrency,
		}
	}
}

// UpdateTokenLimits updates limits for a token
func (cm *ConcurrencyManager) UpdateTokenLimits(tokenID int64, imageLimit, videoLimit int) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.limits[tokenID] = struct {
		imageLimit int
		videoLimit int
	}{
		imageLimit: imageLimit,
		videoLimit: videoLimit,
	}
}

// CanAcquireImage checks if image slot is available
func (cm *ConcurrencyManager) CanAcquireImage(tokenID int64) bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	limit, ok := cm.limits[tokenID]
	if !ok || limit.imageLimit < 0 {
		return true // No limit
	}

	current := cm.imageSlots[tokenID]
	return current < limit.imageLimit
}

// CanAcquireVideo checks if video slot is available
func (cm *ConcurrencyManager) CanAcquireVideo(tokenID int64) bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	limit, ok := cm.limits[tokenID]
	if !ok || limit.videoLimit < 0 {
		return true // No limit
	}

	current := cm.videoSlots[tokenID]
	return current < limit.videoLimit
}

// AcquireImage acquires an image slot
func (cm *ConcurrencyManager) AcquireImage(tokenID int64) bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	limit, ok := cm.limits[tokenID]
	if !ok || limit.imageLimit < 0 {
		cm.imageSlots[tokenID]++
		return true
	}

	if cm.imageSlots[tokenID] >= limit.imageLimit {
		return false
	}

	cm.imageSlots[tokenID]++
	return true
}

// ReleaseImage releases an image slot
func (cm *ConcurrencyManager) ReleaseImage(tokenID int64) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.imageSlots[tokenID] > 0 {
		cm.imageSlots[tokenID]--
	}
}

// AcquireVideo acquires a video slot
func (cm *ConcurrencyManager) AcquireVideo(tokenID int64) bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	limit, ok := cm.limits[tokenID]
	if !ok || limit.videoLimit < 0 {
		cm.videoSlots[tokenID]++
		return true
	}

	if cm.videoSlots[tokenID] >= limit.videoLimit {
		return false
	}

	cm.videoSlots[tokenID]++
	return true
}

// ReleaseVideo releases a video slot
func (cm *ConcurrencyManager) ReleaseVideo(tokenID int64) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.videoSlots[tokenID] > 0 {
		cm.videoSlots[tokenID]--
	}
}
