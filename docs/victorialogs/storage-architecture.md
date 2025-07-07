---
weight: 7
title: Storage Architecture
menu:
  docs:
    identifier: "victorialogs-storage-architecture"
    parent: "victorialogs"
    weight: 7
    title: Storage Architecture
tags:
  - logs
  - storage
  - architecture
aliases:
- /victorialogs/storage-architecture.html
---

# VictoriaLogs 存储架构和协议详解

## 概述

VictoriaLogs 是一个专为日志管理和分析而设计的高性能数据库，采用列式存储架构，针对日志数据的特点进行了深度优化。本文档详细描述了 VictoriaLogs 的存储结构、数据编码、压缩机制以及各种数据摄取协议。

## 存储架构概览

### 整体架构

VictoriaLogs 采用分层存储架构，从上到下包含以下层次：

1. **存储层 (Storage Layer)**: 管理整个数据库的生命周期
2. **分区层 (Partition Layer)**: 按时间范围组织数据
3. **数据块层 (Block Layer)**: 存储实际的日志数据
4. **编码层 (Encoding Layer)**: 负责数据压缩和编码

### 存储目录结构

```
victorialogs-data/
├── partitions/
│   ├── 2024_01/          # 按年月分区
│   │   ├── datadb/       # 数据存储
│   │   └── indexdb/      # 索引存储
│   └── 2024_02/
├── metadata/
└── tmp/
```

## 数据结构详解

### 1. 分区 (Partitions)

分区是 VictoriaLogs 中最大的数据组织单元，具有以下特点：

- **时间分区**: 按天或月进行数据分区
- **独立管理**: 每个分区独立管理其数据和索引
- **自动清理**: 根据保留策略自动删除过期分区

```go
type Partition struct {
    // 分区时间范围
    MinTimestamp int64
    MaxTimestamp int64
    
    // 数据存储路径
    DataPath string
    
    // 分区统计信息
    PartitionStats
}
```

### 2. 数据块 (Blocks)

数据块是存储的基本单元，每个块包含：

#### 块结构

```go
type block struct {
    // 时间戳数组
    timestamps []int64
    
    // 字段列数据
    columns []column
    
    // 常量字段（在整个块中值不变的字段）
    constColumns []Field
}
```

#### 日志条目格式

每个日志条目遵循以下格式：
```
2006-01-02T15:04:05.999999999Z07:00 field1=value1 field2=value2 ... fieldN=valueN
```

### 3. 列存储 (Column Storage)

VictoriaLogs 使用列式存储，将相同字段的值存储在一起：

```go
type column struct {
    // 字段名
    name string
    
    // 字段值数组
    values []string
}
```

#### 列存储优势

1. **高压缩率**: 相同类型的数据聚集存储，压缩效果更好
2. **查询性能**: 只需读取查询涉及的列
3. **内存效率**: 减少不必要的数据加载

### 4. 字段类型 (Field Types)

VictoriaLogs 支持多种数据类型，每种类型都有专门的编码方式：

```go
const (
    // 字符串类型
    valueTypeString = valueType(0)
    
    // 数值类型
    valueTypeUint8  = valueType(1)
    valueTypeUint16 = valueType(4)
    valueTypeUint32 = valueType(5)
    valueTypeUint64 = valueType(6)
    valueTypeInt64  = valueType(10)
    valueTypeFloat64 = valueType(7)
    
    // 特殊类型
    valueTypeIPv4 = valueType(8)
    valueTypeTimestampISO8601 = valueType(9)
)
```

## 编码和压缩机制

### 1. 数据编码

#### 整数编码

VictoriaLogs 使用自适应整数编码：

- **8位整数**: 值范围 [0, 255]
- **16位整数**: 值范围 [0, 65535]
- **32位整数**: 值范围 [0, 2^32-1]
- **64位整数**: 值范围 [0, 2^64-1]

#### 特殊类型编码

1. **IPv4 地址**: 编码为 4 字节
2. **时间戳**: 使用 ISO8601 格式优化编码
3. **浮点数**: IEEE 754 双精度格式

### 2. 压缩算法

#### 字符串压缩

```go
func marshalBytesBlock(dst, src []byte) []byte {
    if len(src) < 128 {
        // 小块数据：无压缩存储
        dst = append(dst, marshalBytesTypePlain)
        dst = append(dst, byte(len(src)))
        return append(dst, src...)
    }
    
    // 大块数据：使用 ZSTD 压缩
    dst = append(dst, marshalBytesTypeZSTD)
    compressLevel := getCompressLevel(len(src))
    // ... 压缩逻辑
}
```

#### 压缩级别策略

- **数据量 ≤ 512 字节**: 压缩级别 1
- **数据量 ≤ 4KB**: 压缩级别 2
- **数据量 > 4KB**: 压缩级别 3

### 3. 布隆过滤器

VictoriaLogs 使用布隆过滤器优化查询性能：

- **词过滤**: 快速跳过不包含特定单词的块
- **短语过滤**: 快速跳过不包含特定短语的块
- **假阳性率**: 控制在可接受范围内

## 数据摄取协议

### 1. 支持的协议

VictoriaLogs 支持多种数据摄取协议：

| 协议类型 | 端点 | 描述 |
|---------|------|------|
| Elasticsearch | `/elasticsearch/_bulk` | 兼容 Elasticsearch 批量 API |
| Loki | `/loki/api/v1/push` | 兼容 Grafana Loki 推送 API |
| JSON Lines | `/jsonline` | 每行一个 JSON 对象 |
| Syslog | UDP/TCP 514 | 标准 syslog 协议 |
| OpenTelemetry | `/opentelemetry/v1/logs` | OpenTelemetry 日志协议 |
| Journald | 本地套接字 | systemd journald 集成 |

### 2. 协议处理流程

#### 通用处理流程

```
客户端请求 -> 协议解析 -> 数据验证 -> 字段提取 -> 存储处理 -> 响应返回
```

#### Elasticsearch 协议示例

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

### 3. 内部协议

#### 集群内部通信

VictoriaLogs 集群模式使用内部协议进行节点间通信：

```go
// 内部插入协议
func RequestHandler(w http.ResponseWriter, r *http.Request) {
    // 协议版本检查
    version := r.FormValue("version")
    if version != netinsert.ProtocolVersion {
        // 版本不匹配处理
        return
    }
    
    // 数据解析和处理
    // ...
}
```

## 查询处理机制

### 1. 查询执行流程

```
LogsQL 查询 -> 语法解析 -> 查询规划 -> 索引查找 -> 数据过滤 -> 结果聚合 -> 结果返回
```

### 2. 索引系统

#### 稀疏索引

- **时间戳索引**: 维护每个块的时间范围
- **流索引**: 按日志流分组索引
- **字段索引**: 为高基数字段建立索引

#### 索引结构

```go
type indexBlockHeader struct {
    // 时间戳范围
    MinTimestamp int64
    MaxTimestamp int64
    
    // 流标识
    StreamID streamID
    
    // 字段统计
    FieldsCount uint32
}
```

### 3. 查询优化

#### 块级过滤

- **时间过滤**: 利用时间戳索引快速跳过不相关的块
- **流过滤**: 通过流标识过滤不相关的数据流
- **字段过滤**: 只读取查询需要的字段列

#### 并行处理

- **块并行**: 多个块并行处理
- **CPU 核心利用**: 充分利用可用的 CPU 核心
- **内存管理**: 控制内存使用避免 OOM

## 存储优化技术

### 1. 数据合并

#### 合并策略

```go
func (bsm *blockStreamMerger) flushIB(bsw *blockStreamWriter, ph *partHeader, itemsMerged *atomic.Uint64) {
    // 检查是否需要合并
    if len(items) == 0 {
        return
    }
    
    // 执行合并操作
    // ...
}
```

#### 合并触发条件

- **块数量阈值**: 当小块数量达到阈值时触发合并
- **时间间隔**: 定期执行合并操作
- **存储空间**: 当存储空间不足时强制合并

### 2. 数据压缩

#### 压缩策略

1. **实时压缩**: 写入时即时压缩
2. **后台压缩**: 后台任务进行深度压缩
3. **自适应压缩**: 根据数据特征选择压缩算法

#### 压缩效果

- **文本数据**: 通常可达到 5-10 倍压缩率
- **结构化数据**: 可达到 10-20 倍压缩率
- **高重复数据**: 可达到 30 倍以上压缩率

### 3. 内存管理

#### 内存池

```go
// 字节缓冲池
var bbPool = &sync.Pool{
    New: func() interface{} {
        return &bytesutil.ByteBuffer{}
    },
}
```

#### 内存优化策略

- **对象池**: 重用频繁分配的对象
- **零拷贝**: 减少不必要的内存复制
- **内存映射**: 对大文件使用内存映射

## 性能特点

### 1. 存储性能

- **写入性能**: 单节点可达 100+ MB/s
- **查询性能**: 亚秒级响应大部分查询
- **压缩效率**: 比传统方案节省 80% 以上存储空间

### 2. 扩展性

- **垂直扩展**: 线性扩展 CPU 和内存
- **水平扩展**: 支持集群模式
- **存储扩展**: 支持动态添加存储空间

### 3. 可靠性

- **数据持久化**: 所有数据持久化到磁盘
- **故障恢复**: 自动检测和恢复损坏的数据
- **备份支持**: 支持热备份和恢复

## 配置和调优

### 1. 存储配置

```go
type StorageConfig struct {
    // 数据保留期
    Retention time.Duration
    
    // 最大磁盘使用量
    MaxDiskSpaceUsageBytes int64
    
    // 刷新间隔
    FlushInterval time.Duration
    
    // 未来数据保留期
    FutureRetention time.Duration
}
```

### 2. 性能调优建议

#### 硬件建议

- **CPU**: 推荐多核心 CPU
- **内存**: 建议 8GB+ 内存
- **存储**: 推荐 SSD 存储
- **网络**: 千兆网络连接

#### 配置建议

- **分区大小**: 根据数据量调整分区策略
- **压缩级别**: 根据 CPU 和存储平衡选择
- **缓存大小**: 根据可用内存调整缓存

## 监控和故障排除

### 1. 监控指标

- **存储使用率**: 监控磁盘空间使用
- **写入速率**: 监控数据写入性能
- **查询延迟**: 监控查询响应时间
- **错误率**: 监控各种错误指标

### 2. 常见问题

#### 存储空间不足

- **症状**: 写入被拒绝，进入只读模式
- **解决**: 增加存储空间或调整保留策略

#### 查询性能下降

- **症状**: 查询响应时间增加
- **解决**: 优化查询条件，增加索引

#### 内存使用过高

- **症状**: 系统内存不足
- **解决**: 调整缓存大小，增加内存

## 总结

VictoriaLogs 通过精心设计的存储架构、高效的编码压缩机制和灵活的数据摄取协议，实现了高性能、低成本的日志存储和查询系统。其列式存储、智能压缩、并行处理等技术特点使其在日志管理领域具有显著优势。

理解这些技术细节有助于更好地使用和优化 VictoriaLogs，充分发挥其在日志分析场景中的潜力。