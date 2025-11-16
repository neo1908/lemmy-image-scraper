package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the application configuration
type Config struct {
	Lemmy      LemmyConfig      `yaml:"lemmy"`
	Storage    StorageConfig    `yaml:"storage"`
	Database   DatabaseConfig   `yaml:"database"`
	Scraper    ScraperConfig    `yaml:"scraper"`
	RunMode    RunModeConfig    `yaml:"run_mode"`
}

// LemmyConfig contains Lemmy instance and authentication settings
type LemmyConfig struct {
	Instance    string   `yaml:"instance"`     // e.g., "lemmy.ml"
	Username    string   `yaml:"username"`
	Password    string   `yaml:"password"`
	Communities []string `yaml:"communities"`  // Optional list of communities to scrape
}

// StorageConfig contains settings for media storage
type StorageConfig struct {
	BaseDirectory string `yaml:"base_directory"`  // Where to save downloaded media
}

// DatabaseConfig contains SQLite database settings
type DatabaseConfig struct {
	Path string `yaml:"path"`  // Path to SQLite database file
}

// ScraperConfig contains scraping behavior settings
type ScraperConfig struct {
	MaxPostsPerRun         int  `yaml:"max_posts_per_run"`           // Maximum posts to scrape per run (total across all pages)
	StopAtSeenPosts        bool `yaml:"stop_at_seen_posts"`          // Stop when encountering previously seen posts
	SkipSeenPosts          bool `yaml:"skip_seen_posts"`             // Skip seen posts but continue scraping (vs stopping)
	EnablePagination       bool `yaml:"enable_pagination"`           // Fetch multiple pages to get more than 50 posts
	SeenPostsThreshold     int  `yaml:"seen_posts_threshold"`        // Stop after encountering this many seen posts in a row
	SortType               string `yaml:"sort_type"`                 // e.g., "Hot", "New", "TopDay"
	IncludeImages          bool `yaml:"include_images"`              // Download images
	IncludeVideos          bool `yaml:"include_videos"`              // Download videos
	IncludeOtherMedia      bool `yaml:"include_other_media"`         // Download other media types
}

// RunModeConfig contains run mode settings
type RunModeConfig struct {
	Mode     string        `yaml:"mode"`      // "once" or "continuous"
	Interval time.Duration `yaml:"interval"`  // Interval for continuous mode (e.g., "5m", "1h")
}

// LoadConfig loads configuration from a YAML file
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Validate required fields
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &config, nil
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Lemmy.Instance == "" {
		return fmt.Errorf("lemmy.instance is required")
	}
	if c.Lemmy.Username == "" {
		return fmt.Errorf("lemmy.username is required")
	}
	if c.Lemmy.Password == "" {
		return fmt.Errorf("lemmy.password is required")
	}
	if c.Storage.BaseDirectory == "" {
		return fmt.Errorf("storage.base_directory is required")
	}
	if c.Database.Path == "" {
		return fmt.Errorf("database.path is required")
	}
	if c.RunMode.Mode != "once" && c.RunMode.Mode != "continuous" {
		return fmt.Errorf("run_mode.mode must be 'once' or 'continuous'")
	}
	if c.RunMode.Mode == "continuous" && c.RunMode.Interval == 0 {
		return fmt.Errorf("run_mode.interval is required for continuous mode")
	}
	return nil
}

// SetDefaults sets default values for optional configuration fields
func (c *Config) SetDefaults() {
	if c.Scraper.MaxPostsPerRun == 0 {
		c.Scraper.MaxPostsPerRun = 50
	}

	// Set default threshold for seen posts
	if c.Scraper.SeenPostsThreshold == 0 {
		c.Scraper.SeenPostsThreshold = 5 // Stop after seeing 5 posts in a row we've already processed
	}

	// If pagination is disabled, limit to 50 (API max per request)
	if !c.Scraper.EnablePagination && c.Scraper.MaxPostsPerRun > 50 {
		c.Scraper.MaxPostsPerRun = 50
	}

	if c.Scraper.SortType == "" {
		c.Scraper.SortType = "Hot"
	}
	// Normalize sort type to match Lemmy API expectations
	c.Scraper.SortType = normalizeSortType(c.Scraper.SortType)

	if !c.Scraper.IncludeImages && !c.Scraper.IncludeVideos && !c.Scraper.IncludeOtherMedia {
		c.Scraper.IncludeImages = true
		c.Scraper.IncludeVideos = true
		c.Scraper.IncludeOtherMedia = true
	}
	if c.RunMode.Mode == "" {
		c.RunMode.Mode = "once"
	}
}

// normalizeSortType converts user-friendly sort type names to API format
func normalizeSortType(sort string) string {
	// Map common variations to the correct API format
	// Based on Lemmy's SortType enum
	sortMap := map[string]string{
		"hot":      "Hot",
		"Hot":      "Hot",
		"new":      "New",
		"New":      "New",
		"topday":   "TopDay",
		"TopDay":   "TopDay",
		"topweek":  "TopWeek",
		"TopWeek":  "TopWeek",
		"topmonth": "TopMonth",
		"TopMonth": "TopMonth",
		"topyear":  "TopYear",
		"TopYear":  "TopYear",
		"topall":   "TopAll",
		"TopAll":   "TopAll",
		"active":   "Active",
		"Active":   "Active",
	}

	if normalized, ok := sortMap[sort]; ok {
		return normalized
	}
	return sort
}
