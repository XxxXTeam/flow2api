package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"flow2api/internal/models"

	_ "github.com/mattn/go-sqlite3"
)

type Database struct {
	db *sql.DB
	mu sync.RWMutex
}

var (
	instance *Database
	once     sync.Once
)

func GetInstance() *Database {
	once.Do(func() {
		instance = &Database{}
	})
	return instance
}

func (d *Database) Init(dbPath string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if dbPath == "" {
		dbPath = filepath.Join("data", "flow2api.db")
	}

	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	var err error
	d.db, err = sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Initialize tables
	return d.initTables()
}

func (d *Database) initTables() error {
	tables := []string{
		`CREATE TABLE IF NOT EXISTS tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			st TEXT NOT NULL UNIQUE,
			at TEXT,
			at_expires DATETIME,
			email TEXT NOT NULL,
			name TEXT DEFAULT '',
			remark TEXT,
			is_active BOOLEAN DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_used_at DATETIME,
			use_count INTEGER DEFAULT 0,
			credits INTEGER DEFAULT 0,
			user_paygate_tier TEXT,
			current_project_id TEXT,
			current_project_name TEXT,
			image_enabled BOOLEAN DEFAULT 1,
			video_enabled BOOLEAN DEFAULT 1,
			image_concurrency INTEGER DEFAULT -1,
			video_concurrency INTEGER DEFAULT -1,
			ban_reason TEXT,
			banned_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS projects (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id TEXT NOT NULL,
			token_id INTEGER NOT NULL,
			project_name TEXT NOT NULL,
			tool_name TEXT DEFAULT 'PINHOLE',
			is_active BOOLEAN DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (token_id) REFERENCES tokens(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS token_stats (
			token_id INTEGER PRIMARY KEY,
			image_count INTEGER DEFAULT 0,
			video_count INTEGER DEFAULT 0,
			success_count INTEGER DEFAULT 0,
			error_count INTEGER DEFAULT 0,
			last_success_at DATETIME,
			last_error_at DATETIME,
			today_image_count INTEGER DEFAULT 0,
			today_video_count INTEGER DEFAULT 0,
			today_error_count INTEGER DEFAULT 0,
			today_date TEXT,
			consecutive_error_count INTEGER DEFAULT 0,
			FOREIGN KEY (token_id) REFERENCES tokens(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id TEXT NOT NULL UNIQUE,
			token_id INTEGER NOT NULL,
			model TEXT NOT NULL,
			prompt TEXT NOT NULL,
			status TEXT DEFAULT 'processing',
			progress INTEGER DEFAULT 0,
			result_urls TEXT,
			error_message TEXT,
			scene_id TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			completed_at DATETIME,
			FOREIGN KEY (token_id) REFERENCES tokens(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS admin_config (
			id INTEGER PRIMARY KEY DEFAULT 1,
			username TEXT NOT NULL,
			password TEXT NOT NULL,
			api_key TEXT NOT NULL,
			error_ban_threshold INTEGER DEFAULT 3
		)`,
		`CREATE TABLE IF NOT EXISTS proxy_config (
			id INTEGER PRIMARY KEY DEFAULT 1,
			enabled BOOLEAN DEFAULT 0,
			proxy_url TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS cache_config (
			id INTEGER PRIMARY KEY DEFAULT 1,
			cache_enabled BOOLEAN DEFAULT 0,
			cache_timeout INTEGER DEFAULT 7200,
			cache_base_url TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS debug_config (
			id INTEGER PRIMARY KEY DEFAULT 1,
			enabled BOOLEAN DEFAULT 0,
			log_requests BOOLEAN DEFAULT 1,
			log_responses BOOLEAN DEFAULT 1,
			mask_token BOOLEAN DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS captcha_config (
			id INTEGER PRIMARY KEY DEFAULT 1,
			captcha_method TEXT DEFAULT 'browser',
			yescaptcha_api_key TEXT DEFAULT '',
			yescaptcha_base_url TEXT DEFAULT 'https://api.yescaptcha.com',
			website_key TEXT DEFAULT '6LdsFiUsAAAAAIjVDZcuLhaHiDn5nnHVXVRQGeMV',
			page_action TEXT DEFAULT 'FLOW_GENERATION',
			browser_proxy_enabled BOOLEAN DEFAULT 0,
			browser_proxy_url TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS generation_config (
			id INTEGER PRIMARY KEY DEFAULT 1,
			image_timeout INTEGER DEFAULT 300,
			video_timeout INTEGER DEFAULT 1500
		)`,
	}

	for _, table := range tables {
		if _, err := d.db.Exec(table); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	// Initialize default configs if not exist
	d.initDefaultConfigs()

	return nil
}

func (d *Database) initDefaultConfigs() {
	// Admin config
	d.db.Exec(`INSERT OR IGNORE INTO admin_config (id, username, password, api_key, error_ban_threshold) 
		VALUES (1, 'admin', 'admin123', 'flow2api', 3)`)

	// Proxy config
	d.db.Exec(`INSERT OR IGNORE INTO proxy_config (id, enabled, proxy_url) VALUES (1, 0, '')`)

	// Cache config
	d.db.Exec(`INSERT OR IGNORE INTO cache_config (id, cache_enabled, cache_timeout, cache_base_url) VALUES (1, 0, 7200, '')`)

	// Debug config
	d.db.Exec(`INSERT OR IGNORE INTO debug_config (id, enabled, log_requests, log_responses, mask_token) VALUES (1, 0, 1, 1, 1)`)

	// Captcha config
	d.db.Exec(`INSERT OR IGNORE INTO captcha_config (id, captcha_method, yescaptcha_api_key, yescaptcha_base_url, website_key, page_action) 
		VALUES (1, 'browser', '', 'https://api.yescaptcha.com', '6LdsFiUsAAAAAIjVDZcuLhaHiDn5nnHVXVRQGeMV', 'FLOW_GENERATION')`)

	// Generation config
	d.db.Exec(`INSERT OR IGNORE INTO generation_config (id, image_timeout, video_timeout) VALUES (1, 300, 1500)`)
}

func (d *Database) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

// ========== Token CRUD ==========

func (d *Database) AddToken(token *models.Token) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	result, err := d.db.Exec(`
		INSERT INTO tokens (st, at, at_expires, email, name, remark, is_active, credits, user_paygate_tier,
			current_project_id, current_project_name, image_enabled, video_enabled, image_concurrency, video_concurrency)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		token.ST, token.AT, token.ATExpires, token.Email, token.Name, token.Remark, token.IsActive,
		token.Credits, token.UserPaygateTier, token.CurrentProjectID, token.CurrentProjectName,
		token.ImageEnabled, token.VideoEnabled, token.ImageConcurrency, token.VideoConcurrency)
	if err != nil {
		return 0, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	// Initialize token stats
	d.db.Exec(`INSERT INTO token_stats (token_id) VALUES (?)`, id)

	return id, nil
}

func (d *Database) GetToken(id int64) (*models.Token, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	token := &models.Token{}
	var atExpires, createdAt, lastUsedAt, bannedAt sql.NullTime
	var at, name, remark, userPaygateTier, projectID, projectName, banReason sql.NullString

	err := d.db.QueryRow(`
		SELECT id, st, at, at_expires, email, name, remark, is_active, created_at, last_used_at, use_count,
			credits, user_paygate_tier, current_project_id, current_project_name,
			image_enabled, video_enabled, image_concurrency, video_concurrency, ban_reason, banned_at
		FROM tokens WHERE id = ?`, id).Scan(
		&token.ID, &token.ST, &at, &atExpires, &token.Email, &name, &remark, &token.IsActive,
		&createdAt, &lastUsedAt, &token.UseCount, &token.Credits, &userPaygateTier,
		&projectID, &projectName, &token.ImageEnabled, &token.VideoEnabled,
		&token.ImageConcurrency, &token.VideoConcurrency, &banReason, &bannedAt)
	if err != nil {
		return nil, err
	}

	if at.Valid {
		token.AT = at.String
	}
	if atExpires.Valid {
		token.ATExpires = &atExpires.Time
	}
	if name.Valid {
		token.Name = name.String
	}
	if remark.Valid {
		token.Remark = remark.String
	}
	if createdAt.Valid {
		token.CreatedAt = &createdAt.Time
	}
	if lastUsedAt.Valid {
		token.LastUsedAt = &lastUsedAt.Time
	}
	if userPaygateTier.Valid {
		token.UserPaygateTier = userPaygateTier.String
	}
	if projectID.Valid {
		token.CurrentProjectID = projectID.String
	}
	if projectName.Valid {
		token.CurrentProjectName = projectName.String
	}
	if banReason.Valid {
		token.BanReason = banReason.String
	}
	if bannedAt.Valid {
		token.BannedAt = &bannedAt.Time
	}

	return token, nil
}

func (d *Database) GetTokenByST(st string) (*models.Token, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var id int64
	err := d.db.QueryRow(`SELECT id FROM tokens WHERE st = ?`, st).Scan(&id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	d.mu.RUnlock()
	token, err := d.GetToken(id)
	d.mu.RLock()
	return token, err
}

func (d *Database) GetAllTokens() ([]*models.Token, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`SELECT id FROM tokens ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	d.mu.RUnlock()
	tokens := make([]*models.Token, 0, len(ids))
	for _, id := range ids {
		token, err := d.GetToken(id)
		if err == nil && token != nil {
			tokens = append(tokens, token)
		}
	}
	d.mu.RLock()

	return tokens, nil
}

func (d *Database) GetActiveTokens() ([]*models.Token, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`SELECT id FROM tokens WHERE is_active = 1 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	d.mu.RUnlock()
	tokens := make([]*models.Token, 0, len(ids))
	for _, id := range ids {
		token, err := d.GetToken(id)
		if err == nil && token != nil {
			tokens = append(tokens, token)
		}
	}
	d.mu.RLock()

	return tokens, nil
}

func (d *Database) UpdateToken(id int64, updates map[string]interface{}) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(updates) == 0 {
		return nil
	}

	query := "UPDATE tokens SET "
	args := make([]interface{}, 0, len(updates)+1)
	first := true

	for key, value := range updates {
		if !first {
			query += ", "
		}
		query += key + " = ?"
		args = append(args, value)
		first = false
	}

	query += " WHERE id = ?"
	args = append(args, id)

	_, err := d.db.Exec(query, args...)
	return err
}

func (d *Database) DeleteToken(id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`DELETE FROM tokens WHERE id = ?`, id)
	return err
}

// ========== Token Stats ==========

func (d *Database) GetTokenStats(tokenID int64) (*models.TokenStats, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	stats := &models.TokenStats{TokenID: tokenID}
	var lastSuccessAt, lastErrorAt sql.NullTime
	var todayDate sql.NullString

	err := d.db.QueryRow(`
		SELECT image_count, video_count, success_count, error_count, last_success_at, last_error_at,
			today_image_count, today_video_count, today_error_count, today_date, consecutive_error_count
		FROM token_stats WHERE token_id = ?`, tokenID).Scan(
		&stats.ImageCount, &stats.VideoCount, &stats.SuccessCount, &stats.ErrorCount,
		&lastSuccessAt, &lastErrorAt, &stats.TodayImageCount, &stats.TodayVideoCount,
		&stats.TodayErrorCount, &todayDate, &stats.ConsecutiveErrorCount)
	if err != nil {
		if err == sql.ErrNoRows {
			return stats, nil
		}
		return nil, err
	}

	if lastSuccessAt.Valid {
		stats.LastSuccessAt = &lastSuccessAt.Time
	}
	if lastErrorAt.Valid {
		stats.LastErrorAt = &lastErrorAt.Time
	}
	if todayDate.Valid {
		stats.TodayDate = todayDate.String
	}

	return stats, nil
}

func (d *Database) IncrementTokenStats(tokenID int64, statType string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	today := time.Now().Format("2006-01-02")

	// Reset today's counters if date changed
	d.db.Exec(`UPDATE token_stats SET today_image_count = 0, today_video_count = 0, today_error_count = 0, today_date = ? 
		WHERE token_id = ? AND (today_date IS NULL OR today_date != ?)`, today, tokenID, today)

	var query string
	switch statType {
	case "image":
		query = `UPDATE token_stats SET image_count = image_count + 1, today_image_count = today_image_count + 1, 
			success_count = success_count + 1, last_success_at = CURRENT_TIMESTAMP, consecutive_error_count = 0 WHERE token_id = ?`
	case "video":
		query = `UPDATE token_stats SET video_count = video_count + 1, today_video_count = today_video_count + 1,
			success_count = success_count + 1, last_success_at = CURRENT_TIMESTAMP, consecutive_error_count = 0 WHERE token_id = ?`
	case "error":
		query = `UPDATE token_stats SET error_count = error_count + 1, today_error_count = today_error_count + 1,
			last_error_at = CURRENT_TIMESTAMP, consecutive_error_count = consecutive_error_count + 1 WHERE token_id = ?`
	default:
		return fmt.Errorf("unknown stat type: %s", statType)
	}

	_, err := d.db.Exec(query, tokenID)
	return err
}

func (d *Database) ResetErrorCount(tokenID int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`UPDATE token_stats SET consecutive_error_count = 0 WHERE token_id = ?`, tokenID)
	return err
}

// ========== Project ==========

func (d *Database) AddProject(project *models.Project) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	result, err := d.db.Exec(`
		INSERT INTO projects (project_id, token_id, project_name, tool_name, is_active)
		VALUES (?, ?, ?, ?, ?)`,
		project.ProjectID, project.TokenID, project.ProjectName, project.ToolName, project.IsActive)
	if err != nil {
		return 0, err
	}

	return result.LastInsertId()
}

// ========== Task ==========

func (d *Database) CreateTask(task *models.Task) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	resultURLs := ""
	if len(task.ResultURLs) > 0 {
		data, _ := json.Marshal(task.ResultURLs)
		resultURLs = string(data)
	}

	result, err := d.db.Exec(`
		INSERT INTO tasks (task_id, token_id, model, prompt, status, progress, result_urls, error_message, scene_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.TaskID, task.TokenID, task.Model, task.Prompt, task.Status, task.Progress,
		resultURLs, task.ErrorMessage, task.SceneID)
	if err != nil {
		return 0, err
	}

	return result.LastInsertId()
}

func (d *Database) UpdateTask(taskID string, updates map[string]interface{}) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(updates) == 0 {
		return nil
	}

	query := "UPDATE tasks SET "
	args := make([]interface{}, 0, len(updates)+1)
	first := true

	for key, value := range updates {
		if !first {
			query += ", "
		}
		query += key + " = ?"
		if key == "result_urls" {
			if urls, ok := value.([]string); ok {
				data, _ := json.Marshal(urls)
				args = append(args, string(data))
			} else {
				args = append(args, value)
			}
		} else {
			args = append(args, value)
		}
		first = false
	}

	query += " WHERE task_id = ?"
	args = append(args, taskID)

	_, err := d.db.Exec(query, args...)
	return err
}

// ========== Admin Config ==========

func (d *Database) GetAdminConfig() (*models.AdminConfig, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	config := &models.AdminConfig{}
	err := d.db.QueryRow(`SELECT id, username, password, api_key, error_ban_threshold FROM admin_config WHERE id = 1`).Scan(
		&config.ID, &config.Username, &config.Password, &config.APIKey, &config.ErrorBanThreshold)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func (d *Database) UpdateAdminConfig(updates map[string]interface{}) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(updates) == 0 {
		return nil
	}

	query := "UPDATE admin_config SET "
	args := make([]interface{}, 0, len(updates))
	first := true

	for key, value := range updates {
		if !first {
			query += ", "
		}
		query += key + " = ?"
		args = append(args, value)
		first = false
	}

	query += " WHERE id = 1"
	_, err := d.db.Exec(query, args...)
	return err
}

// ========== Proxy Config ==========

func (d *Database) GetProxyConfig() (*models.ProxyConfig, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	config := &models.ProxyConfig{}
	var proxyURL sql.NullString
	err := d.db.QueryRow(`SELECT id, enabled, proxy_url FROM proxy_config WHERE id = 1`).Scan(
		&config.ID, &config.Enabled, &proxyURL)
	if err != nil {
		return nil, err
	}
	if proxyURL.Valid {
		config.ProxyURL = proxyURL.String
	}
	return config, nil
}

func (d *Database) UpdateProxyConfig(enabled bool, proxyURL string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`UPDATE proxy_config SET enabled = ?, proxy_url = ? WHERE id = 1`, enabled, proxyURL)
	return err
}

// ========== Cache Config ==========

func (d *Database) GetCacheConfig() (*models.CacheConfigDB, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	config := &models.CacheConfigDB{}
	var baseURL sql.NullString
	err := d.db.QueryRow(`SELECT id, cache_enabled, cache_timeout, cache_base_url FROM cache_config WHERE id = 1`).Scan(
		&config.ID, &config.CacheEnabled, &config.CacheTimeout, &baseURL)
	if err != nil {
		return nil, err
	}
	if baseURL.Valid {
		config.CacheBaseURL = baseURL.String
	}
	return config, nil
}

func (d *Database) UpdateCacheConfig(enabled bool, timeout int, baseURL string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`UPDATE cache_config SET cache_enabled = ?, cache_timeout = ?, cache_base_url = ?, updated_at = CURRENT_TIMESTAMP WHERE id = 1`,
		enabled, timeout, baseURL)
	return err
}

// ========== Debug Config ==========

func (d *Database) GetDebugConfig() (*models.DebugConfigDB, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	config := &models.DebugConfigDB{}
	err := d.db.QueryRow(`SELECT id, enabled, log_requests, log_responses, mask_token FROM debug_config WHERE id = 1`).Scan(
		&config.ID, &config.Enabled, &config.LogRequests, &config.LogResponses, &config.MaskToken)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func (d *Database) UpdateDebugConfig(enabled bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`UPDATE debug_config SET enabled = ?, updated_at = CURRENT_TIMESTAMP WHERE id = 1`, enabled)
	return err
}

// ========== Captcha Config ==========

func (d *Database) GetCaptchaConfig() (*models.CaptchaConfigDB, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	config := &models.CaptchaConfigDB{}
	var proxyURL sql.NullString
	err := d.db.QueryRow(`SELECT id, captcha_method, yescaptcha_api_key, yescaptcha_base_url, website_key, page_action, 
		browser_proxy_enabled, browser_proxy_url FROM captcha_config WHERE id = 1`).Scan(
		&config.ID, &config.CaptchaMethod, &config.YesCaptchaAPIKey, &config.YesCaptchaBaseURL,
		&config.WebsiteKey, &config.PageAction, &config.BrowserProxyEnabled, &proxyURL)
	if err != nil {
		return nil, err
	}
	if proxyURL.Valid {
		config.BrowserProxyURL = proxyURL.String
	}
	return config, nil
}

func (d *Database) UpdateCaptchaConfig(updates map[string]interface{}) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(updates) == 0 {
		return nil
	}

	query := "UPDATE captcha_config SET "
	args := make([]interface{}, 0, len(updates))
	first := true

	for key, value := range updates {
		if !first {
			query += ", "
		}
		query += key + " = ?"
		args = append(args, value)
		first = false
	}

	query += ", updated_at = CURRENT_TIMESTAMP WHERE id = 1"
	_, err := d.db.Exec(query, args...)
	return err
}

// ========== Generation Config ==========

func (d *Database) GetGenerationConfig() (*models.GenerationConfigDB, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	config := &models.GenerationConfigDB{}
	err := d.db.QueryRow(`SELECT id, image_timeout, video_timeout FROM generation_config WHERE id = 1`).Scan(
		&config.ID, &config.ImageTimeout, &config.VideoTimeout)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func (d *Database) UpdateGenerationConfig(imageTimeout, videoTimeout int) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`UPDATE generation_config SET image_timeout = ?, video_timeout = ? WHERE id = 1`,
		imageTimeout, videoTimeout)
	return err
}
