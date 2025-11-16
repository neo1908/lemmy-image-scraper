package database

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"

	_ "github.com/mattn/go-sqlite3"
	"github.com/neo1908/lemmy-image-scraper/pkg/models"
)

// DB represents the database connection
type DB struct {
	*sql.DB
}

// New creates a new database connection and initializes the schema
func New(dbPath string) (*DB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	database := &DB{db}
	if err := database.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return database, nil
}

// initSchema creates the database tables if they don't exist
func (db *DB) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS scraped_media (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		post_id INTEGER NOT NULL,
		post_title TEXT NOT NULL,
		community_name TEXT NOT NULL,
		community_id INTEGER NOT NULL,
		author_name TEXT NOT NULL,
		author_id INTEGER NOT NULL,
		media_url TEXT NOT NULL,
		media_hash TEXT NOT NULL UNIQUE,
		file_name TEXT NOT NULL,
		file_path TEXT NOT NULL,
		file_size INTEGER NOT NULL,
		media_type TEXT NOT NULL,
		post_url TEXT NOT NULL,
		post_score INTEGER NOT NULL,
		post_created DATETIME NOT NULL,
		downloaded_at DATETIME NOT NULL,
		UNIQUE(post_id, media_url)
	);

	CREATE TABLE IF NOT EXISTS scraped_posts (
		post_id INTEGER PRIMARY KEY,
		post_title TEXT NOT NULL,
		community_name TEXT NOT NULL,
		community_id INTEGER NOT NULL,
		author_name TEXT NOT NULL,
		author_id INTEGER NOT NULL,
		post_created DATETIME NOT NULL,
		scraped_at DATETIME NOT NULL,
		had_media BOOLEAN NOT NULL,
		media_count INTEGER NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_media_hash ON scraped_media(media_hash);
	CREATE INDEX IF NOT EXISTS idx_post_id ON scraped_media(post_id);
	CREATE INDEX IF NOT EXISTS idx_community_name ON scraped_media(community_name);
	CREATE INDEX IF NOT EXISTS idx_downloaded_at ON scraped_media(downloaded_at);
	CREATE INDEX IF NOT EXISTS idx_scraped_posts_community ON scraped_posts(community_name);
	CREATE INDEX IF NOT EXISTS idx_scraped_posts_scraped_at ON scraped_posts(scraped_at);
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	return nil
}

// MediaExists checks if media with the given hash already exists
func (db *DB) MediaExists(hash string) (bool, error) {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM scraped_media WHERE media_hash = ?)`
	err := db.QueryRow(query, hash).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check media existence: %w", err)
	}
	return exists, nil
}

// PostExists checks if a post has already been scraped
func (db *DB) PostExists(postID int64) (bool, error) {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM scraped_posts WHERE post_id = ?)`
	err := db.QueryRow(query, postID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check post existence: %w", err)
	}
	return exists, nil
}

// MarkPostAsScraped records that we've processed a post (with or without media)
func (db *DB) MarkPostAsScraped(postView *models.PostView, mediaCount int) error {
	query := `
		INSERT OR REPLACE INTO scraped_posts (
			post_id, post_title, community_name, community_id,
			author_name, author_id, post_created, scraped_at,
			had_media, media_count
		) VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'), ?, ?)
	`

	_, err := db.Exec(query,
		postView.Post.ID,
		postView.Post.Name,
		postView.Community.Name,
		postView.Community.ID,
		postView.Creator.Name,
		postView.Creator.ID,
		postView.Post.Published,
		mediaCount > 0,
		mediaCount,
	)
	if err != nil {
		return fmt.Errorf("failed to mark post as scraped: %w", err)
	}

	return nil
}

// SaveMedia saves a scraped media record to the database
func (db *DB) SaveMedia(media *models.ScrapedMedia) error {
	query := `
		INSERT INTO scraped_media (
			post_id, post_title, community_name, community_id,
			author_name, author_id, media_url, media_hash,
			file_name, file_path, file_size, media_type,
			post_url, post_score, post_created, downloaded_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := db.Exec(query,
		media.PostID, media.PostTitle, media.CommunityName, media.CommunityID,
		media.AuthorName, media.AuthorID, media.MediaURL, media.MediaHash,
		media.FileName, media.FilePath, media.FileSize, media.MediaType,
		media.PostURL, media.PostScore, media.PostCreated, media.DownloadedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to save media: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert id: %w", err)
	}

	media.ID = id
	return nil
}

// GetMediaByHash retrieves a media record by its hash
func (db *DB) GetMediaByHash(hash string) (*models.ScrapedMedia, error) {
	media := &models.ScrapedMedia{}
	query := `
		SELECT id, post_id, post_title, community_name, community_id,
		       author_name, author_id, media_url, media_hash,
		       file_name, file_path, file_size, media_type,
		       post_url, post_score, post_created, downloaded_at
		FROM scraped_media WHERE media_hash = ?
	`

	err := db.QueryRow(query, hash).Scan(
		&media.ID, &media.PostID, &media.PostTitle, &media.CommunityName, &media.CommunityID,
		&media.AuthorName, &media.AuthorID, &media.MediaURL, &media.MediaHash,
		&media.FileName, &media.FilePath, &media.FileSize, &media.MediaType,
		&media.PostURL, &media.PostScore, &media.PostCreated, &media.DownloadedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get media by hash: %w", err)
	}

	return media, nil
}

// GetStats returns statistics about scraped media
func (db *DB) GetStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Total media count
	var totalCount int
	err := db.QueryRow(`SELECT COUNT(*) FROM scraped_media`).Scan(&totalCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get total count: %w", err)
	}
	stats["total_media"] = totalCount

	// Count by media type
	rows, err := db.Query(`SELECT media_type, COUNT(*) FROM scraped_media GROUP BY media_type`)
	if err != nil {
		return nil, fmt.Errorf("failed to get media type counts: %w", err)
	}
	defer rows.Close()

	typeCounts := make(map[string]int)
	for rows.Next() {
		var mediaType string
		var count int
		if err := rows.Scan(&mediaType, &count); err != nil {
			return nil, fmt.Errorf("failed to scan media type count: %w", err)
		}
		typeCounts[mediaType] = count
	}
	stats["by_type"] = typeCounts

	// Count by community
	rows, err = db.Query(`SELECT community_name, COUNT(*) FROM scraped_media GROUP BY community_name ORDER BY COUNT(*) DESC LIMIT 10`)
	if err != nil {
		return nil, fmt.Errorf("failed to get community counts: %w", err)
	}
	defer rows.Close()

	communityCounts := make(map[string]int)
	for rows.Next() {
		var communityName string
		var count int
		if err := rows.Scan(&communityName, &count); err != nil {
			return nil, fmt.Errorf("failed to scan community count: %w", err)
		}
		communityCounts[communityName] = count
	}
	stats["top_communities"] = communityCounts

	return stats, nil
}

// HashContent computes the SHA256 hash of content
func HashContent(content io.Reader) (string, error) {
	hasher := sha256.New()
	if _, err := io.Copy(hasher, content); err != nil {
		return "", fmt.Errorf("failed to hash content: %w", err)
	}
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

// Close closes the database connection
func (db *DB) Close() error {
	return db.DB.Close()
}
