package esquery

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestFormatESResponse(t *testing.T) {
	t.Helper()

	// Sample results for testing
	sampleResults := []map[string]string{
		{"_id": "1", "message": "Hello World", "user": "userA", "_stream_id": "stream1", "_log_offset": "10"},
		{"_id": "2", "message": "Another log", "user": "userB", "_stream_id": "stream2", "_log_offset": "20"},
	}
	minimalESQuery := &ESQuery{} // A minimal query, can be nil if FormatESResponse handles it

	t.Run("SuccessCase", func(t *testing.T) {
		totalHits := int64(len(sampleResults))
		execTime := int64(15) // 15ms

		jsonBytes, err := FormatESResponse(sampleResults, minimalESQuery, totalHits, execTime, nil)
		if err != nil {
			t.Fatalf("FormatESResponse failed: %v", err)
		}

		var response ESResponse
		if err := json.Unmarshal(jsonBytes, &response); err != nil {
			t.Fatalf("Failed to unmarshal response JSON: %v. JSON was: %s", err, string(jsonBytes))
		}

		// Assert top-level fields
		if response.Took != execTime {
			t.Errorf("Expected Took %d, got %d", execTime, response.Took)
		}
		if response.TimedOut != false {
			t.Errorf("Expected TimedOut false, got %v", response.TimedOut)
		}
		if response.Error != nil {
			t.Errorf("Expected no error in response, got %+v", response.Error)
		}

		// Assert _shards
		expectedShards := &ESShardsInfo{Total: 1, Successful: 1, Skipped: 0, Failed: 0}
		if !reflect.DeepEqual(response.Shards, expectedShards) {
			t.Errorf("Expected Shards %+v, got %+v", expectedShards, response.Shards)
		}

		// Assert hits.total
		if response.Hits == nil {
			t.Fatalf("response.Hits is nil")
		}
		if response.Hits.Total == nil {
			t.Fatalf("response.Hits.Total is nil")
		}
		if response.Hits.Total.Value != totalHits {
			t.Errorf("Expected Hits.Total.Value %d, got %d", totalHits, response.Hits.Total.Value)
		}
		if response.Hits.Total.Relation != "eq" { // Default relation
			t.Errorf("Expected Hits.Total.Relation 'eq', got '%s'", response.Hits.Total.Relation)
		}

		// Assert hits.hits (content)
		if len(response.Hits.Hits) != len(sampleResults) {
			t.Errorf("Expected %d hits, got %d", len(sampleResults), len(response.Hits.Hits))
		}

		for i, hit := range response.Hits.Hits {
			expectedSource := make(map[string]interface{})
			for k, v := range sampleResults[i] {
				expectedSource[k] = v // In current FormatESResponse, values remain strings
			}

			if !reflect.DeepEqual(hit.Source, expectedSource) {
				t.Errorf("Hit %d: Expected _source %+v, got %+v", i, expectedSource, hit.Source)
			}
			if hit.Index != "victorialogs" {
				t.Errorf("Hit %d: Expected _index 'victorialogs', got '%s'", i, hit.Index)
			}
			if hit.Type != "_doc" {
				t.Errorf("Hit %d: Expected _type '_doc', got '%s'", i, hit.Type)
			}
			// Check _id if it was present in sampleResults
			if expectedID, ok := sampleResults[i]["_id"]; ok {
				if hit.ID != expectedID {
					t.Errorf("Hit %d: Expected _id '%s', got '%s'", i, expectedID, hit.ID)
				}
			}
		}

		// Assert aggregations (should be nil)
		if response.Aggregations != nil {
			t.Errorf("Expected Aggregations to be nil, got %+v", response.Aggregations)
		}
	})

	t.Run("ErrorCase", func(t *testing.T) {
		queryErr := fmt.Errorf("simulated query execution error: connection refused")
		execTime := int64(5) // 5ms

		jsonBytes, err := FormatESResponse(nil, minimalESQuery, 0, execTime, queryErr)
		if err != nil {
			t.Fatalf("FormatESResponse failed: %v", err)
		}

		var response ESResponse
		if err := json.Unmarshal(jsonBytes, &response); err != nil {
			t.Fatalf("Failed to unmarshal error response JSON: %v. JSON was: %s", err, string(jsonBytes))
		}

		if response.Error == nil {
			t.Fatalf("Expected error in response, but response.Error is nil")
		}
		if response.Error.Type != "query_execution_exception" {
			t.Errorf("Expected Error.Type 'query_execution_exception', got '%s'", response.Error.Type)
		}
		if response.Error.Reason != queryErr.Error() {
			t.Errorf("Expected Error.Reason '%s', got '%s'", queryErr.Error(), response.Error.Reason)
		}

		// Check that _shards is still present as per FormatESResponse logic
		if response.Shards == nil {
			t.Fatalf("Expected _shards info even on error, but got nil")
		}
		if response.Shards.Failed != 1 || response.Shards.Successful != 0 {
			t.Errorf("Expected Shards to indicate failure, got %+v", response.Shards)
		}

		// Hits should be nil or empty when there's a top-level error
		if response.Hits != nil && (len(response.Hits.Hits) > 0 || response.Hits.Total.Value > 0) {
			t.Errorf("Expected Hits to be nil or empty on error, got %+v", response.Hits)
		}
	})

	t.Run("SuccessCaseWithNilQueryAndResults", func(t *testing.T) {
		totalHits := int64(0)
		execTime := int64(2)

		jsonBytes, err := FormatESResponse(nil, nil, totalHits, execTime, nil)
		if err != nil {
			t.Fatalf("FormatESResponse failed: %v", err)
		}

		var response ESResponse
		if err := json.Unmarshal(jsonBytes, &response); err != nil {
			t.Fatalf("Failed to unmarshal response JSON: %v. JSON was: %s", err, string(jsonBytes))
		}

		if response.Took != execTime {
			t.Errorf("Expected Took %d, got %d", execTime, response.Took)
		}
		if response.Hits.Total.Value != 0 {
			t.Errorf("Expected Hits.Total.Value 0, got %d", response.Hits.Total.Value)
		}
		if len(response.Hits.Hits) != 0 {
			t.Errorf("Expected 0 hits, got %d", len(response.Hits.Hits))
		}
	})
}
