package errmap_test

import (
	"strings"
	"testing"

	"github.com/FinancePlanner/norviq-mcp/internal/api"
	"github.com/FinancePlanner/norviq-mcp/internal/errmap"
)

// The 403 message is the one place a user is sent to the website to upgrade,
// so it has to name a domain we actually own.
func TestForbiddenPointsAtTheRealSite(t *testing.T) {
	msg := errmap.Friendly(&api.APIError{Status: 403})
	if !strings.Contains(msg, "https://www.norviq.org") {
		t.Errorf("403 message should link https://www.norviq.org, got %q", msg)
	}
	if strings.Contains(msg, "norviqa") {
		t.Errorf("403 message still names a retired domain: %q", msg)
	}
}

// A consent refusal must not be mistaken for a missing Pro plan.
func TestForbiddenConsentRequiredExplainsAIDataSharing(t *testing.T) {
	msg := errmap.Friendly(&api.APIError{Status: 403, Body: `{"error":true,"code":"forbidden","reason":"AI_CONSENT_REQUIRED"}`})
	if !strings.Contains(msg, "AI data sharing") {
		t.Errorf("consent refusal should explain AI data sharing, got %q", msg)
	}
	if strings.Contains(msg, "Pro") {
		t.Errorf("consent refusal should not mention Pro: %q", msg)
	}
}
