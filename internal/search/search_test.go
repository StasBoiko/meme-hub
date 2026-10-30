// Package search provides full-text search capabilities for meme messages via ZincSearch.
package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

	if err := s.DeleteIndex(ctx); err != nil {
		t.Fatalf("unexpected DeleteIndex error: %v", err)
	}
}

// TestClient_IndexMessage tests document indexing in ZincSearch.
func TestClient_IndexMessage(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		doc        Document
		wantErr    bool
	}{
		{
			name:       "successful document indexing with id",
			statusCode: http.StatusOK,
			doc: Document{
				ID:        42,
				UserID:    1,
				Author:    "Alice",
				Avatar:    "https://example.com/avatar.png",
				Text:      "Lovely meme!",
				CreatedAt: time.Now().UTC(),
			},
			wantErr: false,
		},
		{
			name:       "successful document indexing with 201 created",
			statusCode: http.StatusCreated,
			doc: Document{
				ID:   43,
				Text: "Created meme",
			},
			wantErr: false,
		},
		{
			name:       "zincsearch returns 500 error",
			statusCode: http.StatusInternalServerError,
			doc: Document{
				ID:   44,
				Text: "Failed",
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("unexpected HTTP method: %s", r.Method)
				}
				if !strings.HasPrefix(r.URL.Path, "/api/test_messages/_doc") {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}

				user, pass, ok := r.BasicAuth()
				if !ok || user != "admin" || pass != "secret" {
					t.Errorf("unexpected basic auth: user=%q, pass=%q, ok=%v", user, pass, ok)
				}

				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(`{"id":"42","message":"ok"}`))
			}))
			defer server.Close()

			client, err := NewClientWithAuth(server.URL, "admin", "secret", "test_messages", server.Client())
			if err != nil {
				t.Fatalf("NewClientWithAuth: %v", err)
			}

			err = client.IndexMessage(context.Background(), tc.doc)
			if (err != nil) != tc.wantErr {
				t.Fatalf("IndexMessage() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// TestClient_SearchMessages tests searching documents with mock ZincSearch responses.
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
			query: "meme",
			limit: 5,
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/api/test_index/_search" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}

				response := map[string]any{
					"hits": map[string]any{
						"hits": []map[string]any{
							{
								"_source": Document{
									ID:        1,
									Author:    "Bob",
									Text:      "Welcome to Meme Hub!",
									CreatedAt: now,
								},
							},
						},
					},
				}
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

			client, err := NewClientWithAuth(server.URL, "admin", "password", "test_index", server.Client())
			if err != nil {
				t.Fatalf("NewClientWithAuth: %v", err)
			}

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

// TestClient_Ping tests the Ping method on ZincSearch client.
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
				if r.URL.Path != "/version" {
					t.Errorf("unexpected ping path: %s", r.URL.Path)
				}
				w.WriteHeader(tc.statusCode)
			}))
			defer server.Close()

			client, err := NewClientWithAuth(server.URL, "admin", "password", DefaultIndex, server.Client())
			if err != nil {
				t.Fatalf("NewClientWithAuth: %v", err)
			}

			err = client.Ping(context.Background())
			if (err != nil) != tc.wantErr {
				t.Fatalf("Ping() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// TestClient_DeleteIndex tests deleting an index via ZincSearch API.
func TestClient_DeleteIndex(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{
			name:       "successful index deletion",
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "index not found 404 is ignored without error",
			statusCode: http.StatusNotFound,
			wantErr:    false,
		},
		{
			name:       "server error returns error",
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete || r.URL.Path != "/api/index/test_delete" {
					t.Errorf("unexpected delete request: %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(tc.statusCode)
			}))
			defer server.Close()

			client, err := NewClientWithAuth(server.URL, "admin", "password", "test_delete", server.Client())
			if err != nil {
				t.Fatalf("NewClientWithAuth: %v", err)
			}

			err = client.DeleteIndex(context.Background())
			if (err != nil) != tc.wantErr {
				t.Fatalf("DeleteIndex() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// TestNewClient tests the constructor with various URL forms and embedded credentials.
func TestNewClient(t *testing.T) {
	client, err := NewClient("http://localhost:4080")
	if err != nil {
		t.Fatalf("unexpected NewClient error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.indexName != DefaultIndex {
		t.Fatalf("expected indexName = %s, got %s", DefaultIndex, client.indexName)
	}
	if client.username != "admin" || client.password != "password" {
		t.Fatalf("expected admin/password, got %s/%s", client.username, client.password)
	}

	authClient, err := NewClient("http://customuser:custompass@localhost:4080/")
	if err != nil {
		t.Fatalf("unexpected NewClient with auth error: %v", err)
	}
	if authClient.username != "customuser" || authClient.password != "custompass" {
		t.Fatalf("expected customuser/custompass, got %s/%s", authClient.username, authClient.password)
	}
	if authClient.baseURL != "http://localhost:4080" {
		t.Fatalf("expected baseURL without user info, got %s", authClient.baseURL)
	}

	_, err = NewClient("")
	if err == nil {
		t.Fatal("expected error on empty URL")
	}
}
