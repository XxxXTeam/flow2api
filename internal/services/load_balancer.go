package services

import (
	"sync"
	"time"

	"flow2api/internal/models"
)

// LoadBalancer handles token selection for generation
type LoadBalancer struct {
	tokenManager       *TokenManager
	concurrencyManager *ConcurrencyManager
	mu                 sync.RWMutex
}

// NewLoadBalancer creates a new load balancer
func NewLoadBalancer(tm *TokenManager, cm *ConcurrencyManager) *LoadBalancer {
	return &LoadBalancer{
		tokenManager:       tm,
		concurrencyManager: cm,
	}
}

// SelectToken selects an appropriate token for generation
func (lb *LoadBalancer) SelectToken(forImage, forVideo bool, model string) (*models.Token, error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	tokens, err := lb.tokenManager.GetActiveTokens()
	if err != nil {
		return nil, err
	}

	var bestToken *models.Token
	var bestScore float64 = -1

	now := time.Now().UTC()

	for _, token := range tokens {
		// Check if token supports the generation type
		if forImage && !token.ImageEnabled {
			continue
		}
		if forVideo && !token.VideoEnabled {
			continue
		}

		// Check if AT is expired
		if token.ATExpires != nil && token.ATExpires.Before(now) {
			continue
		}

		// Check concurrency limits
		if forImage && token.ImageConcurrency > 0 {
			if !lb.concurrencyManager.CanAcquireImage(token.ID) {
				continue
			}
		}
		if forVideo && token.VideoConcurrency > 0 {
			if !lb.concurrencyManager.CanAcquireVideo(token.ID) {
				continue
			}
		}

		// Calculate score (prefer tokens with more credits and less recent usage)
		score := float64(token.Credits)

		// Boost score for less recently used tokens
		if token.LastUsedAt != nil {
			timeSinceUse := now.Sub(*token.LastUsedAt)
			score += timeSinceUse.Seconds() / 60 // Add 1 point per minute since last use
		} else {
			score += 1000 // Never used, high priority
		}

		if score > bestScore {
			bestScore = score
			bestToken = token
		}
	}

	return bestToken, nil
}
