package esquery

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	// "time" // Removed as it's not used directly in this file after changes

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/logger" // For warnings
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/logstorage"
)

// TranslateESQueryToLogStorageQuery translates an ESQuery into a logstorage.Query
func TranslateESQueryToLogStorageQuery(timestamp int64, esq *ESQuery) (*logstorage.Query, error) {
	if esq == nil {
		return logstorage.ParseQueryAtTimestamp("*", timestamp)
	}

	if esq.Aggs != nil && len(*esq.Aggs) > 0 {
		logger.Warnf("ES query translation: 'aggs' are not yet implemented")
	}
	if esq.Sort != nil && len(esq.Sort) > 0 {
		logger.Warnf("ES query translation: 'sort' is not yet implemented")
	}

	filterStr, err := translateQuery(esq.Query)
	if err != nil {
		return nil, fmt.Errorf("failed to translate query part: %w", err)
	}

	queryStr := "*"
	if filterStr != "" {
		queryStr = fmt.Sprintf("* | filter %s", filterStr)
	}

	// Append limit and offset pipes to the query string
	if esq.Size != nil {
		queryStr = fmt.Sprintf("%s | limit %d", queryStr, *esq.Size)
	} else {
		// Default limit
		queryStr = fmt.Sprintf("%s | limit %d", queryStr, 10) // Default ES size
	}

	if esq.From != nil {
		queryStr = fmt.Sprintf("%s | offset %d", queryStr, *esq.From)
	}

	lq, err := logstorage.ParseQueryAtTimestamp(queryStr, timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to parse translated LogSQL query '%s': %w", queryStr, err)
	}

	return lq, nil
}

// translateQuery translates the "query" part of an ES query to a LogSQL filter string.
func translateQuery(q *Query) (string, error) {
	if q == nil {
		return "", nil
	}

	var filters []string

	if q.Bool != nil {
		s, err := translateBoolQuery(q.Bool)
		if err != nil {
			return "", err
		}
		if s != "" {
			filters = append(filters, s)
		}
	}

	for field, matchQuery := range q.Match {
		s, err := translateMatchQuery(field, matchQuery)
		if err != nil {
			return "", err
		}
		if s != "" {
			filters = append(filters, s)
		}
	}

	for field, termQuery := range q.Term {
		s, err := translateTermQuery(field, termQuery)
		if err != nil {
			return "", err
		}
		if s != "" {
			filters = append(filters, s)
		}
	}

	for field, rangeQuery := range q.Range {
		s, err := translateRangeQuery(field, rangeQuery)
		if err != nil {
			return "", err
		}
		if s != "" {
			filters = append(filters, s)
		}
	}

	if q.QueryString != nil {
		s, err := translateQueryStringQuery(q.QueryString)
		if err != nil {
			return "", err
		}
		if s != "" {
			filters = append(filters, s)
		}
	}

	if len(filters) == 0 {
		return "", nil
	}
	if len(filters) == 1 {
		return filters[0], nil
	}
	return "(" + strings.Join(filters, " AND ") + ")", nil
}

// translateBoolQuery translates a "bool" query to a LogSQL filter string.
func translateBoolQuery(bq *BoolQuery) (string, error) {
	var mustFilters, filterFilters, shouldFilters, mustNotFiltersStr []string

	translateClauses := func(clauses []json.RawMessage) ([]string, error) {
		var translated []string
		for _, rawClause := range clauses {
			var subQuery Query
			if err := json.Unmarshal(rawClause, &subQuery); err != nil {
				return nil, fmt.Errorf("failed to unmarshal bool sub-query: %w", err)
			}
			s, err := translateQuery(&subQuery)
			if err != nil {
				return nil, err
			}
			if s != "" {
				translated = append(translated, s)
			}
		}
		return translated, nil
	}

	var err error
	if len(bq.Must) > 0 {
		mustFilters, err = translateClauses(bq.Must)
		if err != nil {
			return "", err
		}
	}
	if len(bq.Filter) > 0 {
		filterFilters, err = translateClauses(bq.Filter)
		if err != nil {
			return "", err
		}
	}
	if len(bq.Should) > 0 {
		shouldFilters, err = translateClauses(bq.Should)
		if err != nil {
			return "", err
		}
	}
	if len(bq.MustNot) > 0 {
		rawMustNotFilters, err := translateClauses(bq.MustNot)
		if err != nil {
			return "", err
		}
		for _, fStr := range rawMustNotFilters {
			mustNotFiltersStr = append(mustNotFiltersStr, fmt.Sprintf("NOT (%s)", fStr))
		}
	}

	var allFilters []string
	if len(mustFilters) > 0 {
		allFilters = append(allFilters, "("+strings.Join(mustFilters, " AND ")+")")
	}
	if len(filterFilters) > 0 {
		allFilters = append(allFilters, "("+strings.Join(filterFilters, " AND ")+")")
	}
	if len(mustNotFiltersStr) > 0 {
		allFilters = append(allFilters, "("+strings.Join(mustNotFiltersStr, " AND ")+")")
	}

	if len(shouldFilters) > 0 {
		shouldBlock := "(" + strings.Join(shouldFilters, " OR ") + ")"
		if len(allFilters) > 0 {
			allFilters = append(allFilters, shouldBlock)
		} else {
			return shouldBlock, nil
		}
	}

	if len(allFilters) == 0 {
		return "", nil
	}
	return "(" + strings.Join(allFilters, " AND ") + ")", nil
}

func escapeLogSQLValue(value interface{}) string {
	switch v := value.(type) {
	case string:
		return strconv.Quote(v)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", v)
	case float32, float64:
		return fmt.Sprintf("%f", v)
	case bool:
		return fmt.Sprintf("%t", v)
	default:
		return strconv.Quote(fmt.Sprintf("%v", v))
	}
}

// translateTermQuery translates a "term" query to a LogSQL filter string.
func translateTermQuery(field string, tq TermQuery) (string, error) {
	// Removed logstorage.EscapeFilterStringIfNeeded
	return fmt.Sprintf("%s=%s", field, escapeLogSQLValue(tq.Value)), nil
}

// translateMatchQuery translates a "match" query to a LogSQL filter string.
func translateMatchQuery(field string, mq MatchQuery) (string, error) {
	// Removed logstorage.EscapeFilterStringIfNeeded
	// Using field:"value" for phrase search as used by LogSQL parser.
	return fmt.Sprintf("%s:%s", field, escapeLogSQLValue(mq.Query)), nil
}

// translateRangeQuery translates a "range" query to a LogSQL filter string.
func translateRangeQuery(field string, rq RangeQuery) (string, error) {
	var conditions []string
	// Removed logstorage.EscapeFilterStringIfNeeded

	formatRangeValue := func(value interface{}) (string, error) {
		switch v := value.(type) {
		case string:
			if _, err := strconv.ParseFloat(v, 64); err == nil {
				return v, nil
			}
			return strconv.Quote(v), nil
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			return fmt.Sprintf("%d", v), nil
		case float32, float64:
			return fmt.Sprintf("%f", v), nil
		default:
			return "", fmt.Errorf("unsupported type for range value: %T", v)
		}
	}

	addCondition := func(op string, value interface{}) error {
		if value != nil {
			valStr, err := formatRangeValue(value)
			if err != nil {
				return fmt.Errorf("field '%s': %w", field, err)
			}
			conditions = append(conditions, fmt.Sprintf("%s %s %s", field, op, valStr))
		}
		return nil
	}

	if err := addCondition(">", rq.GT); err != nil {
		return "", err
	}
	if err := addCondition(">=", rq.GTE); err != nil {
		return "", err
	}
	if err := addCondition("<", rq.LT); err != nil {
		return "", err
	}
	if err := addCondition("<=", rq.LTE); err != nil {
		return "", err
	}

	if len(conditions) == 0 {
		return "", fmt.Errorf("range query for field '%s' has no conditions (gt, gte, lt, lte)", field)
	}
	return "(" + strings.Join(conditions, " AND ") + ")", nil
}

// translateQueryStringQuery translates a "query_string" query to a LogSQL filter string.
func translateQueryStringQuery(qs *QueryStringQuery) (string, error) {
	if qs.Query == "" {
		return "", nil
	}
	if qs.DefaultField != "" || len(qs.Fields) > 0 {
		logger.Warnf("ES query_string: 'default_field' and 'fields' are not fully translated yet. Query will be applied via _q to default LogStorage search fields.")
	}
	return fmt.Sprintf("_q=%s", escapeLogSQLValue(qs.Query)), nil
}
