package routing

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/gauravfs-14/webhookmind/internal/models"
	"github.com/gauravfs-14/webhookmind/internal/schema"
	"github.com/gauravfs-14/webhookmind/internal/store"
)

// ResolveDestinations evaluates routing rules and returns the matched destinations.
// Falls back to default destinations when no rules match or no rules exist.
func ResolveDestinations(ctx context.Context, pg *store.PostgresStore, sourceID string, rawBody []byte, logger *slog.Logger) ([]models.Destination, error) {
	rules, err := pg.GetRoutingRules(ctx, sourceID)
	if err != nil || len(rules) == 0 {
		// No routing rules — use all active destinations (backward compat).
		return pg.GetDestinationsBySourceID(ctx, sourceID)
	}

	// Parse and flatten payload for condition evaluation.
	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		// Can't evaluate conditions on non-JSON — fall back to defaults.
		return pg.GetDefaultDestinations(ctx, sourceID)
	}

	flat := schema.FlattenJSON(payload)

	// Evaluate rules sorted by priority.
	matchedDestIDs := make(map[string]bool)
	for _, rule := range rules {
		var conditions []Condition
		if err := json.Unmarshal(rule.Conditions, &conditions); err != nil {
			logger.Error("failed to parse routing rule conditions",
				"rule_id", rule.ID,
				"error", err,
			)
			continue
		}

		if EvaluateConditions(conditions, flat, rule.LogicOperator) {
			matchedDestIDs[rule.DestinationID] = true
		}
	}

	if len(matchedDestIDs) == 0 {
		// No rules matched — fall back to default destinations.
		return pg.GetDefaultDestinations(ctx, sourceID)
	}

	// Fetch matched destinations.
	allDests, err := pg.GetDestinationsBySourceID(ctx, sourceID)
	if err != nil {
		return nil, err
	}

	var matched []models.Destination
	for _, d := range allDests {
		if matchedDestIDs[d.ID] {
			matched = append(matched, d)
		}
	}

	if len(matched) == 0 {
		return pg.GetDefaultDestinations(ctx, sourceID)
	}

	return matched, nil
}
