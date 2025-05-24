package esquery

import (
	"encoding/json" // Using standard json for easier struct unmarshalling initially
	"fmt"           // For error wrapping
	"io"
)

// ESQuery represents the top-level structure of an Elasticsearch query.
type ESQuery struct {
	Query *Query    `json:"query,omitempty"`
	Aggs  *Aggs     `json:"aggs,omitempty"` // "aggs" is more common than "aggregations" in ES JSON
	Size  *int      `json:"size,omitempty"` // For limiting number of results
	From  *int      `json:"from,omitempty"` // For pagination
	Sort  []SortKey `json:"sort,omitempty"` // For sorting
}

// Query represents the "query" part of an ES query.
type Query struct {
	Bool        *BoolQuery            `json:"bool,omitempty"`
	Match       map[string]MatchQuery `json:"match,omitempty"` // Field: Query
	Term        map[string]TermQuery  `json:"term,omitempty"`  // Field: Value or map[string]string{"value": "..."}
	Range       map[string]RangeQuery `json:"range,omitempty"` // Field: Range conditions
	QueryString *QueryStringQuery     `json:"query_string,omitempty"`
}

// BoolQuery represents a "bool" query.
type BoolQuery struct {
	Must    []json.RawMessage `json:"must,omitempty"`     // Can contain Query objects, using RawMessage for deferred parsing
	Filter  []json.RawMessage `json:"filter,omitempty"`   // Can contain Query objects
	Should  []json.RawMessage `json:"should,omitempty"`   // Can contain Query objects
	MustNot []json.RawMessage `json:"must_not,omitempty"` // Can contain Query objects
}

// MatchQuery represents a "match" query. (Simplified for now)
type MatchQuery struct {
	Query string `json:"query"`
}

// TermQuery represents a "term" query.
// For simplicity, starting with the object form like {"value": "searchterm"}.
// Also handles cases where it's just a string value directly e.g. "term": {"field": "value"}
// To handle this, we can try unmarshalling into a struct first, and if that fails, try a string.
// However, the provided struct `TermQuery { Value interface{} `json:"value"` }` is more flexible.
type TermQuery struct {
	Value interface{} `json:"value"` // string or number typically
}

// RangeQuery represents a "range" query.
type RangeQuery struct {
	GT  interface{} `json:"gt,omitempty"`  // Greater than
	GTE interface{} `json:"gte,omitempty"` // Greater than or equal to
	LT  interface{} `json:"lt,omitempty"`  // Less than
	LTE interface{} `json:"lte,omitempty"` // Less than or equal to
}

// QueryStringQuery represents a "query_string" query
type QueryStringQuery struct {
	Query           string   `json:"query"`
	DefaultField    string   `json:"default_field,omitempty"`
	AnalyzeWildcard bool     `json:"analyze_wildcard,omitempty"`
	Fields          []string `json:"fields,omitempty"`
}

// Aggs represents the "aggs" or "aggregations" part of an ES query.
type Aggs map[string]Aggregation

// Aggregation represents a single aggregation.
type Aggregation struct {
	Terms         *TermsAggregation         `json:"terms,omitempty"`
	DateHistogram *DateHistogramAggregation `json:"date_histogram,omitempty"`
	// TODO: Add other aggregation types as needed
}

// TermsAggregation represents a "terms" aggregation.
type TermsAggregation struct {
	Field string `json:"field"`
	Size  *int   `json:"size,omitempty"`
}

// DateHistogramAggregation represents a "date_histogram" aggregation.
type DateHistogramAggregation struct {
	Field    string `json:"field"`
	Interval string `json:"interval"`         // e.g., "1d", "1h", "1m"
	Format   string `json:"format,omitempty"` // Optional date format
}

// SortKey represents a single sort criterion
type SortKey map[string]SortOrderOptions // Field name to sort options

// SortOrderOptions defines how to sort on a specific field
type SortOrderOptions struct {
	Order string `json:"order"` // "asc" or "desc"
}

// ParseESQuery parses an Elasticsearch query from the request body.
func ParseESQuery(body io.Reader) (*ESQuery, error) {
	rawBody, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}

	if len(rawBody) == 0 {
		// According to ES docs, an empty request body is valid and often means match_all & no aggs.
		// So, we return an empty ESQuery struct instead of an error.
		return &ESQuery{}, nil
	}

	var esq ESQuery
	if err := json.Unmarshal(rawBody, &esq); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ES query JSON: %w", err)
	}

	// Due to using json.RawMessage in BoolQuery, we need to parse those fields if they exist.
	// This is a simplified approach; a full implementation would recursively parse these.
	// For now, we'll leave them as RawMessage and subsequent processing steps would handle them.
	// This keeps the initial parsing step relatively straightforward.

	return &esq, nil
}
