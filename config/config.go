package config

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds all application configuration settings.
type Config struct {
	BotToken          string
	AllowedUserIDsRaw string
	AllowedUserIDs    map[int64]bool // parsed set of allowed user IDs
	XianyuCookies     string
	ProxyURL          string
	AsocksAPIKey      string
	DeeplAPIKey       string
	DatabasePath      string
}

// Global settings instance
var Settings *Config

func init() {
	// Resolve the base directory where main.go resides
	_, filename, _, _ := runtime.Caller(0)
	baseDir := filepath.Dir(filepath.Dir(filename))
	envPath := filepath.Join(baseDir, ".env")

	// Load environment variables from .env if it exists
	if err := godotenv.Load(envPath); err != nil {
		log.Printf("[Config] Warning: No .env file loaded from %s: %v", envPath, err)
	}

	allowedRaw := getEnv("ALLOWED_USER_IDS_RAW", "")
	if allowedRaw == "" {
		allowedRaw = getEnv("ALLOWED_USER_IDS", "0")
	}
	dbPath := getEnv("DATABASE_PATH", "xianyu_bot.db")
	if !filepath.IsAbs(dbPath) {
		dbPath = filepath.Join(baseDir, dbPath)
	}

	Settings = &Config{
		BotToken:          getEnv("BOT_TOKEN", ""),
		AllowedUserIDsRaw: allowedRaw,
		AllowedUserIDs:    parseAllowedUserIDs(allowedRaw),
		XianyuCookies:     getEnv("XIANYU_COOKIES", ""),
		ProxyURL:          getEnv("PROXY_URL", ""),
		AsocksAPIKey:      getEnv("ASOCKS_API_KEY", ""),
		DeeplAPIKey:       getEnv("DEEPL_API_KEY", ""),
		DatabasePath:      dbPath,
	}

	log.Printf("[Config] Successfully initialized configuration from .env")
}

// getEnv gets an env variable or returns a default value
func getEnv(key, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultVal
}

// parseAllowedUserIDs converts raw comma/semicolon separated IDs to a set map
func parseAllowedUserIDs(raw string) map[int64]bool {
	allowed := make(map[int64]bool)
	// Replace semicolons with commas to normalize
	normalized := strings.ReplaceAll(raw, ";", ",")
	parts := strings.Split(normalized, ",")

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		id, err := strconv.ParseInt(trimmed, 10, 64)
		if err == nil {
			allowed[id] = true
		}
	}
	return allowed
}

// IsUserAllowed checks if a Telegram user ID is authorized to interact with the bot
func (c *Config) IsUserAllowed(userID int64) bool {
	// If 0 is in the allowed user IDs, allow all
	if c.AllowedUserIDs[0] {
		return true
	}
	return c.AllowedUserIDs[userID]
}
