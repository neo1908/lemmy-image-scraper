package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/neo1908/lemmy-image-scraper/pkg/models"
	log "github.com/sirupsen/logrus"
)

// Client represents a Lemmy API client
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	AuthToken  string
}

// NewClient creates a new Lemmy API client
func NewClient(instance string) *Client {
	return &Client{
		BaseURL: fmt.Sprintf("https://%s/api/v3", instance),
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Login authenticates with the Lemmy instance and stores the JWT token
func (c *Client) Login(username, password string) error {
	loginReq := models.LoginRequest{
		UsernameOrEmail: username,
		Password:        password,
	}

	jsonData, err := json.Marshal(loginReq)
	if err != nil {
		return fmt.Errorf("failed to marshal login request: %w", err)
	}

	resp, err := c.HTTPClient.Post(
		fmt.Sprintf("%s/user/login", c.BaseURL),
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return fmt.Errorf("failed to send login request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("login failed with status %d: %s", resp.StatusCode, string(body))
	}

	var loginResp models.LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		return fmt.Errorf("failed to decode login response: %w", err)
	}

	c.AuthToken = loginResp.JWT
	log.Info("Successfully authenticated with Lemmy instance")
	return nil
}

// GetPosts retrieves posts from the Lemmy instance
func (c *Client) GetPosts(params GetPostsParams) (*models.GetPostsResponse, error) {
	queryParams := url.Values{}

	if params.Sort != "" {
		queryParams.Set("sort", params.Sort)
	}
	if params.Page > 0 {
		queryParams.Set("page", fmt.Sprintf("%d", params.Page))
	}
	if params.Limit > 0 {
		queryParams.Set("limit", fmt.Sprintf("%d", params.Limit))
	}
	if params.CommunityID > 0 {
		queryParams.Set("community_id", fmt.Sprintf("%d", params.CommunityID))
	}
	if params.CommunityName != "" {
		queryParams.Set("community_name", params.CommunityName)
	}
	if params.Type != "" {
		queryParams.Set("type_", params.Type)
	}

	reqURL := fmt.Sprintf("%s/post/list?%s", c.BaseURL, queryParams.Encode())

	log.Debugf("Requesting URL: %s", reqURL)

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add Authorization header with Bearer token if authenticated
	if c.AuthToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.AuthToken))
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var postsResp models.GetPostsResponse
	if err := json.NewDecoder(resp.Body).Decode(&postsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	log.Debugf("Retrieved %d posts from API", len(postsResp.Posts))
	return &postsResp, nil
}

// GetCommunityID retrieves the community ID by name
func (c *Client) GetCommunityID(communityName string) (int64, error) {
	queryParams := url.Values{}
	queryParams.Set("name", communityName)

	reqURL := fmt.Sprintf("%s/community?%s", c.BaseURL, queryParams.Encode())

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	// Add Authorization header with Bearer token if authenticated
	if c.AuthToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.AuthToken))
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var communityResp struct {
		CommunityView struct {
			Community models.Community `json:"community"`
		} `json:"community_view"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&communityResp); err != nil {
		return 0, fmt.Errorf("failed to decode response: %w", err)
	}

	return communityResp.CommunityView.Community.ID, nil
}

// GetComments retrieves comments for a post from the Lemmy instance
func (c *Client) GetComments(postID int64, maxDepth, limit int) (*models.GetCommentsResponse, error) {
	queryParams := url.Values{}
	queryParams.Set("post_id", fmt.Sprintf("%d", postID))

	if maxDepth > 0 {
		queryParams.Set("max_depth", fmt.Sprintf("%d", maxDepth))
	}
	if limit > 0 {
		queryParams.Set("limit", fmt.Sprintf("%d", limit))
	}
	queryParams.Set("sort", "Top") // Get best comments first

	reqURL := fmt.Sprintf("%s/comment/list?%s", c.BaseURL, queryParams.Encode())

	log.Debugf("Requesting comments URL: %s", reqURL)

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add Authorization header with Bearer token if authenticated
	if c.AuthToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.AuthToken))
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var commentsResp models.GetCommentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&commentsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	log.Debugf("Retrieved %d comments from API", len(commentsResp.Comments))
	return &commentsResp, nil
}

// GetPostsParams represents parameters for getting posts
type GetPostsParams struct {
	Sort          string // Hot, New, TopDay, etc.
	Page          int
	Limit         int
	CommunityID   int64
	CommunityName string
	Type          string // Local, All, Subscribed
}
