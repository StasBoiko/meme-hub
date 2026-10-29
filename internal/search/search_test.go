// Package search provides full-text search capabilities for guestbook messages via Elasticsearch.
package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
)

// TestNopService tests that NopService methods succeed without errors.
func TestNopService(t *testing.T) {
	s := NewNopService()
	ctx := context.Background()

	if err := s.IndexMessage(ctx, Document{ID: 1, Text: "hello"}); err != nil {
		t.Fatalf("unexpected IndexMessage error: %v", err)
	}

	docs, err := s.SearchMessages(ctx, "hello", 10)
	if err != nil {
		t.Fatalf("unexpected SearchMessages error: %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("expected 0 docs, got %d", len(docs))
	}

	if err := s.Ping(ctx); err != nil {
		t.Fatalf("unexpected Ping error: %v", err)
	}
}

// TestClient_IndexMessage tests document indexing in Elasticsearch.
func TestClient_IndexMessage(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		doc        Document
		wantErr    bool
	}{
		{
			name:       "successful document indexing",
			statusCode: http.StatusCreated,
			doc: Document{
				ID:        42,
				UserID:    1,
				Author:    "Alice",
				Avatar:    "https://example.com/avatar.png",
				Text:      "Lovely guestbook!",
				CreatedAt: time.Now().UTC(),
			},
			wantErr: false,
		},
		{
			name:       "elasticsearch returns 500 error",
			statusCode: http.StatusInternalServerError,
			doc: Document{
				ID:   42,
				Text: "Failed",
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut && r.Method != http.MethodPost {
					t.Errorf("unexpected HTTP method: %s", r.Method)
				}
				w.Header().Set("X-Elastic-Product", "Elasticsearch")
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(`{"result":"created"}`))
			}))
			defer server.Close()

			es, err := elasticsearch.NewClient(elasticsearch.Config{
				Addresses: []string{server.URL},
			})
			if err != nil {
				t.Fatalf("creating elasticsearch client: %v", err)
			}

			client := NewClientWithElastic(es, "test_messages")
			err = client.IndexMessage(context.Background(), tc.doc)
			if (err != nil) != tc.wantErr {
				t.Fatalf("IndexMessage() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// TestClient_SearchMessages tests searching documents with mock Elasticsearch responses.
func TestClient_SearchMessages(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name          string
		query         string
		limit         int
		serverHandler http.HandlerFunc
		wantCount     int
		wantErr       bool
	}{
		{
			name:  "empty query returns empty slice immediately",
			query: "   ",
			limit: 10,
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				t.Error("server should not be called on empty query")
			},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:  "successful search with matching hits",
			query: "guestbook",
			limit: 5,
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				response := map[string]any{
					"hits": map[string]any{
						"hits": []map[string]any{
							{
								"_source": Document{
									ID:        1,
									Author:    "Bob",
									Text:      "Welcome to the guestbook!",
									CreatedAt: now,
								},
							},
						},
					},
				}
				w.Header().Set("X-Elastic-Product", "Elasticsearch")
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(response)
			},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:  "index not found 404 returns empty slice without error",
			query: "something",
			limit: 10,
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Elastic-Product", "Elasticsearch")
				w.WriteHeader(http.StatusNotFound)
			},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:  "server error 500 returns error",
			query: "error",
			limit: 10,
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Elastic-Product", "Elasticsearch")
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantCount: 0,
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(tc.serverHandler)
			defer server.Close()

			es, err := elasticsearch.NewClient(elasticsearch.Config{
				Addresses: []string{server.URL},
			})
			if err != nil {
				t.Fatalf("creating elasticsearch client: %v", err)
			}

			client := NewClientWithElastic(es, "")
			results, err := client.SearchMessages(context.Background(), tc.query, tc.limit)
			if (err != nil) != tc.wantErr {
				t.Fatalf("SearchMessages() error = %v, wantErr %v", err, tc.wantErr)
			}
			if len(results) != tc.wantCount {
				t.Fatalf("SearchMessages() returned %d results, want %d", len(results), tc.wantCount)
			}
		})
	}
}

// TestClient_Ping tests the Ping method on Elasticsearch client.
func TestClient_Ping(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{
			name:       "successful ping",
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "failed ping with 503 service unavailable",
			statusCode: http.StatusServiceUnavailable,
			wantErr:    true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Elastic-Product", "Elasticsearch")
				w.WriteHeader(tc.statusCode)
			}))
			defer server.Close()

			es, err := elasticsearch.NewClient(elasticsearch.Config{
				Addresses: []string{server.URL},
			})
			if err != nil {
				t.Fatalf("creating client: %v", err)
			}

			client := NewClientWithElastic(es, DefaultIndex)
			err = client.Ping(context.Background())
			if (err != nil) != tc.wantErr {
				t.Fatalf("Ping() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// TestNewClient tests the constructor with a valid URL string.
func TestNewClient(t *testing.T) {
	client, err := NewClient("http://localhost:9200")
	if err != nil {
		t.Fatalf("unexpected NewClient error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.indexName != DefaultIndex {
		t.Fatalf("expected indexName = %s, got %s", DefaultIndex, client.indexName)
	}
}
