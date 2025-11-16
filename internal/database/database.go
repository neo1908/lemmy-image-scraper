package database

import (
	"crypto/sha256"
	"fmt"
	"io"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/neo1908/lemmy-image-scraper/pkg/models"
)

// DB represents the database connection
type DB struct {
	*sqlx.DB
}

// New creates a new database connection and initializes the schema
func New(dbPath string) (*DB, error) {
	db, err := sqlx.Open("sqlite3", dbPath)
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

	CREATE TABLE IF NOT EXISTS scraped_comments (
		comment_id INTEGER PRIMARY KEY,
		post_id INTEGER NOT NULL,
		creator_id INTEGER NOT NULL,
		creator_name TEXT NOT NULL,
		content TEXT NOT NULL,
		path TEXT NOT NULL,
		score INTEGER NOT NULL,
		upvotes INTEGER NOT NULL,
		downvotes INTEGER NOT NULL,
		child_count INTEGER NOT NULL,
		published DATETIME NOT NULL,
		updated DATETIME,
		removed BOOLEAN NOT NULL,
		deleted BOOLEAN NOT NULL,
		distinguished BOOLEAN NOT NULL,
		scraped_at DATETIME NOT NULL,
		FOREIGN KEY (post_id) REFERENCES scraped_posts(post_id)
	);

	CREATE INDEX IF NOT EXISTS idx_media_hash ON scraped_media(media_hash);
	CREATE INDEX IF NOT EXISTS idx_post_id ON scraped_media(post_id);
	CREATE INDEX IF NOT EXISTS idx_community_name ON scraped_media(community_name);
	CREATE INDEX IF NOT EXISTS idx_downloaded_at ON scraped_media(downloaded_at);
	CREATE INDEX IF NOT EXISTS idx_scraped_posts_community ON scraped_posts(community_name);
	CREATE INDEX IF NOT EXISTS idx_scraped_posts_scraped_at ON scraped_posts(scraped_at);
	CREATE INDEX IF NOT EXISTS idx_comments_post_id ON scraped_comments(post_id);
	CREATE INDEX IF NOT EXISTS idx_comments_path ON scraped_comments(path);
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
	err := db.Get(&exists, query, hash)
	if err != nil {
		return false, fmt.Errorf("failed to check media existence: %w", err)
	}
	return exists, nil
}

// PostExists checks if a post has already been scraped
func (db *DB) PostExists(postID int64) (bool, error) {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM scraped_posts WHERE post_id = ?)`
	err := db.Get(&exists, query, postID)
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
	query := `SELECT * FROM scraped_media WHERE media_hash = ?`

	err := db.Get(media, query, hash)
	if err != nil {
		// sqlx returns sql.ErrNoRows for Get() when no rows found
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get media by hash: %w", err)
	}

	return media, nil
}

// GetStats returns statistics about scraped media
func (db *DB) GetStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Total media count
	var totalCount int
	err := db.Get(&totalCount, `SELECT COUNT(*) FROM scraped_media`)
	if err != nil {
		return nil, fmt.Errorf("failed to get total count: %w", err)
	}
	stats["total_media"] = totalCount

	// Count by media type
	type TypeCount struct {
		MediaType string `db:"media_type"`
		Count     int    `db:"count"`
	}
	var typeCounts []TypeCount
	err = db.Select(&typeCounts, `SELECT media_type, COUNT(*) as count FROM scraped_media GROUP BY media_type`)
	if err != nil {
		return nil, fmt.Errorf("failed to get media type counts: %w", err)
	}

	typeMap := make(map[string]int)
	for _, tc := range typeCounts {
		typeMap[tc.MediaType] = tc.Count
	}
	stats["by_type"] = typeMap

	// Count by community
	type CommunityCount struct {
		CommunityName string `db:"community_name"`
		Count         int    `db:"count"`
	}
	var communityCounts []CommunityCount
	err = db.Select(&communityCounts, `SELECT community_name, COUNT(*) as count FROM scraped_media GROUP BY community_name ORDER BY count DESC LIMIT 10`)
	if err != nil {
		return nil, fmt.Errorf("failed to get community counts: %w", err)
	}

	communityMap := make(map[string]int)
	for _, cc := range communityCounts {
		communityMap[cc.CommunityName] = cc.Count
	}
	stats["top_communities"] = communityMap

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

// SaveComment saves a comment to the database
func (db *DB) SaveComment(commentView *models.CommentView) error {
	query := `
		INSERT OR REPLACE INTO scraped_comments (
			comment_id, post_id, creator_id, creator_name, content, path,
			score, upvotes, downvotes, child_count, published, updated,
			removed, deleted, distinguished, scraped_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
	`

	var updated interface{}
	if !commentView.Comment.Updated.IsZero() {
		updated = commentView.Comment.Updated
	}

	_, err := db.Exec(query,
		commentView.Comment.ID,
		commentView.Comment.PostID,
		commentView.Creator.ID,
		commentView.Creator.Name,
		commentView.Comment.Content,
		commentView.Comment.Path,
		commentView.Counts.Score,
		commentView.Counts.Upvotes,
		commentView.Counts.Downvotes,
		commentView.Counts.ChildCount,
		commentView.Comment.Published,
		updated,
		commentView.Comment.Removed,
		commentView.Comment.Deleted,
		commentView.Comment.Distinguished,
	)
	if err != nil {
		return fmt.Errorf("failed to save comment: %w", err)
	}

	return nil
}

// Comment represents a comment record from the database
type Comment struct {
	CommentID     int64  `db:"comment_id"`
	PostID        int64  `db:"post_id"`
	CreatorID     int64  `db:"creator_id"`
	CreatorName   string `db:"creator_name"`
	Content       string `db:"content"`
	Path          string `db:"path"`
	Score         int64  `db:"score"`
	Upvotes       int64  `db:"upvotes"`
	Downvotes     int64  `db:"downvotes"`
	ChildCount    int64  `db:"child_count"`
	Published     string `db:"published"`
	Updated       string `db:"updated"`
	Removed       bool   `db:"removed"`
	Deleted       bool   `db:"deleted"`
	Distinguished bool   `db:"distinguished"`
}

// GetCommentsByPostID retrieves all comments for a post, ordered by path for proper threading
func (db *DB) GetCommentsByPostID(postID int64) ([]map[string]interface{}, error) {
	query := `
		SELECT
			comment_id, post_id, creator_id, creator_name, content, path,
			score, upvotes, downvotes, child_count, published,
			COALESCE(updated, '') as updated,
			removed, deleted, distinguished
		FROM scraped_comments
		WHERE post_id = ? AND removed = 0 AND deleted = 0
		ORDER BY path ASC
	`

	var comments []Comment
	err := db.Select(&comments, query, postID)
	if err != nil {
		return nil, fmt.Errorf("failed to query comments: %w", err)
	}

	// Convert to map format for backward compatibility with web UI
	result := make([]map[string]interface{}, len(comments))
	for i, c := range comments {
		result[i] = map[string]interface{}{
			"comment_id":    c.CommentID,
			"post_id":       c.PostID,
			"creator_id":    c.CreatorID,
			"creator_name":  c.CreatorName,
			"content":       c.Content,
			"path":          c.Path,
			"score":         c.Score,
			"upvotes":       c.Upvotes,
			"downvotes":     c.Downvotes,
			"child_count":   c.ChildCount,
			"published":     c.Published,
			"distinguished": c.Distinguished,
		}
		if c.Updated != "" {
			result[i]["updated"] = c.Updated
		}
	}

	return result, nil
}

// CommentsExistForPost checks if comments have been scraped for a post
func (db *DB) CommentsExistForPost(postID int64) (bool, error) {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM scraped_comments WHERE post_id = ? LIMIT 1)`
	err := db.Get(&exists, query, postID)
	if err != nil {
		return false, fmt.Errorf("failed to check comments existence: %w", err)
	}
	return exists, nil
}

// Close closes the database connection
func (db *DB) Close() error {
	return db.DB.Close()
}
