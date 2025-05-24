package esquery

import (
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestParseESQuery(t *testing.T) {
	t.Helper()

	ten := 10
	five := 5

	testCases := []struct {
		name        string
		jsonInput   string
		expected    *ESQuery
		expectError bool
	}{
		{
			name:      "simple term query",
			jsonInput: `{"query":{"term":{"user":"kimchy"}}}`,
			expected: &ESQuery{
				Query: &Query{Term: map[string]TermQuery{"user": {Value: "kimchy"}}},
			},
			expectError: false,
		},
		{
			name: "bool query with all clauses",
			jsonInput: `{
				"query":{
					"bool":{
						"must": [{"term":{"status":"active"}}],
						"filter": [{"range":{"age":{"gte":30}}}],
						"should": [{"match":{"city":"New York"}}],
						"must_not": [{"term":{"tag":"old"}}]
					}
				}
			}`,
			expected: &ESQuery{
				Query: &Query{
					Bool: &BoolQuery{
						Must:    []json.RawMessage{json.RawMessage(`{"term":{"status":"active"}}`)},
						Filter:  []json.RawMessage{json.RawMessage(`{"range":{"age":{"gte":30}}}`)},
						Should:  []json.RawMessage{json.RawMessage(`{"match":{"city":"New York"}}`)},
						MustNot: []json.RawMessage{json.RawMessage(`{"term":{"tag":"old"}}`)},
					},
				},
			},
			expectError: false,
		},
		{
			name:      "range query",
			jsonInput: `{"query":{"range":{"age":{"gte":10,"lte":20}}}}`,
			expected: &ESQuery{
				Query: &Query{Range: map[string]RangeQuery{"age": {GTE: float64(10), LTE: float64(20)}}},
			},
			expectError: false,
		},
		{
			name:      "match query",
			jsonInput: `{"query":{"match":{"message":"this is a test"}}}`,
			expected: &ESQuery{
				Query: &Query{Match: map[string]MatchQuery{"message": {Query: "this is a test"}}},
			},
			expectError: false,
		},
		{
			name:      "query_string query",
			jsonInput: `{"query":{"query_string":{"query":"(new york city) OR (big apple)","default_field":"content"}}}`,
			expected: &ESQuery{
				Query: &Query{QueryString: &QueryStringQuery{Query: "(new york city) OR (big apple)", DefaultField: "content"}},
			},
			expectError: false,
		},
		{
			name:      "query with size and from",
			jsonInput: `{"query":{"term":{"user":"kimchy"}},"size":5,"from":10}`,
			expected: &ESQuery{
				Query: &Query{Term: map[string]TermQuery{"user": {Value: "kimchy"}}},
				Size:  &five,
				From:  &ten,
			},
			expectError: false,
		},
		{
			name:      "query with basic terms aggregation",
			jsonInput: `{"aggs":{"user_terms":{"terms":{"field":"user"}}}}`,
			expected: &ESQuery{
				Aggs: &Aggs{"user_terms": {Terms: &TermsAggregation{Field: "user"}}},
			},
			expectError: false,
		},
		{
			name:        "empty json body {}",
			jsonInput:   `{}`,
			expected:    &ESQuery{}, // As per previous subtask, empty body is valid
			expectError: false,
		},
		{
			name:        "empty string body",
			jsonInput:   ``,
			expected:    &ESQuery{}, // Empty string body is also treated as empty query
			expectError: false,
		},
		{
			name:      "json with only size and from",
			jsonInput: `{"size":5,"from":10}`,
			expected: &ESQuery{
				Size: &five,
				From: &ten,
			},
			expectError: false,
		},
		{
			name:        "malformed json",
			jsonInput:   `{"query":{"term":{"user":"kimchy"}`, // Missing closing brace
			expected:    nil,
			expectError: true,
		},
		{
			name:        "query is a string",
			jsonInput:   `{"query":"this is not an object"}`,
			expected:    nil,  // or specific empty struct if unmarshal allows query to be nil
			expectError: true, // JSON is valid, but structure is wrong for ESQuery.Query (expects object)
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var reader io.Reader
			if tc.jsonInput != "" {
				reader = strings.NewReader(tc.jsonInput)
			} else {
				// Create an empty reader if jsonInput is empty string, but not nil.
				// io.ReadAll will return empty slice for this, which parser handles.
				reader = strings.NewReader("")
			}

			parsed, err := ParseESQuery(reader)

			if tc.expectError {
				if err == nil {
					t.Errorf("Expected an error, but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Did not expect an error, but got: %v", err)
				}
				// For range queries, JSON numbers are float64 by default.
				// Adjust expected if necessary, especially for integer-like floats.
				if tc.expected != nil && tc.expected.Query != nil && tc.expected.Query.Range != nil {
					for k, v := range tc.expected.Query.Range {
						if v.GTE != nil {
							if f, ok := v.GTE.(int); ok {
								v.GTE = float64(f)
							}
						}
						if v.LTE != nil {
							if f, ok := v.LTE.(int); ok {
								v.LTE = float64(f)
							}
						}
						// Similar for GT, LT if used in tests
						tc.expected.Query.Range[k] = v
					}
				}

				if !reflect.DeepEqual(parsed, tc.expected) {
					// For easier debugging, marshal both to JSON and compare strings
					parsedJSON, _ := json.MarshalIndent(parsed, "", "  ")
					expectedJSON, _ := json.MarshalIndent(tc.expected, "", "  ")
					t.Errorf("Parsed query does not match expected.\nExpected:\n%s\nGot:\n%s", string(expectedJSON), string(parsedJSON))
				}
			}
		})
	}
}
