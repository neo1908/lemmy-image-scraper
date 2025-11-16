package models

import "time"

// ScrapedMedia represents a media file that has been scraped and stored
type ScrapedMedia struct {
	ID            int64     `db:"id"`
	PostID        int64     `db:"post_id"`
	PostTitle     string    `db:"post_title"`
	CommunityName string    `db:"community_name"`
	CommunityID   int64     `db:"community_id"`
	AuthorName    string    `db:"author_name"`
	AuthorID      int64     `db:"author_id"`
	MediaURL      string    `db:"media_url"`
	MediaHash     string    `db:"media_hash"`
	FileName      string    `db:"file_name"`
	FilePath      string    `db:"file_path"`
	FileSize      int64     `db:"file_size"`
	MediaType     string    `db:"media_type"`  // "image", "video", "other"
	PostURL       string    `db:"post_url"`
	PostScore     int       `db:"post_score"`
	PostCreated   time.Time `db:"post_created"`
	DownloadedAt  time.Time `db:"downloaded_at"`
}

// Post represents a Lemmy post from the API
type Post struct {
	ID                 int64     `json:"id"`
	Name               string    `json:"name"`
	URL                string    `json:"url,omitempty"`
	Body               string    `json:"body,omitempty"`
	CommunityID        int64     `json:"community_id"`
	CreatorID          int64     `json:"creator_id"`
	Removed            bool      `json:"removed"`
	Locked             bool      `json:"locked"`
	Published          time.Time `json:"published"`
	Updated            time.Time `json:"updated,omitempty"`
	Deleted            bool      `json:"deleted"`
	NSFW               bool      `json:"nsfw"`
	EmbedTitle         string    `json:"embed_title,omitempty"`
	EmbedDescription   string    `json:"embed_description,omitempty"`
	ThumbnailURL       string    `json:"thumbnail_url,omitempty"`
	EmbedVideoURL      string    `json:"embed_video_url,omitempty"`
	LanguageID         int       `json:"language_id"`
	FeaturedCommunity  bool      `json:"featured_community"`
	FeaturedLocal      bool      `json:"featured_local"`
}

// Community represents a Lemmy community
type Community struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Removed     bool   `json:"removed"`
	Published   string `json:"published"`
	Updated     string `json:"updated,omitempty"`
	Deleted     bool   `json:"deleted"`
	NSFW        bool   `json:"nsfw"`
	ActorID     string `json:"actor_id"`
	Local       bool   `json:"local"`
	Icon        string `json:"icon,omitempty"`
	Banner      string `json:"banner,omitempty"`
}

// Person represents a Lemmy user
type Person struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	Avatar    string `json:"avatar,omitempty"`
	Banned    bool   `json:"banned"`
	Published string `json:"published"`
	Updated   string `json:"updated,omitempty"`
	ActorID   string `json:"actor_id"`
	Local     bool   `json:"local"`
	Deleted   bool   `json:"deleted"`
	Admin     bool   `json:"admin"`
	BotAccount bool  `json:"bot_account"`
}

// PostAggregates represents post statistics
type PostAggregates struct {
	ID                 int64     `json:"id"`
	PostID             int64     `json:"post_id"`
	Comments           int       `json:"comments"`
	Score              int       `json:"score"`
	Upvotes            int       `json:"upvotes"`
	Downvotes          int       `json:"downvotes"`
	Published          time.Time `json:"published"`
	NewestCommentTime  time.Time `json:"newest_comment_time"`
}

// PostView represents a post with all associated data from the API
type PostView struct {
	Post                        Post           `json:"post"`
	Creator                     Person         `json:"creator"`
	Community                   Community      `json:"community"`
	CreatorBannedFromCommunity  bool           `json:"creator_banned_from_community"`
	Counts                      PostAggregates `json:"counts"`
	Subscribed                  string         `json:"subscribed"`
	Saved                       bool           `json:"saved"`
	Read                        bool           `json:"read"`
	CreatorBlocked              bool           `json:"creator_blocked"`
	MyVote                      int            `json:"my_vote,omitempty"`
}

// GetPostsResponse represents the API response for getting posts
type GetPostsResponse struct {
	Posts []PostView `json:"posts"`
}

// LoginRequest represents the login API request
type LoginRequest struct {
	UsernameOrEmail string `json:"username_or_email"`
	Password        string `json:"password"`
}

// LoginResponse represents the login API response
type LoginResponse struct {
	JWT                string `json:"jwt"`
	RegistrationCreated bool  `json:"registration_created"`
	VerifyEmailSent    bool   `json:"verify_email_sent"`
}
