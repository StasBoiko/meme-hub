// Package media provides open standard media resolvers.
package media

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// OpenGraphMetadata represents standard OpenGraph and Twitter Cards metadata extracted from HTML <head>.
type OpenGraphMetadata struct {
	Title         string
	Description   string
	SiteName      string
	ImageURL      string
	VideoURL      string
	VideoType     string
	VideoWidth    int
	VideoHeight   int
	TwitterPlayer string
	TwitterStream string
	TwitterImage  string
}

// ParseOpenGraph parses HTML looking for standard OpenGraph (og:*) and Twitter (twitter:*) meta tags.
func ParseOpenGraph(r io.Reader) (*OpenGraphMetadata, error) {
	meta := &OpenGraphMetadata{}
	tokenizer := html.NewTokenizer(r)
	var inTitleTag bool

	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			err := tokenizer.Err()
			if err == io.EOF {
				return meta, nil
			}
			return meta, fmt.Errorf("html parse error: %w", err)
		case html.StartTagToken, html.SelfClosingTagToken:
			tok := tokenizer.Token()
			name := strings.ToLower(tok.Data)
			if name == "title" {
				inTitleTag = true
			}
			if name == "meta" {
				var prop, val string
				for _, a := range tok.Attr {
					k := strings.ToLower(a.Key)
					if k == "property" || k == "name" {
						prop = strings.ToLower(strings.TrimSpace(a.Val))
					}
					if k == "content" {
						val = strings.TrimSpace(a.Val)
					}
				}
				assignMetaTag(meta, prop, val)
			}
		case html.TextToken:
			if inTitleTag && meta.Title == "" {
				meta.Title = strings.TrimSpace(string(tokenizer.Text()))
			}
		case html.EndTagToken:
			tok := tokenizer.Token()
			name := strings.ToLower(tok.Data)
			if name == "title" {
				inTitleTag = false
			}
			if name == "head" {
				return meta, nil
			}
		}
	}
}

// assignMetaTag populates the OpenGraphMetadata struct based on matched meta tag properties.
func assignMetaTag(m *OpenGraphMetadata, prop, val string) {
	if val == "" {
		return
	}
	switch prop {
	case "og:title":
		m.Title = val
	case "og:description":
		m.Description = val
	case "og:site_name":
		m.SiteName = val
	case "og:image", "og:image:url", "og:image:secure_url":
		if m.ImageURL == "" {
			m.ImageURL = val
		}
	case "og:video", "og:video:url", "og:video:secure_url":
		if m.VideoURL == "" {
			m.VideoURL = val
		}
	case "og:video:type":
		m.VideoType = val
	case "og:video:width":
		if w, err := strconv.Atoi(val); err == nil {
			m.VideoWidth = w
		}
	case "og:video:height":
		if h, err := strconv.Atoi(val); err == nil {
			m.VideoHeight = h
		}
	case "twitter:player":
		m.TwitterPlayer = val
	case "twitter:player:stream":
		m.TwitterStream = val
	case "twitter:image", "twitter:image:src":
		m.TwitterImage = val
	case "twitter:title":
		if m.Title == "" {
			m.Title = val
		}
	case "twitter:description":
		if m.Description == "" {
			m.Description = val
		}
	case "twitter:site":
		if m.SiteName == "" {
			m.SiteName = val
		}
	}
}
