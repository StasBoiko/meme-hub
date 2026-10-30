// Package media provides open standard media resolvers.
package media

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// OEmbedResponse models the standard response payload defined by the oEmbed specification (https://oembed.com).
type OEmbedResponse struct {
	Type            string `json:"type"`
	Version         string `json:"version"`
	Title           string `json:"title"`
	AuthorName      string `json:"author_name"`
	AuthorURL       string `json:"author_url"`
	ProviderName    string `json:"provider_name"`
	ProviderURL     string `json:"provider_url"`
	CacheAge        int    `json:"cache_age"`
	ThumbnailURL    string `json:"thumbnail_url"`
	ThumbnailWidth  int    `json:"thumbnail_width"`
	ThumbnailHeight int    `json:"thumbnail_height"`
	Width           int    `json:"width"`
	Height          int    `json:"height"`
	HTML            string `json:"html"`
	URL             string `json:"url"`
}

// DiscoverOEmbedEndpoint parses an HTML body looking for standard <link rel="alternate" type="application/json+oembed"> tags.
func DiscoverOEmbedEndpoint(body io.Reader, pageURL *url.URL) (string, error) {
	tokenizer := html.NewTokenizer(body)
	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			err := tokenizer.Err()
			if err == io.EOF {
				return "", nil
			}
			return "", fmt.Errorf("html tokenizing error: %w", err)
		case html.StartTagToken, html.SelfClosingTagToken:
			tok := tokenizer.Token()
			if tok.Data == "link" {
				var isAlternate, isOEmbed bool
				var href string
				for _, a := range tok.Attr {
					key := strings.ToLower(a.Key)
					val := strings.TrimSpace(a.Val)
					if key == "rel" && strings.EqualFold(val, "alternate") {
						isAlternate = true
					}
					if key == "type" && (strings.EqualFold(val, "application/json+oembed") || strings.EqualFold(val, "text/json+oembed")) {
						isOEmbed = true
					}
					if key == "href" {
						href = val
					}
				}
				if isAlternate && isOEmbed && href != "" {
					parsedHref, err := url.Parse(href)
					if err != nil {
						continue
					}
					if pageURL != nil {
						return pageURL.ResolveReference(parsedHref).String(), nil
					}
					return href, nil
				}
			}
		case html.EndTagToken:
			tok := tokenizer.Token()
			if tok.Data == "head" {
				return "", nil
			}
		}
	}
}

// FetchOEmbed retrieves and decodes the oEmbed payload from an endpoint URL.
func FetchOEmbed(ctx context.Context, client *http.Client, endpointURL string) (*OEmbedResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpointURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build oembed request: %w", err)
	}
	req.Header.Set("User-Agent", "MemeHub/2.0 (+https://meme-hub.local; UniversalMediaBot)")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oembed request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("oembed endpoint returned status: %d", resp.StatusCode)
	}

	var oembed OEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&oembed); err != nil {
		return nil, fmt.Errorf("failed to decode oembed json: %w", err)
	}
	return &oembed, nil
}

var iframeSrcRegex = regexp.MustCompile(`(?i)<iframe[^>]+src=["']([^"']+)["']`)

// ExtractIframeSrc extracts the source URL of an <iframe> element from HTML embed snippets.
func ExtractIframeSrc(htmlSnippet string) string {
	m := iframeSrcRegex.FindStringSubmatch(htmlSnippet)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}
