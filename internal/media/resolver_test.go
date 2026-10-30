package media

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func TestLookupStandardOEmbedEndpoint(t *testing.T) {
	tests := []struct {
		name         string
		url          string
		wantEndpoint string
	}{
		{
			name:         "YouTube watch",
			url:          "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			wantEndpoint: "https://www.youtube.com/oembed",
		},
		{
			name:         "YouTube youtu.be",
			url:          "https://youtu.be/dQw4w9WgXcQ",
			wantEndpoint: "https://www.youtube.com/oembed",
		},
		{
			name:         "Vimeo",
			url:          "https://vimeo.com/76979871",
			wantEndpoint: "https://vimeo.com/api/oembed.json",
		},
		{
			name:         "Dailymotion",
			url:          "https://www.dailymotion.com/video/x7tgad0",
			wantEndpoint: "https://www.dailymotion.com/services/oembed",
		},
		{
			name:         "Coub",
			url:          "https://coub.com/view/1hyxa1",
			wantEndpoint: "https://coub.com/api/oembed.json",
		},
		{
			name:         "Loom",
			url:          "https://www.loom.com/share/221f217704be4b668928c1a4cacb2f0e",
			wantEndpoint: "https://www.loom.com/v1/oembed",
		},
		{
			name:         "Wistia net",
			url:          "https://fast.wistia.net/embed/iframe/gmwg9g412y",
			wantEndpoint: "https://fast.wistia.com/oembed.json",
		},
		{
			name:         "Unknown site",
			url:          "https://example.com/meme",
			wantEndpoint: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := url.Parse(tc.url)
			if err != nil {
				t.Fatalf("failed to parse url: %v", err)
			}
			got := lookupStandardOEmbedEndpoint(tc.url, parsed)
			if tc.wantEndpoint == "" {
				if got != "" {
					t.Errorf("got %q, want empty", got)
				}
			} else {
				if !strings.HasPrefix(got, tc.wantEndpoint) {
					t.Errorf("got %q, want prefix %q", got, tc.wantEndpoint)
				}
			}
		})
	}
}

func TestResolver_DirectMedia(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		wantKind Kind
	}{
		{
			name:     "direct mp4 video",
			url:      "https://example.com/videos/clip.mp4",
			wantKind: KindVideo,
		},
		{
			name:     "direct webm video with query params",
			url:      "https://example.com/stream.webm?token=123",
			wantKind: KindVideo,
		},
		{
			name:     "direct png image",
			url:      "https://example.com/images/meme.png",
			wantKind: KindImage,
		},
		{
			name:     "direct gif animation",
			url:      "https://example.com/funny.gif",
			wantKind: KindImage,
		},
		{
			name:     "direct embed player path",
			url:      "https://customplayer.com/embed/video123",
			wantKind: KindEmbed,
		},
	}

	r := NewResolver(nil)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			info, err := r.Resolve(context.Background(), tc.url)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if info == nil {
				t.Fatal("expected non-nil MediaInfo")
			}
			if info.Kind != tc.wantKind {
				t.Errorf("got kind %v, want %v", info.Kind, tc.wantKind)
			}
		})
	}
}

func TestResolver_OEmbedAutoDiscovery(t *testing.T) {
	var serverURL string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/post/123":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
    <title>Great Meme</title>
    <link rel="alternate" type="application/json+oembed" href="%s/oembed?format=json">
</head>
<body><h1>Hello World</h1></body>
</html>`, serverURL)
		case "/oembed":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{
				"type": "video",
				"version": "1.0",
				"title": "Universal Video Title",
				"author_name": "DevAuthor",
				"provider_name": "UniversalHost",
				"html": "<iframe src=\"https://universalhost.com/embed/abc123\" width=\"640\" height=\"360\"></iframe>",
				"thumbnail_url": "https://universalhost.com/thumb.jpg",
				"width": 640,
				"height": 360
			}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	serverURL = ts.URL

	r := NewResolver(ts.Client())
	info, err := r.Resolve(context.Background(), ts.URL+"/post/123")
	if err != nil {
		t.Fatalf("unexpected error resolving oembed: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil MediaInfo")
	}

	if info.Kind != KindEmbed {
		t.Errorf("got kind %v, want %v", info.Kind, KindEmbed)
	}
	if info.Title != "Universal Video Title" {
		t.Errorf("got title %q, want %q", info.Title, "Universal Video Title")
	}
	if info.EmbedURL != "https://universalhost.com/embed/abc123" {
		t.Errorf("got embed URL %q, want %q", info.EmbedURL, "https://universalhost.com/embed/abc123")
	}
	if info.SiteName != "UniversalHost" {
		t.Errorf("got site name %q, want %q", info.SiteName, "UniversalHost")
	}
}

func TestResolver_OpenGraphFallback(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<!DOCTYPE html>
<html>
<head>
    <meta property="og:title" content="OpenGraph Meme Title">
    <meta property="og:description" content="Hilarious coding meme">
    <meta property="og:image" content="https://example.com/og-preview.jpg">
    <meta property="og:video" content="https://example.com/player/v/999">
    <meta property="og:video:width" content="1080">
    <meta property="og:video:height" content="1920">
    <meta property="og:site_name" content="MemePortal">
</head>
<body>Content</body>
</html>`)
	}))
	defer ts.Close()

	r := NewResolver(ts.Client())
	info, err := r.Resolve(context.Background(), ts.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil MediaInfo")
	}

	if info.Kind != KindEmbed {
		t.Errorf("got kind %v, want %v", info.Kind, KindEmbed)
	}
	if info.EmbedURL != "https://example.com/player/v/999" {
		t.Errorf("got embed URL %q, want %q", info.EmbedURL, "https://example.com/player/v/999")
	}
	if info.Title != "OpenGraph Meme Title" {
		t.Errorf("got title %q, want %q", info.Title, "OpenGraph Meme Title")
	}
	if !info.IsVertical {
		t.Errorf("expected vertical aspect ratio (1080x1920)")
	}
}

func TestResolver_SSRFProtection(t *testing.T) {
	client := NewSafeHTTPClient()
	blockedTargets := []string{
		"http://127.0.0.1:8080/secret",
		"http://localhost:8080/admin",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/internal",
		"http://192.168.1.1/router",
	}

	for _, target := range blockedTargets {
		t.Run(target, func(t *testing.T) {
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
			_, err := client.Do(req)
			if err == nil {
				t.Errorf("expected SSRF error for %s, got nil", target)
			}
		})
	}
}

func TestResolver_ConcurrentCache(t *testing.T) {
	r := NewResolver(nil)
	testURL := "https://example.com/video.mp4"

	var wg sync.WaitGroup
	for range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			info, err := r.Resolve(context.Background(), testURL)
			if err != nil || info == nil || info.Kind != KindVideo {
				t.Errorf("concurrent resolve mismatch: %v, info: %+v", err, info)
			}
		}()
	}
	wg.Wait()
}
