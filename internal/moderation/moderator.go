// Package moderation provides content moderation and spam filtering for meme messages.
package moderation

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Result contains the outcome of a content moderation check.
type Result struct {
	// Approved indicates whether the text passed moderation criteria.
	Approved bool
	// Reason describes the rejection rationale when Approved is false.
	Reason string
}

// Service defines message moderation operations.
type Service interface {
	// Moderate inspects the provided text and returns the moderation result.
	Moderate(text string) Result
}

// RuleBasedModerator performs rule-based content moderation and spam detection.
type RuleBasedModerator struct {
	spamKeywords []string
	urlRegex     *regexp.Regexp
}

// NewRuleBasedModerator creates a new RuleBasedModerator with default rules.
func NewRuleBasedModerator() *RuleBasedModerator {
	return &RuleBasedModerator{
		spamKeywords: []string{
			"free crypto",
			"buy followers",
			"viagra",
			"casino",
			"click here",
			"earn money fast",
			"fast cash",
			"t.me/",
			"telegram:",
			"whatsapp:",
			"bit.ly/",
		},
		urlRegex: regexp.MustCompile(`(?i)https?://[^\s]+`),
	}
}

// Moderate checks the message text against blocked keywords, excessive links, and spam patterns.
func (m *RuleBasedModerator) Moderate(text string) Result {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return Result{Approved: false, Reason: "message cannot be empty"}
	}

	runeCount := utf8.RuneCountInString(trimmed)
	if runeCount < 2 {
		return Result{Approved: false, Reason: "message is too short"}
	}

	lower := strings.ToLower(trimmed)
	for _, kw := range m.spamKeywords {
		if strings.Contains(lower, kw) {
			return Result{
				Approved: false,
				Reason:   "contains prohibited spam keyword: " + kw,
			}
		}
	}

	urls := m.urlRegex.FindAllString(trimmed, -1)
	if len(urls) > 1 {
		return Result{
			Approved: false,
			Reason:   "message contains excessive links",
		}
	}

	if runeCount >= 20 {
		var upperCount, letterCount int
		for _, r := range trimmed {
			if unicode.IsLetter(r) {
				letterCount++
				if unicode.IsUpper(r) {
					upperCount++
				}
			}
		}
		if letterCount > 0 && float64(upperCount)/float64(letterCount) > 0.85 {
			return Result{
				Approved: false,
				Reason:   "message contains excessive uppercase text",
			}
		}
	}

	return Result{
		Approved: true,
		Reason:   "",
	}
}
