# Querying Logs with Elasticsearch Syntax in VictoriaLogs

## Introduction

VictoriaLogs now offers an Elasticsearch-compatible query API, allowing users to leverage familiar Elasticsearch query syntax for searching and analyzing logs. This feature is designed to:

*   Ease migration for users already accustomed to Elasticsearch.
*   Provide a flexible and powerful query language alternative for log exploration.

The compatibility layer is accessible via the `/elasticsearch/_search` HTTP endpoint (supporting `POST` and `GET` with request body).

## Supported Query Types and Clauses

The API supports a subset of the Elasticsearch Query DSL, focusing on common log searching patterns.

### Top-Level Parameters

| Parameter | Description                                                                 | Example                                |
| :-------- | :-------------------------------------------------------------------------- | :------------------------------------- |
| `query`   | The main container for your query clauses (see Query DSL below).            | `{"query": {"term": {"user": "foo"}}}` |
| `size`    | The number of hits to return. Defaults to `10`.                             | `{"size": 100}`                        |
| `from`    | The starting offset for pagination. Defaults to `0`.                        | `{"from": 20}`                         |
| `sort`    | Specifies sorting order. **Parsed but not yet implemented in the backend.** | `{"sort": [{"timestamp": "desc"}]}`   |
| `aggs`    | Defines aggregations. **Parsed but aggregation processing is not yet implemented.** | `{"aggs": {"users": {"terms": {"field": "user.keyword"}}}}` |

### Query DSL (`query` clause)

The following query types can be used within the `query` object:

*   **`bool`**:
    *   Purpose: Combines multiple query clauses with boolean logic.
    *   Sub-clauses:
        *   `must`: All clauses must match (logical AND).
        *   `filter`: Clauses must match, but do not contribute to the score (effectively AND).
        *   `should`: At least one clause should match (logical OR). If used with `must` or `filter`, it influences scoring or requires a minimum match. If `must` or `filter` are not present, at least one `should` clause must match.
        *   `must_not`: All clauses must not match (logical NOT).
    *   Example:
        ```json
        {
          "query": {
            "bool": {
              "must": [
                { "term": { "level": "error" } }
              ],
              "filter": [
                { "range": { "timestamp": { "gte": "now-1h" } } }
              ],
              "should": [
                { "match": { "message": "payment" } }
              ],
              "must_not": [
                { "term": { "source_ip": "127.0.0.1" } }
              ]
            }
          }
        }
        ```

*   **`term`**:
    *   Purpose: Finds documents that contain an exact term in a provided field.
    *   Example:
        ```json
        { "query": { "term": { "http_status_code": 200 } } }
        ```
        ```json
        { "query": { "term": { "user.keyword": "Alice" } } }
        ```

*   **`match`**:
    *   Purpose: Standard full-text search. Analyzes the input text and builds a query.
    *   Current Implementation: Translates to a phrase search in LogSQL (e.g., `field:"text value"`). For multi-word queries, it looks for the words as a sequence.
    *   Example:
        ```json
        { "query": { "match": { "message": "user login failed" } } }
        ```

*   **`range`**:
    *   Purpose: Finds documents with field values within a specified range.
    *   Supported Operators: `gt` (greater than), `gte` (greater than or equal to), `lt` (less than), `lte` (less than or equal to).
    *   Example:
        ```json
        {
          "query": {
            "range": {
              "response_time_ms": {
                "gte": 100,
                "lt": 500
              }
            }
          }
        }
        ```
        ```json
        {
          "query": {
            "range": {
              "@timestamp": { 
                "gte": "2023-01-01T00:00:00Z",
                "lt": "2023-01-02T00:00:00Z"
              }
            }
          }
        }
        ```

*   **`query_string`**:
    *   Purpose: Parses a query string using a specific syntax (similar to Lucene syntax).
    *   Current Implementation: Basic support. The provided query string is translated to LogSQL's `_q="<query_string_content>"` filter, which performs a full-text search over default fields. Field-specific searches within the query string (e.g., `level:error AND message:"failed"`) depend on LogSQL's `_q` parsing capabilities.
    *   Example:
        ```json
        {
          "query": {
            "query_string": {
              "query": "(payment AND (failed OR error)) OR user_id:user123"
            }
          }
        }
        ```

## Aggregations (Current Status)

The `aggs` (or `aggregations`) section in an Elasticsearch query is parsed by VictoriaLogs if present. Basic aggregation types such as `terms` and `date_histogram` are recognized by the parser.

**However, the actual processing of these aggregations in the backend is not yet implemented.**

This means:
*   You can include an `aggs` block in your ES query.
*   The query will be accepted and processed for search hits.
*   The response **will not contain an `aggregations` block with results**, or this block will be empty or null.

Users requiring aggregation capabilities should use VictoriaLogs' native LogSQL, which provides powerful aggregation functions.

## Response Format

The API aims to return responses that mimic the standard Elasticsearch search response format. Key fields include:

*   `took`: Time taken for the query to execute on the server, in milliseconds.
*   `timed_out`: Boolean indicating if the query timed out (currently always `false`).
*   `_shards`: Information about the shards involved (placeholder values for a single-node setup).
    *   `total`: 1
    *   `successful`: 1
    *   `failed`: 0
    *   `skipped`: 0
*   `hits`: Contains the search results.
    *   `total`: An object with:
        *   `value`: Total number of matching documents.
        *   `relation`: Indicates if `value` is accurate ("eq") or a lower bound ("gte"). Currently defaults to "eq".
    *   `max_score`: The maximum score of any hit. Currently a placeholder (e.g., `1.0`) as relevance scoring is not implemented.
    *   `hits`: An array of individual log entries (documents). Each hit includes:
        *   `_index`: Placeholder index name (e.g., "victorialogs").
        *   `_type`: Always "_doc".
        *   `_id`: Document ID. Currently basic (e.g., derived from `_id` or `id` fields in the source log, or empty if not found).
        *   `_score`: Relevance score. Currently a placeholder (e.g., `1.0`).
        *   `_source`: The actual log entry as a JSON object, with fields and values as strings.

## Examples

### 1. Simple Term Query for a User

Find logs where the field `user.id` is exactly "user-123".

**Request:**
```bash
curl -X POST "http://<victoria-logs-addr>:9428/elasticsearch/_search" -H "Content-Type: application/json" -d'
{
  "query": {
    "term": {
      "user.id": "user-123"
    }
  }
}
'
```

**Expected (Conceptual) Response Snippet:**
```json
{
  "took": 5,
  "timed_out": false,
  "_shards": { "total": 1, "successful": 1, "failed": 0, "skipped": 0 },
  "hits": {
    "total": { "value": 150, "relation": "eq" },
    "max_score": 1.0,
    "hits": [
      {
        "_index": "victorialogs",
        "_type": "_doc",
        "_id": "some_log_id_1",
        "_score": 1.0,
        "_source": {
          "user.id": "user-123",
          "message": "User login successful",
          "timestamp": "2023-03-15T10:00:00Z"
        }
      }
      // ... more hits
    ]
  }
}
```

### 2. Boolean Query for Errors from a Specific Service

Find error logs (`level: "error"`) from the `payment-service`.

**Request:**
```bash
curl -X POST "http://<victoria-logs-addr>:9428/elasticsearch/_search" -H "Content-Type: application/json" -d'
{
  "query": {
    "bool": {
      "must": [
        { "term": { "level": "error" } },
        { "term": { "service_name.keyword": "payment-service" } }
      ]
    }
  }
}
'
```

### 3. Query String Search with Pagination

Search for logs containing "database connection error" or "timeout", retrieve 5 hits, skipping the first 10.

**Request:**
```bash
curl -X POST "http://<victoria-logs-addr>:9428/elasticsearch/_search" -H "Content-Type: application/json" -d'
{
  "query": {
    "query_string": {
      "query": "\"database connection error\" OR timeout"
    }
  },
  "size": 5,
  "from": 10
}
'
```

## Known Limitations and Differences

*   **Aggregation Processing:** As stated, `aggs` are parsed but not processed. Use LogSQL for aggregations.
*   **Sorting (`sort`):** Parsed but not yet effective in the backend. Results are returned in default LogStorage order.
*   **Scoring (`_score`):** A placeholder score (e.g., 1.0) is returned. Relevance scoring is not implemented.
*   **`query_string` Syntax:** While aiming for Lucene-like compatibility, the full feature set and nuances of Elasticsearch's `query_string` may not be supported. The translation relies on LogSQL's `_q` filter capabilities.
*   **Error Reporting:** Aims to be ES-like, but error details or types might differ.
*   **Field Data Types:** All `_source` fields are currently returned as strings in the JSON response.
*   **Advanced ES Features:** Features like script fields, highlighting, inner hits, and complex nested queries are not supported.

Users are advised to test this compatibility layer thoroughly for their specific use cases and query patterns.

## How it Works (High-Level Overview)

Incoming Elasticsearch queries sent to the `/elasticsearch/_search` endpoint are parsed and then translated into VictoriaLogs' native LogSQL queries. These LogSQL queries are then executed by the VictoriaLogs storage engine, and the results are formatted back into an Elasticsearch-like JSON response.
