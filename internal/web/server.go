package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/neo1908/lemmy-image-scraper/internal/config"
	"github.com/neo1908/lemmy-image-scraper/internal/database"
	"github.com/neo1908/lemmy-image-scraper/pkg/models"
	log "github.com/sirupsen/logrus"
)

// Server represents the web server
type Server struct {
	Config    *config.Config
	DB        *database.DB
	handler   http.Handler
	templates *template.Template
}

// New creates a new web server
func New(cfg *config.Config, db *database.DB) *Server {
	s := &Server{
		Config: cfg,
		DB:     db,
	}
	s.setupRoutes()
	return s
}

// setupRoutes configures the HTTP routes
func (s *Server) setupRoutes() {
	// Parse embedded templates
	s.templates = template.Must(template.New("").Funcs(template.FuncMap{
		"formatFileSize": formatFileSize,
		"formatDate":     formatDate,
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
	}).Parse(indexTemplate + mediaGridTemplate + mediaModalTemplate))

	mux := http.NewServeMux()

	// Main page
	mux.HandleFunc("/", s.handleIndex)

	// HTMX endpoints
	mux.HandleFunc("/media-grid", s.handleMediaGrid)

	// API routes (kept for compatibility)
	mux.HandleFunc("/api/media/", func(w http.ResponseWriter, r *http.Request) {
		// Check if this is a request for a specific media item (has ID after /api/media/)
		idPart := strings.TrimPrefix(r.URL.Path, "/api/media/")
		if idPart != "" && idPart != "/" {
			s.handleGetMediaByID(w, r)
			return
		}
		s.handleGetMedia(w, r)
	})
	mux.HandleFunc("/api/media", s.handleGetMedia)
	mux.HandleFunc("/api/stats", s.handleGetStats)
	mux.HandleFunc("/api/communities", s.handleGetCommunities)
	mux.HandleFunc("/api/comments/", s.handleGetComments)

	// Serve media files
	mux.HandleFunc("/media/", s.handleServeMedia)

	s.handler = mux
}

// Start starts the web server
func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.Config.WebServer.Host, s.Config.WebServer.Port)
	log.Infof("Starting web server on http://%s", addr)
	return http.ListenAndServe(addr, s.handler)
}

// handleIndex serves the main HTML page
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// Get initial data
	stats, _ := s.DB.GetStats()
	communities := s.getCommunityList()

	data := map[string]interface{}{
		"Stats":       stats,
		"Communities": communities,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "index", data); err != nil {
		log.Errorf("Template error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// handleMediaGrid serves the media grid (HTMX partial)
func (s *Server) handleMediaGrid(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	// Parse pagination
	limit := 50
	if l := query.Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}

	offset := 0
	if o := query.Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	// Parse filters
	community := query.Get("community")
	mediaType := query.Get("type")
	sortBy := query.Get("sort")
	if sortBy == "" {
		sortBy = "downloaded_at"
	}
	sortOrder := query.Get("order")
	if sortOrder == "" {
		sortOrder = "DESC"
	}

	media, total := s.getMediaList(community, mediaType, sortBy, sortOrder, limit, offset)

	data := map[string]interface{}{
		"Media":      media,
		"Total":      total,
		"Limit":      limit,
		"Offset":     offset,
		"Community":  community,
		"Type":       mediaType,
		"Sort":       sortBy,
		"SortOrder":  sortOrder,
		"HasPrev":    offset > 0,
		"HasNext":    offset+limit < total,
		"Page":       (offset / limit) + 1,
		"TotalPages": (total + limit - 1) / limit,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "media-grid", data); err != nil {
		log.Errorf("Template error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// handleGetMedia returns a paginated list of media
func (s *Server) handleGetMedia(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	// Parse pagination params
	limit := 50
	if l := query.Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}

	offset := 0
	if o := query.Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	// Parse filter params
	community := query.Get("community")
	mediaType := query.Get("type")
	sortBy := query.Get("sort")
	if sortBy == "" {
		sortBy = "downloaded_at"
	}

	sortOrder := query.Get("order")
	if sortOrder == "" {
		sortOrder = "DESC"
	}

	// Build SQL query
	sqlQuery := `
		SELECT
			id, post_id, post_title, community_name, community_id,
			author_name, author_id, media_url, media_hash,
			file_name, file_path, file_size, media_type,
			post_url, post_score, post_created, downloaded_at
		FROM scraped_media
		WHERE 1=1
	`

	args := []interface{}{}

	if community != "" {
		sqlQuery += " AND community_name = ?"
		args = append(args, community)
	}

	if mediaType != "" {
		sqlQuery += " AND media_type = ?"
		args = append(args, mediaType)
	}

	// Add sorting
	allowedSortFields := map[string]bool{
		"downloaded_at": true,
		"post_created":  true,
		"file_size":     true,
		"post_score":    true,
	}
	if !allowedSortFields[sortBy] {
		sortBy = "downloaded_at"
	}

	if sortOrder != "ASC" && sortOrder != "DESC" {
		sortOrder = "DESC"
	}

	sqlQuery += fmt.Sprintf(" ORDER BY %s %s LIMIT ? OFFSET ?", sortBy, sortOrder)
	args = append(args, limit, offset)

	// Get total count
	countQuery := `SELECT COUNT(*) FROM scraped_media WHERE 1=1`
	countArgs := []interface{}{}
	if community != "" {
		countQuery += " AND community_name = ?"
		countArgs = append(countArgs, community)
	}
	if mediaType != "" {
		countQuery += " AND media_type = ?"
		countArgs = append(countArgs, mediaType)
	}

	var total int
	if err := s.DB.Get(&total, countQuery, countArgs...); err != nil {
		log.Errorf("Failed to get total count: %v", err)
		http.Error(w, "Failed to get count", http.StatusInternalServerError)
		return
	}

	// Execute query using sqlx.Select
	var mediaItems []models.ScrapedMedia
	err := s.DB.Select(&mediaItems, sqlQuery, args...)
	if err != nil {
		log.Errorf("Failed to query media: %v", err)
		http.Error(w, "Failed to query media", http.StatusInternalServerError)
		return
	}

	// Convert to map format for API response
	media := make([]map[string]interface{}, len(mediaItems))
	for i, item := range mediaItems {
		serveURL := fmt.Sprintf("/media/%s", filepath.Join(item.CommunityName, item.FileName))

		media[i] = map[string]interface{}{
			"id":             item.ID,
			"post_id":        item.PostID,
			"post_title":     item.PostTitle,
			"community_name": item.CommunityName,
			"community_id":   item.CommunityID,
			"author_name":    item.AuthorName,
			"author_id":      item.AuthorID,
			"media_url":      item.MediaURL,
			"media_hash":     item.MediaHash,
			"file_name":      item.FileName,
			"file_path":      item.FilePath,
			"file_size":      item.FileSize,
			"media_type":     item.MediaType,
			"post_url":       item.PostURL,
			"post_score":     item.PostScore,
			"post_created":   item.PostCreated.Format(time.RFC3339),
			"downloaded_at":  item.DownloadedAt.Format(time.RFC3339),
			"serve_url":      serveURL,
		}
	}

	response := map[string]interface{}{
		"media":  media,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleGetMediaByID returns a specific media item
func (s *Server) handleGetMediaByID(w http.ResponseWriter, r *http.Request) {
	// Extract ID from URL path
	idStr := strings.TrimPrefix(r.URL.Path, "/api/media/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid media ID", http.StatusBadRequest)
		return
	}

	var (
		postID, communityID, authorID, fileSize int64
		postTitle, communityName, authorName    string
		mediaURL, mediaHash, fileName, filePath string
		mediaType, postURL                      string
		postScore                               int
		postCreated, downloadedAt               string
	)

	query := `
		SELECT
			post_id, post_title, community_name, community_id,
			author_name, author_id, media_url, media_hash,
			file_name, file_path, file_size, media_type,
			post_url, post_score, post_created, downloaded_at
		FROM scraped_media WHERE id = ?
	`

	err = s.DB.QueryRow(query, id).Scan(
		&postID, &postTitle, &communityName, &communityID,
		&authorName, &authorID, &mediaURL, &mediaHash,
		&fileName, &filePath, &fileSize, &mediaType,
		&postURL, &postScore, &postCreated, &downloadedAt,
	)

	if err != nil {
		log.Errorf("Failed to get media by ID: %v", err)
		http.Error(w, "Media not found", http.StatusNotFound)
		return
	}

	serveURL := fmt.Sprintf("/media/%s", filepath.Join(communityName, fileName))

	response := map[string]interface{}{
		"id":             id,
		"post_id":        postID,
		"post_title":     postTitle,
		"community_name": communityName,
		"community_id":   communityID,
		"author_name":    authorName,
		"author_id":      authorID,
		"media_url":      mediaURL,
		"media_hash":     mediaHash,
		"file_name":      fileName,
		"file_path":      filePath,
		"file_size":      fileSize,
		"media_type":     mediaType,
		"post_url":       postURL,
		"post_score":     postScore,
		"post_created":   postCreated,
		"downloaded_at":  downloadedAt,
		"serve_url":      serveURL,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleGetStats returns statistics about scraped media
func (s *Server) handleGetStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.DB.GetStats()
	if err != nil {
		log.Errorf("Failed to get stats: %v", err)
		http.Error(w, "Failed to get stats", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleGetCommunities returns a list of communities with media counts
func (s *Server) handleGetCommunities(w http.ResponseWriter, r *http.Request) {
	type CommunityCount struct {
		Name  string `db:"community_name"`
		Count int    `db:"count"`
	}

	query := `
		SELECT community_name, COUNT(*) as count
		FROM scraped_media
		GROUP BY community_name
		ORDER BY count DESC
	`

	var communities []CommunityCount
	err := s.DB.Select(&communities, query)
	if err != nil {
		log.Errorf("Failed to query communities: %v", err)
		http.Error(w, "Failed to query communities", http.StatusInternalServerError)
		return
	}

	// Convert to map format for API response
	result := make([]map[string]interface{}, len(communities))
	for i, c := range communities {
		result[i] = map[string]interface{}{
			"name":  c.Name,
			"count": c.Count,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"communities": result,
	})
}

// handleGetComments returns comments for a specific media item's post
func (s *Server) handleGetComments(w http.ResponseWriter, r *http.Request) {
	// Extract media ID from URL path
	idStr := strings.TrimPrefix(r.URL.Path, "/api/comments/")
	mediaID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid media ID", http.StatusBadRequest)
		return
	}

	// First, get the post_id for this media item
	var postID int64
	query := `SELECT post_id FROM scraped_media WHERE id = ?`
	err = s.DB.Get(&postID, query, mediaID)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			http.Error(w, "Media not found", http.StatusNotFound)
			return
		}
		log.Errorf("Failed to get post ID for media: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get comments for the post
	comments, err := s.DB.GetCommentsByPostID(postID)
	if err != nil {
		log.Errorf("Failed to get comments: %v", err)
		http.Error(w, "Failed to get comments", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"comments": comments,
		"post_id":  postID,
	})
}

// handleServeMedia serves media files from the storage directory
func (s *Server) handleServeMedia(w http.ResponseWriter, r *http.Request) {
	// Extract path after /media/
	mediaPath := strings.TrimPrefix(r.URL.Path, "/media/")

	// Prevent directory traversal
	if strings.Contains(mediaPath, "..") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// Construct full file path
	fullPath := filepath.Join(s.Config.Storage.BaseDirectory, mediaPath)

	// Check if file exists
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// Serve the file
	http.ServeFile(w, r, fullPath)
}

// Helper functions

func (s *Server) getCommunityList() []map[string]interface{} {
	type CommunityCount struct {
		Name  string `db:"community_name"`
		Count int    `db:"count"`
	}

	query := `
		SELECT community_name, COUNT(*) as count
		FROM scraped_media
		GROUP BY community_name
		ORDER BY count DESC
	`

	var communities []CommunityCount
	err := s.DB.Select(&communities, query)
	if err != nil {
		return []map[string]interface{}{}
	}

	// Convert to map format for template compatibility
	result := make([]map[string]interface{}, len(communities))
	for i, c := range communities {
		result[i] = map[string]interface{}{
			"name":  c.Name,
			"count": c.Count,
		}
	}
	return result
}

func (s *Server) getMediaList(community, mediaType, sortBy, sortOrder string, limit, offset int) ([]map[string]interface{}, int) {
	sqlQuery := `
		SELECT
			id, post_id, post_title, community_name, community_id,
			author_name, author_id, media_url, media_hash,
			file_name, file_path, file_size, media_type,
			post_url, post_score, post_created, downloaded_at
		FROM scraped_media
		WHERE 1=1
	`

	args := []interface{}{}

	if community != "" {
		sqlQuery += " AND community_name = ?"
		args = append(args, community)
	}

	if mediaType != "" {
		sqlQuery += " AND media_type = ?"
		args = append(args, mediaType)
	}

	// Add sorting
	allowedSortFields := map[string]bool{
		"downloaded_at": true,
		"post_created":  true,
		"file_size":     true,
		"post_score":    true,
	}
	if !allowedSortFields[sortBy] {
		sortBy = "downloaded_at"
	}

	if sortOrder != "ASC" && sortOrder != "DESC" {
		sortOrder = "DESC"
	}

	sqlQuery += fmt.Sprintf(" ORDER BY %s %s LIMIT ? OFFSET ?", sortBy, sortOrder)
	args = append(args, limit, offset)

	// Get total count
	countQuery := `SELECT COUNT(*) FROM scraped_media WHERE 1=1`
	countArgs := []interface{}{}
	if community != "" {
		countQuery += " AND community_name = ?"
		countArgs = append(countArgs, community)
	}
	if mediaType != "" {
		countQuery += " AND media_type = ?"
		countArgs = append(countArgs, mediaType)
	}

	var total int
	if err := s.DB.Get(&total, countQuery, countArgs...); err != nil {
		return []map[string]interface{}{}, 0
	}

	// Execute query using sqlx.Select with models.ScrapedMedia
	var mediaItems []models.ScrapedMedia
	err := s.DB.Select(&mediaItems, sqlQuery, args...)
	if err != nil {
		return []map[string]interface{}{}, 0
	}

	// Convert to map format for template compatibility
	media := make([]map[string]interface{}, len(mediaItems))
	for i, item := range mediaItems {
		serveURL := fmt.Sprintf("/media/%s", filepath.Join(item.CommunityName, item.FileName))

		media[i] = map[string]interface{}{
			"id":             item.ID,
			"post_id":        item.PostID,
			"post_title":     item.PostTitle,
			"community_name": item.CommunityName,
			"author_name":    item.AuthorName,
			"media_type":     item.MediaType,
			"file_size":      item.FileSize,
			"post_score":     item.PostScore,
			"post_url":       item.PostURL,
			"serve_url":      serveURL,
			"downloaded_at":  item.DownloadedAt.Format(time.RFC3339),
			"post_created":   item.PostCreated.Format(time.RFC3339),
		}
	}

	return media, total
}

func formatFileSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	}
	if bytes < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
	}
	return fmt.Sprintf("%.1f GB", float64(bytes)/(1024*1024*1024))
}

func formatDate(dateStr string) string {
	t, err := time.Parse(time.RFC3339, dateStr)
	if err != nil {
		return dateStr
	}
	return t.Format("Jan 2, 2006 3:04 PM")
}

// HTML Templates

const indexTemplate = `{{define "index"}}
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Lemmy Media Browser</title>
    <script src="https://unpkg.com/htmx.org@1.9.10"></script>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
            background: #0f0f0f;
            color: #e0e0e0;
            line-height: 1.6;
        }
        .header {
            background: #1a1a1a;
            border-bottom: 1px solid #2a2a2a;
            padding: 12px 16px;
            position: sticky;
            top: 0;
            z-index: 100;
        }
        .header-content {
            max-width: 1400px;
            margin: 0 auto;
            display: flex;
            justify-content: space-between;
            align-items: center;
        }
        .header h1 { font-size: 24px; font-weight: 600; color: #fff; }
        .stats {
            display: flex;
            gap: 24px;
            font-size: 14px;
            color: #999;
        }
        .stats span { font-weight: 600; color: #e0e0e0; }
        .filters {
            background: #1a1a1a;
            border-bottom: 1px solid #2a2a2a;
            padding: 8px 16px;
        }
        .filters-content {
            max-width: 1400px;
            margin: 0 auto;
            display: flex;
            gap: 12px;
            flex-wrap: wrap;
        }
        select {
            background: #2a2a2a;
            color: #e0e0e0;
            border: 1px solid #3a3a3a;
            padding: 6px 12px;
            border-radius: 4px;
            font-size: 14px;
            cursor: pointer;
        }
        select:hover { background: #333; }
        .content {
            max-width: 1400px;
            margin: 0 auto;
            padding: 24px 16px;
        }
        .grid {
            display: grid;
            grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
            gap: 12px;
        }
        @media (min-width: 640px) { .grid { grid-template-columns: repeat(2, 1fr); } }
        @media (min-width: 1024px) { .grid { grid-template-columns: repeat(4, 1fr); } }
        .card {
            background: #1a1a1a;
            border-radius: 8px;
            overflow: hidden;
            cursor: pointer;
            transition: all 0.2s;
        }
        .card:hover {
            transform: translateY(-4px);
            box-shadow: 0 8px 16px rgba(0,0,0,0.4);
        }
        .card-image {
            aspect-ratio: 4/3;
            background: #2a2a2a;
            position: relative;
            overflow: hidden;
        }
        .card-image img, .card-image video {
            width: 100%;
            height: 100%;
            object-fit: cover;
            transition: transform 0.2s;
        }
        .card:hover .card-image img, .card:hover .card-image video { transform: scale(1.05); }
        .card-image .play-overlay {
            position: absolute;
            inset: 0;
            display: flex;
            align-items: center;
            justify-content: center;
            background: rgba(0, 0, 0, 0.3);
            pointer-events: none;
        }
        .card-image .play-overlay svg {
            width: 64px;
            height: 64px;
            fill: rgba(255, 255, 255, 0.9);
            filter: drop-shadow(0 2px 4px rgba(0,0,0,0.5));
        }
        .card-image .icon {
            position: absolute;
            inset: 0;
            display: flex;
            align-items: center;
            justify-content: center;
        }
        .card-image .icon svg {
            width: 48px;
            height: 48px;
            fill: #666;
        }
        .card-info {
            padding: 12px;
        }
        .card-title {
            font-size: 14px;
            font-weight: 500;
            margin-bottom: 4px;
            overflow: hidden;
            text-overflow: ellipsis;
            display: -webkit-box;
            -webkit-line-clamp: 2;
            -webkit-box-orient: vertical;
        }
        .card-meta {
            font-size: 12px;
            color: #999;
            display: flex;
            gap: 8px;
            align-items: center;
        }
        .card-meta span:not(:last-child)::after {
            content: '•';
            margin-left: 8px;
        }
        .pagination {
            margin-top: 32px;
            padding-bottom: 32px;
            display: flex;
            justify-content: center;
            gap: 12px;
            align-items: center;
        }
        .btn {
            background: #2a2a2a;
            color: #e0e0e0;
            border: 1px solid #3a3a3a;
            padding: 8px 16px;
            border-radius: 4px;
            font-size: 14px;
            cursor: pointer;
            transition: background 0.2s;
        }
        .btn:hover:not(:disabled) { background: #333; }
        .btn:disabled {
            opacity: 0.5;
            cursor: not-allowed;
        }
        .loading {
            text-align: center;
            padding: 64px;
            color: #999;
        }
        .modal {
            position: fixed;
            inset: 0;
            background: rgba(0,0,0,0.9);
            z-index: 1000;
            display: none;
            align-items: center;
            justify-content: center;
            padding: 16px;
        }
        .modal.active { display: flex; }
        .modal-content {
            background: #1a1a1a;
            border-radius: 8px;
            max-width: 1200px;
            max-height: 90vh;
            overflow: auto;
            position: relative;
        }
        .modal-header {
            padding: 16px;
            border-bottom: 1px solid #2a2a2a;
            display: flex;
            justify-content: space-between;
            align-items: start;
            position: sticky;
            top: 0;
            background: #1a1a1a;
            z-index: 10;
        }
        .modal-title { font-size: 18px; font-weight: 600; flex: 1; padding-right: 16px; }
        .modal-close {
            background: #2a2a2a;
            border: none;
            color: #e0e0e0;
            width: 32px;
            height: 32px;
            border-radius: 4px;
            cursor: pointer;
            font-size: 20px;
        }
        .modal-close:hover { background: #333; }
        .modal-body { padding: 16px; }
        .modal-image {
            width: 100%;
            max-height: 70vh;
            object-fit: contain;
        }
        .modal-video {
            width: 100%;
            max-height: 70vh;
        }
        .modal-meta {
            margin-top: 16px;
            display: grid;
            grid-template-columns: repeat(2, 1fr);
            gap: 16px;
            font-size: 14px;
            color: #999;
        }
        .modal-meta strong { color: #e0e0e0; }
        .modal-link {
            color: #4a9eff;
            text-decoration: none;
        }
        .modal-link:hover { text-decoration: underline; }
        .comments-section {
            margin-top: 24px;
            padding-top: 24px;
            border-top: 1px solid #2a2a2a;
        }
        .comments-header {
            font-size: 16px;
            font-weight: 600;
            margin-bottom: 16px;
        }
        .comment {
            margin-bottom: 12px;
            padding: 12px;
            background: #2a2a2a;
            border-radius: 4px;
            border-left: 2px solid #3a3a3a;
        }
        .comment-nested {
            margin-left: 24px;
            margin-top: 8px;
            border-left-color: #4a4a4a;
        }
        .comment-header {
            display: flex;
            align-items: center;
            gap: 8px;
            margin-bottom: 8px;
            font-size: 13px;
        }
        .comment-author {
            font-weight: 600;
            color: #4a9eff;
        }
        .comment-score {
            color: #999;
        }
        .comment-score.positive { color: #ff6b35; }
        .comment-time {
            color: #666;
            font-size: 12px;
        }
        .comment-content {
            font-size: 14px;
            line-height: 1.5;
            white-space: pre-wrap;
            word-wrap: break-word;
        }
        .comment-distinguished {
            background: #1a3a1a;
            border-left-color: #2a5a2a;
        }
        .loading-comments {
            text-align: center;
            padding: 24px;
            color: #999;
        }
    </style>
</head>
<body>
    <div class="header">
        <div class="header-content">
            <h1>Lemmy Media</h1>
            <div class="stats">
                {{if .Stats.total_media}}
                    <div><span>{{.Stats.total_media}}</span> items</div>
                    {{range $type, $count := .Stats.by_type}}
                        <div><span>{{$count}}</span> {{$type}}</div>
                    {{end}}
                {{end}}
            </div>
        </div>
    </div>

    <div class="filters">
        <div class="filters-content">
            <select id="community" name="community">
                <option value="">All Communities</option>
                {{range .Communities}}
                    <option value="{{.name}}">{{.name}} ({{.count}})</option>
                {{end}}
            </select>
            <select id="type" name="type">
                <option value="">All Types</option>
                <option value="image">Images</option>
                <option value="video">Videos</option>
                <option value="other">Other</option>
            </select>
            <select id="sort" name="sort">
                <option value="downloaded_at">Downloaded</option>
                <option value="post_created">Posted</option>
                <option value="file_size">File Size</option>
                <option value="post_score">Score</option>
            </select>
            <select id="order" name="order">
                <option value="DESC">Newest</option>
                <option value="ASC">Oldest</option>
            </select>
        </div>
    </div>

    <div class="content">
        <div id="media-container"
             hx-get="/media-grid"
             hx-trigger="load, filterChange from:body"
             hx-include="[name='community'],[name='type'],[name='sort'],[name='order']">
            <div class="loading">Loading...</div>
        </div>
    </div>

    <div id="modal" class="modal" onclick="if(event.target === this) this.classList.remove('active')">
        <div class="modal-content" onclick="event.stopPropagation()">
            <div id="modal-body"></div>
        </div>
    </div>

    <script>
        // Trigger filter updates
        document.querySelectorAll('select').forEach(select => {
            select.addEventListener('change', () => {
                document.body.dispatchEvent(new CustomEvent('filterChange'));
            });
        });

        // Modal functions
        window.openModal = function(id) {
            fetch('/api/media/' + id)
                .then(r => r.json())
                .then(item => {
                    if (item) {
                        showModal(item);
                    }
                });
        };

        function showModal(item) {
            let mediaHTML = '';
            if (item.media_type === 'image') {
                mediaHTML = '<img src="' + item.serve_url + '" class="modal-image" alt="' + item.post_title + '">';
            } else if (item.media_type === 'video') {
                mediaHTML = '<video src="' + item.serve_url + '" class="modal-video" controls></video>';
            } else {
                mediaHTML = '<div style="text-align:center;padding:32px;">Preview not available. <a href="' + item.serve_url + '" class="modal-link" download>Download</a></div>';
            }

            document.getElementById('modal-body').innerHTML =
                '<div class="modal-header">' +
                    '<div class="modal-title">' + item.post_title + '</div>' +
                    '<button class="modal-close" onclick="document.getElementById(\'modal\').classList.remove(\'active\')">&times;</button>' +
                '</div>' +
                '<div class="modal-body">' +
                    mediaHTML +
                    '<div class="modal-meta">' +
                        '<div><strong>Author:</strong> ' + item.author_name + '</div>' +
                        '<div><strong>Community:</strong> ' + item.community_name + '</div>' +
                        '<div><strong>Score:</strong> ' + item.post_score + '</div>' +
                        '<div><strong>Type:</strong> ' + item.media_type + '</div>' +
                        '<div style="grid-column: 1/-1"><strong>Post:</strong> <a href="' + item.post_url + '" target="_blank" class="modal-link">' + item.post_url + '</a></div>' +
                    '</div>' +
                    '<div class="comments-section" id="comments-section">' +
                        '<div class="loading-comments">Loading comments...</div>' +
                    '</div>' +
                '</div>';

            document.getElementById('modal').classList.add('active');

            // Fetch and display comments
            loadComments(item.id);
        }

        function loadComments(mediaId) {
            fetch('/api/comments/' + mediaId)
                .then(r => r.json())
                .then(data => {
                    displayComments(data.comments || []);
                })
                .catch(err => {
                    document.getElementById('comments-section').innerHTML =
                        '<div class="loading-comments">Failed to load comments</div>';
                });
        }

        function displayComments(comments) {
            const section = document.getElementById('comments-section');

            if (comments.length === 0) {
                section.innerHTML = '<div class="comments-header">No comments yet</div>';
                return;
            }

            // Build comment tree based on path
            const commentTree = buildCommentTree(comments);

            section.innerHTML = '<div class="comments-header">' + comments.length + ' Comment' + (comments.length === 1 ? '' : 's') + '</div>' +
                renderCommentTree(commentTree);
        }

        function buildCommentTree(comments) {
            // Sort by path to ensure proper ordering
            comments.sort((a, b) => a.path.localeCompare(b.path));
            return comments;
        }

        function renderCommentTree(comments) {
            let html = '';
            const pathDepthMap = {};

            for (const comment of comments) {
                const depth = (comment.path.match(/\./g) || []).length;
                const nestClass = depth > 0 ? 'comment-nested' : '';
                const distClass = comment.distinguished ? 'comment-distinguished' : '';
                const scoreClass = comment.score > 0 ? 'positive' : '';

                const timeAgo = formatTimeAgo(comment.published);

                html += '<div class="comment ' + nestClass + ' ' + distClass + '" style="margin-left: ' + (depth * 24) + 'px;">' +
                    '<div class="comment-header">' +
                        '<span class="comment-author">' + escapeHtml(comment.creator_name) + '</span>' +
                        '<span class="comment-score ' + scoreClass + '">↑ ' + comment.score + '</span>' +
                        '<span class="comment-time">' + timeAgo + '</span>' +
                    '</div>' +
                    '<div class="comment-content">' + escapeHtml(comment.content) + '</div>' +
                '</div>';
            }

            return html;
        }

        function formatTimeAgo(dateStr) {
            const date = new Date(dateStr);
            const now = new Date();
            const seconds = Math.floor((now - date) / 1000);

            if (seconds < 60) return seconds + 's ago';
            if (seconds < 3600) return Math.floor(seconds / 60) + 'm ago';
            if (seconds < 86400) return Math.floor(seconds / 3600) + 'h ago';
            if (seconds < 2592000) return Math.floor(seconds / 86400) + 'd ago';
            return Math.floor(seconds / 2592000) + 'mo ago';
        }

        function escapeHtml(text) {
            const div = document.createElement('div');
            div.textContent = text;
            return div.innerHTML;
        }
    </script>
</body>
</html>
{{end}}`

const mediaGridTemplate = `{{define "media-grid"}}
<div class="grid">
    {{range .Media}}
    <div class="card" onclick="openModal({{.id}})">
        <div class="card-image">
            {{if eq .media_type "image"}}
                <img src="{{.serve_url}}" alt="{{.post_title}}" loading="lazy">
            {{else if eq .media_type "video"}}
                <video src="{{.serve_url}}" preload="metadata" muted playsinline loading="lazy"></video>
                <div class="play-overlay">
                    <svg viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg>
                </div>
            {{else}}
                <div class="icon">
                    <svg viewBox="0 0 20 20"><path fill-rule="evenodd" d="M4 4a2 2 0 012-2h4.586A2 2 0 0112 2.586L15.414 6A2 2 0 0116 7.414V16a2 2 0 01-2 2H6a2 2 0 01-2-2V4z" clip-rule="evenodd"/></svg>
                </div>
            {{end}}
        </div>
        <div class="card-info">
            <div class="card-title" title="{{.post_title}}">{{.post_title}}</div>
            <div class="card-meta">
                <span>{{.community_name}}</span>
                <span>{{.post_score}} pts</span>
                <span>{{.media_type}}</span>
            </div>
        </div>
    </div>
    {{end}}
</div>

{{if or .HasPrev .HasNext}}
<div class="pagination">
    <button class="btn"
            {{if .HasPrev}}
            hx-get="/media-grid?offset={{sub .Offset .Limit}}&limit={{.Limit}}&community={{.Community}}&type={{.Type}}&sort={{.Sort}}&order={{.SortOrder}}"
            hx-target="#media-container"
            {{else}}disabled{{end}}>
        ← Previous
    </button>
    <span style="color: #999; font-size: 14px;">Page {{.Page}} of {{.TotalPages}}</span>
    <button class="btn"
            {{if .HasNext}}
            hx-get="/media-grid?offset={{add .Offset .Limit}}&limit={{.Limit}}&community={{.Community}}&type={{.Type}}&sort={{.Sort}}&order={{.SortOrder}}"
            hx-target="#media-container"
            {{else}}disabled{{end}}>
        Next →
    </button>
</div>
{{end}}
{{end}}`

const mediaModalTemplate = ``
