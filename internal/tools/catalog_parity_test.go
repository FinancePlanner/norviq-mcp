package tools_test

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/FinancePlanner/norviq-mcp/internal/tools"
)

// The backend owns one ActionCatalog shared by the HTTP controllers and the
// in-process assistant — which since 2026-09 genuinely includes Telegram and the
// persistent assistant, rather than only claiming to. This Go service hand-writes its
// tool structs against the same API, so nothing structural stopped the two
// diverging — that is how a tool decoding GET /v1/stocks as {items,nextCursor}
// shipped while the endpoint returns a bare array.
//
// These tests pin the relationship. They do not require a live backend: the
// catalog is snapshotted in testdata and refreshed with `make catalog-snapshot`.

type catalogSnapshot struct {
	Actions []struct {
		Name        string `json:"name"`
		Destructive bool   `json:"destructive"`
	} `json:"actions"`
}

func loadCatalog(t *testing.T) catalogSnapshot {
	t.Helper()
	raw, err := os.ReadFile("testdata/action-catalog.json")
	if err != nil {
		t.Fatalf("read catalog snapshot: %v", err)
	}
	var snap catalogSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("parse catalog snapshot: %v", err)
	}
	if len(snap.Actions) == 0 {
		t.Fatal("catalog snapshot is empty")
	}
	return snap
}

// mcpToCatalog maps each MCP write tool to the catalog action it corresponds to.
//
// The surfaces differ on purpose in one respect: MCP batches, because it asks for
// one confirmation per call and confirming seven watchlist rows individually is
// seven prompts for one user action. The assistant works a row at a time, so its
// catalog action is singular. That divergence is declared here rather than left
// to be rediscovered.
var mcpToCatalog = map[string]string{
	"upsert_watchlist_items": "upsert_watchlist_item",
	"remove_watchlist_items": "remove_watchlist_item",
	"record_trades":          "record_trade",
	"update_trade":           "record_trade",
	"delete_trade":           "delete_trade",
	"update_watchlist_item":  "upsert_watchlist_item",
	"add_position":           "add_position",
	"sell_position":          "sell_position",
	"delete_position":        "delete_position",
	"add_expense":            "add_expense",
	"update_expense":         "update_expense",
	"delete_expense":         "delete_expense",
	"add_goal":               "add_goal",
	"update_goal":            "update_goal",
	"delete_goal":            "delete_goal",
}

// mcpOnly lists write tools that deliberately have no catalog counterpart yet.
//
// Everything here is unreachable from Telegram and the in-app assistant. Adding a
// catalog action for one of these is what makes it reachable; until then the gap
// is explicit rather than invisible.
var mcpOnly = map[string]string{
	"create_watchlist_list":    "watchlist list management is app-only for now",
	"delete_watchlist_list":    "watchlist list management is app-only for now",
	"create_price_target":      "targets are not in the catalog yet",
	"delete_price_target":      "targets are not in the catalog yet",
	"add_research_note":        "research notes are not in the catalog yet",
	"delete_research_note":     "research notes are not in the catalog yet",
	"import_expenses_csv":      "bulk import is an MCP-shaped operation",
	"create_budget_snapshot":   "budget predates the catalog",
	"update_budget_snapshot":   "budget predates the catalog",
	"delete_budget_snapshot":   "budget predates the catalog",
	"add_budget_item":          "budget predates the catalog",
	"update_budget_item":       "budget predates the catalog",
	"delete_budget_item":       "budget predates the catalog",
	"add_recurring_expense":    "recurring expenses predate the catalog",
	"update_recurring_expense": "recurring expenses predate the catalog",
	"delete_recurring_expense": "recurring expenses predate the catalog",
}

func TestEveryWriteToolIsMappedOrDeclaredMCPOnly(t *testing.T) {
	snap := loadCatalog(t)
	known := map[string]bool{}
	for _, action := range snap.Actions {
		known[action.Name] = true
	}

	var unaccounted []string
	for _, name := range tools.WriteToolNames() {
		if reason, ok := mcpOnly[name]; ok {
			if reason == "" {
				t.Errorf("%s is listed as MCP-only with no reason", name)
			}
			continue
		}
		target, mapped := mcpToCatalog[name]
		if !mapped {
			unaccounted = append(unaccounted, name)
			continue
		}
		if !known[target] {
			t.Errorf("%s maps to catalog action %q, which is not in the catalog", name, target)
		}
	}
	sort.Strings(unaccounted)
	if len(unaccounted) > 0 {
		t.Errorf(
			"these write tools are neither mapped to a catalog action nor declared MCP-only: %s\n"+
				"Add a catalog action in the backend, or add an entry to mcpOnly saying why not.",
			strings.Join(unaccounted, ", "),
		)
	}
}

func TestDestructiveClassificationAgrees(t *testing.T) {
	snap := loadCatalog(t)
	destructive := map[string]bool{}
	for _, action := range snap.Actions {
		destructive[action.Name] = action.Destructive
	}

	// A tool the backend guards with a required confirm must not be presented by
	// MCP as an ordinary write, and vice versa. The two surfaces disagreeing about
	// what is dangerous is the failure this catches.
	for mcpName, catalogName := range mcpToCatalog {
		if !destructive[catalogName] {
			continue
		}
		var annotated bool
		for _, name := range tools.WriteToolNames() {
			if name == mcpName {
				annotated = true
				break
			}
		}
		if !annotated {
			t.Errorf(
				"%s maps to destructive catalog action %q but is not in WriteToolNames()",
				mcpName, catalogName,
			)
		}
	}
}
