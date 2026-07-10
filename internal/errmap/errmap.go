// Package errmap turns backend HTTP errors into user-facing tool messages.
package errmap

import (
	"errors"

	"github.com/FinancePlanner/norviq-mcp/internal/api"
)

// Friendly converts a backend error into a message suitable to show the end
// user through their AI client.
func Friendly(err error) string {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 401:
			return "Your norviq connection needs to be re-authorized. Please reconnect the norviq connector."
		case 403:
			return "This action requires norviq Pro, or your token is missing the required permission. Upgrade at norviqa.io or reconnect with the needed scope."
		case 404:
			return "That item was not found in your norviq account."
		case 429:
			return "You're sending requests too quickly. Wait a moment and try again."
		default:
			return "norviq couldn't complete that request right now. Please try again shortly."
		}
	}
	return "Something went wrong talking to norviq: " + err.Error()
}
