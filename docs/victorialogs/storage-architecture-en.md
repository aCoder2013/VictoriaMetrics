---
weight: 8
title: Storage Architecture (English)
menu:
  docs:
    identifier: "victorialogs-storage-architecture-en"
    parent: "victorialogs"
    weight: 8
    title: Storage Architecture (English)
tags:
  - logs
  - storage
  - architecture
aliases:
- /victorialogs/storage-architecture-en.html
---

# VictoriaLogs Storage Architecture and Protocols

## Overview

VictoriaLogs is a high-performance database designed specifically for log management and analysis. It uses a columnar storage architecture optimized for log data characteristics. This document provides a detailed description of VictoriaLogs' storage structure, data encoding, compression mechanisms, and various data ingestion protocols.

## Storage Architecture Overview

### Overall Architecture

VictoriaLogs employs a layered storage architecture with the following hierarchy from top to bottom:

1. **Storage Layer**: Manages the entire database lifecycle
2. **Partition Layer**: Organizes data by time ranges
3. **Block Layer**: Stores actual log data
4. **Encoding Layer**: Handles data compression and encoding

### Storage Directory Structure

```
victorialogs-data/
├── partitions/
│   ├── 2024_01/          # Monthly partitions
│   │   ├── datadb/       # Data storage
│   │   └── indexdb/      # Index storage
│   └── 2024_02/
├── metadata/
└── tmp/
```

## Data Structure Details

### 1. Partitions

Partitions are the largest data organization units in VictoriaLogs with the following characteristics:

- **Time-based Partitioning**: Data is partitioned by day or month
- **Independent Management**: Each partition independently manages its data and indexes
- **Automatic Cleanup**: Expired partitions are automatically deleted based on retention policies

```go
type Partition struct {
    // Partition time range
    MinTimestamp int64
    MaxTimestamp int64
    
    // Data storage path
    DataPath string
    
    // Partition statistics
    PartitionStats
}
```

### 2. Blocks

Blocks are the basic storage units, each block contains:

#### Block Structure

```go
type block struct {
    // Timestamp array
    timestamps []int64
    
    // Field column data
    columns []column
    
    // Constant fields (fields with constant values across all block entries)
    constColumns []Field
}
```

#### Log Entry Format

Each log entry follows the format:
```
2006-01-02T15:04:05.999999999Z07:00 field1=value1 field2=value2 ... fieldN=valueN
```

### 3. Column Storage

VictoriaLogs uses columnar storage, storing values of the same field together:

```go
type column struct {
    // Field name
    name string
    
    // Field values array
    values []string
}
```

#### Column Storage Advantages

1. **High Compression Ratio**: Similar data types stored together compress better
2. **Query Performance**: Only read columns involved in queries
3. **Memory Efficiency**: Reduces unnecessary data loading

### 4. Field Types

VictoriaLogs supports multiple data types, each with specialized encoding:

```go
const (
    // String type
    valueTypeString = valueType(0)
    
    // Numeric types
    valueTypeUint8  = valueType(1)
    valueTypeUint16 = valueType(4)
    valueTypeUint32 = valueType(5)
    valueTypeUint64 = valueType(6)
    valueTypeInt64  = valueType(10)
    valueTypeFloat64 = valueType(7)
    
    // Special types
    valueTypeIPv4 = valueType(8)
    valueTypeTimestampISO8601 = valueType(9)
)
```

## Encoding and Compression Mechanisms

### 1. Data Encoding

#### Integer Encoding

VictoriaLogs uses adaptive integer encoding:

- **8-bit integers**: Value range [0, 255]
- **16-bit integers**: Value range [0, 65535]
- **32-bit integers**: Value range [0, 2^32-1]
- **64-bit integers**: Value range [0, 2^64-1]

#### Special Type Encoding

1. **IPv4 addresses**: Encoded as 4 bytes
2. **Timestamps**: Optimized encoding using ISO8601 format
3. **Floating-point numbers**: IEEE 754 double precision format

### 2. Compression Algorithms

#### String Compression

```go
func marshalBytesBlock(dst, src []byte) []byte {
    if len(src) < 128 {
        // Small blocks: store without compression
        dst = append(dst, marshalBytesTypePlain)
        dst = append(dst, byte(len(src)))
        return append(dst, src...)
    }
    
    // Large blocks: use ZSTD compression
    dst = append(dst, marshalBytesTypeZSTD)
    compressLevel := getCompressLevel(len(src))
    // ... compression logic
}
```

#### Compression Level Strategy

- **Data size ≤ 512 bytes**: Compression level 1
- **Data size ≤ 4KB**: Compression level 2
- **Data size > 4KB**: Compression level 3

### 3. Bloom Filters

VictoriaLogs uses bloom filters to optimize query performance:

- **Word filtering**: Quickly skip blocks without specific words
- **Phrase filtering**: Quickly skip blocks without specific phrases
- **False positive rate**: Controlled within acceptable range

## Data Ingestion Protocols

### 1. Supported Protocols

VictoriaLogs supports multiple data ingestion protocols:

| Protocol | Endpoint | Description |
|----------|----------|-------------|
| Elasticsearch | `/elasticsearch/_bulk` | Compatible with Elasticsearch bulk API |
| Loki | `/loki/api/v1/push` | Compatible with Grafana Loki push API |
| JSON Lines | `/jsonline` | One JSON object per line |
| Syslog | UDP/TCP 514 | Standard syslog protocol |
| OpenTelemetry | `/opentelemetry/v1/logs` | OpenTelemetry logs protocol |
| Journald | Local socket | systemd journald integration |

### 2. Protocol Processing Flow

#### General Processing Flow

```
Client Request -> Protocol Parsing -> Data Validation -> Field Extraction -> Storage Processing -> Response Return
```

#### Elasticsearch Protocol Example

```json
{
    "index": {
        "_index": "logs-2024.01.01",
        "_type": "_doc"
    }
}
{
    "@timestamp": "2024-01-01T10:00:00Z",
    "level": "info",
    "message": "User logged in",
    "user_id": "12345"
}
```

### 3. Internal Protocols

#### Inter-node Communication in Cluster

VictoriaLogs cluster mode uses internal protocols for inter-node communication:

```go
// Internal insert protocol
func RequestHandler(w http.ResponseWriter, r *http.Request) {
    // Protocol version check
    version := r.FormValue("version")
    if version != netinsert.ProtocolVersion {
        // Version mismatch handling
        return
    }
    
    // Data parsing and processing
    // ...
}
```

## Query Processing Mechanism

### 1. Query Execution Flow

```
LogsQL Query -> Syntax Parsing -> Query Planning -> Index Lookup -> Data Filtering -> Result Aggregation -> Result Return
```

### 2. Index System

#### Sparse Index

- **Timestamp index**: Maintains time ranges for each block
- **Stream index**: Groups index by log streams
- **Field index**: Builds indexes for high-cardinality fields

#### Index Structure

```go
type indexBlockHeader struct {
    // Timestamp range
    MinTimestamp int64
    MaxTimestamp int64
    
    // Stream identifier
    StreamID streamID
    
    // Field statistics
    FieldsCount uint32
}
```

### 3. Query Optimization

#### Block-level Filtering

- **Time filtering**: Uses timestamp indexes to quickly skip irrelevant blocks
- **Stream filtering**: Filters irrelevant data streams by stream identifiers
- **Field filtering**: Only reads field columns needed for queries

#### Parallel Processing

- **Block parallelism**: Multiple blocks processed in parallel
- **CPU core utilization**: Fully utilizes available CPU cores
- **Memory management**: Controls memory usage to avoid OOM

## Storage Optimization Techniques

### 1. Data Merging

#### Merge Strategy

```go
func (bsm *blockStreamMerger) flushIB(bsw *blockStreamWriter, ph *partHeader, itemsMerged *atomic.Uint64) {
    // Check if merge is needed
    if len(items) == 0 {
        return
    }
    
    // Execute merge operation
    // ...
}
```

#### Merge Trigger Conditions

- **Block count threshold**: Triggers merge when small block count reaches threshold
- **Time interval**: Periodically executes merge operations
- **Storage space**: Forces merge when storage space is insufficient

### 2. Data Compression

#### Compression Strategy

1. **Real-time compression**: Compresses data during writes
2. **Background compression**: Deep compression via background tasks
3. **Adaptive compression**: Selects compression algorithms based on data characteristics

#### Compression Effectiveness

- **Text data**: Usually achieves 5-10x compression ratio
- **Structured data**: Can achieve 10-20x compression ratio
- **High repetitive data**: Can achieve 30x+ compression ratio

### 3. Memory Management

#### Memory Pools

```go
// Byte buffer pool
var bbPool = &sync.Pool{
    New: func() interface{} {
        return &bytesutil.ByteBuffer{}
    },
}
```

#### Memory Optimization Strategies

- **Object pooling**: Reuses frequently allocated objects
- **Zero-copy**: Reduces unnecessary memory copies
- **Memory mapping**: Uses memory mapping for large files

## Performance Characteristics

### 1. Storage Performance

- **Write performance**: Single node can achieve 100+ MB/s
- **Query performance**: Sub-second response for most queries
- **Compression efficiency**: Saves 80%+ storage space compared to traditional solutions

### 2. Scalability

- **Vertical scaling**: Linear scaling with CPU and memory
- **Horizontal scaling**: Supports cluster mode
- **Storage scaling**: Supports dynamic storage expansion

### 3. Reliability

- **Data persistence**: All data persisted to disk
- **Fault recovery**: Automatic detection and recovery of corrupted data
- **Backup support**: Supports hot backup and recovery

## Configuration and Tuning

### 1. Storage Configuration

```go
type StorageConfig struct {
    // Data retention period
    Retention time.Duration
    
    // Maximum disk usage
    MaxDiskSpaceUsageBytes int64
    
    // Flush interval
    FlushInterval time.Duration
    
    // Future data retention period
    FutureRetention time.Duration
}
```

### 2. Performance Tuning Recommendations

#### Hardware Recommendations

- **CPU**: Multi-core CPU recommended
- **Memory**: 8GB+ memory recommended
- **Storage**: SSD storage recommended
- **Network**: Gigabit network connection

#### Configuration Recommendations

- **Partition size**: Adjust partition strategy based on data volume
- **Compression level**: Choose based on CPU and storage balance
- **Cache size**: Adjust cache based on available memory

## Monitoring and Troubleshooting

### 1. Monitoring Metrics

- **Storage utilization**: Monitor disk space usage
- **Write rate**: Monitor data write performance
- **Query latency**: Monitor query response time
- **Error rate**: Monitor various error metrics

### 2. Common Issues

#### Insufficient Storage Space

- **Symptoms**: Writes rejected, enters read-only mode
- **Solution**: Increase storage space or adjust retention policies

#### Query Performance Degradation

- **Symptoms**: Increased query response time
- **Solution**: Optimize query conditions, add indexes

#### High Memory Usage

- **Symptoms**: System memory shortage
- **Solution**: Adjust cache size, increase memory

## Summary

VictoriaLogs achieves high-performance, low-cost log storage and query systems through carefully designed storage architecture, efficient encoding/compression mechanisms, and flexible data ingestion protocols. Its columnar storage, intelligent compression, parallel processing, and other technical features provide significant advantages in log management scenarios.

Understanding these technical details helps better use and optimize VictoriaLogs to fully leverage its potential in log analysis scenarios.