// Package predicates defines hardcoded synonym groups for open-vocabulary
// predicate matching in graph retrieval. Not implemented in Stage 1.
package predicates

// Groups maps semantic categories to canonical predicates.
// Used by graph traversal to expand query intent without requiring
// exact predicate string match.
// v1: hardcoded. v2: embedding-based dynamic matching.
var Groups = map[string][]string{
	"location": {
		"lives_in", "located_in", "resides_in",
		"moved_to", "based_in", "home_in",
	},
	"employment": {
		"works_at", "worked_at", "employed_by",
		"works_for", "joined", "founded",
	},
	"ownership": {
		"owns", "has_pet", "has",
	},
	"family": {
		"has_partner", "has_child", "has_parent",
		"married_to", "related_to",
	},
	"identity": {
		"is_a", "named", "called",
	},
}

// ExpandPredicate returns all predicates in the same group as the
// given predicate, including itself.
// Returns []string{predicate} if no group found (passthrough).
func ExpandPredicate(predicate string) []string {
	for _, members := range Groups {
		for _, m := range members {
			if m == predicate {
				return members
			}
		}
	}
	return []string{predicate}
}
