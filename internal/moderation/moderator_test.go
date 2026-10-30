// Package moderation provides content moderation and spam filtering for meme messages.
package moderation

import (
	"strings"
	"testing"
)

// TestRuleBasedModerator_Moderate tests moderation evaluation across various valid and spam message inputs.
func TestRuleBasedModerator_Moderate(t *testing.T) {
	moderator := NewRuleBasedModerator()

	tests := []struct {
		name         string
		text         string
		wantApproved bool
		wantReason   string
	}{
		{
			name:         "clean standard message",
			text:         "Hello world! Wonderful meme hub application.",
			wantApproved: true,
			wantReason:   "",
		},
		{
			name:         "clean message with single link",
			text:         "Check out my project documentation at https://example.com/docs",
			wantApproved: true,
			wantReason:   "",
		},
		{
			name:         "empty string rejected",
			text:         "   ",
			wantApproved: false,
			wantReason:   "message cannot be empty",
		},
		{
			name:         "single rune message rejected",
			text:         "a",
			wantApproved: false,
			wantReason:   "message is too short",
		},
		{
			name:         "spam keyword free crypto rejected",
			text:         "Claim your FREE CRYPTO tokens now at this amazing portal!",
			wantApproved: false,
			wantReason:   "contains prohibited spam keyword: free crypto",
		},
		{
			name:         "spam keyword casino rejected",
			text:         "Play online CASINO games and win huge jackpots today!",
			wantApproved: false,
			wantReason:   "contains prohibited spam keyword: casino",
		},
		{
			name:         "spam keyword telegram link rejected",
			text:         "Join our private VIP channel on t.me/super_deals for insider info.",
			wantApproved: false,
			wantReason:   "contains prohibited spam keyword: t.me/",
		},
		{
			name:         "excessive links rejected",
			text:         "Visit https://site1.com and also check https://site2.com for special offers.",
			wantApproved: false,
			wantReason:   "message contains excessive links",
		},
		{
			name:         "excessive uppercase screaming rejected",
			text:         "ATTENTION EVERYONE THIS IS A MAJOR ANNOUNCEMENT PLEASE READ IMMEDIATELY!!!",
			wantApproved: false,
			wantReason:   "message contains excessive uppercase text",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			res := moderator.Moderate(tc.text)
			if res.Approved != tc.wantApproved {
				t.Fatalf("Moderate(%q) approved = %v, want %v (reason: %q)", tc.text, res.Approved, tc.wantApproved, res.Reason)
			}
			if tc.wantReason != "" && !strings.Contains(res.Reason, tc.wantReason) {
				t.Errorf("Moderate(%q) reason = %q, want substring %q", tc.text, res.Reason, tc.wantReason)
			}
		})
	}
}
