// Package config loads and validates service configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port     string
	LogLevel string

	PostgresUser     string
	PostgresPassword string
	PostgresDB       string
	PostgresHost     string
	PostgresPort     string

	CohereAPIKey         string
	CohereRerankModel    string
	CohereTimeoutSeconds int

	OpenAIAPIKey              string
	OpenAIExtractionModel     string
	OpenAIRepresentationModel string
	OpenAIEmbeddingModel      string

	LLMTimeoutSeconds       int
	EmbeddingTimeoutSeconds int
}

// Load reads environment variables and returns a validated Config.
// Returns an error if required variables are missing.
func Load() (*Config, error) {
	cfg := &Config{
		Port:     getEnv("PORT", "8080"),
		LogLevel: getEnv("LOG_LEVEL", "info"),

		PostgresUser:     os.Getenv("POSTGRES_USER"),
		PostgresPassword: os.Getenv("POSTGRES_PASSWORD"),
		PostgresDB:       os.Getenv("POSTGRES_DB"),
		PostgresHost:     getEnv("POSTGRES_HOST", "localhost"),
		PostgresPort:     getEnv("POSTGRES_PORT", "5432"),

		CohereAPIKey:      os.Getenv("COHERE_API_KEY"),
		CohereRerankModel: getEnv("COHERE_RERANK_MODEL", "rerank-english-v3.0"),

		OpenAIAPIKey:              os.Getenv("OPENAI_API_KEY"),
		OpenAIExtractionModel:     getEnv("OPENAI_EXTRACTION_MODEL", "gpt-4o-mini"),
		OpenAIRepresentationModel: getEnv("OPENAI_REPRESENTATION_MODEL", "gpt-4o"),
		OpenAIEmbeddingModel:      getEnv("OPENAI_EMBEDDING_MODEL", "text-embedding-3-small"),
	}

	var err error
	cfg.CohereTimeoutSeconds, err = parseInt("COHERE_TIMEOUT_SECONDS", "5")
	if err != nil {
		return nil, err
	}
	cfg.LLMTimeoutSeconds, err = parseInt("LLM_TIMEOUT_SECONDS", "30")
	if err != nil {
		return nil, err
	}
	cfg.EmbeddingTimeoutSeconds, err = parseInt("EMBEDDING_TIMEOUT_SECONDS", "10")
	if err != nil {
		return nil, err
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) DatabaseURL() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		c.PostgresUser, c.PostgresPassword,
		c.PostgresHost, c.PostgresPort,
		c.PostgresDB,
	)
}

func (c *Config) validate() error {
	if c.PostgresUser == "" {
		return fmt.Errorf("POSTGRES_USER is required")
	}
	if c.PostgresPassword == "" {
		return fmt.Errorf("POSTGRES_PASSWORD is required")
	}
	if c.PostgresDB == "" {
		return fmt.Errorf("POSTGRES_DB is required")
	}
	if c.Port == "" {
		return fmt.Errorf("PORT is required")
	}
	return nil
}

func getEnv(key, defaultVal string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return defaultVal
}

func parseInt(envKey, defaultVal string) (int, error) {
	raw := getEnv(envKey, defaultVal)
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid value for %s: %w", envKey, err)
	}
	return v, nil
}
