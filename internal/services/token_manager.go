package services

import (
	"fmt"
	"log"
	"sync"
	"time"

	"flow2api/internal/client"
	"flow2api/internal/database"
	"flow2api/internal/models"
)

// TokenManager handles token lifecycle
type TokenManager struct {
	db         *database.Database
	flowClient *client.FlowClient
	mu         sync.Mutex
}

// NewTokenManager creates a new token manager
func NewTokenManager(db *database.Database, flowClient *client.FlowClient) *TokenManager {
	return &TokenManager{
		db:         db,
		flowClient: flowClient,
	}
}

// GetAllTokens returns all tokens
func (tm *TokenManager) GetAllTokens() ([]*models.Token, error) {
	return tm.db.GetAllTokens()
}

// GetActiveTokens returns all active tokens
func (tm *TokenManager) GetActiveTokens() ([]*models.Token, error) {
	return tm.db.GetActiveTokens()
}

// GetToken returns a token by ID
func (tm *TokenManager) GetToken(id int64) (*models.Token, error) {
	return tm.db.GetToken(id)
}

// DeleteToken deletes a token
func (tm *TokenManager) DeleteToken(id int64) error {
	return tm.db.DeleteToken(id)
}

// EnableToken enables a token and resets error count
func (tm *TokenManager) EnableToken(id int64) error {
	if err := tm.db.UpdateToken(id, map[string]interface{}{"is_active": true}); err != nil {
		return err
	}
	return tm.db.ResetErrorCount(id)
}

// DisableToken disables a token
func (tm *TokenManager) DisableToken(id int64) error {
	return tm.db.UpdateToken(id, map[string]interface{}{"is_active": false})
}

// AddToken adds a new token
func (tm *TokenManager) AddToken(st, projectID, projectName, remark string, imageEnabled, videoEnabled bool, imageConcurrency, videoConcurrency int) (*models.Token, error) {
	// Check if ST already exists
	existing, _ := tm.db.GetTokenByST(st)
	if existing != nil {
		return nil, fmt.Errorf("Token already exists (email: %s)", existing.Email)
	}

	// Convert ST to AT
	log.Println("[AddToken] Converting ST to AT...")
	result, err := tm.flowClient.STToAT(st)
	if err != nil {
		return nil, fmt.Errorf("ST to AT failed: %w", err)
	}

	at, _ := result["access_token"].(string)
	expires, _ := result["expires"].(string)
	userInfo, _ := result["user"].(map[string]interface{})

	email := ""
	name := ""
	if userInfo != nil {
		email, _ = userInfo["email"].(string)
		name, _ = userInfo["name"].(string)
	}

	var atExpires *time.Time
	if expires != "" {
		if t, err := time.Parse(time.RFC3339, expires); err == nil {
			atExpires = &t
		}
	}

	// Get credits
	credits := 0
	userPaygateTier := ""
	if creditsResult, err := tm.flowClient.GetCredits(at); err == nil {
		if c, ok := creditsResult["credits"].(float64); ok {
			credits = int(c)
		}
		if tier, ok := creditsResult["userPaygateTier"].(string); ok {
			userPaygateTier = tier
		}
	}

	// Handle project
	if projectID == "" {
		if projectName == "" {
			projectName = time.Now().Format("Jan 02 - 15:04")
		}
		var err error
		projectID, err = tm.flowClient.CreateProject(st, projectName)
		if err != nil {
			return nil, fmt.Errorf("failed to create project: %w", err)
		}
		log.Printf("[AddToken] Created project: %s (ID: %s)", projectName, projectID)
	} else if projectName == "" {
		projectName = time.Now().Format("Jan 02 - 15:04")
	}

	// Create token
	token := &models.Token{
		ST:                 st,
		AT:                 at,
		ATExpires:          atExpires,
		Email:              email,
		Name:               name,
		Remark:             remark,
		IsActive:           true,
		Credits:            credits,
		UserPaygateTier:    userPaygateTier,
		CurrentProjectID:   projectID,
		CurrentProjectName: projectName,
		ImageEnabled:       imageEnabled,
		VideoEnabled:       videoEnabled,
		ImageConcurrency:   imageConcurrency,
		VideoConcurrency:   videoConcurrency,
	}

	tokenID, err := tm.db.AddToken(token)
	if err != nil {
		return nil, err
	}
	token.ID = tokenID

	// Save project
	project := &models.Project{
		ProjectID:   projectID,
		TokenID:     tokenID,
		ProjectName: projectName,
		ToolName:    "PINHOLE",
		IsActive:    true,
	}
	tm.db.AddProject(project)

	log.Printf("[AddToken] Token added (ID: %d, Email: %s)", tokenID, email)
	return token, nil
}

// UpdateToken updates a token
func (tm *TokenManager) UpdateToken(id int64, updates map[string]interface{}) error {
	// Check if token is banned for 429, clear ban if not expired
	token, err := tm.db.GetToken(id)
	if err != nil {
		return err
	}

	if token != nil && token.BanReason == "429_rate_limit" {
		isExpired := false
		if token.ATExpires != nil {
			isExpired = token.ATExpires.Before(time.Now().UTC())
		}
		if !isExpired {
			log.Printf("[UpdateToken] Token %d edited, clearing 429 ban", id)
			updates["ban_reason"] = nil
			updates["banned_at"] = nil
		}
	}

	return tm.db.UpdateToken(id, updates)
}

// IsATValid checks if AT is valid, refreshes if needed
func (tm *TokenManager) IsATValid(id int64) (bool, error) {
	token, err := tm.db.GetToken(id)
	if err != nil || token == nil {
		return false, err
	}

	if token.AT == "" {
		log.Printf("[AT_CHECK] Token %d: AT missing, refreshing", id)
		return tm.refreshATInternal(id)
	}

	if token.ATExpires == nil {
		log.Printf("[AT_CHECK] Token %d: AT expires unknown, refreshing", id)
		return tm.refreshATInternal(id)
	}

	// Check if expiring within 1 hour
	timeUntilExpiry := time.Until(*token.ATExpires)
	if timeUntilExpiry < time.Hour {
		log.Printf("[AT_CHECK] Token %d: AT expiring in %.0fs, refreshing", id, timeUntilExpiry.Seconds())
		return tm.refreshATInternal(id)
	}

	return true, nil
}

// RefreshAT refreshes the access token and returns the updated token
func (tm *TokenManager) RefreshAT(id int64) (*models.Token, error) {
	success, err := tm.refreshATInternal(id)
	if err != nil || !success {
		return nil, err
	}
	return tm.db.GetToken(id)
}

// refreshATInternal refreshes the access token (internal)
func (tm *TokenManager) refreshATInternal(id int64) (bool, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	token, err := tm.db.GetToken(id)
	if err != nil || token == nil {
		return false, err
	}

	log.Printf("[AT_REFRESH] Token %d: Starting refresh...", id)

	result, err := tm.flowClient.STToAT(token.ST)
	if err != nil {
		log.Printf("[AT_REFRESH] Token %d: Failed - %v", id, err)
		tm.DisableToken(id)
		return false, err
	}

	newAT, _ := result["access_token"].(string)
	expires, _ := result["expires"].(string)

	var newATExpires *time.Time
	if expires != "" {
		if t, err := time.Parse(time.RFC3339, expires); err == nil {
			newATExpires = &t
		}
	}

	updates := map[string]interface{}{
		"at": newAT,
	}
	if newATExpires != nil {
		updates["at_expires"] = newATExpires
	}

	if err := tm.db.UpdateToken(id, updates); err != nil {
		return false, err
	}

	log.Printf("[AT_REFRESH] Token %d: Success", id)

	// Also refresh credits
	if creditsResult, err := tm.flowClient.GetCredits(newAT); err == nil {
		if credits, ok := creditsResult["credits"].(float64); ok {
			tm.db.UpdateToken(id, map[string]interface{}{"credits": int(credits)})
		}
	}

	return true, nil
}

// EnsureProjectExists ensures token has a project
func (tm *TokenManager) EnsureProjectExists(id int64) (string, error) {
	token, err := tm.db.GetToken(id)
	if err != nil || token == nil {
		return "", fmt.Errorf("token not found")
	}

	if token.CurrentProjectID != "" {
		return token.CurrentProjectID, nil
	}

	projectName := time.Now().Format("Jan 02 - 15:04")
	projectID, err := tm.flowClient.CreateProject(token.ST, projectName)
	if err != nil {
		return "", fmt.Errorf("failed to create project: %w", err)
	}

	log.Printf("[PROJECT] Created project for token %d: %s", id, projectName)

	tm.db.UpdateToken(id, map[string]interface{}{
		"current_project_id":   projectID,
		"current_project_name": projectName,
	})

	project := &models.Project{
		ProjectID:   projectID,
		TokenID:     id,
		ProjectName: projectName,
		ToolName:    "PINHOLE",
		IsActive:    true,
	}
	tm.db.AddProject(project)

	return projectID, nil
}

// RecordUsage records token usage
func (tm *TokenManager) RecordUsage(id int64, isVideo bool) error {
	tm.db.UpdateToken(id, map[string]interface{}{
		"last_used_at": time.Now(),
	})

	statType := "image"
	if isVideo {
		statType = "video"
	}
	return tm.db.IncrementTokenStats(id, statType)
}

// RecordError records token error
func (tm *TokenManager) RecordError(id int64) error {
	if err := tm.db.IncrementTokenStats(id, "error"); err != nil {
		return err
	}

	// Check if should auto-disable
	stats, err := tm.db.GetTokenStats(id)
	if err != nil {
		return err
	}

	adminConfig, err := tm.db.GetAdminConfig()
	if err != nil {
		return err
	}

	if stats != nil && stats.ConsecutiveErrorCount >= adminConfig.ErrorBanThreshold {
		log.Printf("[TOKEN_BAN] Token %d consecutive errors (%d) reached threshold (%d), disabling",
			id, stats.ConsecutiveErrorCount, adminConfig.ErrorBanThreshold)
		return tm.DisableToken(id)
	}

	return nil
}

// RecordSuccess records successful request
func (tm *TokenManager) RecordSuccess(id int64) error {
	return tm.db.ResetErrorCount(id)
}

// BanTokenFor429 bans token due to 429 error
func (tm *TokenManager) BanTokenFor429(id int64) error {
	log.Printf("[429_BAN] Banning Token %d (reason: 429 Rate Limit)", id)
	return tm.db.UpdateToken(id, map[string]interface{}{
		"is_active":  false,
		"ban_reason": "429_rate_limit",
		"banned_at":  time.Now().UTC(),
	})
}

// AutoUnban429Tokens automatically unbans 429-banned tokens after 12 hours
func (tm *TokenManager) AutoUnban429Tokens() error {
	tokens, err := tm.db.GetAllTokens()
	if err != nil {
		return err
	}

	now := time.Now().UTC()

	for _, token := range tokens {
		if token.BanReason != "429_rate_limit" || token.IsActive || token.BannedAt == nil {
			continue
		}

		// Check if token is expired
		if token.ATExpires != nil && token.ATExpires.Before(now) {
			log.Printf("[AUTO_UNBAN] Token %d expired, skipping", token.ID)
			continue
		}

		// Check if 12 hours have passed
		timeSinceBan := now.Sub(*token.BannedAt)
		if timeSinceBan >= 12*time.Hour {
			log.Printf("[AUTO_UNBAN] Unbanning Token %d (banned %.1f hours ago)", token.ID, timeSinceBan.Hours())
			tm.db.UpdateToken(token.ID, map[string]interface{}{
				"is_active":  true,
				"ban_reason": nil,
				"banned_at":  nil,
			})
			tm.db.ResetErrorCount(token.ID)
		}
	}

	return nil
}

// RefreshCredits refreshes token credits
func (tm *TokenManager) RefreshCredits(id int64) (int, error) {
	token, err := tm.db.GetToken(id)
	if err != nil || token == nil {
		return 0, err
	}

	valid, err := tm.IsATValid(id)
	if !valid || err != nil {
		return 0, err
	}

	token, _ = tm.db.GetToken(id)

	result, err := tm.flowClient.GetCredits(token.AT)
	if err != nil {
		return 0, err
	}

	credits := 0
	if c, ok := result["credits"].(float64); ok {
		credits = int(c)
	}

	tm.db.UpdateToken(id, map[string]interface{}{"credits": credits})
	return credits, nil
}

// GetTokenStats returns token statistics
func (tm *TokenManager) GetTokenStats(id int64) (*models.TokenStats, error) {
	return tm.db.GetTokenStats(id)
}
