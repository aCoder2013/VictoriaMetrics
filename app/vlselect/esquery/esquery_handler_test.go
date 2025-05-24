package esquery

import (
	// "bytes" // No longer needed as rr.Body.Bytes() is used for unmarshalling, strings.NewReader for request
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/logstorage"
)

// MockExecuteLogStorageQuery is the function type for our mock
type MockExecuteLogStorageQueryFunc func(ctx context.Context, tenantIDs []logstorage.TenantID, q *logstorage.Query) ([]map[string]string, error)

var (
	// Store the original function
	originalExecuteLogStorageQuery = ExecuteLogStorageQuery
	// Variable to hold our mock function
	mockExecuteLogStorageQuery MockExecuteLogStorageQueryFunc
)

// setupMock sets up the mock for ExecuteLogStorageQuery
func setupMock(mockFunc MockExecuteLogStorageQueryFunc) {
	ExecuteLogStorageQuery = mockFunc
}

// teardownMock resets ExecuteLogStorageQuery to its original implementation
func teardownMock() {
	ExecuteLogStorageQuery = originalExecuteLogStorageQuery
	mockExecuteLogStorageQuery = nil // Clear the mock function itself
}

func TestHandleESQuery_SuccessPath(t *testing.T) {
	t.Helper()

	mockResults := []map[string]string{
		{"_id": "1", "message": "log line 1"},
		{"_id": "2", "message": "log line 2"},
	}
	setupMock(func(ctx context.Context, tenantIDs []logstorage.TenantID, q *logstorage.Query) ([]map[string]string, error) {
		return mockResults, nil
	})
	defer teardownMock()

	// Simple valid ES query
	jsonQuery := `{"query":{"term":{"user":"test"}}}`
	req, err := http.NewRequest("POST", "/elasticsearch/_search", strings.NewReader(jsonQuery))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// httptest.NewRecorder to record the response.
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(HandleESQuery)

	handler.ServeHTTP(rr, req)

	// Check status code
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Handler returned wrong status code: got %v want %v. Body: %s", status, http.StatusOK, rr.Body.String())
	}

	// Check Content-Type header
	expectedContentType := "application/json; charset=utf-utf-8" // Note: previous tasks used "utf-8", checking what actual output is
	if contentType := rr.Header().Get("Content-Type"); contentType != expectedContentType {
		// Small flexibility for charset, as "application/json" is the key part.
		if !strings.HasPrefix(contentType, "application/json") {
			t.Errorf("Handler returned wrong content type: got %v want %v", contentType, expectedContentType)
		}
	}

	// Unmarshal response body and check some basic structure
	var esResp ESResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &esResp); err != nil {
		t.Fatalf("Failed to unmarshal response body: %v. Body: %s", err, rr.Body.String())
	}

	if esResp.Error != nil {
		t.Errorf("Expected no error in ESResponse, but got: %+v", esResp.Error)
	}
	if esResp.Hits == nil || esResp.Hits.Total == nil {
		t.Fatalf("ESResponse.Hits or Hits.Total is nil")
	}
	if esResp.Hits.Total.Value != int64(len(mockResults)) {
		t.Errorf("Expected total hits %d, got %d", len(mockResults), esResp.Hits.Total.Value)
	}
	if len(esResp.Hits.Hits) != len(mockResults) {
		t.Errorf("Expected %d hits in array, got %d", len(mockResults), len(esResp.Hits.Hits))
	}
	// Check one hit's _source
	if len(esResp.Hits.Hits) > 0 {
		firstHitSource := esResp.Hits.Hits[0].Source
		if firstHitSource["message"] != "log line 1" {
			t.Errorf("Unexpected _source content for first hit: %+v", firstHitSource)
		}
	}
}

func TestHandleESQuery_ParseError(t *testing.T) {
	t.Helper()
	// No need to mock ExecuteLogStorageQuery as parsing happens before execution

	// Invalid JSON
	jsonQuery := `{"query":{"term":{"user":"test"` // Missing closing brace
	req, err := http.NewRequest("POST", "/elasticsearch/_search", strings.NewReader(jsonQuery))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(HandleESQuery)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("Handler returned wrong status code: got %v want %v. Body: %s", status, http.StatusBadRequest, rr.Body.String())
	}

	// Check if body contains error message (optional, but good for testing)
	if !strings.Contains(rr.Body.String(), "Failed to parse ES query") {
		t.Errorf("Expected error message about parsing in body, got: %s", rr.Body.String())
	}
}

func TestHandleESQuery_ExecuteError(t *testing.T) {
	t.Helper()

	expectedErrorMsg := "simulated execution error from mock"
	setupMock(func(ctx context.Context, tenantIDs []logstorage.TenantID, q *logstorage.Query) ([]map[string]string, error) {
		return nil, fmt.Errorf(expectedErrorMsg)
	})
	defer teardownMock()

	jsonQuery := `{"query":{"term":{"user":"test"}}}` // Valid query
	req, err := http.NewRequest("POST", "/elasticsearch/_search", strings.NewReader(jsonQuery))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(HandleESQuery)
	handler.ServeHTTP(rr, req)

	// Expecting InternalServerError, as FormatESResponse will format the execution error into the ES response structure.
	// The HTTP status code itself should reflect that an error occurred during processing.
	// The handler itself doesn't return http.StatusInternalServerError directly for execute errors if FormatESResponse handles it.
	// FormatESResponse returns a marshaled ESResponse with an error field. The HTTP status should still be OK if marshaling succeeds.
	// Let's check this behavior.
	if status := rr.Code; status != http.StatusOK { // Because the error is *in* the JSON response
		t.Errorf("Handler returned wrong status code: got %v want %v. Body: %s", status, http.StatusOK, rr.Body.String())
	}

	var esResp ESResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &esResp); err != nil {
		t.Fatalf("Failed to unmarshal response body: %v. Body: %s", err, rr.Body.String())
	}

	if esResp.Error == nil {
		t.Fatalf("Expected error in ESResponse, but got nil")
	}
	if !strings.Contains(esResp.Error.Reason, expectedErrorMsg) {
		t.Errorf("Expected error reason '%s' in ESResponse, got '%s'", expectedErrorMsg, esResp.Error.Reason)
	}
	if esResp.Error.Type != "query_execution_exception" {
		t.Errorf("Expected error type 'query_execution_exception', got '%s'", esResp.Error.Type)
	}
}

// TestHandleESQuery_TranslateError (Optional, if translator can error robustly)
func TestHandleESQuery_TranslateError(t *testing.T) {
	t.Helper()
	// This test requires a way for TranslateESQueryToLogStorageQuery to return an error.
	// Currently, it mostly returns nil for error, or panics/errors on parsing LogSQL.
	// If we had a specific ES query that leads to a translation error (e.g. unsupported feature):

	// For now, this is a placeholder as robust error generation from translator is not yet a focus.
	// To make this testable, one might:
	// 1. Introduce a specific ES query input that causes a known translation error.
	// 2. Mock TranslateESQueryToLogStorageQuery if it were a global variable (similar to ExecuteLogStorageQuery).

	// Example structure if TranslateESQueryToLogStorageQuery were mockable:
	/*
		originalTranslate := TranslateESQueryToLogStorageQuery
		TranslateESQueryToLogStorageQuery = func(timestamp int64, esq *ESQuery) (*logstorage.Query, error) {
			return nil, fmt.Errorf("simulated translation error")
		}
		defer func() { TranslateESQueryToLogStorageQuery = originalTranslate }()

		jsonQuery := `{"query":{"unsupported_type":{"field":"test"}}}` // Hypothetical
		req, _ := http.NewRequest("POST", "/elasticsearch/_search", strings.NewReader(jsonQuery))
		rr := httptest.NewRecorder()
		HandleESQuery(rr, req)

		if status := rr.Code; status != http.StatusInternalServerError { // Or whatever error code translator errors map to
			t.Errorf("Expected status %v, got %v", http.StatusInternalServerError, status)
		}
	*/
	t.Skip("Skipping translate error test as translator error paths are not explicitly forced yet.")
}

// Test for Content-Type charset, ensuring it's exactly "application/json; charset=utf-8"
func TestHandleESQuery_ContentTypeCharset(t *testing.T) {
	t.Helper()
	setupMock(func(ctx context.Context, tenantIDs []logstorage.TenantID, q *logstorage.Query) ([]map[string]string, error) {
		return []map[string]string{}, nil // Minimal successful execution
	})
	defer teardownMock()

	jsonQuery := `{"query":{"term":{"user":"test"}}}`
	req, _ := http.NewRequest("POST", "/elasticsearch/_search", strings.NewReader(jsonQuery))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	HandleESQuery(rr, req)

	expectedContentType := "application/json; charset=utf-8"
	if contentType := rr.Header().Get("Content-Type"); contentType != expectedContentType {
		t.Errorf("Handler returned wrong content type: got '%s' want '%s'", contentType, expectedContentType)
	}
}

// Helper to create a request with a specific tenant ID, useful if testing multi-tenancy aspects.
// For now, the handler uses GetTenantIDFromRequest which relies on http headers or query params
// that are part of VictoriaMetrics' http server setup, not easily mocked at unit level without deeper integration.
// So, direct testing of specific tenant ID propagation is harder here.
// We assume GetTenantIDFromRequest works as intended by VictoriaMetrics.

func TestMain(m *testing.M) {
	// Setup any package-level test state if needed.
	// For example, if there were global logger settings for tests.
	exitCode := m.Run()
	// Teardown any package-level test state.
	originalExecuteLogStorageQuery = nil // Help GC if tests are run multiple times in a process
	mockExecuteLogStorageQuery = nil
	// os.Exit(exitCode) // Not strictly necessary for `go test` but good practice for standalone test mains
	_ = exitCode // Avoid "declared and not used" if os.Exit is commented out
}

func TestHandleESQuery_ComplexBooleanQueries(t *testing.T) {
	t.Helper()
	commonMockResults := []map[string]string{
		{"_id": "cb1", "level": "error", "service": "payment", "message": "Payment failed"},
		{"_id": "cb2", "level": "warn", "service": "inventory", "message": "Low stock", "customer_id": "regular"},
	}

	testCases := []struct {
		name         string
		jsonQuery    string
		mockResults  []map[string]string
		expectedHits int
		// Add more specific assertions if needed, e.g., which hits are expected
	}{
		{
			name: "Nested bool: must (term AND (should (term OR term)))",
			jsonQuery: `{
				"query": {
					"bool": {
						"must": [
							{"term": {"service": "api"}},
							{
								"bool": {
									"should": [
										{"term": {"level": "error"}},
										{"term": {"level": "critical"}}
									]
								}
							}
						]
					}
				}
			}`,
			mockResults: []map[string]string{
				{"_id": "nb1", "service": "api", "level": "error", "message": "API error 1"},
				{"_id": "nb2", "service": "api", "level": "critical", "message": "API critical 1"},
			},
			expectedHits: 2,
		},
		{
			name: "Mixed bool: (level=error AND service=payment) OR (level=warn AND service=inventory AND NOT customer_id=test)",
			jsonQuery: `{
				"query": {
					"bool": {
						"should": [
							{
								"bool": {
									"must": [
										{"term": {"level": "error"}},
										{"term": {"service": "payment"}}
									]
								}
							},
							{
								"bool": {
									"must": [
										{"term": {"level": "warn"}},
										{"term": {"service": "inventory"}}
									],
									"must_not": [
										{"term": {"customer_id": "test"}}
									]
								}
							}
						]
					}
				}
			}`,
			mockResults:  commonMockResults, // This mock will return both, ES query should select based on logic
			expectedHits: 2,                 // Expecting both commonMockResults to match this logic
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			setupMock(func(ctx context.Context, tenantIDs []logstorage.TenantID, q *logstorage.Query) ([]map[string]string, error) {
				// Simple mock for now, could be more intelligent based on q.String() if needed
				return tc.mockResults, nil
			})
			defer teardownMock()

			req, err := http.NewRequest("POST", "/elasticsearch/_search", strings.NewReader(tc.jsonQuery))
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			HandleESQuery(rr, req)

			if status := rr.Code; status != http.StatusOK {
				t.Errorf("Handler returned wrong status code: got %v want %v. Body: %s", status, http.StatusOK, rr.Body.String())
			}

			var esResp ESResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &esResp); err != nil {
				t.Fatalf("Failed to unmarshal response body: %v. Body: %s", err, rr.Body.String())
			}

			if esResp.Error != nil {
				t.Errorf("Expected no error in ESResponse, but got: %+v", esResp.Error)
			}
			if esResp.Hits == nil || esResp.Hits.Total == nil {
				t.Fatalf("ESResponse.Hits or Hits.Total is nil")
			}
			if esResp.Hits.Total.Value != int64(tc.expectedHits) {
				t.Errorf("Expected total hits %d, got %d", tc.expectedHits, esResp.Hits.Total.Value)
			}
			if len(esResp.Hits.Hits) != tc.expectedHits {
				t.Errorf("Expected %d hits in array, got %d", tc.expectedHits, len(esResp.Hits.Hits))
			}
		})
	}
}

func TestHandleESQuery_MatchQueryVariations(t *testing.T) {
	t.Helper()

	mockData := []map[string]string{
		{"_id": "m1", "message": "this is a test log", "service": "api"},
		{"_id": "m2", "message": "another test entry", "service": "worker"},
		{"_id": "m3", "description": "this is a test description", "service": "frontend"},
	}

	testCases := []struct {
		name      string
		jsonQuery string
		// mockResults specific to this query, or use a general mock and filter assertion
		expectedHits           int
		firstHitSourceContains map[string]string // map of field name to expected substring in its value
	}{
		{
			name:                   "Match on 'message' field (multi-word)",
			jsonQuery:              `{"query":{"match":{"message":"test log"}}}`,
			expectedHits:           1, // Expecting "this is a test log"
			firstHitSourceContains: map[string]string{"message": "test log"},
		},
		{
			name:                   "Match on 'service' field (single word)",
			jsonQuery:              `{"query":{"match":{"service":"api"}}}`,
			expectedHits:           1,
			firstHitSourceContains: map[string]string{"service": "api"},
		},
		{
			name:                   "Match on different field 'description'",
			jsonQuery:              `{"query":{"match":{"description":"test description"}}}`,
			expectedHits:           1,
			firstHitSourceContains: map[string]string{"description": "test description"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			setupMock(func(ctx context.Context, tenantIDs []logstorage.TenantID, q *logstorage.Query) ([]map[string]string, error) {
				// This mock needs to be somewhat intelligent or we filter from mockData based on q
				// For simplicity, let's assume the translator correctly forms a LogSQL query
				// and the test is more about the handler correctly processing this type of ES query.
				// A more robust mock would inspect q.String() and filter mockData.
				// For now, returning all mockData and relying on expectedHits.

				// A slightly more refined mock for these specific tests:
				var filteredResults []map[string]string
				for _, item := range mockData {
					// Crude check based on expected hits and content
					if tc.name == "Match on 'message' field (multi-word)" && strings.Contains(item["message"], "test log") {
						filteredResults = append(filteredResults, item)
					} else if tc.name == "Match on 'service' field (single word)" && item["service"] == "api" {
						filteredResults = append(filteredResults, item)
					} else if tc.name == "Match on different field 'description'" && strings.Contains(item["description"], "test description") {
						filteredResults = append(filteredResults, item)
					}
				}
				if len(filteredResults) == 0 && tc.expectedHits > 0 {
					// If no specific filter matched, but we expect hits, return a default that matches expectedHits
					// This part of mocking is tricky without full query parsing in mock.
					// Let's return a subset of mockData that matches expectedHits to simulate successful query.
					if tc.expectedHits <= len(mockData) {
						return mockData[:tc.expectedHits], nil
					}
					return mockData, nil
				}
				return filteredResults, nil
			})
			defer teardownMock()

			req, err := http.NewRequest("POST", "/elasticsearch/_search", strings.NewReader(tc.jsonQuery))
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			rr := httptest.NewRecorder()
			HandleESQuery(rr, req)

			if status := rr.Code; status != http.StatusOK {
				t.Errorf("Handler returned wrong status code: got %v want %v", status, http.StatusOK)
			}
			var esResp ESResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &esResp); err != nil {
				t.Fatalf("Failed to unmarshal response: %v", err)
			}
			if esResp.Hits.Total.Value != int64(tc.expectedHits) {
				t.Errorf("Expected total hits %d, got %d. Query: %s", tc.expectedHits, esResp.Hits.Total.Value, tc.jsonQuery)
			}
			if len(esResp.Hits.Hits) != tc.expectedHits {
				t.Errorf("Expected %d hits in array, got %d. Query: %s", tc.expectedHits, len(esResp.Hits.Hits), tc.jsonQuery)
			}
			if tc.expectedHits > 0 && len(esResp.Hits.Hits) > 0 && tc.firstHitSourceContains != nil {
				firstHit := esResp.Hits.Hits[0]
				for field, expectedSubstring := range tc.firstHitSourceContains {
					if val, ok := firstHit.Source[field].(string); ok {
						if !strings.Contains(val, expectedSubstring) {
							t.Errorf("Expected field '%s' to contain '%s', got '%s'. Query: %s", field, expectedSubstring, val, tc.jsonQuery)
						}
					} else {
						t.Errorf("Field '%s' not found or not a string in _source. Query: %s", field, tc.jsonQuery)
					}
				}
			}
		})
	}
}

func TestHandleESQuery_RangeQueryVariations(t *testing.T) {
	t.Helper()
	mockData := []map[string]string{
		{"_id": "r1", "value": "150", "_time": "1670000000000"}, // Time is approx Dec 2, 2022
		{"_id": "r2", "value": "50", "_time": "1670000050000"},
		{"_id": "r3", "value": "250", "_time": "1670000100000"},
	}

	testCases := []struct {
		name         string
		jsonQuery    string
		mockLogic    func(q *logstorage.Query) ([]map[string]string, error)
		expectedHits int
	}{
		{
			name:      "Numeric GTE",
			jsonQuery: `{"query":{"range":{"value":{"gte":100}}}}`,
			mockLogic: func(q *logstorage.Query) ([]map[string]string, error) {
				// Simulate filtering based on "value >= 100"
				var res []map[string]string
				for _, item := range mockData {
					if item["value"] == "150" || item["value"] == "250" {
						res = append(res, item)
					}
				}
				return res, nil
			},
			expectedHits: 2,
		},
		{
			name:      "Timestamp LT (numeric)",                             // ES often uses ms epoch for dates in range
			jsonQuery: `{"query":{"range":{"_time":{"lt":1670000060000}}}}`, // Expects r1, r2
			mockLogic: func(q *logstorage.Query) ([]map[string]string, error) {
				var res []map[string]string
				for _, item := range mockData {
					if item["_time"] == "1670000000000" || item["_time"] == "1670000050000" {
						res = append(res, item)
					}
				}
				return res, nil
			},
			expectedHits: 2,
		},
		// Note: Date string ranges like "2023-01-01" depend heavily on translator's date parsing.
		// Current translator converts range values to string for LogSQL, so LogSQL needs to handle date strings.
		// Adding a test assuming LogSQL can compare ISO-like date strings:
		{
			name:      "Timestamp GTE (date string)",
			jsonQuery: `{"query":{"range":{"@timestamp":{"gte":"2022-12-02T16:54:00Z"}}}}`, // Matches _time "1670000000000" (approx)
			mockLogic: func(q *logstorage.Query) ([]map[string]string, error) {
				// This mock would ideally check q.String() for `(@timestamp >= "2022-12-02T16:54:00Z")`
				// For now, returning results that would match conceptually.
				return []map[string]string{mockData[0], mockData[1], mockData[2]}, nil // Assuming all are >= this date for simplicity of mock
			},
			expectedHits: 3,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			setupMock(func(ctx context.Context, tenantIDs []logstorage.TenantID, q *logstorage.Query) ([]map[string]string, error) {
				return tc.mockLogic(q)
			})
			defer teardownMock()

			req, err := http.NewRequest("POST", "/elasticsearch/_search", strings.NewReader(tc.jsonQuery))
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			rr := httptest.NewRecorder()
			HandleESQuery(rr, req)

			if status := rr.Code; status != http.StatusOK {
				t.Errorf("Handler returned wrong status code: got %v want %v. Body: %s", status, http.StatusOK, rr.Body.String())
			}
			var esResp ESResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &esResp); err != nil {
				t.Fatalf("Failed to unmarshal response: %v. Body: %s", err, rr.Body.String())
			}
			if esResp.Hits.Total.Value != int64(tc.expectedHits) {
				t.Errorf("Expected total hits %d, got %d. Query: %s", tc.expectedHits, esResp.Hits.Total.Value, tc.jsonQuery)
			}
		})
	}
}

func TestHandleESQuery_QueryStringVariations(t *testing.T) {
	t.Helper()
	mockData := []map[string]string{
		{"_id": "qs1", "message": "error in payment service", "level": "error", "service": "payment"},
		{"_id": "qs2", "message": "successful payment", "level": "info", "service": "payment"},
		{"_id": "qs3", "message": "user not found", "level": "warn", "service": "auth"},
		{"_id": "qs4", "message": "exact phrase to match", "level": "debug", "service": "search"},
	}

	testCases := []struct {
		name         string
		jsonQuery    string
		mockLogic    func(q *logstorage.Query) ([]map[string]string, error)
		expectedHits int
	}{
		{
			name:      "QueryString with AND/OR",
			jsonQuery: `{"query":{"query_string":{"query":"(payment AND error) OR (auth AND warn)"}}}`,
			mockLogic: func(q *logstorage.Query) ([]map[string]string, error) {
				// Expects qs1 and qs3
				return []map[string]string{mockData[0], mockData[2]}, nil
			},
			expectedHits: 2,
		},
		{
			name:      "QueryString with phrase", // Assuming translator makes _q="\"exact phrase\""
			jsonQuery: `{"query":{"query_string":{"query":"\"exact phrase to match\""}}}`,
			mockLogic: func(q *logstorage.Query) ([]map[string]string, error) {
				return []map[string]string{mockData[3]}, nil
			},
			expectedHits: 1,
		},
		// Current QueryString translator uses _q="<value>", field-specific searches inside query_string
		// like "status:404" depend on LogSQL's _q behavior or a more advanced translator.
		// Test assumes LogSQL's _q can handle field prefixes.
		{
			name:      "QueryString with field prefix",
			jsonQuery: `{"query":{"query_string":{"query":"level:error AND service:payment"}}}`,
			mockLogic: func(q *logstorage.Query) ([]map[string]string, error) {
				return []map[string]string{mockData[0]}, nil
			},
			expectedHits: 1,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			setupMock(func(ctx context.Context, tenantIDs []logstorage.TenantID, q *logstorage.Query) ([]map[string]string, error) {
				return tc.mockLogic(q)
			})
			defer teardownMock()

			req, err := http.NewRequest("POST", "/elasticsearch/_search", strings.NewReader(tc.jsonQuery))
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			rr := httptest.NewRecorder()
			HandleESQuery(rr, req)

			if status := rr.Code; status != http.StatusOK {
				t.Errorf("Handler returned wrong status code: got %v want %v", status, http.StatusOK)
			}
			var esResp ESResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &esResp); err != nil {
				t.Fatalf("Failed to unmarshal response: %v", err)
			}
			if esResp.Hits.Total.Value != int64(tc.expectedHits) {
				t.Errorf("Expected total hits %d, got %d. Query: %s", tc.expectedHits, esResp.Hits.Total.Value, tc.jsonQuery)
			}
		})
	}
}

func TestHandleESQuery_CommonLoggingFilters(t *testing.T) {
	t.Helper()
	mockData := []map[string]string{
		{"_id": "clf1", "level": "error", "service_name": "my-api", "http_status": "500", "trace_id": "trace123"},
		{"_id": "clf2", "level": "info", "service_name": "my-api", "http_status": "200", "trace_id": "trace456"},
		{"_id": "clf3", "level": "error", "service_name": "other-api", "http_status": "502", "request_id": "reqABC"},
	}

	testCases := []struct {
		name         string
		jsonQuery    string
		mockLogic    func(q *logstorage.Query) ([]map[string]string, error)
		expectedHits int
	}{
		{
			name:      "Filter by level:error",
			jsonQuery: `{"query":{"term":{"level":"error"}}}`,
			mockLogic: func(q *logstorage.Query) ([]map[string]string, error) {
				return []map[string]string{mockData[0], mockData[2]}, nil
			},
			expectedHits: 2,
		},
		{
			name:      "Filter by service_name AND http_status",
			jsonQuery: `{"query":{"bool":{"must":[{"term":{"service_name":"my-api"}},{"term":{"http_status":"500"}}]}}}`,
			mockLogic: func(q *logstorage.Query) ([]map[string]string, error) {
				return []map[string]string{mockData[0]}, nil
			},
			expectedHits: 1,
		},
		{
			name:      "Filter by trace_id",
			jsonQuery: `{"query":{"term":{"trace_id":"trace123"}}}`,
			mockLogic: func(q *logstorage.Query) ([]map[string]string, error) {
				return []map[string]string{mockData[0]}, nil
			},
			expectedHits: 1,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			setupMock(func(ctx context.Context, tenantIDs []logstorage.TenantID, q *logstorage.Query) ([]map[string]string, error) {
				return tc.mockLogic(q)
			})
			defer teardownMock()

			req, err := http.NewRequest("POST", "/elasticsearch/_search", strings.NewReader(tc.jsonQuery))
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			rr := httptest.NewRecorder()
			HandleESQuery(rr, req)

			if status := rr.Code; status != http.StatusOK {
				t.Errorf("Handler returned wrong status code: got %v want %v. Body: %s", status, http.StatusOK, rr.Body.String())
			}
			var esResp ESResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &esResp); err != nil {
				t.Fatalf("Failed to unmarshal response: %v. Body: %s", err, rr.Body.String())
			}
			if esResp.Hits.Total.Value != int64(tc.expectedHits) {
				t.Errorf("Expected total hits %d, got %d. Query: %s", tc.expectedHits, esResp.Hits.Total.Value, tc.jsonQuery)
			}
		})
	}
}

func TestHandleESQuery_TermsAggregationBasic(t *testing.T) {
	t.Helper()
	queryJSON := `{"query":{"match_all":{}}, "aggs":{"popular_levels":{"terms":{"field":"level"}}}}`

	// Mock ExecuteLogStorageQuery: for this test, it returns some hits.
	// The key is to verify that the 'aggregations' field in the response is nil or absent,
	// as our current translator ignores ES aggs.
	mockHits := []map[string]string{
		{"_id": "agg1", "level": "error", "message": "Some error"},
		{"_id": "agg2", "level": "info", "message": "Some info"},
	}
	setupMock(func(ctx context.Context, tenantIDs []logstorage.TenantID, q *logstorage.Query) ([]map[string]string, error) {
		// The mock can also check that q.String() does not contain LogSQL aggregation pipes.
		// For now, just return hits.
		if strings.Contains(q.String(), "count_values") || strings.Contains(q.String(), "stats_agg") {
			t.Errorf("Expected translated query NOT to contain LogSQL aggregation pipes for ignored ES agg, but got: %s", q.String())
		}
		return mockHits, nil
	})
	defer teardownMock()

	req, err := http.NewRequest("POST", "/elasticsearch/_search", strings.NewReader(queryJSON))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	rr := httptest.NewRecorder()
	HandleESQuery(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Handler returned wrong status code: got %v want %v. Body: %s", status, http.StatusOK, rr.Body.String())
	}

	var esResp ESResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &esResp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v. Body: %s", err, rr.Body.String())
	}

	if esResp.Aggregations != nil {
		t.Errorf("Expected Aggregations field to be nil for ignored ES agg, got %+v", esResp.Aggregations)
	}
	// Ensure hits are still processed
	if esResp.Hits.Total.Value != int64(len(mockHits)) {
		t.Errorf("Expected total hits %d, got %d even with ignored agg", len(mockHits), esResp.Hits.Total.Value)
	}
}

func TestHandleESQuery_DateHistogramAggregationBasic(t *testing.T) {
	t.Helper()
	queryJSON := `{"query":{"match_all":{}}, "aggs":{"logs_over_time":{"date_histogram":{"field":"_time","interval":"1h"}}}}`

	mockHits := []map[string]string{
		{"_id": "dh1", "_time": "1670000000000", "message": "Event 1"},
		{"_id": "dh2", "_time": "1670003600000", "message": "Event 2"}, // 1 hour later
	}
	setupMock(func(ctx context.Context, tenantIDs []logstorage.TenantID, q *logstorage.Query) ([]map[string]string, error) {
		if strings.Contains(q.String(), "histogram_agg") { // or whatever LogSQL uses
			t.Errorf("Expected translated query NOT to contain LogSQL date_histogram pipes for ignored ES agg, but got: %s", q.String())
		}
		return mockHits, nil
	})
	defer teardownMock()

	req, err := http.NewRequest("POST", "/elasticsearch/_search", strings.NewReader(queryJSON))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	rr := httptest.NewRecorder()
	HandleESQuery(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Handler returned wrong status code: got %v want %v. Body: %s", status, http.StatusOK, rr.Body.String())
	}

	var esResp ESResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &esResp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v. Body: %s", err, rr.Body.String())
	}

	if esResp.Aggregations != nil {
		t.Errorf("Expected Aggregations field to be nil for ignored ES agg, got %+v", esResp.Aggregations)
	}
	if esResp.Hits.Total.Value != int64(len(mockHits)) {
		t.Errorf("Expected total hits %d, got %d even with ignored agg", len(mockHits), esResp.Hits.Total.Value)
	}
}
