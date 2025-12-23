package browser

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"flow2api/internal/config"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// PersonalCaptchaService handles reCAPTCHA with persistent browser profile (for logged-in sessions)
type PersonalCaptchaService struct {
	browser     *rod.Browser
	launcher    *launcher.Launcher
	xvfbCmd     *exec.Cmd
	display     string
	websiteKey  string
	userDataDir string
	mu          sync.Mutex
	initialized bool
}

var (
	personalInstance *PersonalCaptchaService
	personalOnce     sync.Once
)

// GetPersonalCaptchaService returns singleton instance for personal mode
func GetPersonalCaptchaService() *PersonalCaptchaService {
	personalOnce.Do(func() {
		cwd, _ := os.Getwd()
		personalInstance = &PersonalCaptchaService{
			websiteKey:  "6LdsFiUsAAAAAIjVDZcuLhaHiDn5nnHVXVRQGeMV",
			userDataDir: filepath.Join(cwd, "browser_data"),
		}
	})
	return personalInstance
}

// Initialize starts xvfb and browser with persistent user data
func (c *PersonalCaptchaService) Initialize() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.initialized {
		return nil
	}

	log.Printf("[PersonalCaptcha] Initializing with user data dir: %s", c.userDataDir)

	// Create user data directory if not exists
	if err := os.MkdirAll(c.userDataDir, 0755); err != nil {
		return fmt.Errorf("failed to create user data dir: %w", err)
	}

	// Start Xvfb
	if err := c.startXvfb(); err != nil {
		return fmt.Errorf("failed to start xvfb: %w", err)
	}

	// Get captcha config for proxy
	cfg := config.Get()
	var proxyURL string
	if cfg.Captcha.BrowserProxyEnabled && cfg.Captcha.BrowserProxyURL != "" {
		proxyURL = cfg.Captcha.BrowserProxyURL
	}

	// Find system-installed browser
	browserPath, found := launcher.LookPath()
	if !found {
		commonPaths := []string{
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/usr/bin/google-chrome",
			"/usr/bin/google-chrome-stable",
			"/snap/bin/chromium",
			"/opt/google/chrome/chrome",
		}
		for _, p := range commonPaths {
			if _, err := os.Stat(p); err == nil {
				browserPath = p
				found = true
				break
			}
		}
	}

	if !found || browserPath == "" {
		c.stopXvfb()
		return fmt.Errorf("no browser found. Please install chromium or chrome")
	}

	log.Printf("[PersonalCaptcha] Using system browser: %s", browserPath)

	// Configure launcher with system browser and user data directory
	c.launcher = launcher.New().
		Bin(browserPath).
		UserDataDir(c.userDataDir).
		Headless(false). // Use xvfb
		Set("disable-blink-features", "AutomationControlled").
		Set("disable-dev-shm-usage").
		Set("no-sandbox").
		Set("disable-setuid-sandbox").
		Set("disable-infobars").
		Set("disable-extensions").
		Set("window-size", "1280,720").
		Set("lang", "en-US").
		Env("DISPLAY", c.display)

	if proxyURL != "" {
		c.launcher = c.launcher.Proxy(proxyURL)
		log.Printf("[PersonalCaptcha] Using proxy: %s", proxyURL)
	}

	// Launch browser
	url, err := c.launcher.Launch()
	if err != nil {
		c.stopXvfb()
		return fmt.Errorf("failed to launch browser: %w", err)
	}

	c.browser = rod.New().ControlURL(url)
	if err := c.browser.Connect(); err != nil {
		c.stopXvfb()
		return fmt.Errorf("failed to connect to browser: %w", err)
	}

	c.initialized = true
	log.Printf("[PersonalCaptcha] ✅ Browser initialized with persistent profile (dir=%s)", c.userDataDir)
	return nil
}

// startXvfb starts the Xvfb virtual display
func (c *PersonalCaptchaService) startXvfb() error {
	for display := 99; display < 200; display++ {
		displayStr := fmt.Sprintf(":%d", display)
		lockFile := fmt.Sprintf("/tmp/.X%d-lock", display)
		if _, err := os.Stat(lockFile); os.IsNotExist(err) {
			c.display = displayStr
			break
		}
	}
	if c.display == "" {
		c.display = ":99"
	}

	c.xvfbCmd = exec.Command("Xvfb", c.display, "-screen", "0", "1280x720x24", "-ac")
	c.xvfbCmd.Stdout = nil
	c.xvfbCmd.Stderr = nil

	if err := c.xvfbCmd.Start(); err != nil {
		return fmt.Errorf("failed to start Xvfb: %w", err)
	}

	time.Sleep(500 * time.Millisecond)
	log.Printf("[PersonalCaptcha] Xvfb started on display %s", c.display)
	return nil
}

// stopXvfb stops the Xvfb process
func (c *PersonalCaptchaService) stopXvfb() {
	if c.xvfbCmd != nil && c.xvfbCmd.Process != nil {
		c.xvfbCmd.Process.Kill()
		c.xvfbCmd.Wait()
		c.xvfbCmd = nil
	}
}

// GetToken obtains a reCAPTCHA token using persistent browser session
func (c *PersonalCaptchaService) GetToken(projectID string) (string, error) {
	if !c.initialized {
		if err := c.Initialize(); err != nil {
			return "", err
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	startTime := time.Now()
	websiteURL := fmt.Sprintf("https://labs.google/fx/tools/flow/project/%s", projectID)

	log.Printf("[PersonalCaptcha] Getting token for: %s", websiteURL)

	// Create new page (tab) in existing browser context
	page, err := c.browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		return "", fmt.Errorf("failed to create page: %w", err)
	}
	defer page.Close()

	// Set viewport
	page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width:  1280,
		Height: 720,
	})

	// Navigate to page
	err = page.Navigate(websiteURL)
	if err != nil {
		log.Printf("[PersonalCaptcha] Navigation warning: %v", err)
	}

	// Wait for page to load
	page.WaitLoad()
	time.Sleep(1 * time.Second)

	// Check if reCAPTCHA is loaded
	log.Println("[PersonalCaptcha] Checking reCAPTCHA...")
	scriptLoaded, _ := page.Eval(`() => !!(window.grecaptcha && window.grecaptcha.execute)`)

	if scriptLoaded == nil || !scriptLoaded.Value.Bool() {
		log.Println("[PersonalCaptcha] Injecting reCAPTCHA script...")
		_, _ = page.Eval(fmt.Sprintf(`() => {
			const script = document.createElement('script');
			script.src = 'https://www.google.com/recaptcha/api.js?render=%s';
			script.async = true;
			script.defer = true;
			document.head.appendChild(script);
		}`, c.websiteKey))
		time.Sleep(2 * time.Second)
	}

	// Wait for reCAPTCHA ready
	for i := 0; i < 20; i++ {
		ready, _ := page.Eval(`() => !!(window.grecaptcha && window.grecaptcha.execute)`)
		if ready != nil && ready.Value.Bool() {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Execute reCAPTCHA
	log.Println("[PersonalCaptcha] Executing reCAPTCHA...")
	result, err := page.Eval(fmt.Sprintf(`async () => {
		try {
			return await window.grecaptcha.execute('%s', { action: 'FLOW_GENERATION' });
		} catch (e) {
			return null;
		}
	}`, c.websiteKey))

	if err != nil {
		return "", fmt.Errorf("failed to execute reCAPTCHA: %w", err)
	}

	duration := time.Since(startTime)

	if result != nil && result.Value.Str() != "" {
		token := result.Value.Str()
		log.Printf("[PersonalCaptcha] ✅ Token obtained (took %dms)", duration.Milliseconds())
		return token, nil
	}

	return "", fmt.Errorf("failed to get token: empty response")
}

// OpenLoginWindow opens a browser window for manual Google login
func (c *PersonalCaptchaService) OpenLoginWindow() error {
	if !c.initialized {
		if err := c.Initialize(); err != nil {
			return err
		}
	}

	page, err := c.browser.Page(proto.TargetCreateTarget{URL: "https://accounts.google.com/"})
	if err != nil {
		return fmt.Errorf("failed to open login page: %w", err)
	}

	log.Println("[PersonalCaptcha] ============================================")
	log.Println("[PersonalCaptcha] 请在浏览器中登录Google账号")
	log.Println("[PersonalCaptcha] 登录完成后，无需关闭浏览器")
	log.Println("[PersonalCaptcha] 下次运行时会自动使用此登录状态")
	log.Printf("[PersonalCaptcha] 用户数据目录: %s", c.userDataDir)
	log.Println("[PersonalCaptcha] ============================================")

	// Wait for user to login (blocking)
	page.WaitLoad()

	return nil
}

// Close shuts down the browser and xvfb
func (c *PersonalCaptchaService) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.browser != nil {
		c.browser.Close()
		c.browser = nil
	}

	if c.launcher != nil {
		c.launcher.Cleanup()
		c.launcher = nil
	}

	c.stopXvfb()
	c.initialized = false

	log.Println("[PersonalCaptcha] Service closed")
	return nil
}

// ProxyConfig holds parsed proxy configuration
type ProxyConfig struct {
	Server   string
	Username string
	Password string
}

// ParseProxyURL parses proxy URL and extracts components
func ParseProxyURL(proxyURL string) *ProxyConfig {
	if proxyURL == "" {
		return nil
	}

	// Pattern: protocol://[username:password@]host:port
	pattern := regexp.MustCompile(`^(socks5|http|https)://(?:([^:]+):([^@]+)@)?([^:]+):(\d+)$`)
	matches := pattern.FindStringSubmatch(proxyURL)

	if len(matches) < 6 {
		return nil
	}

	protocol := matches[1]
	username := matches[2]
	password := matches[3]
	host := matches[4]
	port := matches[5]

	config := &ProxyConfig{
		Server: fmt.Sprintf("%s://%s:%s", protocol, host, port),
	}

	if username != "" && password != "" {
		config.Username = username
		config.Password = password
	}

	return config
}

// ValidateBrowserProxyURL validates proxy URL format for browser use
func ValidateBrowserProxyURL(proxyURL string) (bool, string) {
	if proxyURL == "" {
		return true, ""
	}

	config := ParseProxyURL(proxyURL)
	if config == nil {
		return false, "代理URL格式错误，正确格式：http://host:port 或 socks5://host:port"
	}

	// Get protocol from server
	pattern := regexp.MustCompile(`^(socks5|http|https)://`)
	matches := pattern.FindStringSubmatch(config.Server)
	if len(matches) < 2 {
		return false, "无法识别代理协议"
	}

	protocol := matches[1]

	// SOCKS5 doesn't support authentication in browser
	if protocol == "socks5" && config.Username != "" {
		return false, "浏览器不支持带认证的SOCKS5代理，请使用HTTP代理或移除SOCKS5认证"
	}

	return true, ""
}
