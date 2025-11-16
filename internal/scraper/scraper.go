package scraper

import (
	"strings"

	"github.com/neo1908/lemmy-image-scraper/internal/api"
	"github.com/neo1908/lemmy-image-scraper/internal/config"
	"github.com/neo1908/lemmy-image-scraper/internal/database"
	"github.com/neo1908/lemmy-image-scraper/internal/downloader"
	"github.com/neo1908/lemmy-image-scraper/pkg/models"
	log "github.com/sirupsen/logrus"
)

// Scraper handles the scraping logic
type Scraper struct {
	Config     *config.Config
	API        *api.Client
	DB         *database.DB
	Downloader *downloader.Downloader
}

// New creates a new Scraper instance
func New(cfg *config.Config, apiClient *api.Client, db *database.DB, dl *downloader.Downloader) *Scraper {
	return &Scraper{
		Config:     cfg,
		API:        apiClient,
		DB:         db,
		Downloader: dl,
	}
}

// Run executes the scraping process
func (s *Scraper) Run() error {
	log.Info("Starting scrape run")

	if len(s.Config.Lemmy.Communities) == 0 {
		// Scrape from hot page
		log.Info("No communities specified, scraping from hot page")
		return s.scrapeHotPage()
	}

	// Scrape specific communities
	for _, community := range s.Config.Lemmy.Communities {
		log.Infof("Scraping community: %s", community)
		if err := s.scrapeCommunity(community); err != nil {
			log.Errorf("Failed to scrape community %s: %v", community, err)
			continue
		}
	}

	return nil
}

// scrapeHotPage scrapes posts from the instance's hot page
func (s *Scraper) scrapeHotPage() error {
	return s.scrapeWithPagination("hot", api.GetPostsParams{
		Sort: s.Config.Scraper.SortType,
	})
}

// scrapeCommunity scrapes posts from a specific community
func (s *Scraper) scrapeCommunity(communityName string) error {
	return s.scrapeWithPagination(communityName, api.GetPostsParams{
		Sort:          s.Config.Scraper.SortType,
		CommunityName: communityName,
	})
}

// scrapeWithPagination handles paginated scraping to get more than 50 posts
func (s *Scraper) scrapeWithPagination(source string, baseParams api.GetPostsParams) error {
	totalDownloaded := 0
	totalSkipped := 0
	totalErrors := 0
	totalProcessed := 0
	consecutiveSeenPosts := 0
	page := 1

	for {
		// Calculate how many more posts we can fetch
		remainingPosts := s.Config.Scraper.MaxPostsPerRun - totalProcessed
		if remainingPosts <= 0 {
			log.Infof("Reached maximum posts limit (%d)", s.Config.Scraper.MaxPostsPerRun)
			break
		}

		// Set page and limit for this request
		params := baseParams
		params.Page = page
		params.Limit = min(50, remainingPosts) // API max is 50 per request

		log.Debugf("Fetching page %d with limit %d", page, params.Limit)

		downloaded, skipped, errors, postsReturned, seenInRow, shouldStop := s.scrapePosts(params, source, consecutiveSeenPosts)

		totalDownloaded += downloaded
		totalSkipped += skipped
		totalErrors += errors
		totalProcessed += postsReturned

		consecutiveSeenPosts = seenInRow

		// Check if we should stop
		if shouldStop {
			log.Infof("Stopping pagination due to idempotency rules")
			break
		}

		// If we got fewer posts than requested, we've reached the end
		if postsReturned < params.Limit {
			log.Debugf("Received fewer posts than requested (%d < %d), reached end of available posts", postsReturned, params.Limit)
			break
		}

		// Only continue to next page if pagination is enabled
		if !s.Config.Scraper.EnablePagination {
			log.Debug("Pagination disabled, stopping after first page")
			break
		}

		page++
	}

	log.Infof("Scrape complete for %s: %d downloaded, %d skipped, %d errors (total %d posts processed)",
		source, totalDownloaded, totalSkipped, totalErrors, totalProcessed)
	return nil
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// scrapePosts fetches and processes posts based on the given parameters
// Returns: downloaded, skipped, errors, postsReturned, consecutiveSeenPosts, shouldStop
func (s *Scraper) scrapePosts(params api.GetPostsParams, source string, currentConsecutiveSeen int) (int, int, int, int, int, bool) {
	postsResp, err := s.API.GetPosts(params)
	if err != nil {
		log.Errorf("Failed to get posts: %v", err)
		return 0, 0, 1, 0, currentConsecutiveSeen, true
	}

	postsReturned := len(postsResp.Posts)
	log.Debugf("Retrieved %d posts from %s (page %d)", postsReturned, source, params.Page)

	downloaded := 0
	skipped := 0
	errors := 0
	consecutiveSeenPosts := currentConsecutiveSeen

	for _, postView := range postsResp.Posts {
		// Check if we've already scraped this post
		exists, err := s.DB.PostExists(postView.Post.ID)
		if err != nil {
			log.Errorf("Failed to check if post exists: %v", err)
			continue
		}

		if exists {
			consecutiveSeenPosts++

			// Check if we should stop based on threshold
			if s.Config.Scraper.StopAtSeenPosts {
				if consecutiveSeenPosts >= s.Config.Scraper.SeenPostsThreshold {
					log.Infof("Encountered %d previously seen posts in a row (threshold: %d), stopping",
						consecutiveSeenPosts, s.Config.Scraper.SeenPostsThreshold)
					return downloaded, skipped, errors, postsReturned, consecutiveSeenPosts, true
				}
			}

			// Skip this post if configured to do so
			if s.Config.Scraper.SkipSeenPosts || s.Config.Scraper.StopAtSeenPosts {
				log.Debugf("Skipping previously seen post (ID: %d)", postView.Post.ID)
				skipped++
				continue
			}
		} else {
			// Reset counter when we find a new post
			consecutiveSeenPosts = 0
		}

		// Extract media URLs from the post
		mediaURLs := s.extractMediaURLs(postView)
		mediaDownloaded := 0

		if len(mediaURLs) == 0 {
			log.Debugf("No media found in post: %s (ID: %d)", postView.Post.Name, postView.Post.ID)
		} else {
			// Download each media URL
			for _, mediaURL := range mediaURLs {
				// Check if we should download this type of media
				if !downloader.ShouldDownload(
					mediaURL,
					s.Config.Scraper.IncludeImages,
					s.Config.Scraper.IncludeVideos,
					s.Config.Scraper.IncludeOtherMedia,
				) {
					log.Debugf("Skipping media (type not enabled): %s", mediaURL)
					skipped++
					continue
				}

				_, err := s.Downloader.DownloadMedia(mediaURL, postView)
				if err != nil {
					if strings.Contains(err.Error(), "already exists") {
						log.Debugf("Media already exists: %s", mediaURL)
						skipped++
					} else {
						log.Errorf("Failed to download media from %s: %v", mediaURL, err)
						errors++
					}
					continue
				}

				downloaded++
				mediaDownloaded++
			}
		}

		// Mark this post as scraped (even if it had no media)
		if err := s.DB.MarkPostAsScraped(&postView, mediaDownloaded); err != nil {
			log.Errorf("Failed to mark post %d as scraped: %v", postView.Post.ID, err)
		}

		// Fetch and store comments if the post had media
		if mediaDownloaded > 0 {
			s.scrapeComments(postView.Post.ID)
		}
	}

	return downloaded, skipped, errors, postsReturned, consecutiveSeenPosts, false
}

// scrapeComments fetches and stores comments for a post
func (s *Scraper) scrapeComments(postID int64) {
	// Check if we already have comments for this post
	exists, err := s.DB.CommentsExistForPost(postID)
	if err != nil {
		log.Errorf("Failed to check if comments exist for post %d: %v", postID, err)
		return
	}
	if exists {
		log.Debugf("Comments already exist for post %d, skipping", postID)
		return
	}

	// Fetch comments from API (max_depth=10, limit=500 to get most comments)
	commentsResp, err := s.API.GetComments(postID, 10, 500)
	if err != nil {
		log.Errorf("Failed to fetch comments for post %d: %v", postID, err)
		return
	}

	if len(commentsResp.Comments) == 0 {
		log.Debugf("No comments found for post %d", postID)
		return
	}

	// Save each comment to the database
	savedCount := 0
	for _, commentView := range commentsResp.Comments {
		// Skip removed or deleted comments
		if commentView.Comment.Removed || commentView.Comment.Deleted {
			continue
		}

		if err := s.DB.SaveComment(&commentView); err != nil {
			log.Errorf("Failed to save comment %d: %v", commentView.Comment.ID, err)
			continue
		}
		savedCount++
	}

	log.Debugf("Saved %d/%d comments for post %d", savedCount, len(commentsResp.Comments), postID)
}

// extractMediaURLs extracts all media URLs from a post
// Only returns the highest quality version available
func (s *Scraper) extractMediaURLs(postView models.PostView) []string {
	var urls []string

	// Priority 1: Main post URL (highest quality, direct link to media)
	if postView.Post.URL != "" && isMediaURL(postView.Post.URL) {
		urls = append(urls, postView.Post.URL)
		// If we have a main URL, skip the thumbnail as it's lower quality

		// However, still check for embedded video as it might be different content
		if postView.Post.EmbedVideoURL != "" && isMediaURL(postView.Post.EmbedVideoURL) {
			urls = append(urls, postView.Post.EmbedVideoURL)
		}

		return urls
	}

	// Priority 2: Embedded video URL (if no main URL)
	if postView.Post.EmbedVideoURL != "" && isMediaURL(postView.Post.EmbedVideoURL) {
		urls = append(urls, postView.Post.EmbedVideoURL)
		return urls
	}

	// Priority 3: Thumbnail URL (fallback, only if no other media found)
	if postView.Post.ThumbnailURL != "" && isMediaURL(postView.Post.ThumbnailURL) {
		urls = append(urls, postView.Post.ThumbnailURL)
	}

	return urls
}

// isMediaURL checks if a URL points to a media file
func isMediaURL(url string) bool {
	url = strings.ToLower(url)

	// Image extensions
	imageExts := []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".svg"}
	for _, ext := range imageExts {
		if strings.Contains(url, ext) {
			return true
		}
	}

	// Video extensions
	videoExts := []string{".mp4", ".webm", ".mov", ".avi", ".mkv", ".m4v", ".flv"}
	for _, ext := range videoExts {
		if strings.Contains(url, ext) {
			return true
		}
	}

	// Check if it's from common image/video hosting services
	mediaHosts := []string{
		"i.imgur.com",
		"i.redd.it",
		"v.redd.it",
		"preview.redd.it",
		"external-preview.redd.it",
		"lemmy.world/pictrs",
		"lemmy.ml/pictrs",
		"pictrs",
	}

	for _, host := range mediaHosts {
		if strings.Contains(url, host) {
			return true
		}
	}

	return false
}
