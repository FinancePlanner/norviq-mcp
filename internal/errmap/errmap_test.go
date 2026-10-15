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
