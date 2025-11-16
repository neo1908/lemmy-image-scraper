package downloader

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/neo1908/lemmy-image-scraper/internal/database"
	"github.com/neo1908/lemmy-image-scraper/pkg/models"
	log "github.com/sirupsen/logrus"
)

// Downloader handles downloading and storing media files
type Downloader struct {
	DB          *database.DB
	HTTPClient  *http.Client
	BaseDir     string
}

// New creates a new Downloader instance
func New(db *database.DB, baseDir string) *Downloader {
	return &Downloader{
		DB: db,
		HTTPClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		BaseDir: baseDir,
	}
}

// DownloadMedia downloads a media file from a URL and stores it with deduplication
func (d *Downloader) DownloadMedia(mediaURL string, postView models.PostView) (*models.ScrapedMedia, error) {
	// Skip empty URLs
	if mediaURL == "" {
		return nil, fmt.Errorf("empty media URL")
	}

	log.Debugf("Attempting to download media from: %s", mediaURL)

	// Download the file content
	resp, err := d.HTTPClient.Get(mediaURL)
	if err != nil {
		return nil, fmt.Errorf("failed to download media: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// Read content into memory for hashing and writing
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read media content: %w", err)
	}

	// Calculate hash
	hash, err := database.HashContent(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("failed to hash content: %w", err)
	}

	// Check if media already exists
	exists, err := d.DB.MediaExists(hash)
	if err != nil {
		return nil, fmt.Errorf("failed to check media existence: %w", err)
	}

	if exists {
		log.Debugf("Media already exists (hash: %s), skipping download", hash[:16])
		existing, err := d.DB.GetMediaByHash(hash)
		if err != nil {
			return nil, fmt.Errorf("failed to get existing media: %w", err)
		}
		return existing, nil
	}

	// Determine media type and file extension
	mediaType := determineMediaType(resp.Header.Get("Content-Type"), mediaURL)
	fileExt := getFileExtension(resp.Header.Get("Content-Type"), mediaURL)

	// Create filename: postID_originalname or postID.ext
	originalName := filepath.Base(mediaURL)
	// Clean the original name
	originalName = strings.Split(originalName, "?")[0] // Remove query parameters

	fileName := fmt.Sprintf("%d_%s", postView.Post.ID, originalName)
	if !strings.Contains(fileName, ".") {
		fileName = fmt.Sprintf("%d%s", postView.Post.ID, fileExt)
	}

	// Create community directory
	communityDir := filepath.Join(d.BaseDir, sanitizePath(postView.Community.Name))
	if err := os.MkdirAll(communityDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create community directory: %w", err)
	}

	// Full file path
	filePath := filepath.Join(communityDir, fileName)

	// Write file to disk
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	// Create database record
	scrapedMedia := &models.ScrapedMedia{
		PostID:        postView.Post.ID,
		PostTitle:     postView.Post.Name,
		CommunityName: postView.Community.Name,
		CommunityID:   postView.Community.ID,
		AuthorName:    postView.Creator.Name,
		AuthorID:      postView.Creator.ID,
		MediaURL:      mediaURL,
		MediaHash:     hash,
		FileName:      fileName,
		FilePath:      filePath,
		FileSize:      int64(len(content)),
		MediaType:     mediaType,
		PostURL:       mediaURL,
		PostScore:     postView.Counts.Score,
		PostCreated:   postView.Post.Published,
		DownloadedAt:  time.Now(),
	}

	// Save to database
	if err := d.DB.SaveMedia(scrapedMedia); err != nil {
		// Clean up file if database save fails
		os.Remove(filePath)
		return nil, fmt.Errorf("failed to save media to database: %w", err)
	}

	log.Infof("Downloaded media: %s (%s, %d bytes)", fileName, mediaType, len(content))
	return scrapedMedia, nil
}

// determineMediaType determines the media type from content type and URL
func determineMediaType(contentType, url string) string {
	contentType = strings.ToLower(contentType)
	url = strings.ToLower(url)

	if strings.Contains(contentType, "image") ||
	   strings.HasSuffix(url, ".jpg") || strings.HasSuffix(url, ".jpeg") ||
	   strings.HasSuffix(url, ".png") || strings.HasSuffix(url, ".gif") ||
	   strings.HasSuffix(url, ".webp") || strings.HasSuffix(url, ".bmp") {
		return "image"
	}

	if strings.Contains(contentType, "video") ||
	   strings.HasSuffix(url, ".mp4") || strings.HasSuffix(url, ".webm") ||
	   strings.HasSuffix(url, ".mov") || strings.HasSuffix(url, ".avi") ||
	   strings.HasSuffix(url, ".mkv") || strings.HasSuffix(url, ".m4v") {
		return "video"
	}

	return "other"
}

// getFileExtension determines the file extension from content type and URL
func getFileExtension(contentType, url string) string {
	// Try to get extension from URL first
	urlExt := filepath.Ext(url)
	if urlExt != "" {
		// Remove query parameters
		urlExt = strings.Split(urlExt, "?")[0]
		return urlExt
	}

	// Fallback to content type
	contentType = strings.ToLower(contentType)
	switch {
	case strings.Contains(contentType, "jpeg"):
		return ".jpg"
	case strings.Contains(contentType, "png"):
		return ".png"
	case strings.Contains(contentType, "gif"):
		return ".gif"
	case strings.Contains(contentType, "webp"):
		return ".webp"
	case strings.Contains(contentType, "mp4"):
		return ".mp4"
	case strings.Contains(contentType, "webm"):
		return ".webm"
	default:
		return ".bin"
	}
}

// sanitizePath removes invalid characters from path names
func sanitizePath(path string) string {
	// Replace invalid characters with underscores
	invalid := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"}
	result := path
	for _, char := range invalid {
		result = strings.ReplaceAll(result, char, "_")
	}
	return result
}

// ShouldDownload checks if a media URL should be downloaded based on type and config
func ShouldDownload(url string, includeImages, includeVideos, includeOther bool) bool {
	mediaType := determineMediaType("", url)

	switch mediaType {
	case "image":
		return includeImages
	case "video":
		return includeVideos
	case "other":
		return includeOther
	default:
		return false
	}
}
