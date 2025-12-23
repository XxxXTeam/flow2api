package config

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Global     GlobalConfig     `toml:"global"`
	Server     ServerConfig     `toml:"server"`
	Flow       FlowConfig       `toml:"flow"`
	Cache      CacheConfig      `toml:"cache"`
	Debug      DebugConfig      `toml:"debug"`
	Generation GenerationConfig `toml:"generation"`
	Captcha    CaptchaConfig    `toml:"captcha"`

	mu sync.RWMutex
}

type GlobalConfig struct {
	APIKey        string `toml:"api_key"`
	AdminUsername string `toml:"admin_username"`
	AdminPassword string `toml:"admin_password"`
}

type ServerConfig struct {
	Host string `toml:"host"`
	Port int    `toml:"port"`
}

type FlowConfig struct {
	LabsBaseURL     string  `toml:"labs_base_url"`
	APIBaseURL      string  `toml:"api_base_url"`
	Timeout         int     `toml:"timeout"`
	MaxRetries      int     `toml:"max_retries"`
	PollInterval    float64 `toml:"poll_interval"`
	MaxPollAttempts int     `toml:"max_poll_attempts"`
}

type CacheConfig struct {
	Enabled bool   `toml:"enabled"`
	Timeout int    `toml:"timeout"`
	BaseURL string `toml:"base_url"`
}

type DebugConfig struct {
	Enabled      bool `toml:"enabled"`
	LogRequests  bool `toml:"log_requests"`
	LogResponses bool `toml:"log_responses"`
	MaskToken    bool `toml:"mask_token"`
}

type GenerationConfig struct {
	ImageTimeout int `toml:"image_timeout"`
	VideoTimeout int `toml:"video_timeout"`
}

type CaptchaConfig struct {
	CaptchaMethod       string `toml:"captcha_method"`
	YesCaptchaAPIKey    string `toml:"yescaptcha_api_key"`
	YesCaptchaBaseURL   string `toml:"yescaptcha_base_url"`
	WebsiteKey          string `toml:"website_key"`
	PageAction          string `toml:"page_action"`
	BrowserProxyEnabled bool   `toml:"browser_proxy_enabled"`
	BrowserProxyURL     string `toml:"browser_proxy_url"`
}

var (
	cfg  *Config
	once sync.Once
)

func Load(configPath string) (*Config, error) {
	var err error
	once.Do(func() {
		cfg = &Config{}

		// Set defaults
		cfg.Server.Host = "0.0.0.0"
		cfg.Server.Port = 8000
		cfg.Flow.LabsBaseURL = "https://labs.google/fx/api"
		cfg.Flow.APIBaseURL = "https://aisandbox-pa.googleapis.com/v1"
		cfg.Flow.Timeout = 120
		cfg.Flow.MaxRetries = 3
		cfg.Flow.PollInterval = 3.0
		cfg.Flow.MaxPollAttempts = 500
		cfg.Cache.Timeout = 7200
		cfg.Generation.ImageTimeout = 300
		cfg.Generation.VideoTimeout = 1500
		cfg.Captcha.CaptchaMethod = "browser"
		cfg.Captcha.YesCaptchaBaseURL = "https://api.yescaptcha.com"
		cfg.Captcha.WebsiteKey = "6LdsFiUsAAAAAIjVDZcuLhaHiDn5nnHVXVRQGeMV"
		cfg.Captcha.PageAction = "FLOW_GENERATION"
		cfg.Global.APIKey = "flow2api"
		cfg.Global.AdminUsername = "admin"
		cfg.Global.AdminPassword = "admin123"

		// Load from file if exists
		if configPath == "" {
			configPath = filepath.Join("config", "setting.toml")
		}

		if _, statErr := os.Stat(configPath); statErr == nil {
			_, err = toml.DecodeFile(configPath, cfg)
		}
	})

	return cfg, err
}

func Get() *Config {
	if cfg == nil {
		cfg, _ = Load("")
	}
	return cfg
}

func (c *Config) SetAPIKey(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Global.APIKey = key
}

func (c *Config) GetAPIKey() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Global.APIKey
}

func (c *Config) SetAdminCredentials(username, password string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Global.AdminUsername = username
	c.Global.AdminPassword = password
}

func (c *Config) SetCacheEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Cache.Enabled = enabled
}

func (c *Config) SetCacheTimeout(timeout int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Cache.Timeout = timeout
}

func (c *Config) SetCacheBaseURL(url string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Cache.BaseURL = url
}

func (c *Config) SetDebugEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Debug.Enabled = enabled
}

func (c *Config) SetCaptchaMethod(method string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Captcha.CaptchaMethod = method
}

func (c *Config) SetImageTimeout(timeout int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Generation.ImageTimeout = timeout
}

func (c *Config) SetVideoTimeout(timeout int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Generation.VideoTimeout = timeout
}
