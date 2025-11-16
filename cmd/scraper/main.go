package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/neo1908/lemmy-image-scraper/internal/api"
	"github.com/neo1908/lemmy-image-scraper/internal/config"
	"github.com/neo1908/lemmy-image-scraper/internal/database"
	"github.com/neo1908/lemmy-image-scraper/internal/downloader"
	"github.com/neo1908/lemmy-image-scraper/internal/scraper"
	log "github.com/sirupsen/logrus"
)

var (
	configPath = flag.String("config", "config.yaml", "Path to configuration file")
	verbose    = flag.Bool("verbose", false, "Enable verbose logging")
	stats      = flag.Bool("stats", false, "Display statistics and exit")
)

func main() {
	flag.Parse()

	// Configure logging
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})
	if *verbose {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.InfoLevel)
	}

	log.Info("Starting Lemmy Media Scraper")

	// Load configuration
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	cfg.SetDefaults()

	log.Infof("Loaded configuration from %s", *configPath)
	log.Infof("Instance: %s", cfg.Lemmy.Instance)
	log.Infof("Storage directory: %s", cfg.Storage.BaseDirectory)
	log.Infof("Run mode: %s", cfg.RunMode.Mode)

	// Initialize database
	db, err := database.New(cfg.Database.Path)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	log.Infof("Database initialized at %s", cfg.Database.Path)

	// Display stats if requested
	if *stats {
		displayStats(db)
		return
	}

	// Create storage directory
	if err := os.MkdirAll(cfg.Storage.BaseDirectory, 0755); err != nil {
		log.Fatalf("Failed to create storage directory: %v", err)
	}

	// Initialize API client
	apiClient := api.NewClient(cfg.Lemmy.Instance)

	// Login
	log.Info("Authenticating with Lemmy instance...")
	if err := apiClient.Login(cfg.Lemmy.Username, cfg.Lemmy.Password); err != nil {
		log.Fatalf("Failed to authenticate: %v", err)
	}

	// Initialize downloader
	dl := downloader.New(db, cfg.Storage.BaseDirectory)

	// Initialize scraper
	s := scraper.New(cfg, apiClient, db, dl)

	// Run based on mode
	if cfg.RunMode.Mode == "once" {
		runOnce(s)
	} else {
		runContinuous(s, cfg.RunMode.Interval)
	}
}

// runOnce runs the scraper once and exits
func runOnce(s *scraper.Scraper) {
	log.Info("Running in one-time mode")
	if err := s.Run(); err != nil {
		log.Errorf("Scraper error: %v", err)
		os.Exit(1)
	}
	log.Info("Scrape completed successfully")
}

// runContinuous runs the scraper on an interval
func runContinuous(s *scraper.Scraper, interval time.Duration) {
	log.Infof("Running in continuous mode with interval: %s", interval)

	// Create a channel to listen for interrupt signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Create ticker for interval
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately first time
	if err := s.Run(); err != nil {
		log.Errorf("Scraper error: %v", err)
	}

	for {
		select {
		case <-ticker.C:
			log.Info("Starting scheduled scrape run")
			if err := s.Run(); err != nil {
				log.Errorf("Scraper error: %v", err)
			}
		case sig := <-sigChan:
			log.Infof("Received signal %v, shutting down gracefully", sig)
			return
		}
	}
}

// displayStats shows statistics about scraped media
func displayStats(db *database.DB) {
	stats, err := db.GetStats()
	if err != nil {
		log.Fatalf("Failed to get stats: %v", err)
	}

	fmt.Println("\n=== Lemmy Media Scraper Statistics ===")
	fmt.Printf("\nTotal media files: %d\n", stats["total_media"])

	if typeCounts, ok := stats["by_type"].(map[string]int); ok && len(typeCounts) > 0 {
		fmt.Println("\nBy media type:")
		for mediaType, count := range typeCounts {
			fmt.Printf("  %s: %d\n", mediaType, count)
		}
	}

	if communityCounts, ok := stats["top_communities"].(map[string]int); ok && len(communityCounts) > 0 {
		fmt.Println("\nTop communities:")
		for community, count := range communityCounts {
			fmt.Printf("  %s: %d\n", community, count)
		}
	}

	fmt.Println()
}
