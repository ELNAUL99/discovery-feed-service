package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Redis    RedisConfig
	Kafka    KafkaConfig
	AI       AIConfig
	Features FeatureFlags
}

type ServerConfig struct {
	Port         string
	Environment  string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

type DatabaseConfig struct {
	Host            string
	Port            int
	User            string
	Password        string
	DBName          string
	SSLMode         string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

type RedisConfig struct {
	Host     string
	Port     int
	Password string
	DB       int
	PoolSize int
}

type KafkaConfig struct {
	Brokers []string
	Topic   string
	Enabled bool
}

type AIConfig struct {
	Enabled    bool
	Provider   string
	APIKey     string
	Endpoint   string
	MockMode   bool
}

type FeatureFlags struct {
	EnableTracing      bool
	EnableABTesting    bool
	EnablePersonalization bool
	EnableKafkaEvents  bool
}

func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Port:         getEnv("SERVER_PORT", "8080"),
			Environment:  getEnv("ENV", "development"),
			ReadTimeout:  getDuration("SERVER_READ_TIMEOUT", 15*time.Second),
			WriteTimeout: getDuration("SERVER_WRITE_TIMEOUT", 15*time.Second),
		},
		Database: DatabaseConfig{
			Host:            getEnv("DB_HOST", "localhost"),
			Port:            getInt("DB_PORT", 5432),
			User:            getEnv("DB_USER", "feeduser"),
			Password:        getEnv("DB_PASSWORD", "feedpass"),
			DBName:          getEnv("DB_NAME", "discoveryfeed"),
			SSLMode:         getEnv("DB_SSLMODE", "disable"),
			MaxOpenConns:    getInt("DB_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    getInt("DB_MAX_IDLE_CONNS", 5),
			ConnMaxLifetime: getDuration("DB_CONN_MAX_LIFETIME", 5*time.Minute),
		},
		Redis: RedisConfig{
			Host:     getEnv("REDIS_HOST", "localhost"),
			Port:     getInt("REDIS_PORT", 6379),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       getInt("REDIS_DB", 0),
			PoolSize: getInt("REDIS_POOL_SIZE", 10),
		},
		Kafka: KafkaConfig{
			Brokers: getSlice("KAFKA_BROKERS", []string{"localhost:9092"}),
			Topic:   getEnv("KAFKA_TOPIC", "feed-events"),
			Enabled: getBool("KAFKA_ENABLED", false),
		},
		AI: AIConfig{
			Enabled:  getBool("AI_ENABLED", true),
			Provider: getEnv("AI_PROVIDER", "mock"),
			APIKey:   getEnv("AI_API_KEY", ""),
			Endpoint: getEnv("AI_ENDPOINT", ""),
			MockMode: getBool("AI_MOCK_MODE", true),
		},
		Features: FeatureFlags{
			EnableTracing:         getBool("FEATURE_TRACING", true),
			EnableABTesting:       getBool("FEATURE_AB_TESTING", true),
			EnablePersonalization: getBool("FEATURE_PERSONALIZATION", true),
			EnableKafkaEvents:     getBool("FEATURE_KAFKA_EVENTS", false),
		},
	}
}

func (c *DatabaseConfig) DSN() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.DBName, c.SSLMode)
}

func (c *RedisConfig) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolVal, err := strconv.ParseBool(value); err == nil {
			return boolVal
		}
	}
	return defaultValue
}

func getDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

func getSlice(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		return []string{value}
	}
	return defaultValue
}
