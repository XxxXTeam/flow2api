package browser

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"sync"
	"time"

	"flow2api/internal/config"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// CaptchaService handles reCAPTCHA token generation using rod and xvfb
type CaptchaService struct {
	browser     *rod.Browser
	launcher    *launcher.Launcher
	xvfbCmd     *exec.Cmd
	display     string
	websiteKey  string
	mu          sync.Mutex
	initialized bool
}

var (
	captchaInstance *CaptchaService
	captchaOnce     sync.Once
)

// GetCaptchaService returns singleton instance
func GetCaptchaService() *CaptchaService {
	captchaOnce.Do(func() {
		captchaInstance = &CaptchaService{
			websiteKey: "6LdsFiUsAAAAAIjVDZcuLhaHiDn5nnHVXVRQGeMV",
		}
	})
	return captchaInstance
}

// Initialize starts xvfb and browser
func (c *CaptchaService) Initialize() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.initialized {
		return nil
	}

	log.Println("[BrowserCaptcha] Initializing with xvfb...")

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
		// Try common browser paths
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

	log.Printf("[BrowserCaptcha] Using system browser: %s", browserPath)

	// Configure launcher with system browser
	c.launcher = launcher.New().
		Bin(browserPath).
		Headless(false). // Use xvfb instead of headless
		Set("disable-blink-features", "AutomationControlled").
		Set("disable-dev-shm-usage").
		Set("no-sandbox").
		Set("disable-setuid-sandbox").
		Set("disable-infobars").
		Set("disable-background-networking").
		Set("disable-background-timer-throttling").
		Set("disable-backgrounding-occluded-windows").
		Set("disable-breakpad").
		Set("disable-component-extensions-with-background-pages").
		Set("disable-component-update").
		Set("disable-default-apps").
		Set("disable-extensions").
		Set("disable-features", "TranslateUI,BlinkGenPropertyTrees,IsolateOrigins,site-per-process").
		Set("disable-hang-monitor").
		Set("disable-ipc-flooding-protection").
		Set("disable-popup-blocking").
		Set("disable-prompt-on-repost").
		Set("disable-renderer-backgrounding").
		Set("disable-sync").
		Set("enable-features", "NetworkService,NetworkServiceInProcess").
		Set("force-color-profile", "srgb").
		Set("metrics-recording-only").
		Set("no-first-run").
		Set("password-store", "basic").
		Set("use-mock-keychain").
		Set("ignore-certificate-errors").
		Set("window-size", "1920,1080").
		Set("start-maximized").
		Set("lang", "en-US").
		Set("user-agent", getRandomUserAgent()).
		Env("DISPLAY", c.display)

	if proxyURL != "" {
		c.launcher = c.launcher.Proxy(proxyURL)
		log.Printf("[BrowserCaptcha] Using proxy: %s", proxyURL)
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
	log.Printf("[BrowserCaptcha] ✅ Browser initialized with xvfb (display=%s, proxy=%s)", c.display, proxyURL)
	return nil
}

// startXvfb starts the Xvfb virtual display
func (c *CaptchaService) startXvfb() error {
	// Find an available display number
	for display := 99; display < 200; display++ {
		displayStr := fmt.Sprintf(":%d", display)
		lockFile := fmt.Sprintf("/tmp/.X%d-lock", display)

		// Check if display is available
		if _, err := os.Stat(lockFile); os.IsNotExist(err) {
			c.display = displayStr
			break
		}
	}

	if c.display == "" {
		c.display = ":99"
	}

	// Start Xvfb
	c.xvfbCmd = exec.Command("Xvfb", c.display, "-screen", "0", "1920x1080x24", "-ac")
	c.xvfbCmd.Stdout = nil
	c.xvfbCmd.Stderr = nil

	if err := c.xvfbCmd.Start(); err != nil {
		return fmt.Errorf("failed to start Xvfb: %w", err)
	}

	// Wait for Xvfb to be ready
	time.Sleep(500 * time.Millisecond)

	log.Printf("[BrowserCaptcha] Xvfb started on display %s", c.display)
	return nil
}

// stopXvfb stops the Xvfb process
func (c *CaptchaService) stopXvfb() {
	if c.xvfbCmd != nil && c.xvfbCmd.Process != nil {
		c.xvfbCmd.Process.Kill()
		c.xvfbCmd.Wait()
		c.xvfbCmd = nil
		log.Println("[BrowserCaptcha] Xvfb stopped")
	}
}

// GetToken obtains a reCAPTCHA token for the given project
func (c *CaptchaService) GetToken(projectID string) (string, error) {
	if !c.initialized {
		if err := c.Initialize(); err != nil {
			return "", err
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	startTime := time.Now()
	websiteURL := fmt.Sprintf("https://labs.google/fx/tools/flow/project/%s", projectID)

	log.Printf("[BrowserCaptcha] Getting token for: %s", websiteURL)

	// Create new page
	page, err := c.browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		return "", fmt.Errorf("failed to create page: %w", err)
	}
	defer page.Close()

	// Setup browser environment via CDP protocol
	if err := c.setupBrowserEnvironment(page); err != nil {
		log.Printf("[BrowserCaptcha] Warning: Failed to setup browser environment: %v", err)
	}

	// Navigate to page
	err = page.Navigate(websiteURL)
	if err != nil {
		log.Printf("[BrowserCaptcha] Navigation error (may be expected): %v", err)
	}

	// Wait for page to load
	page.WaitLoad()

	// Small delay after page load
	time.Sleep(1 * time.Second)

	// Check if reCAPTCHA is loaded
	log.Println("[BrowserCaptcha] Checking reCAPTCHA...")

	scriptLoaded, err := page.Eval(`() => {
		return window.grecaptcha && typeof window.grecaptcha.execute === 'function';
	}`)
	if err != nil || !scriptLoaded.Value.Bool() {
		// Inject reCAPTCHA script
		log.Println("[BrowserCaptcha] Injecting reCAPTCHA script...")
		_, err = page.Eval(fmt.Sprintf(`() => {
			return new Promise((resolve) => {
				const script = document.createElement('script');
				script.src = 'https://www.google.com/recaptcha/api.js?render=%s';
				script.async = true;
				script.defer = true;
				script.onload = () => resolve(true);
				script.onerror = () => resolve(false);
				document.head.appendChild(script);
			});
		}`, c.websiteKey))
		if err != nil {
			return "", fmt.Errorf("failed to inject script: %w", err)
		}
	}

	// Wait for reCAPTCHA to be ready
	log.Println("[BrowserCaptcha] Waiting for reCAPTCHA to initialize...")
	for i := 0; i < 20; i++ {
		ready, _ := page.Eval(`() => {
			return window.grecaptcha && typeof window.grecaptcha.execute === 'function';
		}`)
		if ready != nil && ready.Value.Bool() {
			log.Printf("[BrowserCaptcha] reCAPTCHA ready (waited %.1fs)", float64(i)*0.5)
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Extra wait for initialization
	time.Sleep(1 * time.Second)

	// Execute reCAPTCHA
	log.Println("[BrowserCaptcha] Executing reCAPTCHA...")
	result, err := page.Eval(fmt.Sprintf(`async () => {
		try {
			if (!window.grecaptcha) {
				return { error: 'grecaptcha not found' };
			}

			await new Promise((resolve, reject) => {
				const timeout = setTimeout(() => reject(new Error('timeout')), 15000);
				if (window.grecaptcha && window.grecaptcha.ready) {
					window.grecaptcha.ready(() => {
						clearTimeout(timeout);
						resolve();
					});
				} else {
					clearTimeout(timeout);
					resolve();
				}
			});

			const token = await window.grecaptcha.execute('%s', {
				action: 'FLOW_GENERATION'
			});

			return { token: token };
		} catch (error) {
			return { error: error.message };
		}
	}`, c.websiteKey))

	if err != nil {
		return "", fmt.Errorf("failed to execute reCAPTCHA: %w", err)
	}

	duration := time.Since(startTime)

	// Parse result
	resultMap := result.Value.Map()
	if errVal, ok := resultMap["error"]; ok && errVal.Str() != "" {
		return "", fmt.Errorf("reCAPTCHA error: %s", errVal.Str())
	}

	if tokenVal, ok := resultMap["token"]; ok {
		token := tokenVal.Str()
		if token != "" {
			log.Printf("[BrowserCaptcha] ✅ Token obtained (took %dms)", duration.Milliseconds())
			return token, nil
		}
	}

	return "", fmt.Errorf("failed to get token: empty response")
}

// Close shuts down the browser and xvfb
func (c *CaptchaService) Close() error {
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

	log.Println("[BrowserCaptcha] Service closed")
	return nil
}

// getRandomUserAgent returns a random realistic Chrome user agent
func getRandomUserAgent() string {
	userAgents := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36",
	}
	return userAgents[rand.Intn(len(userAgents))]
}

// setupBrowserEnvironment configures browser environment via CDP protocol
func (c *CaptchaService) setupBrowserEnvironment(page *rod.Page) error {
	// Set User-Agent via CDP
	userAgent := getRandomUserAgent()
	err := proto.NetworkSetUserAgentOverride{
		UserAgent:      userAgent,
		AcceptLanguage: "en-US,en;q=0.9",
		Platform:       "Win32",
	}.Call(page)
	if err != nil {
		log.Printf("[BrowserEnv] Failed to set user agent: %v", err)
	}

	// Set viewport and device metrics via CDP
	screenWidth := 1920
	screenHeight := 1080
	err = proto.EmulationSetDeviceMetricsOverride{
		Width:             1920,
		Height:            1080,
		DeviceScaleFactor: 1,
		Mobile:            false,
		ScreenWidth:       &screenWidth,
		ScreenHeight:      &screenHeight,
	}.Call(page)
	if err != nil {
		log.Printf("[BrowserEnv] Failed to set device metrics: %v", err)
	}

	// Set geolocation (optional, simulates real location)
	lat := 37.7749
	lng := -122.4194
	acc := 100.0
	err = proto.EmulationSetGeolocationOverride{
		Latitude:  &lat,
		Longitude: &lng,
		Accuracy:  &acc,
	}.Call(page)
	if err != nil {
		log.Printf("[BrowserEnv] Failed to set geolocation: %v", err)
	}

	// Set timezone
	err = proto.EmulationSetTimezoneOverride{
		TimezoneID: "America/Los_Angeles",
	}.Call(page)
	if err != nil {
		log.Printf("[BrowserEnv] Failed to set timezone: %v", err)
	}

	// Set locale
	err = proto.EmulationSetLocaleOverride{
		Locale: "en-US",
	}.Call(page)
	if err != nil {
		log.Printf("[BrowserEnv] Failed to set locale: %v", err)
	}

	// Disable webdriver flag via CDP
	_, err = proto.PageAddScriptToEvaluateOnNewDocument{
		Source: `Object.defineProperty(navigator, 'webdriver', {get: () => undefined});`,
	}.Call(page)
	if err != nil {
		log.Printf("[BrowserEnv] Failed to disable webdriver flag: %v", err)
	}

	// Enable network domain first
	err = proto.NetworkEnable{}.Call(page)
	if err != nil {
		log.Printf("[BrowserEnv] Failed to enable network: %v", err)
	}

	// Set extra HTTP headers using page method
	page.SetExtraHeaders([]string{
		"Accept-Language", "en-US,en;q=0.9",
		"Sec-Ch-Ua", `"Not_A Brand";v="8", "Chromium";v="120", "Google Chrome";v="120"`,
		"Sec-Ch-Ua-Mobile", "?0",
		"Sec-Ch-Ua-Platform", `"Windows"`,
	})

	log.Println("[BrowserEnv] ✅ Browser environment configured via CDP")
	return nil
}
