package config

import (
	"log/slog"
	"sync"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
	"github.com/joho/godotenv"
)

// Config содержит всю конфигурацию приложения.
type Config struct {
	Database    DatabaseConfig `yaml:"database"`                                     // Конфигурация базы данных.
	Redmine     Redmine        `yaml:"redmine"`                                      // Конфигурация redmine.
	LogLevel    string         `yaml:"log_level" env:"LOG_LEVEL" env-default:"Info"` // Режим логирования debug, info, warn, error
	Env         string         `env:"ENV" env-default:"dev"`                         //dev, prod, local
	Debug       bool           `env:"DEBUG" env-default:"false"`                     // Режим отладки pprof
	DebugPort   string         `env:"DEBUG_PORT" env-default:"8080"`
	PatchConfig string         `env:"PATCH_CONFIG" env-default:"./config/config.yaml"`
}

// DatabaseConfig содержит конфигурацию для работы с базой данных.
type DatabaseConfig struct {
	Timeout  time.Duration `yaml:"timeout" env:"BD_TIMEOUT" env-default:"20s"` // Тайм-аут для операций с базой данных.
	Host     string        `yaml:"host" env:"BD_HOST"`
	Port     int           `yaml:"port" env:"BD_PORT" env-default:"5432"`
	User     string        `yaml:"user" env:"BD_USER"`
	Password string        `yaml:"password" env:"BD_PASSWORD"`
	DBName   string        `yaml:"dbname" env:"BD_DBNAME" `
	SSLMode  string        `yaml:"sslmode" env:"BD_SSL_MODE" env-default:"disable"`
	Schema   string        `yaml:"schema" env:"BD_SCHEMA" env-default:"public"` //dev, public
}

type Redmine struct {
	ProjectID  int       `yaml:"project_id" env:"PROJECT_ID" env-default:"25"`
	StartDate  time.Time `ignored:"true"`
	EndDate    time.Time `ignored:"true"`
	IssuePatch string    `yaml:"issue_patch" env:"ISSUE_PATCH" env-default:""`
}

var (
	instance *Config
	once     sync.Once
)

func MustGetConfig() *Config {
	once.Do(func() {
		instance = &Config{}

		_ = godotenv.Load(".env")

		err := cleanenv.ReadEnv(instance)
		if err != nil {
			slog.Error("Error reading env", "err", err)
			panic(err)
		}

		_ = cleanenv.ReadConfig(instance.PatchConfig, instance)

	})
	return instance
}
