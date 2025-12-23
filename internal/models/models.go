package models

import (
	"time"
)

// Token represents a Flow API token
type Token struct {
	ID                 int64      `json:"id"`
	ST                 string     `json:"st"`                   // Session Token
	AT                 string     `json:"at,omitempty"`         // Access Token
	ATExpires          *time.Time `json:"at_expires,omitempty"` // AT expiration time
	Email              string     `json:"email"`
	Name               string     `json:"name,omitempty"`
	Remark             string     `json:"remark,omitempty"`
	IsActive           bool       `json:"is_active"`
	CreatedAt          *time.Time `json:"created_at,omitempty"`
	LastUsedAt         *time.Time `json:"last_used_at,omitempty"`
	UseCount           int        `json:"use_count"`
	Credits            int        `json:"credits"`
	UserPaygateTier    string     `json:"user_paygate_tier,omitempty"`
	CurrentProjectID   string     `json:"current_project_id,omitempty"`
	CurrentProjectName string     `json:"current_project_name,omitempty"`
	ImageEnabled       bool       `json:"image_enabled"`
	VideoEnabled       bool       `json:"video_enabled"`
	ImageConcurrency   int        `json:"image_concurrency"`
	VideoConcurrency   int        `json:"video_concurrency"`
	BanReason          string     `json:"ban_reason,omitempty"`
	BannedAt           *time.Time `json:"banned_at,omitempty"`
}

// Project represents a Flow project
type Project struct {
	ID          int64      `json:"id"`
	ProjectID   string     `json:"project_id"`
	TokenID     int64      `json:"token_id"`
	ProjectName string     `json:"project_name"`
	ToolName    string     `json:"tool_name"`
	IsActive    bool       `json:"is_active"`
	CreatedAt   *time.Time `json:"created_at,omitempty"`
}

// TokenStats represents token usage statistics
type TokenStats struct {
	TokenID               int64      `json:"token_id"`
	ImageCount            int        `json:"image_count"`
	VideoCount            int        `json:"video_count"`
	SuccessCount          int        `json:"success_count"`
	ErrorCount            int        `json:"error_count"`
	LastSuccessAt         *time.Time `json:"last_success_at,omitempty"`
	LastErrorAt           *time.Time `json:"last_error_at,omitempty"`
	TodayImageCount       int        `json:"today_image_count"`
	TodayVideoCount       int        `json:"today_video_count"`
	TodayErrorCount       int        `json:"today_error_count"`
	TodayDate             string     `json:"today_date,omitempty"`
	ConsecutiveErrorCount int        `json:"consecutive_error_count"`
}

// Task represents a generation task
type Task struct {
	ID           int64      `json:"id"`
	TaskID       string     `json:"task_id"`
	TokenID      int64      `json:"token_id"`
	Model        string     `json:"model"`
	Prompt       string     `json:"prompt"`
	Status       string     `json:"status"` // processing, completed, failed
	Progress     int        `json:"progress"`
	ResultURLs   []string   `json:"result_urls,omitempty"`
	ErrorMessage string     `json:"error_message,omitempty"`
	SceneID      string     `json:"scene_id,omitempty"`
	CreatedAt    *time.Time `json:"created_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
}

// AdminConfig represents admin configuration
type AdminConfig struct {
	ID                int64  `json:"id"`
	Username          string `json:"username"`
	Password          string `json:"password"`
	APIKey            string `json:"api_key"`
	ErrorBanThreshold int    `json:"error_ban_threshold"`
}

// ProxyConfig represents proxy configuration
type ProxyConfig struct {
	ID       int64  `json:"id"`
	Enabled  bool   `json:"enabled"`
	ProxyURL string `json:"proxy_url,omitempty"`
}

// CacheConfigDB represents cache configuration in database
type CacheConfigDB struct {
	ID           int64      `json:"id"`
	CacheEnabled bool       `json:"cache_enabled"`
	CacheTimeout int        `json:"cache_timeout"`
	CacheBaseURL string     `json:"cache_base_url,omitempty"`
	CreatedAt    *time.Time `json:"created_at,omitempty"`
	UpdatedAt    *time.Time `json:"updated_at,omitempty"`
}

// DebugConfigDB represents debug configuration in database
type DebugConfigDB struct {
	ID           int64      `json:"id"`
	Enabled      bool       `json:"enabled"`
	LogRequests  bool       `json:"log_requests"`
	LogResponses bool       `json:"log_responses"`
	MaskToken    bool       `json:"mask_token"`
	CreatedAt    *time.Time `json:"created_at,omitempty"`
	UpdatedAt    *time.Time `json:"updated_at,omitempty"`
}

// CaptchaConfigDB represents captcha configuration in database
type CaptchaConfigDB struct {
	ID                  int64      `json:"id"`
	CaptchaMethod       string     `json:"captcha_method"`
	YesCaptchaAPIKey    string     `json:"yescaptcha_api_key"`
	YesCaptchaBaseURL   string     `json:"yescaptcha_base_url"`
	WebsiteKey          string     `json:"website_key"`
	PageAction          string     `json:"page_action"`
	BrowserProxyEnabled bool       `json:"browser_proxy_enabled"`
	BrowserProxyURL     string     `json:"browser_proxy_url,omitempty"`
	CreatedAt           *time.Time `json:"created_at,omitempty"`
	UpdatedAt           *time.Time `json:"updated_at,omitempty"`
}

// GenerationConfigDB represents generation configuration in database
type GenerationConfigDB struct {
	ID           int64 `json:"id"`
	ImageTimeout int   `json:"image_timeout"`
	VideoTimeout int   `json:"video_timeout"`
}

// ChatMessage represents an OpenAI-compatible chat message
type ChatMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []ContentPart
}

// ContentPart represents a part of multimodal content
type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL represents an image URL in content
type ImageURL struct {
	URL string `json:"url"`
}

// ChatCompletionRequest represents an OpenAI-compatible chat completion request
type ChatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	Temperature *float64      `json:"temperature,omitempty"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
	Image       string        `json:"image,omitempty"` // deprecated
	Video       string        `json:"video,omitempty"` // deprecated
}

// ChatCompletionResponse represents an OpenAI-compatible chat completion response
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
}

// Choice represents a choice in chat completion response
type Choice struct {
	Index        int          `json:"index"`
	Message      *ChatMessage `json:"message,omitempty"`
	Delta        *Delta       `json:"delta,omitempty"`
	FinishReason string       `json:"finish_reason,omitempty"`
}

// Delta represents a delta in streaming response
type Delta struct {
	Role             string `json:"role,omitempty"`
	Content          string `json:"content,omitempty"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail represents error details
type ErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

// ModelConfig represents model configuration
type ModelConfig struct {
	Type           string `json:"type"`       // image or video
	VideoType      string `json:"video_type"` // t2v, i2v, r2v
	ModelName      string `json:"model_name"` // for image
	ModelKey       string `json:"model_key"`  // for video
	AspectRatio    string `json:"aspect_ratio"`
	SupportsImages bool   `json:"supports_images"`
	MinImages      int    `json:"min_images"`
	MaxImages      int    `json:"max_images"`
}

// ModelConfigs contains all supported models
var ModelConfigs = map[string]ModelConfig{
	// Image generation - GEM_PIX (Gemini 2.5 Flash)
	"gemini-2.5-flash-image-landscape": {
		Type: "image", ModelName: "GEM_PIX", AspectRatio: "IMAGE_ASPECT_RATIO_LANDSCAPE",
	},
	"gemini-2.5-flash-image-portrait": {
		Type: "image", ModelName: "GEM_PIX", AspectRatio: "IMAGE_ASPECT_RATIO_PORTRAIT",
	},
	// Image generation - GEM_PIX_2 (Gemini 3.0 Pro)
	"gemini-3.0-pro-image-landscape": {
		Type: "image", ModelName: "GEM_PIX_2", AspectRatio: "IMAGE_ASPECT_RATIO_LANDSCAPE",
	},
	"gemini-3.0-pro-image-portrait": {
		Type: "image", ModelName: "GEM_PIX_2", AspectRatio: "IMAGE_ASPECT_RATIO_PORTRAIT",
	},
	// Image generation - IMAGEN_3_5 (Imagen 4.0)
	"imagen-4.0-generate-preview-landscape": {
		Type: "image", ModelName: "IMAGEN_3_5", AspectRatio: "IMAGE_ASPECT_RATIO_LANDSCAPE",
	},
	"imagen-4.0-generate-preview-portrait": {
		Type: "image", ModelName: "IMAGEN_3_5", AspectRatio: "IMAGE_ASPECT_RATIO_PORTRAIT",
	},
	// T2V - Text to Video
	"veo_3_1_t2v_fast_portrait": {
		Type: "video", VideoType: "t2v", ModelKey: "veo_3_1_t2v_fast_portrait",
		AspectRatio: "VIDEO_ASPECT_RATIO_PORTRAIT", SupportsImages: false,
	},
	"veo_3_1_t2v_fast_landscape": {
		Type: "video", VideoType: "t2v", ModelKey: "veo_3_1_t2v_fast",
		AspectRatio: "VIDEO_ASPECT_RATIO_LANDSCAPE", SupportsImages: false,
	},
	"veo_2_1_fast_d_15_t2v_portrait": {
		Type: "video", VideoType: "t2v", ModelKey: "veo_2_1_fast_d_15_t2v",
		AspectRatio: "VIDEO_ASPECT_RATIO_PORTRAIT", SupportsImages: false,
	},
	"veo_2_1_fast_d_15_t2v_landscape": {
		Type: "video", VideoType: "t2v", ModelKey: "veo_2_1_fast_d_15_t2v",
		AspectRatio: "VIDEO_ASPECT_RATIO_LANDSCAPE", SupportsImages: false,
	},
	"veo_2_0_t2v_portrait": {
		Type: "video", VideoType: "t2v", ModelKey: "veo_2_0_t2v",
		AspectRatio: "VIDEO_ASPECT_RATIO_PORTRAIT", SupportsImages: false,
	},
	"veo_2_0_t2v_landscape": {
		Type: "video", VideoType: "t2v", ModelKey: "veo_2_0_t2v",
		AspectRatio: "VIDEO_ASPECT_RATIO_LANDSCAPE", SupportsImages: false,
	},
	// I2V - Image to Video (First/Last frame)
	"veo_3_1_i2v_s_fast_fl_portrait": {
		Type: "video", VideoType: "i2v", ModelKey: "veo_3_1_i2v_s_fast_fl",
		AspectRatio: "VIDEO_ASPECT_RATIO_PORTRAIT", SupportsImages: true, MinImages: 1, MaxImages: 2,
	},
	"veo_3_1_i2v_s_fast_fl_landscape": {
		Type: "video", VideoType: "i2v", ModelKey: "veo_3_1_i2v_s_fast_fl",
		AspectRatio: "VIDEO_ASPECT_RATIO_LANDSCAPE", SupportsImages: true, MinImages: 1, MaxImages: 2,
	},
	"veo_2_1_fast_d_15_i2v_portrait": {
		Type: "video", VideoType: "i2v", ModelKey: "veo_2_1_fast_d_15_i2v",
		AspectRatio: "VIDEO_ASPECT_RATIO_PORTRAIT", SupportsImages: true, MinImages: 1, MaxImages: 2,
	},
	"veo_2_1_fast_d_15_i2v_landscape": {
		Type: "video", VideoType: "i2v", ModelKey: "veo_2_1_fast_d_15_i2v",
		AspectRatio: "VIDEO_ASPECT_RATIO_LANDSCAPE", SupportsImages: true, MinImages: 1, MaxImages: 2,
	},
	"veo_2_0_i2v_portrait": {
		Type: "video", VideoType: "i2v", ModelKey: "veo_2_0_i2v",
		AspectRatio: "VIDEO_ASPECT_RATIO_PORTRAIT", SupportsImages: true, MinImages: 1, MaxImages: 2,
	},
	"veo_2_0_i2v_landscape": {
		Type: "video", VideoType: "i2v", ModelKey: "veo_2_0_i2v",
		AspectRatio: "VIDEO_ASPECT_RATIO_LANDSCAPE", SupportsImages: true, MinImages: 1, MaxImages: 2,
	},
	// R2V - Reference Images to Video
	"veo_3_0_r2v_fast_portrait": {
		Type: "video", VideoType: "r2v", ModelKey: "veo_3_0_r2v_fast",
		AspectRatio: "VIDEO_ASPECT_RATIO_PORTRAIT", SupportsImages: true, MinImages: 0, MaxImages: -1,
	},
	"veo_3_0_r2v_fast_landscape": {
		Type: "video", VideoType: "r2v", ModelKey: "veo_3_0_r2v_fast",
		AspectRatio: "VIDEO_ASPECT_RATIO_LANDSCAPE", SupportsImages: true, MinImages: 0, MaxImages: -1,
	},
}
