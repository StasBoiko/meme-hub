// Package media provides a universal media resolution pipeline conforming to open standards
// including Direct Media probing, oEmbed protocol auto-discovery, and OpenGraph/Twitter Cards extraction.
package media

import (
	"html/template"
)

// Kind describes the rendering category of a media resource.
type Kind string

const (
	// KindVideo represents direct playable HTML5 video streams (.mp4, .webm, etc.).
	KindVideo Kind = "video"
	// KindImage represents static or animated image files (.jpg, .png, .gif, .webp, etc.).
	KindImage Kind = "image"
	// KindEmbed represents responsive iframe media players from oEmbed or video transforms.
	KindEmbed Kind = "embed"
	// KindCard represents rich link preview cards with OpenGraph or fallback metadata.
	KindCard Kind = "card"
)

// PlatformInfo holds branding and visual metadata for external link cards.
type PlatformInfo struct {
	Name  string
	Icon  string
	Class string
}

// MediaInfo holds unified, display-ready data about a resolved media URL.
type MediaInfo struct {
	Kind         Kind
	OriginalURL  string
	EmbedURL     string
	EmbedHTML    template.HTML
	DirectURL    string
	Title        string
	Description  string
	ThumbnailURL string
	SiteName     string
	AuthorName   string
	Width        int
	Height       int
	IsVertical   bool
	Platform     PlatformInfo
}
