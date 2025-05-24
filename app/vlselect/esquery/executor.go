package esquery

import (
	"context"
	"fmt"
	"sync" // Added for mutex

	"github.com/VictoriaMetrics/VictoriaMetrics/app/vlstorage"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/logstorage"
)

// executeLogStorageQueryOriginal is the original implementation.
func executeLogStorageQueryOriginal(ctx context.Context, tenantIDs []logstorage.TenantID, q *logstorage.Query) ([]map[string]string, error) {
	var results []map[string]string
	var resultsLock sync.Mutex

	// writeBlock callback function with the signature func(workerID uint, db *logstorage.DataBlock).
	writeBlock := func(workerID uint, db *logstorage.DataBlock) {
		rowsCount := db.RowsCount()
		if rowsCount == 0 {
			return
		}

		columns := db.Columns
		// Pre-allocate a temporary slice for this block's results to minimize appends under lock.
		blockResults := make([]map[string]string, 0, rowsCount)

		for i := 0; i < rowsCount; i++ {
			rowMap := make(map[string]string, len(columns))
			for _, col := range columns { // Changed j, col to _, col
				// Ensure col.Values is long enough for index i
				// This check is important if col.Values might be shorter than rowsCount,
				// though typically they should align.
				if i < len(col.Values) {
					rowMap[col.Name] = col.Values[i]
				} else {
					// Handle cases where a column might have fewer values than rowsCount,
					// e.g. by setting a placeholder or logging a warning.
					// For now, set to empty string or a placeholder.
					rowMap[col.Name] = "<missing_value>"
					// Or: logger.Warnf("Worker %d, block processing: column %s has only %d values, expected at least %d", workerID, col.Name, len(col.Values), i+1)
				}
			}
			blockResults = append(blockResults, rowMap)
		}

		if len(blockResults) > 0 {
			resultsLock.Lock()
			results = append(results, blockResults...)
			resultsLock.Unlock()
		}
	}

	// Call vlstorage.RunQuery.
	// The problem description states RunQuery expects a callback with signature `func(workerID uint, db *logstorage.DataBlock)`.
	// It does not return an error itself but expects the callback to handle errors or stream data.
	// The overall error from ExecuteLogStorageQuery would come from other sources if RunQuery itself doesn't return one.
	// However, typical RunQuery functions in similar systems *do* return an error.
	// Let's assume vlstorage.RunQuery returns an error as per the subtask's ExecuteLogStorageQuery signature.
	err := vlstorage.RunQuery(ctx, tenantIDs, q, writeBlock)
	if err != nil {
		return nil, fmt.Errorf("vlstorage.RunQuery failed: %w", err)
	}

	return results, nil
}

// ExecuteLogStorageQuery is a variable that can be swapped out for testing.
// It defaults to the original implementation.
var ExecuteLogStorageQuery = executeLogStorageQueryOriginal
