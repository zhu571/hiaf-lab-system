// Package config 集中解析服务端环境变量（AR-12）。
//
// 目标：默认值、类型转换、必填校验、脱敏打印只有一处；main 不再散读 os.Getenv。
// 未迁移的模块仍可继续用 os.Getenv（后续按模块收口），但新增配置一律加在这里。
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 是 server 的启动配置。字段按关注点分组，避免 main 里出现长串参数。
type Config struct {
	HTTP        HTTP
	DB          DB
	Auth        Auth
	AI          AI
	App         App
	Instruments Instruments
	Scheduler   Scheduler
	Features    Features
}

type HTTP struct {
	Port              string
	ReadHeaderTimeout time.Duration
	IdleTimeout       time.Duration
	RequestTimeout    time.Duration
}

type DB struct {
	Host         string
	Port         string
	User         string
	Name         string
	PasswordFile string
	PasswordEnv  string
	MaxOpen      int
	MaxIdle      int
}

type Auth struct {
	JWTSecretFile    string
	JWTSecretEnv     string
	ServiceTokenFile string
	ServiceTokenEnv  string
}

type AI struct {
	PyAgentURL       string
	PyAgentTokenFile string
}

type App struct {
	SelfBaseURL    string
	AttachmentDir  string
	RepoRoot       string
	ScanStorageDir string
	ScanMachine    string
}

type Instruments struct {
	E5063AAddr         string
	HiokiIM3536Addr    string
	Keysight33210AAddr string
}

type Scheduler struct {
	TodosEnabled   bool
	WeeklyEnabled  bool
	WeeklyAuthorID string
}

// Features 是渐进式改造的开关：默认开启新行为，保留 false 回滚路径。
type Features struct {
	IdempotencyV2        bool
	Recoverer            bool
	RequestTimeouts      bool
	RequestIDPropagation bool
}

// Load 读取并校验配置。任何必填/类型/范围错误都返回 error，由 main 直接退出。
func Load() (*Config, error) {
	port := env("PORT", "8000")
	if err := validatePort(port); err != nil {
		return nil, fmt.Errorf("config: PORT: %w", err)
	}

	readHeader, err := envDuration("HTTP_READ_HEADER_TIMEOUT", 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("config: HTTP_READ_HEADER_TIMEOUT: %w", err)
	}
	idle, err := envDuration("HTTP_IDLE_TIMEOUT", 120*time.Second)
	if err != nil {
		return nil, fmt.Errorf("config: HTTP_IDLE_TIMEOUT: %w", err)
	}
	requestTimeout, err := envDuration("HTTP_REQUEST_TIMEOUT", 60*time.Second)
	if err != nil {
		return nil, fmt.Errorf("config: HTTP_REQUEST_TIMEOUT: %w", err)
	}

	maxOpen, err := envInt("DB_MAX_OPEN", 25)
	if err != nil {
		return nil, fmt.Errorf("config: DB_MAX_OPEN: %w", err)
	}
	maxIdle, err := envInt("DB_MAX_IDLE", 5)
	if err != nil {
		return nil, fmt.Errorf("config: DB_MAX_IDLE: %w", err)
	}
	if maxOpen < 1 || maxIdle < 0 || maxIdle > maxOpen {
		return nil, fmt.Errorf("config: DB_MAX_OPEN/DB_MAX_IDLE invalid (max_open=%d max_idle=%d)", maxOpen, maxIdle)
	}

	todosEnabled, err := envBool("TODOS_SCHEDULER_ENABLED", true)
	if err != nil {
		return nil, fmt.Errorf("config: TODOS_SCHEDULER_ENABLED: %w", err)
	}
	weeklyEnabled, err := envBool("WEEKLY_SCHEDULER_ENABLED", true)
	if err != nil {
		return nil, fmt.Errorf("config: WEEKLY_SCHEDULER_ENABLED: %w", err)
	}
	idempotencyV2, err := envBool("IDEMPOTENCY_V2", true)
	if err != nil {
		return nil, fmt.Errorf("config: IDEMPOTENCY_V2: %w", err)
	}
	recoverer, err := envBool("RECOVERER_ENABLED", true)
	if err != nil {
		return nil, fmt.Errorf("config: RECOVERER_ENABLED: %w", err)
	}
	requestTimeouts, err := envBool("HTTP_TIMEOUTS_ENABLED", true)
	if err != nil {
		return nil, fmt.Errorf("config: HTTP_TIMEOUTS_ENABLED: %w", err)
	}
	requestIDPropagation, err := envBool("REQUEST_ID_PROPAGATION", true)
	if err != nil {
		return nil, fmt.Errorf("config: REQUEST_ID_PROPAGATION: %w", err)
	}

	selfBase := env("SELF_BASE_URL", "http://127.0.0.1:"+port)

	return &Config{
		HTTP: HTTP{
			Port:              port,
			ReadHeaderTimeout: readHeader,
			IdleTimeout:       idle,
			RequestTimeout:    requestTimeout,
		},
		DB: DB{
			Host:         env("DB_HOST", "localhost"),
			Port:         env("DB_PORT", "5432"),
			User:         env("DB_USER", "lab"),
			Name:         env("DB_NAME", "lab"),
			PasswordFile: env("DB_PASSWORD_FILE", "/run/secrets/db_password"),
			PasswordEnv:  "DB_PASSWORD",
			MaxOpen:      maxOpen,
			MaxIdle:      maxIdle,
		},
		Auth: Auth{
			JWTSecretFile:    env("JWT_SECRET_FILE", "/run/secrets/jwt_key"),
			JWTSecretEnv:     "JWT_SECRET",
			ServiceTokenFile: env("SERVICE_TOKEN_FILE", "/run/secrets/service_token"),
			ServiceTokenEnv:  "SERVICE_TOKEN",
		},
		AI: AI{
			PyAgentURL:       env("PY_AGENT_INTERPRET_URL", ""),
			PyAgentTokenFile: env("PY_AGENT_INTERNAL_TOKEN_FILE", "/run/secrets/agent_password"),
		},
		App: App{
			SelfBaseURL:    selfBase,
			AttachmentDir:  env("ATTACHMENT_DIR", "./uploads/"),
			RepoRoot:       env("REPO_ROOT", "/opt/hiaf-lab-system"),
			ScanStorageDir: env("SCAN_STORAGE_DIR", "./uploads/scans"),
			ScanMachine:    env("SCAN_MACHINE", "gascell"),
		},
		Instruments: Instruments{
			E5063AAddr:         env("E5063A_ADDR", "10.51.12.157:5025"),
			HiokiIM3536Addr:    env("HIOKI_IM3536_ADDR", "10.51.12.101:3500"),
			Keysight33210AAddr: env("KEYSIGHT_33210A_ADDR", ""),
		},
		Scheduler: Scheduler{
			TodosEnabled:   todosEnabled,
			WeeklyEnabled:  weeklyEnabled,
			WeeklyAuthorID: env("WEEKLY_SUMMARY_AUTHOR_ID", ""),
		},
		Features: Features{
			IdempotencyV2:        idempotencyV2,
			Recoverer:            recoverer,
			RequestTimeouts:      requestTimeouts,
			RequestIDPropagation: requestIDPropagation,
		},
	}, nil
}

// LogRedacted 打印生效配置；只打印路径与开关，不打印 secret 内容。
func (c *Config) LogRedacted() {
	slog.Info("config loaded",
		"port", c.HTTP.Port,
		"db_host", c.DB.Host,
		"db_name", c.DB.Name,
		"db_user", c.DB.User,
		"db_pool", fmt.Sprintf("%d/%d", c.DB.MaxOpen, c.DB.MaxIdle),
		"py_agent_url", c.AI.PyAgentURL,
		"todos_scheduler", c.Scheduler.TodosEnabled,
		"weekly_scheduler", c.Scheduler.WeeklyEnabled,
		"idempotency_v2", c.Features.IdempotencyV2,
		"recoverer", c.Features.Recoverer,
		"request_timeouts", c.Features.RequestTimeouts,
		"request_id_propagation", c.Features.RequestIDPropagation,
	)
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("want integer, got %q", raw)
	}
	return v, nil
}

func envDuration(key string, def time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def, nil
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("want duration (e.g. 5s), got %q", raw)
	}
	if v <= 0 {
		return 0, fmt.Errorf("must be positive, got %q", raw)
	}
	return v, nil
}

func envBool(key string, def bool) (bool, error) {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if raw == "" {
		return def, nil
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("want boolean, got %q", raw)
	}
}

func validatePort(raw string) error {
	p, err := strconv.Atoi(raw)
	if err != nil {
		return fmt.Errorf("want integer, got %q", raw)
	}
	if p < 1 || p > 65535 {
		return fmt.Errorf("out of range 1-65535: %d", p)
	}
	return nil
}
