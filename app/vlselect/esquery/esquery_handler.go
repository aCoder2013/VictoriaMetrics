package esquery

import (
	// "context" // No longer needed here as r.Context() is used
	"fmt"
	"net/http"
	"time"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/logger"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/logstorage" // Added for GetTenantIDFromRequest
)

// HandleESQuery is the main entry point for processing Elasticsearch queries.
func HandleESQuery(w http.ResponseWriter, r *http.Request) {
	logger.Infof("HandleESQuery called for path: %s", r.URL.Path)

	parsedESQuery, err := ParseESQuery(r.Body)
	if err != nil {
		logger.Errorf("Failed to parse ES query: %s", err)
		http.Error(w, fmt.Sprintf("Failed to parse ES query: %s", err), http.StatusBadRequest)
		return
	}
	logger.Infof("Successfully parsed ES query: %+v", parsedESQuery)

	timestamp := time.Now().UnixNano()
	translatedLogStorageQuery, err := TranslateESQueryToLogStorageQuery(timestamp, parsedESQuery)
	if err != nil {
		logger.Errorf("Failed to translate ES query to LogStorage query: %s", err)
		http.Error(w, fmt.Sprintf("Failed to translate ES query: %s", err), http.StatusInternalServerError)
		return
	}
	logQueryStr := translatedLogStorageQuery.String()
	logger.Infof("Successfully translated ES query to LogStorage query: %s", logQueryStr)

	// Execute the query
	// TODO: Properly handle multi-tenancy if `selectTenants` is used. For now, using default tenant ID.
	// tenantIDs, err := logstorage.GetTenantIDsFromRequest(r)
	// For single-node VictoriaLogs, GetTenantIDFromRequest is often used.
	tenantID, err := logstorage.GetTenantIDFromRequest(r)
	if err != nil {
		logger.Errorf("Failed to get tenantID from request: %s", err)
		http.Error(w, fmt.Sprintf("Failed to get tenantID: %s", err), http.StatusInternalServerError)
		return
	}
	tenantIDs := []logstorage.TenantID{tenantID} // ExecuteLogStorageQuery expects a slice

	ctx := r.Context()      // Use request context
	startTime := time.Now() // Record start time for execution duration

	results, err := ExecuteLogStorageQuery(ctx, tenantIDs, translatedLogStorageQuery)

	executionTimeMs := time.Since(startTime).Milliseconds()

	// err from ExecuteLogStorageQuery will be passed to FormatESResponse to be handled as an ESError
	// totalHitsValue is len(results) for now. This might need refinement if pagination/limits mean more results exist.
	responseBytes, formatErr := FormatESResponse(results, parsedESQuery, int64(len(results)), executionTimeMs, err)
	if formatErr != nil {
		logger.Errorf("Failed to format ES response: %s", formatErr)
		// If err (from ExecuteLogStorageQuery) was already set, it's been handled by FormatESResponse.
		// This new error is specifically from formatting.
		http.Error(w, fmt.Sprintf("Failed to format response: %s", formatErr), http.StatusInternalServerError)
		return
	}

	if err != nil { // queryErr was handled by FormatESResponse, but we still log the original execution error
		logger.Errorf("Failed to execute LogStorage query (error formatted into ES response): %s", err)
	} else {
		logger.Infof("Successfully executed LogStorage query. Got %d results. Took %d ms.", len(results), executionTimeMs)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if _, writeErr := w.Write(responseBytes); writeErr != nil {
		logger.Errorf("Failed to write ES response: %s", writeErr)
		// Client might have disconnected, not much we can do here.
	}
}
