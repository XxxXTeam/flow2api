package client

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"time"

	"flow2api/internal/browser"
	"flow2api/internal/config"

	"github.com/google/uuid"
)

// FlowClient handles communication with Flow API
type FlowClient struct {
	httpClient  *http.Client
	labsBaseURL string
	apiBaseURL  string
	proxyURL    string
}

// NewFlowClient creates a new Flow API client
func NewFlowClient(proxyURL string) *FlowClient {
	cfg := config.Get()

	transport := &http.Transport{}
	if proxyURL != "" {
		if proxyParsed, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(proxyParsed)
		}
	}

	return &FlowClient{
		httpClient: &http.Client{
			Timeout:   time.Duration(cfg.Flow.Timeout) * time.Second,
			Transport: transport,
		},
		labsBaseURL: cfg.Flow.LabsBaseURL,
		apiBaseURL:  cfg.Flow.APIBaseURL,
		proxyURL:    proxyURL,
	}
}

// makeRequest performs an HTTP request with authentication
func (c *FlowClient) makeRequest(method, urlStr string, body interface{}, useST bool, stToken string, useAT bool, atToken string) (map[string]interface{}, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequest(method, urlStr, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	if useST && stToken != "" {
		req.Header.Set("Cookie", fmt.Sprintf("__Secure-next-auth.session-token=%s", stToken))
	}

	if useAT && atToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", atToken))
	}

	cfg := config.Get()
	if cfg.Debug.Enabled {
		log.Printf("[FlowClient] %s %s", method, urlStr)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP Error %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result, nil
}

// STToAT converts Session Token to Access Token
func (c *FlowClient) STToAT(st string) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/auth/session", c.labsBaseURL)
	return c.makeRequest("GET", url, nil, true, st, false, "")
}

// CreateProject creates a new project
func (c *FlowClient) CreateProject(st, title string) (string, error) {
	url := fmt.Sprintf("%s/trpc/project.createProject", c.labsBaseURL)
	body := map[string]interface{}{
		"json": map[string]interface{}{
			"projectTitle": title,
			"toolName":     "PINHOLE",
		},
	}

	result, err := c.makeRequest("POST", url, body, true, st, false, "")
	if err != nil {
		return "", err
	}

	// Parse result to get project ID
	if resultData, ok := result["result"].(map[string]interface{}); ok {
		if data, ok := resultData["data"].(map[string]interface{}); ok {
			if jsonData, ok := data["json"].(map[string]interface{}); ok {
				if innerResult, ok := jsonData["result"].(map[string]interface{}); ok {
					if projectID, ok := innerResult["projectId"].(string); ok {
						return projectID, nil
					}
				}
			}
		}
	}

	return "", fmt.Errorf("failed to parse project ID from response")
}

// DeleteProject deletes a project
func (c *FlowClient) DeleteProject(st, projectID string) error {
	url := fmt.Sprintf("%s/trpc/project.deleteProject", c.labsBaseURL)
	body := map[string]interface{}{
		"json": map[string]interface{}{
			"projectToDeleteId": projectID,
		},
	}

	_, err := c.makeRequest("POST", url, body, true, st, false, "")
	return err
}

// GetCredits retrieves credit balance
func (c *FlowClient) GetCredits(at string) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/credits", c.apiBaseURL)
	return c.makeRequest("GET", url, nil, false, "", true, at)
}

// UploadImage uploads an image and returns mediaGenerationId
func (c *FlowClient) UploadImage(at string, imageBytes []byte, aspectRatio string) (string, error) {
	// Convert video aspect ratio to image aspect ratio
	if len(aspectRatio) > 6 && aspectRatio[:6] == "VIDEO_" {
		aspectRatio = "IMAGE_" + aspectRatio[6:]
	}

	imageBase64 := base64.StdEncoding.EncodeToString(imageBytes)

	url := fmt.Sprintf("%s:uploadUserImage", c.apiBaseURL)
	body := map[string]interface{}{
		"imageInput": map[string]interface{}{
			"rawImageBytes":  imageBase64,
			"mimeType":       "image/jpeg",
			"isUserUploaded": true,
			"aspectRatio":    aspectRatio,
		},
		"clientContext": map[string]interface{}{
			"sessionId": c.generateSessionID(),
			"tool":      "ASSET_MANAGER",
		},
	}

	result, err := c.makeRequest("POST", url, body, false, "", true, at)
	if err != nil {
		return "", err
	}

	// Parse result
	if mediaGen, ok := result["mediaGenerationId"].(map[string]interface{}); ok {
		if mediaID, ok := mediaGen["mediaGenerationId"].(string); ok {
			return mediaID, nil
		}
	}

	return "", fmt.Errorf("failed to parse media ID from response")
}

// GenerateImage generates an image
func (c *FlowClient) GenerateImage(at, projectID, prompt, modelName, aspectRatio string, imageInputs []map[string]interface{}) (map[string]interface{}, error) {
	recaptchaToken := c.getRecaptchaToken(projectID)
	sessionID := c.generateSessionID()

	url := fmt.Sprintf("%s/projects/%s/flowMedia:batchGenerateImages", c.apiBaseURL, projectID)

	requestData := map[string]interface{}{
		"clientContext": map[string]interface{}{
			"recaptchaToken": recaptchaToken,
			"projectId":      projectID,
			"sessionId":      sessionID,
			"tool":           "PINHOLE",
		},
		"seed":             rand.Intn(99999),
		"imageModelName":   modelName,
		"imageAspectRatio": aspectRatio,
		"prompt":           prompt,
		"imageInputs":      imageInputs,
	}

	body := map[string]interface{}{
		"clientContext": map[string]interface{}{
			"recaptchaToken": recaptchaToken,
			"sessionId":      sessionID,
		},
		"requests": []interface{}{requestData},
	}

	return c.makeRequest("POST", url, body, false, "", true, at)
}

// GenerateVideoText generates video from text
func (c *FlowClient) GenerateVideoText(at, projectID, prompt, modelKey, aspectRatio, userPaygateTier string) (map[string]interface{}, error) {
	recaptchaToken := c.getRecaptchaToken(projectID)
	sessionID := c.generateSessionID()
	sceneID := uuid.New().String()

	url := fmt.Sprintf("%s/video:batchAsyncGenerateVideoText", c.apiBaseURL)

	body := map[string]interface{}{
		"clientContext": map[string]interface{}{
			"recaptchaToken":  recaptchaToken,
			"sessionId":       sessionID,
			"projectId":       projectID,
			"tool":            "PINHOLE",
			"userPaygateTier": userPaygateTier,
		},
		"requests": []interface{}{
			map[string]interface{}{
				"aspectRatio": aspectRatio,
				"seed":        rand.Intn(99999),
				"textInput": map[string]interface{}{
					"prompt": prompt,
				},
				"videoModelKey": modelKey,
				"metadata": map[string]interface{}{
					"sceneId": sceneID,
				},
			},
		},
	}

	return c.makeRequest("POST", url, body, false, "", true, at)
}

// GenerateVideoReferenceImages generates video from reference images
func (c *FlowClient) GenerateVideoReferenceImages(at, projectID, prompt, modelKey, aspectRatio string, referenceImages []map[string]interface{}, userPaygateTier string) (map[string]interface{}, error) {
	recaptchaToken := c.getRecaptchaToken(projectID)
	sessionID := c.generateSessionID()
	sceneID := uuid.New().String()

	url := fmt.Sprintf("%s/video:batchAsyncGenerateVideoReferenceImages", c.apiBaseURL)

	body := map[string]interface{}{
		"clientContext": map[string]interface{}{
			"recaptchaToken":  recaptchaToken,
			"sessionId":       sessionID,
			"projectId":       projectID,
			"tool":            "PINHOLE",
			"userPaygateTier": userPaygateTier,
		},
		"requests": []interface{}{
			map[string]interface{}{
				"aspectRatio": aspectRatio,
				"seed":        rand.Intn(99999),
				"textInput": map[string]interface{}{
					"prompt": prompt,
				},
				"videoModelKey":   modelKey,
				"referenceImages": referenceImages,
				"metadata": map[string]interface{}{
					"sceneId": sceneID,
				},
			},
		},
	}

	return c.makeRequest("POST", url, body, false, "", true, at)
}

// GenerateVideoStartEnd generates video from start and end frames
func (c *FlowClient) GenerateVideoStartEnd(at, projectID, prompt, modelKey, aspectRatio, startMediaID, endMediaID, userPaygateTier string) (map[string]interface{}, error) {
	recaptchaToken := c.getRecaptchaToken(projectID)
	sessionID := c.generateSessionID()
	sceneID := uuid.New().String()

	url := fmt.Sprintf("%s/video:batchAsyncGenerateVideoStartAndEndImage", c.apiBaseURL)

	requestData := map[string]interface{}{
		"aspectRatio": aspectRatio,
		"seed":        rand.Intn(99999),
		"textInput": map[string]interface{}{
			"prompt": prompt,
		},
		"videoModelKey": modelKey,
		"startImage": map[string]interface{}{
			"mediaId": startMediaID,
		},
		"metadata": map[string]interface{}{
			"sceneId": sceneID,
		},
	}

	if endMediaID != "" {
		requestData["endImage"] = map[string]interface{}{
			"mediaId": endMediaID,
		}
	}

	body := map[string]interface{}{
		"clientContext": map[string]interface{}{
			"recaptchaToken":  recaptchaToken,
			"sessionId":       sessionID,
			"projectId":       projectID,
			"tool":            "PINHOLE",
			"userPaygateTier": userPaygateTier,
		},
		"requests": []interface{}{requestData},
	}

	return c.makeRequest("POST", url, body, false, "", true, at)
}

// CheckVideoStatus checks video generation status
func (c *FlowClient) CheckVideoStatus(at string, operations []map[string]interface{}) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/video:batchCheckAsyncVideoGenerationStatus", c.apiBaseURL)
	body := map[string]interface{}{
		"operations": operations,
	}

	return c.makeRequest("POST", url, body, false, "", true, at)
}

// generateSessionID generates a session ID
func (c *FlowClient) generateSessionID() string {
	return fmt.Sprintf(";%d", time.Now().UnixMilli())
}

// getRecaptchaToken gets reCAPTCHA token
func (c *FlowClient) getRecaptchaToken(projectID string) string {
	cfg := config.Get()

	if cfg.Captcha.CaptchaMethod == "browser" {
		// Standard browser mode with xvfb (headless)
		service := browser.GetCaptchaService()
		token, err := service.GetToken(projectID)
		if err != nil {
			log.Printf("[reCAPTCHA] Browser error: %v", err)
			return ""
		}
		return token
	}

	if cfg.Captcha.CaptchaMethod == "personal" {
		// Personal mode with persistent browser profile (for logged-in sessions)
		service := browser.GetPersonalCaptchaService()
		token, err := service.GetToken(projectID)
		if err != nil {
			log.Printf("[reCAPTCHA] Personal browser error: %v", err)
			return ""
		}
		return token
	}

	// YesCaptcha fallback
	if cfg.Captcha.YesCaptchaAPIKey == "" {
		return ""
	}

	return c.getYesCaptchaToken(projectID)
}

// getYesCaptchaToken gets token from YesCaptcha service
func (c *FlowClient) getYesCaptchaToken(projectID string) string {
	cfg := config.Get()
	websiteURL := fmt.Sprintf("https://labs.google/fx/tools/flow/project/%s", projectID)

	// Create task
	createURL := fmt.Sprintf("%s/createTask", cfg.Captcha.YesCaptchaBaseURL)
	createBody := map[string]interface{}{
		"clientKey": cfg.Captcha.YesCaptchaAPIKey,
		"task": map[string]interface{}{
			"websiteURL": websiteURL,
			"websiteKey": cfg.Captcha.WebsiteKey,
			"type":       "RecaptchaV3TaskProxylessM1",
			"pageAction": cfg.Captcha.PageAction,
		},
	}

	bodyBytes, _ := json.Marshal(createBody)
	resp, err := http.Post(createURL, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		log.Printf("[YesCaptcha] Create task error: %v", err)
		return ""
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	taskID, ok := result["taskId"].(string)
	if !ok {
		log.Printf("[YesCaptcha] No taskId in response")
		return ""
	}

	log.Printf("[YesCaptcha] Created task: %s", taskID)

	// Poll for result
	getURL := fmt.Sprintf("%s/getTaskResult", cfg.Captcha.YesCaptchaBaseURL)
	for i := 0; i < 40; i++ {
		time.Sleep(3 * time.Second)

		getBody := map[string]interface{}{
			"clientKey": cfg.Captcha.YesCaptchaAPIKey,
			"taskId":    taskID,
		}

		bodyBytes, _ := json.Marshal(getBody)
		resp, err := http.Post(getURL, "application/json", bytes.NewReader(bodyBytes))
		if err != nil {
			continue
		}

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		if solution, ok := result["solution"].(map[string]interface{}); ok {
			if token, ok := solution["gRecaptchaResponse"].(string); ok && token != "" {
				return token
			}
		}
	}

	return ""
}
