package api

import (
	"crypto/rand"
	"encoding/hex"
	"sync"

	"flow2api/internal/config"
	"flow2api/internal/database"
	"flow2api/internal/services"

	"github.com/gofiber/fiber/v2"
)

// AdminHandler handles admin API routes
type AdminHandler struct {
	tokenManager *services.TokenManager
	db           *database.Database
	cfg          *config.Config
	adminTokens  sync.Map
}

// NewAdminHandler creates a new admin handler
func NewAdminHandler(tm *services.TokenManager, db *database.Database, cfg *config.Config) *AdminHandler {
	return &AdminHandler{
		tokenManager: tm,
		db:           db,
		cfg:          cfg,
	}
}

// SetupAdminRoutes configures admin routes
func (h *AdminHandler) SetupAdminRoutes(app *fiber.App) {
	// Auth (frontend uses /api/login)
	app.Post("/api/login", h.Login)
	app.Post("/api/logout", h.adminAuthMiddleware, h.Logout)

	// Stats
	app.Get("/api/stats", h.adminAuthMiddleware, h.GetStats)

	// Tokens
	app.Get("/api/tokens", h.adminAuthMiddleware, h.GetTokens)
	app.Post("/api/tokens", h.adminAuthMiddleware, h.AddToken)
	app.Put("/api/tokens/:id", h.adminAuthMiddleware, h.UpdateToken)
	app.Delete("/api/tokens/:id", h.adminAuthMiddleware, h.DeleteToken)
	app.Post("/api/tokens/:id/enable", h.adminAuthMiddleware, h.EnableToken)
	app.Post("/api/tokens/:id/disable", h.adminAuthMiddleware, h.DisableToken)
	app.Post("/api/tokens/:id/refresh-credits", h.adminAuthMiddleware, h.RefreshCredits)
	app.Post("/api/tokens/:id/refresh-at", h.adminAuthMiddleware, h.RefreshAT)
	app.Post("/api/tokens/import", h.adminAuthMiddleware, h.ImportTokens)

	// Admin config
	app.Get("/api/admin/config", h.adminAuthMiddleware, h.GetAdminConfig)
	app.Post("/api/admin/config", h.adminAuthMiddleware, h.UpdateAdminConfig)
	app.Post("/api/admin/password", h.adminAuthMiddleware, h.ChangePassword)
	app.Post("/api/admin/apikey", h.adminAuthMiddleware, h.UpdateAPIKey)
	app.Post("/api/admin/debug", h.adminAuthMiddleware, h.UpdateDebugConfig)

	// Proxy config
	app.Get("/api/proxy/config", h.adminAuthMiddleware, h.GetProxyConfig)
	app.Post("/api/proxy/config", h.adminAuthMiddleware, h.UpdateProxyConfig)

	// Cache config
	app.Get("/api/cache/config", h.adminAuthMiddleware, h.GetCacheConfig)
	app.Post("/api/cache/config", h.adminAuthMiddleware, h.UpdateCacheConfig)
	app.Post("/api/cache/enabled", h.adminAuthMiddleware, h.UpdateCacheEnabled)
	app.Post("/api/cache/base-url", h.adminAuthMiddleware, h.UpdateCacheBaseURL)

	// Captcha config
	app.Get("/api/captcha/config", h.adminAuthMiddleware, h.GetCaptchaConfig)
	app.Post("/api/captcha/config", h.adminAuthMiddleware, h.UpdateCaptchaConfig)

	// Generation timeout config
	app.Get("/api/generation/timeout", h.adminAuthMiddleware, h.GetGenerationConfig)
	app.Post("/api/generation/timeout", h.adminAuthMiddleware, h.UpdateGenerationConfig)

	// Token auto-refresh config
	app.Get("/api/token-refresh/config", h.adminAuthMiddleware, h.GetTokenRefreshConfig)
	app.Post("/api/token-refresh/config", h.adminAuthMiddleware, h.UpdateTokenRefreshConfig)

	// Logs
	app.Get("/api/logs", h.adminAuthMiddleware, h.GetLogs)
}

func (h *AdminHandler) adminAuthMiddleware(c *fiber.Ctx) error {
	auth := c.Get("Authorization")
	if auth == "" || len(auth) < 8 {
		return c.Status(401).JSON(fiber.Map{"error": "Missing authorization"})
	}

	token := auth[7:] // Remove "Bearer "
	if _, ok := h.adminTokens.Load(token); !ok {
		return c.Status(401).JSON(fiber.Map{"error": "Invalid or expired admin token"})
	}

	c.Locals("adminToken", token)
	return c.Next()
}

func (h *AdminHandler) generateToken() string {
	bytes := make([]byte, 32)
	rand.Read(bytes)
	return "admin-" + hex.EncodeToString(bytes)
}

// Login handles admin login
func (h *AdminHandler) Login(c *fiber.Ctx) error {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
	}

	adminConfig, err := h.db.GetAdminConfig()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to get admin config"})
	}

	if req.Username != adminConfig.Username || req.Password != adminConfig.Password {
		return c.Status(401).JSON(fiber.Map{"error": "Invalid credentials"})
	}

	token := h.generateToken()
	h.adminTokens.Store(token, true)

	return c.JSON(fiber.Map{
		"success":  true,
		"token":    token,
		"username": adminConfig.Username,
	})
}

// Logout handles admin logout
func (h *AdminHandler) Logout(c *fiber.Ctx) error {
	token := c.Locals("adminToken").(string)
	h.adminTokens.Delete(token)
	return c.JSON(fiber.Map{"success": true, "message": "Logged out"})
}

// ChangePassword changes admin password
func (h *AdminHandler) ChangePassword(c *fiber.Ctx) error {
	var req struct {
		Username    string `json:"username"`
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
	}

	adminConfig, _ := h.db.GetAdminConfig()
	if req.OldPassword != adminConfig.Password {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid old password"})
	}

	updates := map[string]interface{}{"password": req.NewPassword}
	if req.Username != "" {
		updates["username"] = req.Username
	}

	if err := h.db.UpdateAdminConfig(updates); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to update password"})
	}

	// Clear all admin tokens
	h.adminTokens.Range(func(key, _ interface{}) bool {
		h.adminTokens.Delete(key)
		return true
	})

	return c.JSON(fiber.Map{"success": true, "message": "Password changed, please re-login"})
}

// GetTokens returns all tokens
func (h *AdminHandler) GetTokens(c *fiber.Ctx) error {
	tokens, err := h.tokenManager.GetAllTokens()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	var result []fiber.Map
	for _, t := range tokens {
		stats, _ := h.tokenManager.GetTokenStats(t.ID)

		item := fiber.Map{
			"id":                   t.ID,
			"st":                   t.ST,
			"at":                   t.AT,
			"token":                t.AT,
			"email":                t.Email,
			"name":                 t.Name,
			"remark":               t.Remark,
			"is_active":            t.IsActive,
			"credits":              t.Credits,
			"user_paygate_tier":    t.UserPaygateTier,
			"current_project_id":   t.CurrentProjectID,
			"current_project_name": t.CurrentProjectName,
			"image_enabled":        t.ImageEnabled,
			"video_enabled":        t.VideoEnabled,
			"image_concurrency":    t.ImageConcurrency,
			"video_concurrency":    t.VideoConcurrency,
			"use_count":            t.UseCount,
			"ban_reason":           t.BanReason,
		}

		if t.ATExpires != nil {
			item["at_expires"] = t.ATExpires.Format("2006-01-02T15:04:05Z")
		}
		if t.CreatedAt != nil {
			item["created_at"] = t.CreatedAt.Format("2006-01-02T15:04:05Z")
		}
		if t.LastUsedAt != nil {
			item["last_used_at"] = t.LastUsedAt.Format("2006-01-02T15:04:05Z")
		}
		if t.BannedAt != nil {
			item["banned_at"] = t.BannedAt.Format("2006-01-02T15:04:05Z")
		}

		if stats != nil {
			item["stats"] = fiber.Map{
				"image_count":             stats.ImageCount,
				"video_count":             stats.VideoCount,
				"success_count":           stats.SuccessCount,
				"error_count":             stats.ErrorCount,
				"today_image_count":       stats.TodayImageCount,
				"today_video_count":       stats.TodayVideoCount,
				"today_error_count":       stats.TodayErrorCount,
				"consecutive_error_count": stats.ConsecutiveErrorCount,
			}
		}

		result = append(result, item)
	}

	return c.JSON(fiber.Map{"tokens": result})
}

// AddToken adds a new token
func (h *AdminHandler) AddToken(c *fiber.Ctx) error {
	var req struct {
		ST               string `json:"st"`
		ProjectID        string `json:"project_id"`
		ProjectName      string `json:"project_name"`
		Remark           string `json:"remark"`
		ImageEnabled     bool   `json:"image_enabled"`
		VideoEnabled     bool   `json:"video_enabled"`
		ImageConcurrency int    `json:"image_concurrency"`
		VideoConcurrency int    `json:"video_concurrency"`
	}
	req.ImageEnabled = true
	req.VideoEnabled = true
	req.ImageConcurrency = -1
	req.VideoConcurrency = -1

	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
	}

	if req.ST == "" {
		return c.Status(400).JSON(fiber.Map{"error": "ST is required"})
	}

	token, err := h.tokenManager.AddToken(
		req.ST, req.ProjectID, req.ProjectName, req.Remark,
		req.ImageEnabled, req.VideoEnabled, req.ImageConcurrency, req.VideoConcurrency,
	)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"success": true, "token": token})
}

// UpdateToken updates a token
func (h *AdminHandler) UpdateToken(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid token ID"})
	}

	var req map[string]interface{}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
	}

	updates := make(map[string]interface{})
	if v, ok := req["st"]; ok {
		updates["st"] = v
	}
	if v, ok := req["project_id"]; ok {
		updates["current_project_id"] = v
	}
	if v, ok := req["project_name"]; ok {
		updates["current_project_name"] = v
	}
	if v, ok := req["remark"]; ok {
		updates["remark"] = v
	}
	if v, ok := req["image_enabled"]; ok {
		updates["image_enabled"] = v
	}
	if v, ok := req["video_enabled"]; ok {
		updates["video_enabled"] = v
	}
	if v, ok := req["image_concurrency"]; ok {
		updates["image_concurrency"] = v
	}
	if v, ok := req["video_concurrency"]; ok {
		updates["video_concurrency"] = v
	}

	if err := h.tokenManager.UpdateToken(int64(id), updates); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"success": true})
}

// DeleteToken deletes a token
func (h *AdminHandler) DeleteToken(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid token ID"})
	}

	if err := h.tokenManager.DeleteToken(int64(id)); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"success": true})
}

// EnableToken enables a token
func (h *AdminHandler) EnableToken(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid token ID"})
	}

	if err := h.tokenManager.EnableToken(int64(id)); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"success": true})
}

// DisableToken disables a token
func (h *AdminHandler) DisableToken(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid token ID"})
	}

	if err := h.tokenManager.DisableToken(int64(id)); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"success": true})
}

// RefreshCredits refreshes token credits
func (h *AdminHandler) RefreshCredits(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid token ID"})
	}

	credits, err := h.tokenManager.RefreshCredits(int64(id))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"success": true, "credits": credits})
}

// Config endpoints
func (h *AdminHandler) GetProxyConfig(c *fiber.Ctx) error {
	cfg, _ := h.db.GetProxyConfig()
	return c.JSON(cfg)
}

func (h *AdminHandler) UpdateProxyConfig(c *fiber.Ctx) error {
	var req struct {
		Enabled  bool   `json:"proxy_enabled"`
		ProxyURL string `json:"proxy_url"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
	}
	if err := h.db.UpdateProxyConfig(req.Enabled, req.ProxyURL); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

func (h *AdminHandler) GetCacheConfig(c *fiber.Ctx) error {
	cfg, _ := h.db.GetCacheConfig()
	return c.JSON(cfg)
}

func (h *AdminHandler) UpdateCacheConfig(c *fiber.Ctx) error {
	var req struct {
		Enabled bool   `json:"cache_enabled"`
		Timeout int    `json:"cache_timeout"`
		BaseURL string `json:"cache_base_url"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
	}
	if err := h.db.UpdateCacheConfig(req.Enabled, req.Timeout, req.BaseURL); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	h.cfg.SetCacheEnabled(req.Enabled)
	h.cfg.SetCacheTimeout(req.Timeout)
	h.cfg.SetCacheBaseURL(req.BaseURL)
	return c.JSON(fiber.Map{"success": true})
}

func (h *AdminHandler) GetDebugConfig(c *fiber.Ctx) error {
	cfg, _ := h.db.GetDebugConfig()
	return c.JSON(cfg)
}

func (h *AdminHandler) UpdateDebugConfig(c *fiber.Ctx) error {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
	}
	if err := h.db.UpdateDebugConfig(req.Enabled); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	h.cfg.SetDebugEnabled(req.Enabled)
	return c.JSON(fiber.Map{"success": true})
}

func (h *AdminHandler) GetCaptchaConfig(c *fiber.Ctx) error {
	cfg, _ := h.db.GetCaptchaConfig()
	return c.JSON(cfg)
}

func (h *AdminHandler) UpdateCaptchaConfig(c *fiber.Ctx) error {
	var req map[string]interface{}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
	}
	if err := h.db.UpdateCaptchaConfig(req); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	if method, ok := req["captcha_method"].(string); ok {
		h.cfg.SetCaptchaMethod(method)
	}
	return c.JSON(fiber.Map{"success": true})
}

func (h *AdminHandler) GetGenerationConfig(c *fiber.Ctx) error {
	cfg, _ := h.db.GetGenerationConfig()
	return c.JSON(cfg)
}

func (h *AdminHandler) UpdateGenerationConfig(c *fiber.Ctx) error {
	var req struct {
		ImageTimeout int `json:"image_timeout"`
		VideoTimeout int `json:"video_timeout"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
	}
	if err := h.db.UpdateGenerationConfig(req.ImageTimeout, req.VideoTimeout); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	h.cfg.SetImageTimeout(req.ImageTimeout)
	h.cfg.SetVideoTimeout(req.VideoTimeout)
	return c.JSON(fiber.Map{"success": true})
}

func (h *AdminHandler) GetAdminConfig(c *fiber.Ctx) error {
	cfg, _ := h.db.GetAdminConfig()
	return c.JSON(fiber.Map{
		"username":            cfg.Username,
		"api_key":             cfg.APIKey,
		"error_ban_threshold": cfg.ErrorBanThreshold,
	})
}

func (h *AdminHandler) UpdateAdminConfig(c *fiber.Ctx) error {
	var req struct {
		ErrorBanThreshold int `json:"error_ban_threshold"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
	}
	if err := h.db.UpdateAdminConfig(map[string]interface{}{"error_ban_threshold": req.ErrorBanThreshold}); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

func (h *AdminHandler) UpdateAPIKey(c *fiber.Ctx) error {
	var req struct {
		NewAPIKey string `json:"new_api_key"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
	}
	if err := h.db.UpdateAdminConfig(map[string]interface{}{"api_key": req.NewAPIKey}); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	h.cfg.SetAPIKey(req.NewAPIKey)
	return c.JSON(fiber.Map{"success": true})
}

// GetStats returns statistics
func (h *AdminHandler) GetStats(c *fiber.Ctx) error {
	tokens, _ := h.tokenManager.GetAllTokens()

	var totalTokens, activeTokens int
	var totalImages, totalVideos, totalErrors int
	var todayImages, todayVideos, todayErrors int

	totalTokens = len(tokens)
	for _, t := range tokens {
		if t.IsActive {
			activeTokens++
		}
		stats, _ := h.tokenManager.GetTokenStats(t.ID)
		if stats != nil {
			totalImages += stats.ImageCount
			totalVideos += stats.VideoCount
			totalErrors += stats.ErrorCount
			todayImages += stats.TodayImageCount
			todayVideos += stats.TodayVideoCount
			todayErrors += stats.TodayErrorCount
		}
	}

	return c.JSON(fiber.Map{
		"total_tokens":  totalTokens,
		"active_tokens": activeTokens,
		"total_images":  totalImages,
		"total_videos":  totalVideos,
		"total_errors":  totalErrors,
		"today_images":  todayImages,
		"today_videos":  todayVideos,
		"today_errors":  todayErrors,
	})
}

// RefreshAT refreshes access token for a token
func (h *AdminHandler) RefreshAT(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid token ID"})
	}

	token, err := h.tokenManager.RefreshAT(int64(id))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "detail": err.Error()})
	}

	result := fiber.Map{
		"id":    token.ID,
		"email": token.Email,
	}
	if token.ATExpires != nil {
		result["at_expires"] = token.ATExpires.Format("2006-01-02T15:04:05Z")
	}

	return c.JSON(fiber.Map{"success": true, "token": result})
}

// ImportTokens imports tokens from JSON
func (h *AdminHandler) ImportTokens(c *fiber.Ctx) error {
	var req struct {
		Tokens []struct {
			ST               string `json:"session_token"`
			AT               string `json:"access_token"`
			Email            string `json:"email"`
			IsActive         bool   `json:"is_active"`
			ImageEnabled     bool   `json:"image_enabled"`
			VideoEnabled     bool   `json:"video_enabled"`
			ImageConcurrency int    `json:"image_concurrency"`
			VideoConcurrency int    `json:"video_concurrency"`
		} `json:"tokens"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
	}

	var added, updated int
	for _, t := range req.Tokens {
		if t.ST == "" && t.AT == "" {
			continue
		}
		st := t.ST
		if st == "" {
			st = t.AT
		}
		_, err := h.tokenManager.AddToken(st, "", "", "", t.ImageEnabled, t.VideoEnabled, t.ImageConcurrency, t.VideoConcurrency)
		if err != nil {
			updated++
		} else {
			added++
		}
	}

	return c.JSON(fiber.Map{"success": true, "added": added, "updated": updated})
}

// UpdateCacheEnabled updates cache enabled status
func (h *AdminHandler) UpdateCacheEnabled(c *fiber.Ctx) error {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
	}

	cfg, _ := h.db.GetCacheConfig()
	if err := h.db.UpdateCacheConfig(req.Enabled, cfg.CacheTimeout, cfg.CacheBaseURL); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	h.cfg.SetCacheEnabled(req.Enabled)
	return c.JSON(fiber.Map{"success": true})
}

// UpdateCacheBaseURL updates cache base URL
func (h *AdminHandler) UpdateCacheBaseURL(c *fiber.Ctx) error {
	var req struct {
		BaseURL string `json:"base_url"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
	}

	cfg, _ := h.db.GetCacheConfig()
	if err := h.db.UpdateCacheConfig(cfg.CacheEnabled, cfg.CacheTimeout, req.BaseURL); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	h.cfg.SetCacheBaseURL(req.BaseURL)
	return c.JSON(fiber.Map{"success": true})
}

// GetTokenRefreshConfig returns token auto-refresh configuration
func (h *AdminHandler) GetTokenRefreshConfig(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"success":      true,
		"auto_refresh": true,
	})
}

// UpdateTokenRefreshConfig updates token auto-refresh configuration
func (h *AdminHandler) UpdateTokenRefreshConfig(c *fiber.Ctx) error {
	var req struct {
		AutoRefresh bool `json:"auto_refresh"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
	}
	return c.JSON(fiber.Map{"success": true})
}

// GetLogs returns request logs
func (h *AdminHandler) GetLogs(c *fiber.Ctx) error {
	// Return empty logs for now - can be enhanced with actual logging
	return c.JSON([]fiber.Map{})
}
