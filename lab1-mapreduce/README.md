# Lab 1: MapReduce 实现

## 概述

本实验实现了一个分布式MapReduce系统，包含协调器（Coordinator）和工作进程（Worker）两个主要组件。系统能够处理大规模数据的并行计算任务。

## 架构设计

### 核心组件

1. **Coordinator（协调器）**
   - 负责任务分发和管理
   - 跟踪任务状态和超时处理
   - 处理工作进程的RPC请求

2. **Worker（工作进程）**
   - 执行Map和Reduce任务
   - 与协调器通信获取任务
   - 处理中间文件的读写

3. **RPC通信**
   - 定义了GetTask和TaskCompleted两个RPC接口
   - 使用Unix域套接字进行通信

## 实现细节

### 任务类型

```go
type TaskType int

const (
    MapTask TaskType = iota
    ReduceTask
    WaitTask
    ExitTask
)
```

- **MapTask**: Map阶段任务，处理输入文件
- **ReduceTask**: Reduce阶段任务，聚合中间结果
- **WaitTask**: 等待任务，当没有可用任务时返回
- **ExitTask**: 退出任务，所有工作完成后返回

### 任务状态管理

```go
type TaskStatus int

const (
    Idle TaskStatus = iota
    InProgress
    Completed
)
```

协调器维护每个任务的状态，并实现超时重试机制（10秒超时）。

### Map阶段实现

1. **输入处理**: 读取输入文件内容
2. **Map函数应用**: 调用用户定义的Map函数
3. **分区**: 使用哈希函数将键值对分配到不同的Reduce任务
4. **中间文件写入**: 将结果写入`mr-X-Y`格式的中间文件

```go
func ihash(key string) int {
    h := fnv.New32a()
    h.Write([]byte(key))
    return int(h.Sum32() & 0x7fffffff)
}
```

### Reduce阶段实现

1. **中间文件读取**: 读取所有相关的中间文件
2. **排序**: 按键对所有键值对进行排序
3. **Reduce函数应用**: 对每个唯一键应用Reduce函数
4. **输出写入**: 将最终结果写入输出文件

### 容错机制

1. **超时检测**: 协调器定期检查任务是否超时
2. **任务重试**: 超时任务会被重新分配给其他工作进程
3. **状态同步**: 通过RPC确保任务完成状态的一致性

## 文件结构

```
lab1-mapreduce/
├── mr/
│   ├── coordinator.go    # 协调器实现
│   ├── worker.go         # 工作进程实现
│   └── rpc.go           # RPC定义
├── main/
│   ├── mrcoordinator.go  # 协调器主程序
│   └── mrworker.go      # 工作进程主程序
├── mrapps/
│   └── wc.go            # 词频统计示例应用
└── README.md            # 本文档
```

## 使用方法

### 1. 编译应用程序

```bash
cd lab1-mapreduce
go build -buildmode=plugin mrapps/wc.go
```

### 2. 启动协调器

```bash
go run main/mrcoordinator.go input*.txt
```

### 3. 启动工作进程

```bash
# 在不同终端中启动多个工作进程
go run main/mrworker.go wc.so
go run main/mrworker.go wc.so
go run main/mrworker.go wc.so
```

### 4. 查看结果

```bash
cat mr-out-* | sort
```

## 测试

可以使用提供的测试脚本验证实现的正确性：

```bash
bash test-mr.sh
```

测试包括：
- 基本功能测试
- 并行性测试
- 容错性测试

## 性能特点

1. **并行处理**: Map和Reduce任务可以并行执行
2. **负载均衡**: 任务动态分配给空闲的工作进程
3. **容错性**: 自动处理工作进程故障和任务超时
4. **可扩展性**: 支持任意数量的工作进程

## 关键设计决策

1. **中间文件格式**: 使用JSON编码确保跨平台兼容性
2. **通信机制**: 使用Unix域套接字提高性能
3. **超时策略**: 10秒超时平衡了容错性和性能
4. **文件命名**: 使用`mr-X-Y`格式便于管理和清理

## 扩展可能

1. **动态负载均衡**: 根据工作进程性能调整任务分配
2. **检查点机制**: 支持任务的中间状态保存
3. **网络通信**: 支持跨机器的分布式执行
4. **资源管理**: 添加内存和CPU使用限制