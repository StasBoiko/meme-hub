// Package search provides full-text search capabilities for guestbook messages via Elasticsearch.
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
)

// DefaultIndex defines the default Elasticsearch index name for guestbook entries.
const DefaultIndex = "guestbook_messages"

// Document represents a searchable guestbook entry in Elasticsearch.
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

// Service defines full-text search operations for guestbook messages.
type Service interface {
	// IndexMessage indexes or updates a message document in Elasticsearch.
	IndexMessage(ctx context.Context, doc Document) error
	// SearchMessages searches for messages matching the given query string.
	SearchMessages(ctx context.Context, query string, limit int) ([]Document, error)
	// Ping checks connectivity to the Elasticsearch service.
	Ping(ctx context.Context) error
}

// Client implements Service using the official Elasticsearch v8 client.
type Client struct {
	es        *elasticsearch.Client
	indexName string
}

// NewClient creates a new Elasticsearch search client connected to the given URL.
func NewClient(rawURL string) (*Client, error) {
	cfg := elasticsearch.Config{
		Addresses: []string{rawURL},
	}
	es, err := elasticsearch.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating elasticsearch client: %w", err)
	}

	return &Client{
		es:        es,
		indexName: DefaultIndex,
	}, nil
}

// NewClientWithElastic creates a Client using an existing elasticsearch.Client instance and index name.
func NewClientWithElastic(es *elasticsearch.Client, indexName string) *Client {
	if indexName == "" {
		indexName = DefaultIndex
	}
	return &Client{
		es:        es,
		indexName: indexName,
	}
}

// IndexMessage stores or updates a guestbook document in Elasticsearch.
func (c *Client) IndexMessage(ctx context.Context, doc Document) error {
	data, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshaling search document: %w", err)
	}

	req := esapi.IndexRequest{
		Index:      c.indexName,
		DocumentID: strconv.Itoa(doc.ID),
		Body:       bytes.NewReader(data),
		Refresh:    "true",
	}

	res, err := req.Do(ctx, c.es)
	if err != nil {
		return fmt.Errorf("indexing document in elasticsearch: %w", err)
	}
	defer func() {
		_ = res.Body.Close()
	}()

	if res.IsError() {
		return fmt.Errorf("elasticsearch indexing error: status code %d", res.StatusCode)
	}

	return nil
}

// SearchMessages searches for messages matching the query string across text and author fields.
func (c *Client) SearchMessages(ctx context.Context, query string, limit int) ([]Document, error) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return []Document{}, nil
	}
	if limit <= 0 {
		limit = 100
	}

	queryBody := map[string]any{
		"size": limit,
		"query": map[string]any{
			"multi_match": map[string]any{
				"query":     trimmed,
				"fields":    []string{"text^2", "author"},
				"fuzziness": "AUTO",
			},
		},
		"sort": []any{
			map[string]any{"created_at": map[string]any{"order": "desc"}},
		},
	}

	data, err := json.Marshal(queryBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling search query: %w", err)
	}

	req := esapi.SearchRequest{
		Index: []string{c.indexName},
		Body:  bytes.NewReader(data),
	}

	res, err := req.Do(ctx, c.es)
	if err != nil {
		return nil, fmt.Errorf("executing elasticsearch search request: %w", err)
	}
	defer func() {
		_ = res.Body.Close()
	}()

	if res.IsError() {
		if res.StatusCode == http.StatusNotFound {
			return []Document{}, nil
		}
		return nil, fmt.Errorf("elasticsearch search error: status code %d", res.StatusCode)
	}

	var searchResponse struct {
		Hits struct {
			Hits []struct {
				Source Document `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}

	if err := json.NewDecoder(res.Body).Decode(&searchResponse); err != nil {
		return nil, fmt.Errorf("decoding elasticsearch response: %w", err)
	}

	docs := make([]Document, len(searchResponse.Hits.Hits))
	for i, hit := range searchResponse.Hits.Hits {
		docs[i] = hit.Source
	}

	return docs, nil
}

// Ping verifies connectivity to the Elasticsearch service.
func (c *Client) Ping(ctx context.Context) error {
	res, err := c.es.Ping(c.es.Ping.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("pinging elasticsearch: %w", err)
	}
	defer func() {
		_ = res.Body.Close()
	}()

	if res.IsError() {
		return fmt.Errorf("elasticsearch ping returned status code %d", res.StatusCode)
	}

	return nil
}

// NopService provides a no-op implementation of Service for test suites and offline operation.
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
