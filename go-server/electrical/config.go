package electrical

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

type DeviceConfig struct {
	Source string
	Addr   string
}

type InfluxConfig struct {
	Addr   string
	Token  string
	Org    string
	Bucket string
}

type Config struct {
	Enabled               bool
	MESTEKAddr            string
	KeithleyAddr          string
	Devices               []DeviceConfig
	Influx                InfluxConfig
	QueryInterval         time.Duration
	ReconnectDelay        time.Duration
	SingleShotTimeout     time.Duration
	SingleShotMinInterval time.Duration
	BacklogCapacity       int
	WriteBatchSize        int
}

func DefaultConfig() Config {
	return Config{
		Enabled:      false,
		MESTEKAddr:   "10.51.12.217:4001",
		KeithleyAddr: "10.51.12.221:4001",
		Devices: []DeviceConfig{
			{Source: SourceRIGOL, Addr: "10.51.12.154:5555"},
			{Source: SourceTek, Addr: "10.51.12.141:4000"},
			{Source: Source33512B, Addr: "10.51.12.219:5025"},
			{Source: Source33210A, Addr: "10.51.12.220:5025"},
		},
		QueryInterval:         2 * time.Second,
		ReconnectDelay:        2 * time.Second,
		SingleShotTimeout:     18 * time.Second,
		SingleShotMinInterval: 5 * time.Second,
		BacklogCapacity:       4096,
		WriteBatchSize:        500,
	}
}

func ConfigFromEnv() (Config, error) {
	cfg := DefaultConfig()
	if raw := os.Getenv("ELECTRICAL_MONITOR_ENABLED"); raw != "" {
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("ELECTRICAL_MONITOR_ENABLED: %w", err)
		}
		cfg.Enabled = enabled
	}
	cfg.MESTEKAddr = envOr("ELECTRICAL_MESTEK_ADDR", cfg.MESTEKAddr)
	cfg.KeithleyAddr = envOr("ELECTRICAL_6485_ADDR", cfg.KeithleyAddr)
	envNames := []string{"ELECTRICAL_RIGOL_ADDR", "ELECTRICAL_TEK_ADDR", "ELECTRICAL_33512B_ADDR", "ELECTRICAL_33210A_ADDR"}
	for i := range cfg.Devices {
		cfg.Devices[i].Addr = envOr(envNames[i], cfg.Devices[i].Addr)
	}
	token, err := common.ReadSecret("/run/secrets/influxdb_token", "INFLUXDB_TOKEN")
	if err != nil {
		return Config{}, fmt.Errorf("read influxdb token: %w", err)
	}
	cfg.Influx = InfluxConfig{
		Addr:   os.Getenv("INFLUXDB_ADDR"),
		Token:  token,
		Org:    os.Getenv("INFLUXDB_ORG"),
		Bucket: os.Getenv("INFLUXDB_BUCKET"),
	}
	if cfg.Influx.Addr == "" || cfg.Influx.Org == "" || cfg.Influx.Bucket == "" {
		return Config{}, fmt.Errorf("INFLUXDB_ADDR, INFLUXDB_ORG, and INFLUXDB_BUCKET are required")
	}
	return cfg, nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
