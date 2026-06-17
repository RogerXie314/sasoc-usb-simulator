package config

import (
	"fmt"
	"os"

	"github.com/spf13/viper"
)

type Config struct {
	Server    ServerConfig    `mapstructure:"server"`
	Sasoc     SasocConfig     `mapstructure:"sasoc"`
	Database  DatabaseConfig  `mapstructure:"database"`
	Simulator SimulatorConfig `mapstructure:"simulator"`
	Pressure  PressureConfig  `mapstructure:"pressure"`
	Debug     bool            `mapstructure:"debug"`
}

type ServerConfig struct {
	Port  int  `mapstructure:"port"`
	Debug bool `mapstructure:"debug"`
}

type SasocConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

type DatabaseConfig struct {
	Path string `mapstructure:"path"`
}

type SimulatorConfig struct {
	HeartbeatInterval int  `mapstructure:"heartbeat_interval"`
	ReconnectInterval int  `mapstructure:"reconnect_interval"`
	ReadTimeout       int  `mapstructure:"read_timeout"`
	OfflineTimeout    int  `mapstructure:"offline_timeout"`
	MaxStations       int  `mapstructure:"max_stations"`
	Encrypt           bool `mapstructure:"encrypt"`
	Compress          bool `mapstructure:"compress"`
}

type PressureConfig struct {
	CollectInterval int    `mapstructure:"collect_interval"`
	ReportFormat    string `mapstructure:"report_format"`
}

func Load() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./configs")

	// 环境变量覆盖
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("read config: %w", err)
		}
		// 配置文件不存在，使用默认值
		fmt.Println("config file not found, using defaults")
	}

	cfg := &Config{}
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// 设置默认值
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Sasoc.Port == 0 {
		cfg.Sasoc.Port = 4567
	}
	if cfg.Simulator.HeartbeatInterval == 0 {
		cfg.Simulator.HeartbeatInterval = 30
	}
	if cfg.Simulator.ReconnectInterval == 0 {
		cfg.Simulator.ReconnectInterval = 60
	}
	if cfg.Simulator.ReadTimeout == 0 {
		cfg.Simulator.ReadTimeout = 600
	}
	if cfg.Simulator.OfflineTimeout == 0 {
		cfg.Simulator.OfflineTimeout = 100
	}
	if cfg.Simulator.MaxStations == 0 {
		cfg.Simulator.MaxStations = 100
	}
	if cfg.Pressure.CollectInterval == 0 {
		cfg.Pressure.CollectInterval = 5
	}
	if cfg.Pressure.ReportFormat == "" {
		cfg.Pressure.ReportFormat = "html"
	}

	cfg.Debug = cfg.Server.Debug

	return cfg, nil
}

// LoadFromFile 从指定文件加载配置
func LoadFromFile(path string) (*Config, error) {
	viper.SetConfigFile(path)
	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	cfg := &Config{}
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	return cfg, nil
}

// GenerateDefaultConfig 生成默认配置文件
func GenerateDefaultConfig(path string) error {
	content := `server:
  port: 8080
  debug: true

sasoc:
  host: "192.168.1.1"
  port: 4567

database:
  path: "usb-simulator.db"

simulator:
  heartbeat_interval: 30
  reconnect_interval: 60
  read_timeout: 600
  offline_timeout: 100
  max_stations: 100
  encrypt: true
  compress: true

pressure:
  collect_interval: 5
  report_format: "html"
`
	return os.WriteFile(path, []byte(content), 0644)
}

// SaveConfig 将运行时配置持久化到 config.yaml
// 使用 WriteConfigAs 确保写入到当前工作目录的 config.yaml
func SaveConfig(cfg *Config) error {
	viper.Set("server.port", cfg.Server.Port)
	viper.Set("server.debug", cfg.Server.Debug)
	viper.Set("sasoc.host", cfg.Sasoc.Host)
	viper.Set("sasoc.port", cfg.Sasoc.Port)
	viper.Set("database.path", cfg.Database.Path)
	viper.Set("simulator.heartbeat_interval", cfg.Simulator.HeartbeatInterval)
	viper.Set("simulator.reconnect_interval", cfg.Simulator.ReconnectInterval)
	viper.Set("simulator.read_timeout", cfg.Simulator.ReadTimeout)
	viper.Set("simulator.offline_timeout", cfg.Simulator.OfflineTimeout)
	viper.Set("simulator.max_stations", cfg.Simulator.MaxStations)
	viper.Set("simulator.encrypt", cfg.Simulator.Encrypt)
	viper.Set("simulator.compress", cfg.Simulator.Compress)
	viper.Set("pressure.collect_interval", cfg.Pressure.CollectInterval)
	viper.Set("pressure.report_format", cfg.Pressure.ReportFormat)

	// 使用 WriteConfigAs 写入到当前工作目录
	return viper.WriteConfigAs("config.yaml")
}
