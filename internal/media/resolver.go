// Package media implements open standard media resolution (Direct Media, oEmbed, OpenGraph/Twitter Cards).
package media

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// standardProviders maps domain suffixes to standard oEmbed endpoint bases as defined in the official oEmbed registry.
var standardProviders = map[string]string{
	"youtube.com":     "https://www.youtube.com/oembed",
	"youtu.be":        "https://www.youtube.com/oembed",
	"vimeo.com":       "https://vimeo.com/api/oembed.json",
	"dailymotion.com": "https://www.dailymotion.com/services/oembed",
	"coub.com":        "https://coub.com/api/oembed.json",
	"loom.com":        "https://www.loom.com/v1/oembed",
	"wistia.com":      "https://fast.wistia.com/oembed.json",
	"wistia.net":      "https://fast.wistia.com/oembed.json",
}

// Resolver orchestrates the media resolution pipeline with SSRF-safe networking and caching.
type Resolver struct {
	client *http.Client
	cache  map[string]*MediaInfo
	mu     sync.RWMutex
}

// isPrivateOrLocalIP checks whether an IP address is private, loopback, or link-local.
func isPrivateOrLocalIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

// NewSafeHTTPClient constructs an http.Client with basic SSRF protection and a 3-second timeout.
func NewSafeHTTPClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: 3 * time.Second,
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if isPrivateOrLocalIP(ip.IP) {
					return nil, fmt.Errorf("ssrf blocked: access to address %s is forbidden", ip.IP)
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
		ResponseHeaderTimeout: 3 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   3 * time.Second,
	}
}

// NewResolver constructs a Resolver with an injected HTTP client.
func NewResolver(client *http.Client) *Resolver {
	if client == nil {
		client = NewSafeHTTPClient()
	}
	return &Resolver{
		client: client,
		cache:  make(map[string]*MediaInfo),
	}
}

// DefaultResolver is the default package-level resolver instance.
var DefaultResolver = NewResolver(nil)

// Resolve executes the open standard media resolution pipeline: Direct Media -> oEmbed Discovery -> OpenGraph Fallback.
func (r *Resolver) Resolve(ctx context.Context, rawURL string) (*MediaInfo, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, nil
	}

	r.mu.RLock()
	if cached, ok := r.cache[trimmed]; ok {
		r.mu.RUnlock()
		return cached, nil
	}
	r.mu.RUnlock()

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}

	// 1. Direct media file extension / structural check
	if direct := checkDirectMedia(trimmed, parsed); direct != nil {
		r.cacheResult(trimmed, direct)
		return direct, nil
	}

	// 2. Standard oEmbed auto-discovery & OpenGraph fallback via remote fetch
	if dynamic := r.resolveRemotePage(ctx, trimmed, parsed); dynamic != nil {
		r.cacheResult(trimmed, dynamic)
		return dynamic, nil
	}

	// 3. Default fallback card
	fallback := &MediaInfo{
		Kind:        KindCard,
		OriginalURL: trimmed,
		SiteName:    parsed.Host,
		Platform:    derivePlatform("", parsed.Host),
	}
	r.cacheResult(trimmed, fallback)
	return fallback, nil
}

// checkDirectMedia performs fast extension and structural inspection for direct media.
func checkDirectMedia(rawURL string, parsed *url.URL) *MediaInfo {
	cleanPath := strings.ToLower(strings.Split(parsed.Path, "?")[0])

	for _, ext := range []string{".mp4", ".webm", ".ogg", ".mov", ".m4v"} {
		if strings.HasSuffix(cleanPath, ext) {
			return &MediaInfo{
				Kind:        KindVideo,
				OriginalURL: rawURL,
				DirectURL:   rawURL,
				SiteName:    "Direct Video",
				Platform: PlatformInfo{
					Name:  "Direct Video",
					Icon:  "🎬",
					Class: "bg-emerald-500/20 text-emerald-400 border-emerald-500/30",
				},
			}
		}
	}

	for _, ext := range []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg", ".bmp", ".ico", ".tiff", ".avif"} {
		if strings.HasSuffix(cleanPath, ext) {
			return &MediaInfo{
				Kind:        KindImage,
				OriginalURL: rawURL,
				DirectURL:   rawURL,
				SiteName:    "Direct Image",
				Platform: PlatformInfo{
					Name:  "Direct Image",
					Icon:  "🖼️",
					Class: "bg-blue-500/20 text-blue-400 border-blue-500/30",
				},
			}
		}
	}

	if strings.Contains(parsed.Path, "/embed/") || strings.HasPrefix(strings.ToLower(parsed.Host), "player.") {
		return &MediaInfo{
			Kind:        KindEmbed,
			OriginalURL: rawURL,
			EmbedURL:    rawURL,
			SiteName:    parsed.Host,
			Platform:    derivePlatform("", parsed.Host),
		}
	}

	return nil
}

// lookupStandardOEmbedEndpoint returns the standard oEmbed API endpoint URL for registered providers.
func lookupStandardOEmbedEndpoint(rawURL string, parsed *url.URL) string {
	host := strings.ToLower(parsed.Host)
	for domain, endpoint := range standardProviders {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return endpoint + "?url=" + url.QueryEscape(rawURL) + "&format=json"
		}
	}
	return ""
}

// resolveRemotePage resolves media metadata via oEmbed auto-discovery/registry and OpenGraph fallback.
func (r *Resolver) resolveRemotePage(ctx context.Context, rawURL string, parsed *url.URL) *MediaInfo {
	endpoint := lookupStandardOEmbedEndpoint(rawURL, parsed)

	if endpoint == "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil
		}
		req.Header.Set("User-Agent", "MemeHub/2.0 (+https://meme-hub.local; UniversalMediaBot)")
		req.Header.Set("Range", "bytes=0-65536")

		resp, err := r.client.Do(req)
		if err != nil {
			return nil
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		if resp.StatusCode < 200 || resp.StatusCode >= 400 {
			return nil
		}

		ct := strings.ToLower(resp.Header.Get("Content-Type"))
		if strings.HasPrefix(ct, "image/") {
			return &MediaInfo{
				Kind:        KindImage,
				OriginalURL: rawURL,
				DirectURL:   rawURL,
				SiteName:    parsed.Host,
				Platform: PlatformInfo{
					Name:  "Direct Image",
					Icon:  "🖼️",
					Class: "bg-blue-500/20 text-blue-400 border-blue-500/30",
				},
			}
		}
		if strings.HasPrefix(ct, "video/") {
			return &MediaInfo{
				Kind:        KindVideo,
				OriginalURL: rawURL,
				DirectURL:   rawURL,
				SiteName:    parsed.Host,
				Platform: PlatformInfo{
					Name:  "Direct Video",
					Icon:  "🎬",
					Class: "bg-emerald-500/20 text-emerald-400 border-emerald-500/30",
				},
			}
		}

		bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		if err != nil || len(bodyBytes) == 0 {
			return nil
		}

		discovered, discErr := DiscoverOEmbedEndpoint(strings.NewReader(string(bodyBytes)), parsed)
		if discErr == nil && discovered != "" {
			endpoint = discovered
		} else {
			meta, ogErr := ParseOpenGraph(strings.NewReader(string(bodyBytes)))
			if ogErr == nil && meta != nil {
				siteName := meta.SiteName
				if siteName == "" {
					siteName = parsed.Host
				}
				videoURL := meta.VideoURL
				if videoURL == "" {
					videoURL = meta.TwitterPlayer
				}
				if videoURL != "" {
					isVertical := meta.VideoHeight > meta.VideoWidth && meta.VideoWidth > 0
					return &MediaInfo{
						Kind:         KindEmbed,
						OriginalURL:  rawURL,
						EmbedURL:     videoURL,
						Title:        meta.Title,
						Description:  meta.Description,
						ThumbnailURL: meta.ImageURL,
						SiteName:     siteName,
						Width:        meta.VideoWidth,
						Height:       meta.VideoHeight,
						IsVertical:   isVertical,
						Platform:     derivePlatform(siteName, parsed.Host),
					}
				}
				if meta.ImageURL != "" || meta.Title != "" {
					return &MediaInfo{
						Kind:         KindCard,
						OriginalURL:  rawURL,
						Title:        meta.Title,
						Description:  meta.Description,
						ThumbnailURL: meta.ImageURL,
						SiteName:     siteName,
						Platform:     derivePlatform(siteName, parsed.Host),
					}
				}
			}
		}
	}

	if endpoint != "" {
		oembed, err := FetchOEmbed(ctx, r.client, endpoint)
		if err == nil && oembed != nil {
			siteName := oembed.ProviderName
			if siteName == "" {
				siteName = parsed.Host
			}
			info := &MediaInfo{
				OriginalURL:  rawURL,
				Title:        oembed.Title,
				AuthorName:   oembed.AuthorName,
				ThumbnailURL: oembed.ThumbnailURL,
				SiteName:     siteName,
				Width:        oembed.Width,
				Height:       oembed.Height,
				Platform:     derivePlatform(siteName, parsed.Host),
			}
			if oembed.Type == "photo" && oembed.URL != "" {
				info.Kind = KindImage
				info.DirectURL = oembed.URL
				return info
			}
			info.Kind = KindEmbed
			if oembed.HTML != "" {
				info.EmbedHTML = template.HTML(oembed.HTML)
				info.EmbedURL = ExtractIframeSrc(oembed.HTML)
			}
			if info.EmbedURL == "" {
				info.EmbedURL = rawURL
			}
			if (oembed.Height > oembed.Width && oembed.Width > 0) || strings.Contains(rawURL, "/shorts/") {
				info.IsVertical = true
			}
			return info
		}
	}

	return nil
}

// derivePlatform assigns styling and icon indicators based on provider or host.
func derivePlatform(siteName, host string) PlatformInfo {
	name := strings.TrimSpace(siteName)
	if name == "" {
		name = host
	}
	if name == "" {
		name = "Web Link"
	}

	return PlatformInfo{
		Name:  name,
		Icon:  "🌐",
		Class: "bg-indigo-500/20 text-indigo-300 border-indigo-500/30",
	}
}

func (r *Resolver) cacheResult(url string, info *MediaInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache[url] = info
}

// Resolve is a package-level helper resolving a URL through DefaultResolver.
func Resolve(rawURL string) *MediaInfo {
	info, _ := DefaultResolver.Resolve(context.Background(), rawURL)
	return info
}

// IsSupported reports whether the URL resolves to any valid image, video, or embed.
func IsSupported(rawURL string) bool {
	info := Resolve(rawURL)
	if info == nil {
		return false
	}
	return info.Kind == KindVideo || info.Kind == KindImage || info.Kind == KindEmbed
}

// IsSupportedPlatform reports whether the URL belongs to a recognized embed video platform.
func IsSupportedPlatform(rawURL string) bool {
	info := Resolve(rawURL)
	return info != nil && info.Kind == KindEmbed
}

// VideoEmbedURL returns an embeddable iframe URL for the given resource or empty string.
func VideoEmbedURL(rawURL string) string {
	info := Resolve(rawURL)
	if info != nil && info.Kind == KindEmbed {
		return info.EmbedURL
	}
	return ""
}

// YouTubeEmbedURL returns YouTube embed URL if the URL is from YouTube, otherwise empty string.
func YouTubeEmbedURL(rawURL string) string {
	info := Resolve(rawURL)
	if info != nil && strings.Contains(info.EmbedURL, "youtube") {
		return info.EmbedURL
	}
	return ""
}

// IsImageURL reports whether the URL resolves to an image format.
func IsImageURL(rawURL string) bool {
	info := Resolve(rawURL)
	return info != nil && info.Kind == KindImage
}

// IsVideoURL reports whether the URL resolves to a direct video format.
func IsVideoURL(rawURL string) bool {
	info := Resolve(rawURL)
	return info != nil && info.Kind == KindVideo
}

// IsVerticalEmbed reports whether the media is a vertical video format.
func IsVerticalEmbed(rawURL string) bool {
	info := Resolve(rawURL)
	return info != nil && info.IsVertical
}

// LinkPlatform returns platform metadata for external media URLs.
func LinkPlatform(rawURL string) PlatformInfo {
	info := Resolve(rawURL)
	if info != nil && info.Platform.Name != "" {
		return info.Platform
	}
	return derivePlatform("", "")
}
