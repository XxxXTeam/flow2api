package services

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"flow2api/internal/client"
	"flow2api/internal/config"
	"flow2api/internal/database"
	"flow2api/internal/models"

	"github.com/google/uuid"
)

// GenerationHandler handles image and video generation
type GenerationHandler struct {
	flowClient         *client.FlowClient
	tokenManager       *TokenManager
	loadBalancer       *LoadBalancer
	db                 *database.Database
	concurrencyManager *ConcurrencyManager
	cacheDir           string
}

// NewGenerationHandler creates a new generation handler
func NewGenerationHandler(
	fc *client.FlowClient,
	tm *TokenManager,
	lb *LoadBalancer,
	db *database.Database,
	cm *ConcurrencyManager,
) *GenerationHandler {
	cacheDir := "tmp"
	os.MkdirAll(cacheDir, 0755)

	return &GenerationHandler{
		flowClient:         fc,
		tokenManager:       tm,
		loadBalancer:       lb,
		db:                 db,
		concurrencyManager: cm,
		cacheDir:           cacheDir,
	}
}

// StreamChunk represents a streaming response chunk
type StreamChunk struct {
	Content      string
	Role         string
	FinishReason string
	IsReasoning  bool
}

// HandleGeneration handles generation requests
func (gh *GenerationHandler) HandleGeneration(model, prompt string, images [][]byte, stream bool, chunkChan chan<- string) error {
	defer close(chunkChan)

	startTime := time.Now()

	// Validate model
	modelConfig, ok := models.ModelConfigs[model]
	if !ok {
		errResp := gh.createErrorResponse(fmt.Sprintf("Unsupported model: %s", model))
		chunkChan <- errResp
		return fmt.Errorf("unsupported model: %s", model)
	}

	generationType := modelConfig.Type
	log.Printf("[GENERATION] Starting - Model: %s, Type: %s, Prompt: %.50s...", model, generationType, prompt)

	// Non-streaming: just check availability
	if !stream {
		isImage := generationType == "image"
		isVideo := generationType == "video"
		token, _ := gh.loadBalancer.SelectToken(isImage, isVideo, model)

		var message string
		if token != nil {
			if isImage {
				message = "Tokens available for image generation. Enable streaming to use generation."
			} else {
				message = "Tokens available for video generation. Enable streaming to use generation."
			}
		} else {
			if isImage {
				message = "No tokens available for image generation"
			} else {
				message = "No tokens available for video generation"
			}
		}

		chunkChan <- gh.createCompletionResponse(message, "", true)
		return nil
	}

	// Send start message
	chunkChan <- gh.createStreamChunk(fmt.Sprintf("✨ %s generation task started\n",
		map[bool]string{true: "Video", false: "Image"}[generationType == "video"]), "", false)

	// Select token
	log.Println("[GENERATION] Selecting token...")
	isImage := generationType == "image"
	isVideo := generationType == "video"
	token, err := gh.loadBalancer.SelectToken(isImage, isVideo, model)
	if err != nil || token == nil {
		errMsg := gh.getNoTokenErrorMessage(generationType)
		log.Printf("[GENERATION] %s", errMsg)
		chunkChan <- gh.createStreamChunk(fmt.Sprintf("❌ %s\n", errMsg), "", false)
		chunkChan <- gh.createErrorResponse(errMsg)
		return fmt.Errorf(errMsg)
	}

	log.Printf("[GENERATION] Selected Token: %d (%s)", token.ID, token.Email)

	// Ensure AT is valid
	log.Println("[GENERATION] Checking AT validity...")
	chunkChan <- gh.createStreamChunk("Initializing generation environment...\n", "", false)

	valid, err := gh.tokenManager.IsATValid(token.ID)
	if !valid || err != nil {
		errMsg := "Token AT invalid or refresh failed"
		log.Printf("[GENERATION] %s", errMsg)
		chunkChan <- gh.createStreamChunk(fmt.Sprintf("❌ %s\n", errMsg), "", false)
		chunkChan <- gh.createErrorResponse(errMsg)
		return fmt.Errorf(errMsg)
	}

	// Refresh token (AT may have been updated)
	token, _ = gh.tokenManager.GetToken(token.ID)

	// Ensure project exists
	log.Println("[GENERATION] Checking/creating project...")
	projectID, err := gh.tokenManager.EnsureProjectExists(token.ID)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to ensure project: %v", err)
		chunkChan <- gh.createStreamChunk(fmt.Sprintf("❌ %s\n", errMsg), "", false)
		chunkChan <- gh.createErrorResponse(errMsg)
		return err
	}
	log.Printf("[GENERATION] Project ID: %s", projectID)

	// Handle generation based on type
	var genErr error
	if generationType == "image" {
		log.Println("[GENERATION] Starting image generation...")
		genErr = gh.handleImageGeneration(token, projectID, modelConfig, prompt, images, chunkChan)
	} else {
		log.Println("[GENERATION] Starting video generation...")
		genErr = gh.handleVideoGeneration(token, projectID, modelConfig, prompt, images, chunkChan)
	}

	if genErr != nil {
		// Check for 429 error
		if strings.Contains(genErr.Error(), "429") {
			log.Printf("[429_BAN] Token %d hit 429, banning", token.ID)
			gh.tokenManager.BanTokenFor429(token.ID)
		} else {
			gh.tokenManager.RecordError(token.ID)
		}
		return genErr
	}

	// Record usage
	gh.tokenManager.RecordUsage(token.ID, isVideo)
	gh.tokenManager.RecordSuccess(token.ID)

	log.Printf("[GENERATION] ✅ Completed in %.2fs", time.Since(startTime).Seconds())
	return nil
}

func (gh *GenerationHandler) handleImageGeneration(token *models.Token, projectID string, modelConfig models.ModelConfig, prompt string, images [][]byte, chunkChan chan<- string) error {
	// Acquire concurrency slot
	if !gh.concurrencyManager.AcquireImage(token.ID) {
		errMsg := "Image concurrency limit reached"
		chunkChan <- gh.createStreamChunk(fmt.Sprintf("❌ %s\n", errMsg), "", false)
		chunkChan <- gh.createErrorResponse(errMsg)
		return fmt.Errorf(errMsg)
	}
	defer gh.concurrencyManager.ReleaseImage(token.ID)

	// Upload images if any
	var imageInputs []map[string]interface{}
	if len(images) > 0 {
		chunkChan <- gh.createStreamChunk(fmt.Sprintf("Uploading %d reference image(s)...\n", len(images)), "", false)

		for i, imgBytes := range images {
			mediaID, err := gh.flowClient.UploadImage(token.AT, imgBytes, modelConfig.AspectRatio)
			if err != nil {
				return fmt.Errorf("failed to upload image %d: %w", i+1, err)
			}
			imageInputs = append(imageInputs, map[string]interface{}{
				"name":           mediaID,
				"imageInputType": "IMAGE_INPUT_TYPE_REFERENCE",
			})
			chunkChan <- gh.createStreamChunk(fmt.Sprintf("Uploaded image %d/%d\n", i+1, len(images)), "", false)
		}
	}

	// Generate
	chunkChan <- gh.createStreamChunk("Generating image...\n", "", false)

	result, err := gh.flowClient.GenerateImage(token.AT, projectID, prompt, modelConfig.ModelName, modelConfig.AspectRatio, imageInputs)
	if err != nil {
		errMsg := fmt.Sprintf("Generation failed: %v", err)
		chunkChan <- gh.createStreamChunk(fmt.Sprintf("❌ %s\n", errMsg), "", false)
		chunkChan <- gh.createErrorResponse(errMsg)
		return err
	}

	// Extract URL
	media, ok := result["media"].([]interface{})
	if !ok || len(media) == 0 {
		errMsg := "Empty generation result"
		chunkChan <- gh.createStreamChunk(fmt.Sprintf("❌ %s\n", errMsg), "", false)
		chunkChan <- gh.createErrorResponse(errMsg)
		return fmt.Errorf(errMsg)
	}

	mediaItem := media[0].(map[string]interface{})
	image := mediaItem["image"].(map[string]interface{})
	genImage := image["generatedImage"].(map[string]interface{})
	imageURL := genImage["fifeUrl"].(string)

	// Cache if enabled
	localURL := imageURL
	cfg := config.Get()
	if cfg.Cache.Enabled {
		chunkChan <- gh.createStreamChunk("Caching image...\n", "", false)
		if cachedURL, err := gh.cacheFile(imageURL, "image"); err == nil {
			localURL = cachedURL
			chunkChan <- gh.createStreamChunk("✅ Image cached\n", "", false)
		} else {
			log.Printf("[CACHE] Failed: %v", err)
			chunkChan <- gh.createStreamChunk(fmt.Sprintf("⚠️ Cache failed: %v\n", err), "", false)
		}
	}

	// Return result
	chunkChan <- gh.createStreamChunk(fmt.Sprintf("![Generated Image](%s)", localURL), "stop", true)
	return nil
}

func (gh *GenerationHandler) handleVideoGeneration(token *models.Token, projectID string, modelConfig models.ModelConfig, prompt string, images [][]byte, chunkChan chan<- string) error {
	// Acquire concurrency slot
	if !gh.concurrencyManager.AcquireVideo(token.ID) {
		errMsg := "Video concurrency limit reached"
		chunkChan <- gh.createStreamChunk(fmt.Sprintf("❌ %s\n", errMsg), "", false)
		chunkChan <- gh.createErrorResponse(errMsg)
		return fmt.Errorf(errMsg)
	}
	defer gh.concurrencyManager.ReleaseVideo(token.ID)

	videoType := modelConfig.VideoType
	imageCount := len(images)

	// Validate images based on video type
	if videoType == "t2v" && imageCount > 0 {
		chunkChan <- gh.createStreamChunk("⚠️ T2V model doesn't support images, ignoring...\n", "", false)
		images = nil
		imageCount = 0
	} else if videoType == "i2v" {
		if imageCount < modelConfig.MinImages || imageCount > modelConfig.MaxImages {
			errMsg := fmt.Sprintf("I2V model requires %d-%d images, got %d", modelConfig.MinImages, modelConfig.MaxImages, imageCount)
			chunkChan <- gh.createStreamChunk(fmt.Sprintf("❌ %s\n", errMsg), "", false)
			chunkChan <- gh.createErrorResponse(errMsg)
			return fmt.Errorf(errMsg)
		}
	}

	// Upload images
	var startMediaID, endMediaID string
	var referenceImages []map[string]interface{}

	if videoType == "i2v" && len(images) > 0 {
		if len(images) == 1 {
			chunkChan <- gh.createStreamChunk("Uploading start frame...\n", "", false)
			var err error
			startMediaID, err = gh.flowClient.UploadImage(token.AT, images[0], modelConfig.AspectRatio)
			if err != nil {
				return fmt.Errorf("failed to upload start frame: %w", err)
			}
		} else if len(images) >= 2 {
			chunkChan <- gh.createStreamChunk("Uploading start and end frames...\n", "", false)
			var err error
			startMediaID, err = gh.flowClient.UploadImage(token.AT, images[0], modelConfig.AspectRatio)
			if err != nil {
				return fmt.Errorf("failed to upload start frame: %w", err)
			}
			endMediaID, err = gh.flowClient.UploadImage(token.AT, images[1], modelConfig.AspectRatio)
			if err != nil {
				return fmt.Errorf("failed to upload end frame: %w", err)
			}
		}
	} else if videoType == "r2v" && len(images) > 0 {
		chunkChan <- gh.createStreamChunk(fmt.Sprintf("Uploading %d reference images...\n", len(images)), "", false)
		for i, img := range images {
			mediaID, err := gh.flowClient.UploadImage(token.AT, img, modelConfig.AspectRatio)
			if err != nil {
				return fmt.Errorf("failed to upload reference image %d: %w", i+1, err)
			}
			referenceImages = append(referenceImages, map[string]interface{}{
				"imageUsageType": "IMAGE_USAGE_TYPE_ASSET",
				"mediaId":        mediaID,
			})
		}
	}

	// Submit generation
	chunkChan <- gh.createStreamChunk("Submitting video generation task...\n", "", false)

	userPaygateTier := token.UserPaygateTier
	if userPaygateTier == "" {
		userPaygateTier = "PAYGATE_TIER_ONE"
	}

	var result map[string]interface{}
	var err error

	if videoType == "i2v" && startMediaID != "" {
		result, err = gh.flowClient.GenerateVideoStartEnd(token.AT, projectID, prompt, modelConfig.ModelKey, modelConfig.AspectRatio, startMediaID, endMediaID, userPaygateTier)
	} else if videoType == "r2v" && len(referenceImages) > 0 {
		result, err = gh.flowClient.GenerateVideoReferenceImages(token.AT, projectID, prompt, modelConfig.ModelKey, modelConfig.AspectRatio, referenceImages, userPaygateTier)
	} else {
		result, err = gh.flowClient.GenerateVideoText(token.AT, projectID, prompt, modelConfig.ModelKey, modelConfig.AspectRatio, userPaygateTier)
	}

	if err != nil {
		errMsg := fmt.Sprintf("Video generation failed: %v", err)
		chunkChan <- gh.createStreamChunk(fmt.Sprintf("❌ %s\n", errMsg), "", false)
		chunkChan <- gh.createErrorResponse(errMsg)
		return err
	}

	// Get operations
	operations, ok := result["operations"].([]interface{})
	if !ok || len(operations) == 0 {
		errMsg := "No operations in response"
		chunkChan <- gh.createStreamChunk(fmt.Sprintf("❌ %s\n", errMsg), "", false)
		chunkChan <- gh.createErrorResponse(errMsg)
		return fmt.Errorf(errMsg)
	}

	operation := operations[0].(map[string]interface{})
	operationData := operation["operation"].(map[string]interface{})
	taskID := operationData["name"].(string)

	// Save task
	task := &models.Task{
		TaskID:  taskID,
		TokenID: token.ID,
		Model:   modelConfig.ModelKey,
		Prompt:  prompt,
		Status:  "processing",
	}
	gh.db.CreateTask(task)

	// Poll for result
	chunkChan <- gh.createStreamChunk("Video generating...\n", "", false)

	return gh.pollVideoResult(token, []map[string]interface{}{operation}, chunkChan)
}

func (gh *GenerationHandler) pollVideoResult(token *models.Token, operations []map[string]interface{}, chunkChan chan<- string) error {
	cfg := config.Get()
	maxAttempts := cfg.Flow.MaxPollAttempts
	pollInterval := time.Duration(cfg.Flow.PollInterval * float64(time.Second))

	for attempt := 0; attempt < maxAttempts; attempt++ {
		time.Sleep(pollInterval)

		result, err := gh.flowClient.CheckVideoStatus(token.AT, operations)
		if err != nil {
			log.Printf("[POLL] Error: %v", err)
			continue
		}

		checkedOps, ok := result["operations"].([]interface{})
		if !ok || len(checkedOps) == 0 {
			continue
		}

		op := checkedOps[0].(map[string]interface{})
		status, _ := op["status"].(string)

		// Progress update every ~20 seconds
		if attempt%7 == 0 {
			progress := min(int(float64(attempt)/float64(maxAttempts)*100), 95)
			chunkChan <- gh.createStreamChunk(fmt.Sprintf("Progress: %d%%\n", progress), "", false)
		}

		if status == "MEDIA_GENERATION_STATUS_SUCCESSFUL" {
			opData := op["operation"].(map[string]interface{})
			metadata := opData["metadata"].(map[string]interface{})
			video := metadata["video"].(map[string]interface{})
			videoURL := video["fifeUrl"].(string)

			// Cache if enabled
			localURL := videoURL
			if cfg.Cache.Enabled {
				chunkChan <- gh.createStreamChunk("Caching video...\n", "", false)
				if cachedURL, err := gh.cacheFile(videoURL, "video"); err == nil {
					localURL = cachedURL
					chunkChan <- gh.createStreamChunk("✅ Video cached\n", "", false)
				}
			}

			// Update task
			taskID := opData["name"].(string)
			gh.db.UpdateTask(taskID, map[string]interface{}{
				"status":       "completed",
				"progress":     100,
				"result_urls":  []string{localURL},
				"completed_at": time.Now(),
			})

			// Return result
			chunkChan <- gh.createStreamChunk(fmt.Sprintf("<video src='%s' controls style='max-width:100%%'></video>", localURL), "stop", true)
			return nil
		} else if strings.HasPrefix(status, "MEDIA_GENERATION_STATUS_ERROR") {
			errMsg := fmt.Sprintf("Video generation failed: %s", status)
			chunkChan <- gh.createStreamChunk(fmt.Sprintf("❌ %s\n", errMsg), "", false)
			chunkChan <- gh.createErrorResponse(errMsg)
			return fmt.Errorf(errMsg)
		}
	}

	errMsg := fmt.Sprintf("Video generation timeout (polled %d times)", maxAttempts)
	chunkChan <- gh.createStreamChunk(fmt.Sprintf("❌ %s\n", errMsg), "", false)
	chunkChan <- gh.createErrorResponse(errMsg)
	return fmt.Errorf(errMsg)
}

func (gh *GenerationHandler) cacheFile(urlStr, mediaType string) (string, error) {
	resp, err := http.Get(urlStr)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	ext := ".jpg"
	if mediaType == "video" {
		ext = ".mp4"
	}

	filename := uuid.New().String() + ext
	filePath := filepath.Join(gh.cacheDir, filename)

	file, err := os.Create(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return "", err
	}

	cfg := config.Get()
	baseURL := cfg.Cache.BaseURL
	if baseURL == "" {
		baseURL = fmt.Sprintf("http://localhost:%d", cfg.Server.Port)
	}

	return fmt.Sprintf("%s/tmp/%s", baseURL, filename), nil
}

func (gh *GenerationHandler) getNoTokenErrorMessage(genType string) string {
	if genType == "image" {
		return "No tokens available for image generation. All tokens are disabled, cooling, locked, or expired."
	}
	return "No tokens available for video generation. All tokens are disabled, cooling, quota exhausted, or expired."
}

func (gh *GenerationHandler) createStreamChunk(content, finishReason string, isContent bool) string {
	chunk := map[string]interface{}{
		"id":      fmt.Sprintf("chatcmpl-%d", time.Now().UnixMilli()),
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   "flow2api",
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"delta":         map[string]interface{}{},
				"finish_reason": nil,
			},
		},
	}

	delta := chunk["choices"].([]map[string]interface{})[0]["delta"].(map[string]interface{})

	if isContent {
		delta["content"] = content
	} else {
		delta["reasoning_content"] = content
	}

	if finishReason != "" {
		chunk["choices"].([]map[string]interface{})[0]["finish_reason"] = finishReason
	}

	data, _ := json.Marshal(chunk)
	return fmt.Sprintf("data: %s\n\n", string(data))
}

func (gh *GenerationHandler) createCompletionResponse(content, mediaType string, isAvailabilityCheck bool) string {
	formattedContent := content
	if !isAvailabilityCheck {
		if mediaType == "video" {
			formattedContent = fmt.Sprintf("```html\n<video src='%s' controls></video>\n```", content)
		} else {
			formattedContent = fmt.Sprintf("![Generated Image](%s)", content)
		}
	}

	response := map[string]interface{}{
		"id":      fmt.Sprintf("chatcmpl-%d", time.Now().UnixMilli()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   "flow2api",
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": formattedContent,
				},
				"finish_reason": "stop",
			},
		},
	}

	data, _ := json.Marshal(response)
	return string(data)
}

func (gh *GenerationHandler) createErrorResponse(errMsg string) string {
	response := map[string]interface{}{
		"error": map[string]interface{}{
			"message": errMsg,
			"type":    "invalid_request_error",
			"code":    "generation_failed",
		},
	}

	data, _ := json.Marshal(response)
	return string(data)
}
