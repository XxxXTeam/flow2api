package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"flow2api/internal/api"
	"flow2api/internal/browser"
	"flow2api/internal/client"
	"flow2api/internal/config"
	"flow2api/internal/database"
	"flow2api/internal/services"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

func main() {
	fmt.Println("============================================================")
	fmt.Println("Flow2API (Go Version) Starting...")
	fmt.Println("============================================================")

	// Load configuration
	cfg, err := config.Load("")
	if err != nil {
		log.Printf("Warning: Failed to load config: %v (using defaults)", err)
	}

	// Initialize database
	db := database.GetInstance()
	if err := db.Init(""); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Load configurations from database
	if adminConfig, err := db.GetAdminConfig(); err == nil {
		cfg.SetAdminCredentials(adminConfig.Username, adminConfig.Password)
		cfg.SetAPIKey(adminConfig.APIKey)
	}

	if cacheConfig, err := db.GetCacheConfig(); err == nil {
		cfg.SetCacheEnabled(cacheConfig.CacheEnabled)
		cfg.SetCacheTimeout(cacheConfig.CacheTimeout)
		cfg.SetCacheBaseURL(cacheConfig.CacheBaseURL)
	}

	if generationConfig, err := db.GetGenerationConfig(); err == nil {
		cfg.SetImageTimeout(generationConfig.ImageTimeout)
		cfg.SetVideoTimeout(generationConfig.VideoTimeout)
	}

	if debugConfig, err := db.GetDebugConfig(); err == nil {
		cfg.SetDebugEnabled(debugConfig.Enabled)
	}

	if captchaConfig, err := db.GetCaptchaConfig(); err == nil {
		cfg.SetCaptchaMethod(captchaConfig.CaptchaMethod)
	}

	// Get proxy configuration
	proxyURL := ""
	if proxyConfig, err := db.GetProxyConfig(); err == nil && proxyConfig.Enabled {
		proxyURL = proxyConfig.ProxyURL
	}

	// Initialize browser captcha service based on method
	if cfg.Captcha.CaptchaMethod == "browser" {
		captchaService := browser.GetCaptchaService()
		if err := captchaService.Initialize(); err != nil {
			log.Printf("Warning: Failed to initialize browser captcha: %v", err)
		} else {
			log.Println("✓ Browser captcha service initialized (with xvfb)")
		}
		defer captchaService.Close()
	} else if cfg.Captcha.CaptchaMethod == "personal" {
		personalService := browser.GetPersonalCaptchaService()
		if err := personalService.Initialize(); err != nil {
			log.Printf("Warning: Failed to initialize personal captcha: %v", err)
		} else {
			log.Println("✓ Personal captcha service initialized (persistent profile)")
		}
		defer personalService.Close()
	}

	// Initialize services
	flowClient := client.NewFlowClient(proxyURL)
	tokenManager := services.NewTokenManager(db, flowClient)
	concurrencyManager := services.NewConcurrencyManager()
	loadBalancer := services.NewLoadBalancer(tokenManager, concurrencyManager)
	generationHandler := services.NewGenerationHandler(flowClient, tokenManager, loadBalancer, db, concurrencyManager)

	// Initialize concurrency limits
	tokens, _ := tokenManager.GetAllTokens()
	concurrencyManager.Initialize(tokens)

	// Create Fiber app
	app := fiber.New(fiber.Config{
		AppName:      "Flow2API",
		ServerHeader: "Flow2API",
		BodyLimit:    50 * 1024 * 1024, // 50MB
	})

	// Middleware
	app.Use(logger.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowMethods: "GET,POST,PUT,DELETE,OPTIONS",
		AllowHeaders: "*",
	}))

	// Static files
	app.Static("/tmp", "./tmp")
	app.Static("/static", "./static")

	// HTML routes
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendFile("./static/login.html")
	})
	app.Get("/login", func(c *fiber.Ctx) error {
		return c.SendFile("./static/login.html")
	})
	app.Get("/manage", func(c *fiber.Ctx) error {
		return c.SendFile("./static/manage.html")
	})

	// API routes
	apiHandler := api.NewHandler(generationHandler, tokenManager, cfg)
	apiHandler.SetupRoutes(app)

	// Admin routes
	adminHandler := api.NewAdminHandler(tokenManager, db, cfg)
	adminHandler.SetupAdminRoutes(app)

	// Start auto-unban task
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if err := tokenManager.AutoUnban429Tokens(); err != nil {
				log.Printf("Auto-unban task error: %v", err)
			}
		}
	}()

	// Print startup info
	fmt.Printf("✓ Database initialized\n")
	fmt.Printf("✓ Total tokens: %d\n", len(tokens))
	fmt.Printf("✓ Cache: %s (timeout: %ds)\n", map[bool]string{true: "Enabled", false: "Disabled"}[cfg.Cache.Enabled], cfg.Cache.Timeout)
	fmt.Printf("✓ Captcha method: %s\n", cfg.Captcha.CaptchaMethod)
	fmt.Printf("✓ Server running on http://%s:%d\n", cfg.Server.Host, cfg.Server.Port)
	fmt.Println("============================================================")

	// Graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		fmt.Println("\nFlow2API Shutting down...")
		app.Shutdown()
	}()

	// Start server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	if err := app.Listen(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
