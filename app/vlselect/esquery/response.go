package esquery

import (
	"encoding/json"
	// "fmt"     // No longer needed
	// "strings" // No longer needed
	// "time"    // No longer needed
)

// ESResponse is the top-level structure for an Elasticsearch search response.
type ESResponse struct {
	Took         int64                 `json:"took"` // Execution time in milliseconds
	TimedOut     bool                  `json:"timed_out"`
	Shards       *ESShardsInfo         `json:"_shards"`
	Hits         *ESHits               `json:"hits"`
	Aggregations *ESAggregationsResult `json:"aggregations,omitempty"` // Optional
	Error        *ESError              `json:"error,omitempty"`        // For reporting errors in ES format
}

// ESError represents an error structure in ES response.
type ESError struct {
	Type      string    `json:"type"`
	Reason    string    `json:"reason"`
	RootCause []ESError `json:"root_cause,omitempty"`
}

// ESShardsInfo represents shard information.
type ESShardsInfo struct {
	Total      int `json:"total"`
	Successful int `json:"successful"`
	Skipped    int `json:"skipped"`
	Failed     int `json:"failed"`
}

// ESHits contains the actual search hits.
type ESHits struct {
	Total    *ESTotalHits `json:"total"`
	MaxScore *float64     `json:"max_score,omitempty"` // Can be nil if not applicable
	Hits     []*ESHit     `json:"hits"`
}

// ESTotalHits specifies the total number of hits and their relation.
type ESTotalHits struct {
	Value    int64  `json:"value"`
	Relation string `json:"relation"` // "eq", "gte", etc.
}

// ESHit represents a single search hit.
type ESHit struct {
	Index  string                 `json:"_index"`           // Typically the tenantID or a fixed name
	Type   string                 `json:"_type"`            // Default to "_doc"
	ID     string                 `json:"_id,omitempty"`    // Can be auto-generated or from a field
	Score  *float64               `json:"_score,omitempty"` // Can be nil
	Source map[string]interface{} `json:"_source"`          // The actual document
}

// ESAggregationsResult holds the results of aggregations.
// This is a map where keys are aggregation names.
type ESAggregationsResult map[string]interface{} // Values will be specific agg result structs

// ESTermsAggregationResult for "terms" aggregation.
type ESTermsAggregationResult struct {
	DocCountErrorUpperBound int64           `json:"doc_count_error_upper_bound"` // ES type is long
	SumOtherDocCount        int64           `json:"sum_other_doc_count"`         // ES type is long
	Buckets                 []ESTermsBucket `json:"buckets"`
}

// ESTermsBucket represents a bucket in a terms aggregation.
type ESTermsBucket struct {
	Key      interface{} `json:"key"` // string or number
	DocCount int64       `json:"doc_count"`
	// Optional sub-aggregations can be added here
}

// ESDateHistogramAggregationResult for "date_histogram" aggregation.
type ESDateHistogramAggregationResult struct {
	Buckets []ESDateHistogramBucket `json:"buckets"`
}

// ESDateHistogramBucket represents a bucket in a date histogram aggregation.
type ESDateHistogramBucket struct {
	KeyAsString *string `json:"key_as_string,omitempty"`
	Key         int64   `json:"key"` // Timestamp in milliseconds
	DocCount    int64   `json:"doc_count"`
	// Optional sub-aggregations can be added here
}

// FormatESResponse formats the query results into an Elasticsearch JSON response.
func FormatESResponse(results []map[string]string, esQuery *ESQuery, totalHitsValue int64, executionTimeMs int64, queryErr error) ([]byte, error) {
	if queryErr != nil {
		esError := &ESError{
			Type:   "query_execution_exception", // Generic type
			Reason: queryErr.Error(),
			// RootCause could be populated if queryErr wraps more specific errors
		}
		response := &ESResponse{
			Error: esError,
			Shards: &ESShardsInfo{ // Some clients expect _shards info even on error
				Total:      1,
				Successful: 0,
				Failed:     1,
			},
		}
		return json.Marshal(response)
	}

	hits := make([]*ESHit, 0, len(results))
	defaultScore := 1.0 // Placeholder score

	for _, rowMap := range results {
		source := make(map[string]interface{}, len(rowMap))
		for k, v := range rowMap {
			// TODO: Attempt type conversion for numerics/booleans if possible/needed.
			// For now, all values from rowMap (which are string) become interface{} (still string).
			source[k] = v
		}

		// Try to extract an _id field if present
		idFieldValue := ""
		if idVal, ok := source["_id"]; ok {
			idFieldValue, _ = idVal.(string)
		} else if idVal, ok := source["id"]; ok {
			idFieldValue, _ = idVal.(string)
		}

		hit := &ESHit{
			Index:  "victorialogs", // Placeholder index name
			Type:   "_doc",
			ID:     idFieldValue,  // Use extracted ID or empty string
			Score:  &defaultScore, // All hits get a default score for now
			Source: source,
		}
		hits = append(hits, hit)
	}

	// maxScore can be nil if not applicable or all scores are 1.0
	var maxScoreValue *float64
	if len(hits) > 0 {
		maxScoreValue = &defaultScore
	}

	// Determine relation for total hits. If limit was applied and totalHitsValue == limit, it might be "gte".
	// This is a simplification for now.
	relation := "eq"
	if esQuery != nil && esQuery.Size != nil && totalHitsValue == int64(*esQuery.Size) && totalHitsValue < 10000 {
		// If actual hits == size limit, and size < ES default track_total_hits (10k),
		// it's possible there are more, but this is a very rough heuristic.
		// For true "gte", we'd need to know if the query was capped by `size`.
		// For now, using "eq" is safer unless we have explicit info about more hits.
	}

	response := &ESResponse{
		Took:     executionTimeMs,
		TimedOut: false, // Not directly applicable
		Shards: &ESShardsInfo{
			Total:      1,
			Successful: 1,
			Skipped:    0,
			Failed:     0,
		},
		Hits: &ESHits{
			Total: &ESTotalHits{
				Value:    totalHitsValue,
				Relation: relation,
			},
			MaxScore: maxScoreValue,
			Hits:     hits,
		},
		Aggregations: nil, // No aggregation support yet
	}

	return json.Marshal(response)
}
