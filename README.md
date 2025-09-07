# MIT 6.824 分布式系统实验实现

## 项目概述

本项目完整实现了MIT 6.824分布式系统课程的四个核心实验，涵盖了分布式系统的关键概念和技术。每个实验都基于前一个实验的基础，逐步构建了一个完整的分布式存储系统。

## 实验架构图

```
┌─────────────────────────────────────────────────────────────────┐
│                        MIT 6.824 实验架构                        │
├─────────────────────────────────────────────────────────────────┤
│  Lab 4: 分片键值服务 (Sharded Key/Value Service)                │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐   │
│  │   Shard Group 1 │  │   Shard Group 2 │  │   Shard Group N │   │
│  │   ┌───────────┐ │  │   ┌───────────┐ │  │   ┌───────────┐ │   │
│  │   │  Lab 3    │ │  │   │  Lab 3    │ │  │   │  Lab 3    │ │   │
│  │   │  KVRaft   │ │  │   │  KVRaft   │ │  │   │  KVRaft   │ │   │
│  │   └───────────┘ │  │   └───────────┘ │  │   └───────────┘ │   │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘   │
│                              │                                   │
│                    ┌─────────▼─────────┐                        │
│                    │  Shard Controller │                        │
│                    │    (Lab 2 Raft)   │                        │
│                    └───────────────────┘                        │
├─────────────────────────────────────────────────────────────────┤
│  Lab 3: 容错键值服务 (Fault-tolerant Key/Value Service)          │
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │                    KVRaft Servers                          │ │
│  │  ┌─────────┐    ┌─────────┐    ┌─────────┐                │ │
│  │  │ Server1 │    │ Server2 │    │ Server3 │                │ │
│  │  │ Lab 2   │    │ Lab 2   │    │ Lab 2   │                │ │
│  │  │ Raft    │    │ Raft    │    │ Raft    │                │ │
│  │  └─────────┘    └─────────┘    └─────────┘                │ │
│  └─────────────────────────────────────────────────────────────┘ │
├─────────────────────────────────────────────────────────────────┤
│  Lab 2: Raft共识算法 (Raft Consensus Algorithm)                  │
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │                    Raft Cluster                            │ │
│  │  ┌─────────┐    ┌─────────┐    ┌─────────┐                │ │
│  │  │ Leader  │    │Follower │    │Follower │                │ │
│  │  │Election │    │Log Repl │    │Persist  │                │ │
│  │  │Heartbeat│    │Snapshot │    │Recovery │                │ │
│  │  └─────────┘    └─────────┘    └─────────┘                │ │
│  └─────────────────────────────────────────────────────────────┘ │
├─────────────────────────────────────────────────────────────────┤
│  Lab 1: MapReduce分布式计算 (MapReduce)                          │
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │                   MapReduce System                         │ │
│  │  ┌─────────────┐                    ┌─────────────┐        │ │
│  │  │ Coordinator │◄──────────────────►│   Workers   │        │ │
│  │  │Task Assign  │                    │ Map/Reduce  │        │ │
│  │  │Fault Detect │                    │File Process │        │ │
│  │  └─────────────┘                    └─────────────┘        │ │
│  └─────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

## 实验列表

### [Lab 1: MapReduce](./lab1-mapreduce/README.md)
**分布式计算框架**
- 🎯 **目标**: 实现Google MapReduce论文中的分布式计算系统
- 🔧 **核心组件**: Coordinator（协调器）、Worker（工作进程）
- 📊 **功能特性**:
  - 任务分发和调度
  - 容错和超时处理
  - 中间文件管理
  - 词频统计示例应用
- 🚀 **技术亮点**: 并行计算、故障恢复、负载均衡

### [Lab 2: Raft共识算法](./lab2-raft/README.md)
**分布式一致性协议**
- 🎯 **目标**: 实现Raft共识算法，为分布式系统提供强一致性保证
- 🔧 **核心组件**: 领导者选举、日志复制、状态持久化、日志压缩
- 📊 **功能特性**:
  - 自动领导者选举
  - 日志条目复制和提交
  - 崩溃恢复和状态持久化
  - 快照和日志压缩
- 🚀 **技术亮点**: 强一致性、分区容错、自动故障恢复

### [Lab 3: 容错键值服务](./lab3-kvraft/README.md)
**基于Raft的分布式存储**
- 🎯 **目标**: 在Raft基础上构建容错的键值存储服务
- 🔧 **核心组件**: KVServer（键值服务器）、Clerk（客户端）
- 📊 **功能特性**:
  - Get、Put、Append操作
  - 客户端会话管理
  - 重复检测和幂等性
  - 快照和日志压缩
- 🚀 **技术亮点**: 线性化语义、自动重试、状态机复制

### [Lab 4: 分片键值服务](./lab4-shardkv/README.md)
**水平扩展的分布式存储**
- 🎯 **目标**: 实现分片机制，支持水平扩展和动态负载均衡
- 🔧 **核心组件**: 
  - ShardController（分片控制器）
  - ShardKV（分片键值服务器）
  - 分片迁移机制
- 📊 **功能特性**:
  - 动态分片分配
  - 配置变更和分片迁移
  - 负载均衡算法
  - 多副本组协调
- 🚀 **技术亮点**: 水平扩展、动态重配置、分片迁移

## 项目结构

```
mit6824/
├── go.mod                          # Go模块定义
├── README.md                       # 项目总览（本文件）
├── lab1-mapreduce/                 # Lab 1: MapReduce实现
│   ├── mr/                         # MapReduce核心实现
│   │   ├── coordinator.go          # 协调器实现
│   │   ├── worker.go              # 工作进程实现
│   │   └── rpc.go                 # RPC定义
│   ├── main/                      # 主程序
│   │   ├── mrcoordinator.go       # 协调器启动程序
│   │   └── mrworker.go            # 工作进程启动程序
│   ├── mrapps/                    # MapReduce应用
│   │   └── wc.go                  # 词频统计应用
│   └── README.md                  # Lab 1详细说明
├── lab2-raft/                     # Lab 2: Raft共识算法
│   ├── raft/                      # Raft核心实现
│   │   ├── raft.go                # Raft主要实现
│   │   ├── persister.go           # 持久化管理
│   │   └── config.go              # 测试配置
│   ├── labrpc/                    # RPC模拟框架
│   │   └── labrpc.go              # 网络模拟实现
│   └── README.md                  # Lab 2详细说明
├── lab3-kvraft/                   # Lab 3: 容错键值服务
│   ├── kvraft/                    # KVRaft实现
│   │   ├── server.go              # 服务器实现
│   │   ├── client.go              # 客户端实现
│   │   └── common.go              # 共享定义
│   └── README.md                  # Lab 3详细说明
└── lab4-shardkv/                  # Lab 4: 分片键值服务
    ├── shardctrler/               # 分片控制器
    │   ├── server.go              # 控制器服务器
    │   ├── client.go              # 控制器客户端
    │   └── common.go              # 共享定义
    ├── shardkv/                   # 分片键值服务
    │   ├── server.go              # 分片服务器
    │   ├── client.go              # 分片客户端
    │   └── common.go              # 共享定义
    └── README.md                  # Lab 4详细说明
```

## 快速开始

### 环境要求
- Go 1.21+
- Git
- 支持Unix域套接字的操作系统（Linux/macOS/WSL）

### 安装和运行

1. **克隆项目**
```bash
git clone <repository-url>
cd mit6824
```

2. **安装依赖**
```bash
go mod tidy
```

3. **运行Lab 1 (MapReduce)**
```bash
cd lab1-mapreduce

# 编译应用程序
go build -buildmode=plugin mrapps/wc.go

# 启动协调器
go run main/mrcoordinator.go input*.txt

# 在新终端启动工作进程
go run main/mrworker.go wc.so

# 查看结果
cat mr-out-* | sort
```

4. **运行Lab 2 (Raft)**
```bash
cd lab2-raft/raft
go test -run 2A  # 测试领导者选举
go test -run 2B  # 测试日志复制
go test -run 2C  # 测试持久化
go test -run 2D  # 测试快照
```

5. **运行Lab 3 (KVRaft)**
```bash
cd lab3-kvraft/kvraft
go test -run 3A  # 测试基本功能
go test -run 3B  # 测试快照功能
```

6. **运行Lab 4 (ShardKV)**
```bash
# 测试分片控制器
cd lab4-shardkv/shardctrler
go test

# 测试分片键值服务
cd ../shardkv
go test
```

## 技术特性

### 🔒 **一致性保证**
- **强一致性**: 所有操作都保证线性化语义
- **ACID属性**: 支持原子性、一致性、隔离性、持久性
- **共识算法**: 基于Raft算法的分布式一致性

### 🛡️ **容错机制**
- **自动故障检测**: 心跳机制和超时检测
- **故障恢复**: 自动领导者选举和状态恢复
- **网络分区**: 处理网络分区和脑裂问题
- **数据持久化**: 状态持久化和快照机制

### ⚡ **性能优化**
- **并行处理**: MapReduce并行计算
- **批量操作**: Raft批量日志复制
- **负载均衡**: 分片自动重新分布
- **缓存优化**: 客户端领导者缓存

### 🔧 **可扩展性**
- **水平扩展**: 支持动态添加服务器
- **分片机制**: 数据自动分片和迁移
- **模块化设计**: 清晰的组件分离
- **插件架构**: MapReduce应用插件化

## 核心算法

### Raft共识算法
```go
// 领导者选举
func (rf *Raft) startElection() {
    rf.state = Candidate
    rf.currentTerm++
    rf.votedFor = rf.me
    rf.resetElectionTimer()
    
    // 并行请求选票
    for i := range rf.peers {
        if i != rf.me {
            go rf.sendRequestVote(i, args, reply)
        }
    }
}

// 日志复制
func (rf *Raft) sendHeartbeats() {
    for i := range rf.peers {
        if i != rf.me {
            go rf.sendAppendEntries(i, args, reply)
        }
    }
}
```

### 分片负载均衡
```go
// 分片重新分布算法
func (sc *ShardCtrler) rebalance(config *Config) {
    target := NShards / len(config.Groups)
    extra := NShards % len(config.Groups)
    
    // 计算每个组应该拥有的分片数量
    for _, gid := range sortedGids {
        maxShards := target
        if extra > 0 {
            maxShards++
            extra--
        }
        // 重新分配分片...
    }
}
```

## 测试和验证

### 自动化测试
每个实验都包含完整的测试套件：

```bash
# 运行所有测试
for lab in lab1-mapreduce lab2-raft lab3-kvraft lab4-shardkv; do
    echo "Testing $lab..."
    cd $lab
    go test -v
    cd ..
done
```

### 性能测试
```bash
# 压力测试
go test -run TestConcurrent -race -count=10

# 长时间稳定性测试
go test -run TestUnreliable -timeout=10m
```

### 故障注入测试
- 网络分区模拟
- 随机服务器崩溃
- 消息丢失和延迟
- 并发客户端访问

## 学习路径

### 🎯 **初学者路径**
1. **理论学习**: 阅读MapReduce和Raft论文
2. **代码阅读**: 从Lab 1开始，逐步理解实现
3. **单步调试**: 使用调试器跟踪执行流程
4. **测试验证**: 运行测试用例，理解预期行为

### 🚀 **进阶路径**
1. **性能优化**: 分析瓶颈，优化关键路径
2. **扩展功能**: 添加新特性，如事务支持
3. **故障分析**: 研究各种故障场景的处理
4. **架构改进**: 设计更好的系统架构

### 🔬 **研究方向**
1. **一致性协议**: 研究其他共识算法（PBFT、HotStuff）
2. **分布式存储**: 探索不同的分片策略
3. **性能优化**: 研究批量处理和流水线技术
4. **容错机制**: 设计更强的容错能力

## 常见问题

### Q: 如何调试分布式系统？
A: 
- 使用详细的日志记录
- 添加调试输出和状态检查
- 使用Go的race检测器
- 分析测试失败的场景

### Q: 如何处理网络分区？
A:
- Raft算法自动处理分区
- 多数派继续工作，少数派停止服务
- 分区恢复后自动同步状态

### Q: 如何优化性能？
A:
- 批量处理操作
- 减少网络往返次数
- 使用异步处理
- 优化数据结构和算法

### Q: 如何扩展系统？
A:
- 添加更多副本组
- 实现动态分片
- 支持跨数据中心部署
- 优化分片迁移策略

## 贡献指南

### 代码规范
- 遵循Go语言规范
- 添加详细的注释
- 编写单元测试
- 使用有意义的变量名

### 提交规范
```bash
# 提交格式
git commit -m "[lab1] 修复coordinator超时处理bug"
git commit -m "[lab2] 优化Raft选举性能"
git commit -m "[lab3] 添加快照压缩功能"
git commit -m "[lab4] 改进分片迁移算法"
```

## 参考资料

### 📚 **核心论文**
- [MapReduce: Simplified Data Processing on Large Clusters](https://research.google/pubs/pub62/)
- [In Search of an Understandable Consensus Algorithm (Raft)](https://raft.github.io/raft.pdf)
- [Spanner: Google's Globally-Distributed Database](https://research.google/pubs/pub39966/)

### 🎓 **课程资源**
- [MIT 6.824 Course Website](https://pdos.csail.mit.edu/6.824/)
- [Raft Visualization](https://raft.github.io/)
- [Distributed Systems Lecture Notes](https://pdos.csail.mit.edu/6.824/notes/)

### 🛠️ **工具和库**
- [Go Programming Language](https://golang.org/)
- [Testify Testing Framework](https://github.com/stretchr/testify)
- [Go Race Detector](https://golang.org/doc/articles/race_detector.html)

## 许可证

本项目基于MIT许可证开源，详见LICENSE文件。

## 致谢

感谢MIT 6.824课程团队提供的优秀课程设计和实验框架，这些实验为理解分布式系统提供了宝贵的实践机会。

---

**🌟 如果这个项目对你有帮助，请给个Star支持一下！**

**📧 有问题或建议？欢迎提Issue或Pull Request！**