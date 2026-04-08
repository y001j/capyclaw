package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config is the root configuration struct for CapyClaw.
type Config struct {
	Version   string          `mapstructure:"version" validate:"required"`
	LogLevel  string          `mapstructure:"log_level" validate:"oneof=debug info warn error"`
	Riverbank RiverbankConfig `mapstructure:"riverbank"`
	Rapids    RapidsConfig    `mapstructure:"rapids"`
	Burrow    BurrowConfig    `mapstructure:"burrow"`
	Pond      PondConfig      `mapstructure:"pond"`
	Lodge     LodgeConfig     `mapstructure:"lodge"`
	Wetland   WetlandConfig   `mapstructure:"wetland"`
	Providers ProvidersConfig      `mapstructure:"providers"`
	Search    SearchProvidersConfig `mapstructure:"search"`
	Whiskers  WhiskersConfig        `mapstructure:"whiskers"`
}

// RiverbankConfig holds gateway server settings.
type RiverbankConfig struct {
	Host      string          `mapstructure:"host" validate:"required"`
	Port      int             `mapstructure:"port" validate:"required,min=1,max=65535"`
	TLS       TLSConfig       `mapstructure:"tls"`
	WebSocket WSConfig        `mapstructure:"websocket"`
	Auth      AuthConfig      `mapstructure:"auth"`
}

type TLSConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	CertPath   string `mapstructure:"cert_path"`
	KeyPath    string `mapstructure:"key_path"`
	MinVersion string `mapstructure:"min_version"`
}

type WSConfig struct {
	MaxConnections  int      `mapstructure:"max_connections" validate:"min=1"`
	ReadBufferSize  int      `mapstructure:"read_buffer_size"`
	WriteBufferSize int      `mapstructure:"write_buffer_size"`
	PingInterval    string   `mapstructure:"ping_interval"`
	PongTimeout     string   `mapstructure:"pong_timeout"`
	AllowedOrigins  []string `mapstructure:"allowed_origins"`
}

type AuthConfig struct {
	Provider      string             `mapstructure:"provider" validate:"oneof=oidc jwt device-pairing"`
	OIDC          OIDCConfig         `mapstructure:"oidc"`
	JWT           JWTConfig          `mapstructure:"jwt"`
	DevicePairing DevicePairingConfig `mapstructure:"device_pairing"`
}

type OIDCConfig struct {
	IssuerURL    string   `mapstructure:"issuer_url"`
	ClientID     string   `mapstructure:"client_id"`
	ClientSecret string   `mapstructure:"client_secret"`
	Scopes       []string `mapstructure:"scopes"`
}

type JWTConfig struct {
	SigningMethod string `mapstructure:"signing_method" validate:"oneof=ES256 RS256 EdDSA"`
	PublicKeyPath string `mapstructure:"public_key_path"`
}

type DevicePairingConfig struct {
	Enabled              bool   `mapstructure:"enabled"`
	AutoApproveLoopback  bool   `mapstructure:"auto_approve_loopback"`
	KeyAlgorithm         string `mapstructure:"key_algorithm"`
}

// RapidsConfig holds rate limiting settings.
type RapidsConfig struct {
	Enabled       bool               `mapstructure:"enabled"`
	DefaultLimits RateLimitDefaults   `mapstructure:"default_limits"`
}

type RateLimitDefaults struct {
	RequestsPerMinute  int `mapstructure:"requests_per_minute"`
	RequestsPerHour    int `mapstructure:"requests_per_hour"`
	ConcurrentSessions int `mapstructure:"concurrent_sessions"`
}

// BurrowConfig holds agent core settings.
type BurrowConfig struct {
	DefaultModel    string          `mapstructure:"default_model"`
	FallbackModels  []string        `mapstructure:"fallback_models"`
	MaxContextTokens int            `mapstructure:"max_context_tokens"`
	Compaction      CompactionConfig `mapstructure:"compaction"`
	ToolExecution   ToolExecConfig   `mapstructure:"tool_execution"`
	Sandbox         SandboxConfig    `mapstructure:"sandbox"`
}

type CompactionConfig struct {
	Strategy         string  `mapstructure:"strategy" validate:"oneof=async sync"`
	TriggerThreshold float64 `mapstructure:"trigger_threshold"`
	SummaryModel     string  `mapstructure:"summary_model"`
}

type ToolExecConfig struct {
	DefaultTimeout     string `mapstructure:"default_timeout"`
	MaxConcurrentTools int    `mapstructure:"max_concurrent_tools"`
	MaxIterations      int    `mapstructure:"max_iterations"`
}

type SandboxConfig struct {
	DefaultTier string              `mapstructure:"default_tier" validate:"oneof=wasm gvisor firecracker"`
	WASM        WASMSandboxConfig   `mapstructure:"wasm"`
	GVisor      GVisorSandboxConfig `mapstructure:"gvisor"`
	Firecracker FirecrackerConfig   `mapstructure:"firecracker"`
}

type WASMSandboxConfig struct {
	MaxMemoryMB      int    `mapstructure:"max_memory_mb"`
	MaxExecutionTime string `mapstructure:"max_execution_time"`
	AllowedHostFuncs []string `mapstructure:"allowed_host_functions"`
}

type GVisorSandboxConfig struct {
	RuntimeClass    string `mapstructure:"runtime_class"`
	MaxMemoryMB     int    `mapstructure:"max_memory_mb"`
	MaxCPUMillicores int   `mapstructure:"max_cpu_millicores"`
}

type FirecrackerConfig struct {
	KernelPath  string `mapstructure:"kernel_path"`
	RootFSPath  string `mapstructure:"rootfs_path"`
	MaxMemoryMB int    `mapstructure:"max_memory_mb"`
	MaxVCPUs    int    `mapstructure:"max_vcpus"`
	BootTimeout string `mapstructure:"boot_timeout"`
}

// PondConfig holds database settings.
type PondConfig struct {
	Postgres PostgresConfig `mapstructure:"postgres"`
	Vector   VectorConfig   `mapstructure:"vector"`
}

type PostgresConfig struct {
	Host     string     `mapstructure:"host" validate:"required"`
	Port     int        `mapstructure:"port" validate:"required"`
	Database string     `mapstructure:"database" validate:"required"`
	Username string     `mapstructure:"username" validate:"required"`
	Password string     `mapstructure:"password" validate:"required"`
	SSLMode  string     `mapstructure:"ssl_mode"`
	Pool     PoolConfig `mapstructure:"pool"`
}

type PoolConfig struct {
	MaxConns        int    `mapstructure:"max_conns"`
	MinConns        int    `mapstructure:"min_conns"`
	MaxConnLifetime string `mapstructure:"max_conn_lifetime"`
	MaxConnIdleTime string `mapstructure:"max_conn_idle_time"`
}

type VectorConfig struct {
	EmbeddingModel      string       `mapstructure:"embedding_model"`
	EmbeddingDimensions int          `mapstructure:"embedding_dimensions"`
	EmbeddingBaseURL    string       `mapstructure:"embedding_base_url"`
	EmbeddingAPIKey     string       `mapstructure:"embedding_api_key"`
	HNSW                HNSWConfig   `mapstructure:"hnsw"`
	Search              SearchConfig `mapstructure:"search"`
}

type HNSWConfig struct {
	M              int `mapstructure:"m"`
	EFConstruction int `mapstructure:"ef_construction"`
	EFSearch       int `mapstructure:"ef_search"`
}

type SearchConfig struct {
	VectorWeight          float64 `mapstructure:"vector_weight"`
	FTSWeight             float64 `mapstructure:"fts_weight"`
	MaxResults            int     `mapstructure:"max_results"`
	TemporalDecayHalfLife string  `mapstructure:"temporal_decay_half_life"`
}

// LodgeConfig holds infrastructure service settings.
type LodgeConfig struct {
	Redis    RedisConfig    `mapstructure:"redis"`
	Temporal TemporalConfig `mapstructure:"temporal"`
	Asynq    AsynqConfig    `mapstructure:"asynq"`
}

type RedisConfig struct {
	Addresses []string `mapstructure:"addresses" validate:"required"`
	Password  string   `mapstructure:"password"`
	DB        int      `mapstructure:"db"`
	PoolSize  int      `mapstructure:"pool_size"`
}

type TemporalConfig struct {
	Host      string `mapstructure:"host"`
	Namespace string `mapstructure:"namespace"`
	TaskQueue string `mapstructure:"task_queue"`
}

type AsynqConfig struct {
	RedisAddr   string         `mapstructure:"redis_addr"`
	Concurrency int            `mapstructure:"concurrency"`
	Queues      map[string]int `mapstructure:"queues"`
}

// WetlandConfig holds platform service settings.
type WetlandConfig struct {
	Footprint FootprintConfig `mapstructure:"footprint"`
	Marsh     MarshConfig     `mapstructure:"marsh"`
	Vapor     VaporConfig     `mapstructure:"vapor"`
	Canopy    CanopyConfig    `mapstructure:"canopy"`
}

type FootprintConfig struct {
	Enabled       bool          `mapstructure:"enabled"`
	RetentionDays int           `mapstructure:"retention_days"`
	Export        ExportConfig  `mapstructure:"export"`
}

type ExportConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Endpoint string `mapstructure:"endpoint"`
}

type MarshConfig struct {
	Enabled      bool         `mapstructure:"enabled"`
	DefaultQuota QuotaConfig  `mapstructure:"default_quota"`
}

type QuotaConfig struct {
	MaxAgents          int     `mapstructure:"max_agents"`
	MaxSessionsPerAgent int    `mapstructure:"max_sessions_per_agent"`
	MonthlyTokenBudget  int64  `mapstructure:"monthly_token_budget"`
	MonthlyCostLimitUSD float64 `mapstructure:"monthly_cost_limit_usd"`
	AlertThreshold      float64 `mapstructure:"alert_threshold"`
}

type VaporConfig struct {
	OTel OTelConfig `mapstructure:"otel"`
}

type OTelConfig struct {
	Endpoint    string  `mapstructure:"endpoint"`
	Protocol    string  `mapstructure:"protocol"`
	ServiceName string  `mapstructure:"service_name"`
	SampleRate  float64 `mapstructure:"sample_rate"`
}

type CanopyConfig struct {
	Provider string      `mapstructure:"provider" validate:"oneof=vault sops env"`
	Vault    VaultConfig `mapstructure:"vault"`
}

type VaultConfig struct {
	Address    string `mapstructure:"address"`
	AuthMethod string `mapstructure:"auth_method"`
	MountPath  string `mapstructure:"mount_path"`
	Role       string `mapstructure:"role"`
}

// ProvidersConfig holds LLM provider settings.
type ProvidersConfig struct {
	Anthropic  ProviderConfig `mapstructure:"anthropic"`
	MiniMax    ProviderConfig `mapstructure:"minimax"`
	OpenAI     ProviderConfig `mapstructure:"openai"`
	Google     ProviderConfig `mapstructure:"google"`
	Ollama     ProviderConfig `mapstructure:"ollama"`
	VolcEngine ProviderConfig `mapstructure:"volcengine"`
}

// SearchConfig holds web search provider API keys.
type SearchProvidersConfig struct {
	Bocha  SearchProviderConfig `mapstructure:"bocha"`
	Tavily SearchProviderConfig `mapstructure:"tavily"`
	Brave  SearchProviderConfig `mapstructure:"brave"`
}

type SearchProviderConfig struct {
	APIKey  string `mapstructure:"api_key"`
	BaseURL string `mapstructure:"base_url"`
}

type ProviderConfig struct {
	APIKey     string   `mapstructure:"api_key"`
	BaseURL    string   `mapstructure:"base_url"`
	MaxRetries int      `mapstructure:"max_retries"`
	Timeout    string   `mapstructure:"timeout"`
	Models     []string `mapstructure:"models"`
}

// WhiskersConfig holds channel adapter settings.
type WhiskersConfig struct {
	Telegram ChannelConfig `mapstructure:"telegram"`
	Discord  ChannelConfig `mapstructure:"discord"`
	Slack    SlackConfig   `mapstructure:"slack"`
}

type ChannelConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	BotToken string `mapstructure:"bot_token"`
}

type SlackConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	BotToken string `mapstructure:"bot_token"`
	AppToken string `mapstructure:"app_token"`
}

// Load reads configuration from capyclaw.yaml and environment variables.
func Load() (*Config, error) {
	v := viper.New()

	v.SetConfigName("capyclaw")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("/etc/capyclaw")
	v.AddConfigPath("$HOME/.capyclaw")

	// Environment variable override: CAPYCLAW_SECTION_KEY
	v.SetEnvPrefix("CAPYCLAW")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Set defaults
	setDefaults(v)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config: %w", err)
		}
		// Config file not found — use defaults + env vars
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	if err := Validate(&cfg); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("version", "1")
	v.SetDefault("log_level", "info")
	v.SetDefault("riverbank.host", "0.0.0.0")
	v.SetDefault("riverbank.port", 18789)
	v.SetDefault("riverbank.tls.min_version", "1.3")
	v.SetDefault("riverbank.websocket.max_connections", 10000)
	v.SetDefault("riverbank.websocket.read_buffer_size", 4096)
	v.SetDefault("riverbank.websocket.write_buffer_size", 4096)
	v.SetDefault("riverbank.websocket.ping_interval", "30s")
	v.SetDefault("riverbank.websocket.pong_timeout", "10s")
	v.SetDefault("riverbank.auth.provider", "jwt")
	v.SetDefault("rapids.enabled", true)
	v.SetDefault("rapids.default_limits.requests_per_minute", 60)
	v.SetDefault("rapids.default_limits.requests_per_hour", 1000)
	v.SetDefault("rapids.default_limits.concurrent_sessions", 5)
	v.SetDefault("burrow.default_model", "claude-sonnet-4-20250514")
	v.SetDefault("burrow.max_context_tokens", 200000)
	v.SetDefault("burrow.compaction.strategy", "async")
	v.SetDefault("burrow.compaction.trigger_threshold", 0.80)
	v.SetDefault("burrow.tool_execution.default_timeout", "30s")
	v.SetDefault("burrow.tool_execution.max_concurrent_tools", 5)
	v.SetDefault("burrow.sandbox.default_tier", "wasm")
	v.SetDefault("pond.postgres.port", 5432)
	v.SetDefault("pond.postgres.ssl_mode", "verify-full")
	v.SetDefault("pond.postgres.pool.max_conns", 50)
	v.SetDefault("pond.postgres.pool.min_conns", 5)
	v.SetDefault("pond.vector.embedding_dimensions", 1536)
	v.SetDefault("pond.vector.hnsw.m", 16)
	v.SetDefault("pond.vector.hnsw.ef_construction", 200)
	v.SetDefault("pond.vector.hnsw.ef_search", 100)
	v.SetDefault("pond.vector.search.vector_weight", 0.7)
	v.SetDefault("pond.vector.search.fts_weight", 0.3)
	v.SetDefault("pond.vector.search.max_results", 20)
	v.SetDefault("wetland.footprint.enabled", true)
	v.SetDefault("wetland.footprint.retention_days", 365)
	v.SetDefault("wetland.marsh.enabled", true)
	v.SetDefault("wetland.vapor.otel.protocol", "grpc")
	v.SetDefault("wetland.vapor.otel.sample_rate", 0.1)
	v.SetDefault("wetland.canopy.provider", "vault")
}
