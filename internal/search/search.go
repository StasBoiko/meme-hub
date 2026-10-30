// Package search provides full-text search capabilities for meme messages via ZincSearch.
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultIndex defines the default ZincSearch index name for meme entries.
const DefaultIndex = "meme_messages"

// DefaultTimeout defines the default HTTP request timeout for ZincSearch requests.
const DefaultTimeout = 5 * time.Second

// Document represents a searchable meme entry in ZincSearch.
type Document struct {
	ID        int       `json:"id"`
	UserID    int       `json:"user_id"`
	Author    string    `json:"author"`
	Avatar    string    `json:"avatar"`
	Text      string    `json:"text"`
	MediaURL  string    `json:"media_url,omitempty"`
	Upvotes   int       `json:"upvotes"`
	Downvotes int       `json:"downvotes"`
	Score     int       `json:"score"`
	CreatedAt time.Time `json:"created_at"`
}

// Searcher defines full-text search operations for meme messages.
type Searcher interface {
	// IndexMessage indexes or updates a message document in ZincSearch.
	IndexMessage(ctx context.Context, doc Document) error
	// SearchMessages searches for messages matching the given query string.
	SearchMessages(ctx context.Context, query string, limit int) ([]Document, error)
	// Ping checks connectivity to the ZincSearch service.
	Ping(ctx context.Context) error
	// DeleteIndex deletes the configured ZincSearch index if it exists.
	DeleteIndex(ctx context.Context) error
}

// Service is an alias for Searcher to maintain compatibility across consumers.
type Service = Searcher

// Client implements Searcher using the ZincSearch HTTP REST API.
type Client struct {
	baseURL    string
	username   string
	password   string
	indexName  string
	httpClient *http.Client
}

// NewClient creates a new ZincSearch client connected to the given URL.
func NewClient(rawURL string) (*Client, error) {
	return NewClientWithAuth(rawURL, "admin", "password", DefaultIndex, nil)
}

// NewClientWithAuth creates a ZincSearch Client with explicit authentication, index name, and HTTP client.
func NewClientWithAuth(rawURL, username, password, indexName string, httpClient *http.Client) (*Client, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if trimmed == "" {
		return nil, fmt.Errorf("search URL cannot be empty")
	}

	parsedURL, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("parsing search URL: %w", err)
	}

	// Extract credentials from URL if present.
	if parsedURL.User != nil {
		if u := parsedURL.User.Username(); u != "" {
			username = u
		}
		if p, ok := parsedURL.User.Password(); ok {
			password = p
		}
		parsedURL.User = nil
		trimmed = parsedURL.String()
	}

	if indexName == "" {
		indexName = DefaultIndex
	}

	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: DefaultTimeout,
		}
	}

	return &Client{
		baseURL:    trimmed,
		username:   username,
		password:   password,
		indexName:  indexName,
		httpClient: httpClient,
	}, nil
}

// IndexMessage stores or updates a meme document in ZincSearch.
func (c *Client) IndexMessage(ctx context.Context, doc Document) error {
	data, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshaling search document: %w", err)
	}

	endpoint := fmt.Sprintf("%s/api/%s/_doc", c.baseURL, c.indexName)
	if doc.ID > 0 {
		endpoint = fmt.Sprintf("%s/api/%s/_doc/%d", c.baseURL, c.indexName, doc.ID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("creating indexing request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.username != "" || c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("indexing document in zincsearch: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("zincsearch indexing error: status code %d", resp.StatusCode)
	}

	return nil
}

// SearchMessages searches for messages matching the query string in ZincSearch.
func (c *Client) SearchMessages(ctx context.Context, query string, limit int) ([]Document, error) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return []Document{}, nil
	}
	if limit <= 0 {
		limit = 100
	}

	searchBody := map[string]any{
		"search_type": "querystring",
		"query": map[string]any{
			"term": trimmed,
		},
		"sort_fields": []string{"-created_at"},
		"from":        0,
		"max_results": limit,
	}

	data, err := json.Marshal(searchBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling search query: %w", err)
	}

	endpoint := fmt.Sprintf("%s/api/%s/_search", c.baseURL, c.indexName)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("creating search request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.username != "" || c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing zincsearch search request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusNotFound {
		return []Document{}, nil
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("zincsearch search error: status code %d", resp.StatusCode)
	}

	var searchResponse struct {
		Hits struct {
			Hits []struct {
				Source Document `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&searchResponse); err != nil {
		return nil, fmt.Errorf("decoding zincsearch response: %w", err)
	}

	docs := make([]Document, len(searchResponse.Hits.Hits))
	for i, hit := range searchResponse.Hits.Hits {
		docs[i] = hit.Source
	}

	return docs, nil
}

// Ping verifies connectivity to the ZincSearch service.
func (c *Client) Ping(ctx context.Context) error {
	endpoint := fmt.Sprintf("%s/version", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("creating ping request: %w", err)
	}

	if c.username != "" || c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("pinging zincsearch: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("zincsearch ping returned status code %d", resp.StatusCode)
	}

	return nil
}

// DeleteIndex deletes the configured ZincSearch index if it exists.
func (c *Client) DeleteIndex(ctx context.Context) error {
	endpoint := fmt.Sprintf("%s/api/index/%s", c.baseURL, c.indexName)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return fmt.Errorf("creating delete index request: %w", err)
	}

	if c.username != "" || c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("deleting zincsearch index: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("zincsearch delete index error: status code %d", resp.StatusCode)
	}

	return nil
}

// NopService provides a no-op implementation of Searcher for test suites and offline operation.
type NopService struct{}

// NewNopService creates a new NopService instance.
func NewNopService() *NopService {
	return &NopService{}
}

// IndexMessage performs a no-op document indexing.
func (s *NopService) IndexMessage(ctx context.Context, doc Document) error {
	return nil
}

// SearchMessages performs a no-op search returning empty results.
func (s *NopService) SearchMessages(ctx context.Context, query string, limit int) ([]Document, error) {
	return []Document{}, nil
}

// Ping performs a no-op connectivity check.
func (s *NopService) Ping(ctx context.Context) error {
	return nil
}

// DeleteIndex performs a no-op index deletion.
func (s *NopService) DeleteIndex(ctx context.Context) error {
	return nil
}
