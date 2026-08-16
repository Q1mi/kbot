// Package config 从环境变量加载 kbot 配置。生产密钥只从环境读，绝不写进代码。
package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	// HTTP 服务
	Addr               string   // 监听地址
	CORSAllowedOrigins []string // 浏览器跨域与 WebSocket Origin 白名单

	// 数据库
	DatabaseURL string // Postgres(pgvector) 连接串
	RedisURL    string // Redis 连接串

	// 独立代码执行 Sandbox Runner。
	SandboxRunnerURL        string
	SandboxRunnerToken      string
	SandboxRunnerTimeout    time.Duration
	ToolAllowedHosts        []string
	ToolAllowPrivateNetwork bool
	KBMarkdownAllowedRoots  []string

	// LLM
	LLMBaseURL    string // OpenAI 兼容网关
	LLMAPIKey     string // 旧全局回退密钥；新配置优先使用 Provider Account
	LLMModel      string // 主对话模型（需支持工具调用）
	LLMCheapModel string // 便宜模型：意图路由/分类

	// 数据分级路由的本地 Provider。
	OllamaBaseURL string
	OllamaModel   string

	// KB 向量化。
	EmbedderKind  string // local | openai
	EmbedderDim   int    // 向量维度；必须与 migrations 里 kb_chunks.embedding vector(N) 一致(默认 1536)
	EmbedderModel string // openai 兼容 embedding 模型名

	// JWT 认证
	JWTSecretKey            string // JWT 签名密钥
	CredentialEncryptionKey string // Provider API Key 应用层加密密钥

	// 审计归档与导出使用的对象存储。
	S3Endpoint  string // 如 http://minio:9000
	S3Bucket    string
	S3AccessKey string
	S3SecretKey string

	// Audit 留证。
	AuditArchiveAfterMonths int // 分区超过该月龄 → 归档 MinIO 后 detach+drop（默认 13）

	// OTEL / Langfuse（企业课堂可观测环境）
	OTLPEndpoint       string  // OTLP traces 完整 URL，如 http://langfuse-web:3000/api/public/otel/v1/traces
	OTLPHeaders        string  // 逗号分隔的 HTTP headers；本地 Langfuse 用 Basic Auth
	OTELSampleRatio    float64 // 0..1，课堂默认全采样
	OTELCaptureContent bool    // 是否记录模型输入输出；生产默认关闭，课堂 profile 显式开启
	ServiceVersion     string  // OTel service.version / Langfuse release
	LangfuseUIURL      string  // 浏览器可访问的 UI 地址，与容器内 OTLP 地址分开
	LangfuseProjectID  string  // Headless init 创建的项目 ID，用于拼 Trace 深链

	// 环境标识
	Environment string // dev/staging/prod

	// 集成。
	WebhookSecret   string // 通用 webhook HMAC 密钥
	LarkVerifyToken string // 飞书事件订阅校验 token
	LarkEncryptKey  string // 飞书事件订阅 Encrypt Key；配置后校验签名并解密载荷
	LarkAppID       string // 飞书出站；空则禁用
	LarkAppSecret   string
	LarkAgentID     string // 飞书入站消息触发的 Agent

	// 首启自动 seed admin（仅 dev；prod 应置 false）
	AutoseedAdmin         bool   // 启动时无 admin 用户则自动建一个
	AutoseedAdminEmail    string // 自动 seed 的 admin 邮箱
	AutoseedAdminPassword string // 自动 seed 的 admin 密码
	AutoseedCourseAssets  bool   // 初始化课程 Tool、Skill 与 Agent；需要业务模拟器
}

func Load() Config {
	cfg := Config{
		Addr:                    getenv("KBOT_ADDR", ":8080"),
		CORSAllowedOrigins:      splitList(getenv("KBOT_CORS_ALLOWED_ORIGINS", "http://localhost:8080,http://localhost:5173")),
		DatabaseURL:             getenv("KBOT_DATABASE_URL", "postgres://kbot:kbot@localhost:5432/kbot?sslmode=disable"),
		RedisURL:                getenv("KBOT_REDIS_URL", "redis://localhost:6379/0"),
		SandboxRunnerURL:        getenv("KBOT_SANDBOX_RUNNER_URL", "http://localhost:8081"),
		SandboxRunnerToken:      os.Getenv("KBOT_SANDBOX_RUNNER_TOKEN"),
		SandboxRunnerTimeout:    getenvDuration("KBOT_SANDBOX_TIMEOUT", 30*time.Second) + 30*time.Second,
		ToolAllowedHosts:        splitList(getenv("KBOT_TOOL_ALLOWED_HOSTS", "crossborder-sim,insurance-sim")),
		ToolAllowPrivateNetwork: strings.EqualFold(getenv("KBOT_TOOL_ALLOW_PRIVATE_NETWORK", "false"), "true"),
		KBMarkdownAllowedRoots:  splitList(getenv("KBOT_KB_MARKDOWN_ALLOWED_ROOTS", "projects")),
		LLMBaseURL:              getenv("KBOT_LLM_BASE_URL", "https://api.deepseek.com/v1"),
		LLMAPIKey:               os.Getenv("KBOT_LLM_API_KEY"), // 可选：仅作为未绑定 Model Profile 时的旧路径回退
		LLMModel:                getenv("KBOT_LLM_MODEL", "deepseek-v4-pro"),
		LLMCheapModel:           getenv("KBOT_LLM_CHEAP_MODEL", "deepseek-v4-flash"),
		OllamaBaseURL:           getenv("KBOT_OLLAMA_BASE_URL", ""), // 空则禁用本地 Provider,secret 请求会失败关闭
		OllamaModel:             getenv("KBOT_OLLAMA_MODEL", "qwen2.5:7b"),
		EmbedderKind:            getenv("KBOT_EMBEDDER", "local"),     // 默认 local：离线确定性,make up 即开即用;真实语义检索设 openai
		EmbedderDim:             getenvInt("KBOT_EMBEDDER_DIM", 1536), // 必须 == kb_chunks.embedding vector(1536)
		EmbedderModel:           getenv("KBOT_EMBEDDER_MODEL", "text-embedding-3-small"),

		S3Endpoint:  getenv("KBOT_S3_ENDPOINT", ""),
		S3Bucket:    getenv("KBOT_S3_BUCKET", "kbot"),
		S3AccessKey: getenv("KBOT_S3_ACCESS_KEY", "kbot"),
		S3SecretKey: getenv("KBOT_S3_SECRET_KEY", "kbot123456"),

		AuditArchiveAfterMonths: getenvInt("KBOT_AUDIT_ARCHIVE_AFTER_MONTHS", 13),

		JWTSecretKey:       os.Getenv("KBOT_JWT_SECRET_KEY"), // 必填
		OTLPEndpoint:       getenv("KBOT_OTLP_ENDPOINT", ""), // 可选，为空则禁用
		OTLPHeaders:        getenv("KBOT_OTLP_HEADERS", ""),
		OTELSampleRatio:    getenvFloat("KBOT_OTEL_SAMPLE_RATIO", 1),
		OTELCaptureContent: strings.EqualFold(getenv("KBOT_OTEL_CAPTURE_CONTENT", "false"), "true"),
		ServiceVersion:     getenv("KBOT_SERVICE_VERSION", "dev"),
		LangfuseUIURL:      getenv("KBOT_LANGFUSE_UI_URL", ""),
		LangfuseProjectID:  getenv("KBOT_LANGFUSE_PROJECT_ID", ""),
		Environment:        getenv("KBOT_ENVIRONMENT", "dev"),

		WebhookSecret:   os.Getenv("KBOT_WEBHOOK_SECRET"),
		LarkVerifyToken: os.Getenv("KBOT_LARK_VERIFY_TOKEN"),
		LarkEncryptKey:  os.Getenv("KBOT_LARK_ENCRYPT_KEY"),
		LarkAppID:       os.Getenv("KBOT_LARK_APP_ID"),
		LarkAppSecret:   os.Getenv("KBOT_LARK_APP_SECRET"),
		LarkAgentID:     os.Getenv("KBOT_LARK_AGENT_ID"),

		AutoseedAdmin:         getenv("KBOT_AUTOSEED_ADMIN", "false") == "true",
		AutoseedAdminEmail:    getenv("KBOT_AUTOSEED_ADMIN_EMAIL", "admin@example.com"),
		AutoseedAdminPassword: getenv("KBOT_AUTOSEED_ADMIN_PASSWORD", "admin12345"),
		AutoseedCourseAssets:  getenv("KBOT_AUTOSEED_COURSE_ASSETS", "false") == "true",
	}
	cfg.CredentialEncryptionKey = os.Getenv("KBOT_CREDENTIAL_ENCRYPTION_KEY")
	return cfg
}

// MustValidate 校验必填项；缺失就在启动那一刻失败（快速失败，胜过运行时才崩）。
func (c Config) MustValidate() {
	if err := c.Validate(); err != nil {
		log.Fatal(err)
	}
}

// Validate 返回可测试的配置错误；prod 环境额外拒绝课堂默认密钥和高风险调试开关。
func (c Config) Validate() error {
	var missing []string
	if c.DatabaseURL == "" {
		missing = append(missing, "KBOT_DATABASE_URL")
	}
	if c.JWTSecretKey == "" {
		missing = append(missing, "KBOT_JWT_SECRET_KEY")
	}
	if c.CredentialEncryptionKey == "" {
		missing = append(missing, "KBOT_CREDENTIAL_ENCRYPTION_KEY")
	}
	if c.SandboxRunnerURL == "" {
		missing = append(missing, "KBOT_SANDBOX_RUNNER_URL")
	}
	if c.SandboxRunnerToken == "" {
		missing = append(missing, "KBOT_SANDBOX_RUNNER_TOKEN")
	}
	if len(missing) > 0 {
		return fmt.Errorf("缺少必填配置：%v", missing)
	}

	// 验证 JWT 密钥长度（至少32字节）
	if len(c.JWTSecretKey) < 32 {
		return fmt.Errorf("KBOT_JWT_SECRET_KEY 长度至少需要32字符")
	}
	if len(c.CredentialEncryptionKey) < 32 {
		return fmt.Errorf("KBOT_CREDENTIAL_ENCRYPTION_KEY 长度至少需要32字符")
	}
	if len(c.SandboxRunnerToken) < 32 {
		return fmt.Errorf("KBOT_SANDBOX_RUNNER_TOKEN 长度至少需要32字符")
	}
	if c.WebhookSecret != "" && len(c.WebhookSecret) < 32 {
		return fmt.Errorf("KBOT_WEBHOOK_SECRET 长度至少需要32字符")
	}
	if !strings.EqualFold(c.Environment, "prod") {
		return nil
	}
	weakSecrets := map[string]string{
		"KBOT_JWT_SECRET_KEY":            c.JWTSecretKey,
		"KBOT_CREDENTIAL_ENCRYPTION_KEY": c.CredentialEncryptionKey,
		"KBOT_SANDBOX_RUNNER_TOKEN":      c.SandboxRunnerToken,
	}
	knownDefaults := []string{
		"dev-secret-key-32-chars-minimum",
		"dev-credential-key-minimum-32-chars",
		"kbot-dev-sandbox-runner-token-2026-minimum-32-chars",
	}
	for key, value := range weakSecrets {
		for _, known := range knownDefaults {
			if value == known {
				return fmt.Errorf("prod 环境禁止使用课堂默认密钥：%s", key)
			}
		}
	}
	if c.JWTSecretKey == c.CredentialEncryptionKey {
		return fmt.Errorf("prod 环境要求 JWT 与凭据加密使用不同密钥")
	}
	if c.AutoseedAdmin || c.AutoseedCourseAssets {
		return fmt.Errorf("prod 环境禁止自动初始化课程账号或资产")
	}
	if c.OTELCaptureContent {
		return fmt.Errorf("prod 环境默认禁止采集完整模型输入输出")
	}
	if c.ToolAllowPrivateNetwork {
		return fmt.Errorf("prod 环境禁止全局放行 Tool 私网访问，请使用显式 host allowlist")
	}
	for _, origin := range c.CORSAllowedOrigins {
		if origin == "*" || strings.Contains(origin, "localhost") || strings.Contains(origin, "127.0.0.1") {
			return fmt.Errorf("prod 环境 CORS 来源必须是明确的生产域名：%s", origin)
		}
	}
	if c.S3Endpoint != "" && (c.S3SecretKey == "kbot123456" || len(c.S3SecretKey) < 16) {
		return fmt.Errorf("prod 环境对象存储密钥过弱")
	}
	return nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getenvInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getenvFloat(k string, def float64) float64 {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			if n < 0 {
				return 0
			}
			if n > 1 {
				return 1
			}
			return n
		}
	}
	return def
}

func getenvDuration(k string, def time.Duration) time.Duration {
	if value := os.Getenv(k); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil && parsed > 0 {
			return parsed
		}
	}
	return def
}

func splitList(value string) []string {
	var items []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

// JWTKeyBytes 返回JWT密钥的字节数组
func (c Config) JWTKeyBytes() []byte {
	return []byte(c.JWTSecretKey)
}
