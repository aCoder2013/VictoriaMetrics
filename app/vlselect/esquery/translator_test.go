package esquery

import (
	"encoding/json" // Added for json.RawMessage
	"reflect"
	"strings" // Added for strings.Contains (optional, for robust filter checking)
	"testing"
	"time"
	// logstorage package import removed as it's not directly used by test logic
)

func TestTranslateESQueryToLogStorageQuery(t *testing.T) {
	t.Helper()
	fixedTimestamp := time.Now().UnixNano()

	// Helper to create pointers for size/from, as ESQuery uses *int
	ptrToInt := func(v int) *int { return &v }

	testCases := []struct {
		name           string
		esQuery        *ESQuery
		expectedLogSQL string // Expected output from (*logstorage.Query).String()
		expectError    bool
	}{
		{
			name: "term query",
			esQuery: &ESQuery{
				Query: &Query{Term: map[string]TermQuery{"user": {Value: "kimchy"}}},
			},
			expectedLogSQL: `* | filter user="kimchy" | limit 10`,
			expectError:    false,
		},
		{
			name: "term query with numeric value",
			esQuery: &ESQuery{
				Query: &Query{Term: map[string]TermQuery{"age": {Value: 30}}},
			},
			expectedLogSQL: `* | filter age=30 | limit 10`,
			expectError:    false,
		},
		{
			name: "bool query - must only",
			esQuery: &ESQuery{
				Query: &Query{Bool: &BoolQuery{
					Must: []json.RawMessage{
						json.RawMessage(`{"term":{"status":"active"}}`),
						json.RawMessage(`{"term":{"type":"log"}}`),
					},
				}},
			},
			// Note: LogSQL might optimize (term AND term) to just (term AND term) without outer parens if it's simple enough.
			// The String() method of the query object will produce the canonical form.
			// Expected: `* | filter ((status="active" AND type="log")) | limit 10` - explicit grouping for bool
			expectedLogSQL: `* | filter ((status="active" AND type="log")) | limit 10`,
			expectError:    false,
		},
		{
			name: "bool query - must, filter, should, must_not",
			esQuery: &ESQuery{
				Query: &Query{Bool: &BoolQuery{
					Must:    []json.RawMessage{json.RawMessage(`{"term":{"status":"active"}}`)},
					Filter:  []json.RawMessage{json.RawMessage(`{"range":{"age":{"gte":30}}}`)},
					Should:  []json.RawMessage{json.RawMessage(`{"match":{"city":"New York"}}`), json.RawMessage(`{"match":{"city":"London"}}`)},
					MustNot: []json.RawMessage{json.RawMessage(`{"term":{"tag":"old"}}`)},
				}},
			},
			// Expected: ( (status="active") AND (age >= 30) AND (NOT (tag="old")) AND ((city:"New York" OR city:"London")) )
			// The order of ANDed clauses might vary. The key is that all components are there with correct logic.
			// The String() output will be canonical. This is an approximation.
			// `* | filter (((status="active") AND (age >= 30) AND (NOT (tag="old"))) AND ((city:"New York" OR city:"London"))) | limit 10`
			// Simpler, let's assume a specific order from the implementation:
			expectedLogSQL: `* | filter (((status="active") AND (age >= 30) AND (NOT (tag="old"))) AND (city:"New York" OR city:"London")) | limit 10`,
			expectError:    false,
		},
		{
			name: "range query",
			esQuery: &ESQuery{
				Query: &Query{Range: map[string]RangeQuery{"age": {GTE: float64(10), LTE: float64(20)}}},
			},
			expectedLogSQL: `* | filter (age >= 10 AND age <= 20) | limit 10`,
			expectError:    false,
		},
		{
			name: "match query",
			esQuery: &ESQuery{
				Query: &Query{Match: map[string]MatchQuery{"message": {Query: "this is a test"}}},
			},
			expectedLogSQL: `* | filter message:"this is a test" | limit 10`,
			expectError:    false,
		},
		{
			name: "query_string query",
			esQuery: &ESQuery{
				Query: &Query{QueryString: &QueryStringQuery{Query: "(new york city) OR (big apple)"}},
			},
			expectedLogSQL: `* | filter _q="(new york city) OR (big apple)" | limit 10`,
			expectError:    false,
		},
		{
			name: "query with size and from",
			esQuery: &ESQuery{
				Query: &Query{Term: map[string]TermQuery{"user": {Value: "test"}}},
				Size:  ptrToInt(5),
				From:  ptrToInt(10),
			},
			expectedLogSQL: `* | filter user="test" | limit 5 | offset 10`,
			expectError:    false,
		},
		{
			name: "query with size only",
			esQuery: &ESQuery{
				Query: &Query{Term: map[string]TermQuery{"user": {Value: "test"}}},
				Size:  ptrToInt(20),
			},
			expectedLogSQL: `* | filter user="test" | limit 20`,
			expectError:    false,
		},
		{
			name: "query with from only (should use default limit)",
			esQuery: &ESQuery{
				Query: &Query{Term: map[string]TermQuery{"user": {Value: "test"}}},
				From:  ptrToInt(5),
			},
			expectedLogSQL: `* | filter user="test" | limit 10 | offset 5`, // Default limit 10
			expectError:    false,
		},
		{
			name:           "nil esQuery (match all)",
			esQuery:        nil,
			expectedLogSQL: `* | limit 10`, // Default limit 10
			expectError:    false,
		},
		{
			name:           "empty esQuery (match all)",
			esQuery:        &ESQuery{},
			expectedLogSQL: `* | limit 10`, // Default limit 10
			expectError:    false,
		},
		{
			name: "query with aggs (aggs ignored with warning)",
			esQuery: &ESQuery{
				Query: &Query{Term: map[string]TermQuery{"user": {Value: "test"}}},
				Aggs:  &Aggs{"user_terms": {Terms: &TermsAggregation{Field: "user"}}},
			},
			expectedLogSQL: `* | filter user="test" | limit 10`, // Aggs are ignored
			expectError:    false,
		},
		{
			name: "query with sort (sort ignored with warning)",
			esQuery: &ESQuery{
				Query: &Query{Term: map[string]TermQuery{"user": {Value: "test"}}},
				Sort:  []SortKey{{"timestamp": SortOrderOptions{Order: "desc"}}},
			},
			expectedLogSQL: `* | filter user="test" | limit 10`, // Sort is ignored
			expectError:    false,
		},
		{
			name: "complex bool query with nested structures",
			esQuery: &ESQuery{
				Query: &Query{Bool: &BoolQuery{
					Must: []json.RawMessage{
						json.RawMessage(`{"term":{"status":"active"}}`),
						json.RawMessage(`{"bool":{"should":[{"term":{"tag":"urgent"}},{"term":{"tag":"important"}}]}}`),
					},
					Filter: []json.RawMessage{json.RawMessage(`{"range":{"timestamp":{"gte":"now-1h"}}}`)},
				}},
			},
			// Expected: ( (status="active" AND ((tag="urgent" OR tag="important"))) AND (timestamp >= "now-1h") )
			// This will be complex, relying on the String() method's canonical output.
			// `* | filter (((status="active" AND ((tag="urgent" OR tag="important")))) AND (timestamp >= "now-1h")) | limit 10`
			expectedLogSQL: `* | filter (((status="active" AND ((tag="urgent" OR tag="important")))) AND (timestamp >= "now-1h")) | limit 10`,
			expectError:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			lq, err := TranslateESQueryToLogStorageQuery(fixedTimestamp, tc.esQuery)

			if tc.expectError {
				if err == nil {
					t.Errorf("Expected an error, but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Did not expect an error, but got: %v", err)
				}
				if lq == nil {
					t.Fatalf("Expected a non-nil logstorage.Query, but got nil")
				}
				// Compare the string representation of the query
				// Note: The exact string output can be sensitive to internal logstorage formatting.
				// These expected strings are based on current translator logic.
				actualLogSQL := lq.String()
				if actualLogSQL != tc.expectedLogSQL {
					t.Errorf("Translated LogSQL query does not match expected.\nExpected:\n%s\nGot:\n%s", tc.expectedLogSQL, actualLogSQL)
				}

				// Additionally, verify Limit and Offset if applicable, though String() should cover it.
				// This is more for explicit checking of how pipes are applied.
				// Note: Direct access to lq.Limit or lq.Offset is not possible as they are not exported.
				// The pipes are added to the query string, so lq.String() is the source of truth here.
			}
		})
	}
}

// TestTranslateESQueryToLogStorageQuery_RangeValueTypes tests range query translation with different value types.
func TestTranslateESQueryToLogStorageQuery_RangeValueTypes(t *testing.T) {
	t.Helper()
	fixedTimestamp := time.Now().UnixNano()

	testCases := []struct {
		name           string
		esQuery        *ESQuery
		expectedFilter string // Just the filter part
	}{
		{
			name: "range with integer values",
			esQuery: &ESQuery{
				Query: &Query{Range: map[string]RangeQuery{"count": {GTE: 10, LT: 20}}},
			},
			expectedFilter: "(count >= 10 AND count < 20)",
		},
		{
			name: "range with float values",
			esQuery: &ESQuery{
				Query: &Query{Range: map[string]RangeQuery{"score": {GT: 0.5, LTE: 0.9}}},
			},
			expectedFilter: "(score > 0.500000 AND score <= 0.900000)", // Note float formatting
		},
		{
			name: "range with string value (e.g., date)",
			esQuery: &ESQuery{
				Query: &Query{Range: map[string]RangeQuery{"timestamp": {GTE: "2023-01-01T00:00:00Z"}}},
			},
			expectedFilter: `(timestamp >= "2023-01-01T00:00:00Z")`,
		},
		{
			name: "range with numeric string value",
			esQuery: &ESQuery{
				Query: &Query{Range: map[string]RangeQuery{"version": {GT: "1.2.3"}}}, // Assuming this is treated as string by ES
			},
			expectedFilter: `(version > "1.2.3")`, // If formatRangeValue quotes it. If not, version > 1.2.3
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			lq, err := TranslateESQueryToLogStorageQuery(fixedTimestamp, tc.esQuery)
			if err != nil {
				t.Fatalf("TranslateESQueryToLogStorageQuery failed: %v", err)
			}
			if lq == nil {
				t.Fatalf("Expected a non-nil logstorage.Query, but got nil")
			}

			// Extract filter part from the full query string for comparison
			// Query string is like: `* | filter <filter_part> | limit 10`
			fullQueryStr := lq.String()
			// filterPart := "" // This variable was declared but not used.
			if parts := reflect.ValueOf(lq).Elem().FieldByName("Pipes"); parts.IsValid() {
				// This is a hacky way to get at unexported fields for testing.
				// A better way would be if Query had a method to get just the filter string.
				// For now, we parse the string output.
				// Example: "* | filter (user="test") | limit 10"
				// We need to extract "(user="test")"
				// This is fragile.

				// A simpler approach: check if the expected filter string is contained.
				// This is less precise but avoids complex parsing of the lq.String() output.
				// However, for exact match, we need to reconstruct what the filter part should be.
				// The full string is `* | filter <filter> | limit <N> [| offset <M>]`

				// Let's try to match the expectedLogSQL format from the main test.
				expectedFullSQL := `* | filter ` + tc.expectedFilter + ` | limit 10`
				if fullQueryStr != expectedFullSQL {
					t.Errorf("Translated LogSQL query does not match expected.\nExpected:\n%s\nGot:\n%s", expectedFullSQL, fullQueryStr)
				}

			} else {
				t.Logf("Could not access Pipes field for detailed filter string comparison. Full query: %s", fullQueryStr)
				// Fallback to string contains if direct parsing is too hard.
				if !strings.Contains(fullQueryStr, tc.expectedFilter) {
					t.Errorf("Expected filter part '%s' not found in query '%s'", tc.expectedFilter, fullQueryStr)
				}
			}
		})
	}
}
