package api

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strings"

	"flow2api/internal/config"
	"flow2api/internal/models"
	"flow2api/internal/services"

	"github.com/gofiber/fiber/v2"
)

// Handler holds API handlers
type Handler struct {
	generationHandler *services.GenerationHandler
	tokenManager      *services.TokenManager
	cfg               *config.Config
}

// NewHandler creates a new API handler
func NewHandler(gh *services.GenerationHandler, tm *services.TokenManager, cfg *config.Config) *Handler {
	return &Handler{
		generationHandler: gh,
		tokenManager:      tm,
		cfg:               cfg,
	}
}

// SetupRoutes configures all API routes
func (h *Handler) SetupRoutes(app *fiber.App) {
	// OpenAI-compatible routes
	app.Get("/v1/models", h.authMiddleware, h.ListModels)
	app.Post("/v1/chat/completions", h.authMiddleware, h.ChatCompletions)
}

// authMiddleware verifies API key
func (h *Handler) authMiddleware(c *fiber.Ctx) error {
	auth := c.Get("Authorization")
	if auth == "" {
		return c.Status(401).JSON(fiber.Map{"error": "Missing authorization"})
	}

	apiKey := strings.TrimPrefix(auth, "Bearer ")
	if apiKey != h.cfg.GetAPIKey() {
		return c.Status(401).JSON(fiber.Map{"error": "Invalid API key"})
	}

	return c.Next()
}

// ListModels returns available models
func (h *Handler) ListModels(c *fiber.Ctx) error {
	var modelList []fiber.Map

	for modelID, cfg := range models.ModelConfigs {
		description := cfg.Type + " generation"
		if cfg.Type == "image" {
			description += " - " + cfg.ModelName
		} else {
			description += " - " + cfg.ModelKey
		}

		modelList = append(modelList, fiber.Map{
			"id":          modelID,
			"object":      "model",
			"owned_by":    "flow2api",
			"description": description,
		})
	}

	return c.JSON(fiber.Map{
		"object": "list",
		"data":   modelList,
	})
}

// ChatCompletions handles chat completion requests
func (h *Handler) ChatCompletions(c *fiber.Ctx) error {
	var req models.ChatCompletionRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if len(req.Messages) == 0 {
		return c.Status(400).JSON(fiber.Map{"error": "Messages cannot be empty"})
	}

	// Extract prompt and images
	lastMessage := req.Messages[len(req.Messages)-1]
	prompt, images := h.extractContent(lastMessage)

	// Fallback to deprecated image parameter
	if req.Image != "" && len(images) == 0 {
		if imgBytes := h.parseBase64Image(req.Image); imgBytes != nil {
			images = append(images, imgBytes)
		}
	}

	if prompt == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Prompt cannot be empty"})
	}

	if req.Stream {
		// Streaming response
		c.Set("Content-Type", "text/event-stream")
		c.Set("Cache-Control", "no-cache")
		c.Set("Connection", "keep-alive")
		c.Set("X-Accel-Buffering", "no")

		c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
			chunkChan := make(chan string, 100)

			go func() {
				h.generationHandler.HandleGeneration(req.Model, prompt, images, true, chunkChan)
			}()

			for chunk := range chunkChan {
				w.WriteString(chunk)
				w.Flush()
			}

			w.WriteString("data: [DONE]\n\n")
			w.Flush()
		})

		return nil
	}

	// Non-streaming response
	chunkChan := make(chan string, 100)

	go func() {
		h.generationHandler.HandleGeneration(req.Model, prompt, images, false, chunkChan)
	}()

	var result string
	for chunk := range chunkChan {
		result = chunk
	}

	if result != "" {
		var jsonResult map[string]interface{}
		if err := json.Unmarshal([]byte(result), &jsonResult); err == nil {
			return c.JSON(jsonResult)
		}
		return c.JSON(fiber.Map{"result": result})
	}

	return c.Status(500).JSON(fiber.Map{"error": "Generation failed: No response"})
}

// extractContent extracts prompt and images from message
func (h *Handler) extractContent(msg models.ChatMessage) (string, [][]byte) {
	var prompt string
	var images [][]byte

	switch content := msg.Content.(type) {
	case string:
		prompt = content
	case []interface{}:
		for _, item := range content {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}

			itemType, _ := itemMap["type"].(string)
			if itemType == "text" {
				prompt, _ = itemMap["text"].(string)
			} else if itemType == "image_url" {
				if imageURL, ok := itemMap["image_url"].(map[string]interface{}); ok {
					if url, ok := imageURL["url"].(string); ok {
						if imgBytes := h.parseBase64Image(url); imgBytes != nil {
							images = append(images, imgBytes)
						}
					}
				}
			}
		}
	}

	return prompt, images
}

// parseBase64Image parses base64 image data
func (h *Handler) parseBase64Image(imageURL string) []byte {
	if !strings.HasPrefix(imageURL, "data:image") {
		return nil
	}

	re := regexp.MustCompile(`base64,(.+)`)
	matches := re.FindStringSubmatch(imageURL)
	if len(matches) < 2 {
		return nil
	}

	imageBytes, err := base64.StdEncoding.DecodeString(matches[1])
	if err != nil {
		return nil
	}

	return imageBytes
}
