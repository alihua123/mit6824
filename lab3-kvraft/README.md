# Lab 3: 基于Raft的容错键值服务

## 概述

本实验在Lab 2的Raft共识算法基础上构建了一个容错的键值存储服务。该服务支持Get、Put和Append操作，并能够在网络分区和服务器故障的情况下保持强一致性。

## 架构设计

### 系统组件

1. **KVServer（键值服务器）**
   - 使用Raft算法维护复制状态机
   - 处理客户端的Get、Put、Append请求
   - 管理客户端会话和重复检测
   - 支持日志压缩和快照

2. **Clerk（客户端）**
   - 向KVServer发送请求
   - 处理领导者变更和重试逻辑
   - 维护唯一的客户端ID和序列号

3. **Raft层**
   - 提供强一致性保证
   - 处理日志复制和领导者选举
   - 支持快照和日志压缩

## 实现细节

### 服务器端实现

#### 核心数据结构
```go
type KVServer struct {
    mu      sync.Mutex
    me      int
    rf      *raft.Raft
    applyCh chan raft.ApplyMsg
    dead    int32

    maxraftstate int // snapshot if log grows this big

    data         map[string]string    // key-value store
    clientSeq    map[int64]int64      // client -> last sequence number
    notifyCh     map[int]chan OpResult // index -> notification channel
    lastApplied  int                  // last applied log index
    persister    *raft.Persister
}

type Op struct {
    Type     string // "Get", "Put", "Append"
    Key      string
    Value    string
    ClientId int64
    SeqNum   int64
}
```

#### 请求处理流程

1. **接收客户端请求**
```go
func (kv *KVServer) Get(args *GetArgs, reply *GetReply) {
    op := Op{
        Type:     "Get",
        Key:      args.Key,
        ClientId: args.ClientId,
        SeqNum:   args.SeqNum,
    }

    result := kv.executeOp(op)
    reply.Err = result.Err
    reply.Value = result.Value
}
```

2. **操作执行**
```go
func (kv *KVServer) executeOp(op Op) OpResult {
    index, _, isLeader := kv.rf.Start(op)
    if !isLeader {
        return OpResult{Err: ErrWrongLeader}
    }

    kv.mu.Lock()
    notifyCh := make(chan OpResult, 1)
    kv.notifyCh[index] = notifyCh
    kv.mu.Unlock()

    select {
    case result := <-notifyCh:
        return result
    case <-time.After(500 * time.Millisecond):
        return OpResult{Err: ErrTimeout}
    }
}
```

3. **应用日志条目**
```go
func (kv *KVServer) applyCommand(msg raft.ApplyMsg) {
    kv.mu.Lock()
    defer kv.mu.Unlock()

    if msg.CommandIndex <= kv.lastApplied {
        return
    }

    op := msg.Command.(Op)
    result := OpResult{}

    // Check if this operation has already been applied
    if lastSeq, exists := kv.clientSeq[op.ClientId]; exists && op.SeqNum <= lastSeq {
        // Duplicate operation, return cached result
        if op.Type == "Get" {
            result.Value = kv.data[op.Key]
            result.Err = OK
        } else {
            result.Err = OK
        }
    } else {
        // Apply the operation
        switch op.Type {
        case "Get":
            value, exists := kv.data[op.Key]
            if exists {
                result.Value = value
                result.Err = OK
            } else {
                result.Err = ErrNoKey
            }
        case "Put":
            kv.data[op.Key] = op.Value
            result.Err = OK
        case "Append":
            kv.data[op.Key] += op.Value
            result.Err = OK
        }
        kv.clientSeq[op.ClientId] = op.SeqNum
    }

    kv.lastApplied = msg.CommandIndex

    // Notify waiting RPC handler
    if notifyCh, exists := kv.notifyCh[msg.CommandIndex]; exists {
        select {
        case notifyCh <- result:
        default:
        }
        delete(kv.notifyCh, msg.CommandIndex)
    }

    // Check if we need to take a snapshot
    if kv.maxraftstate != -1 && kv.persister.RaftStateSize() >= kv.maxraftstate {
        kv.takeSnapshot(msg.CommandIndex)
    }
}
```

### 客户端实现

#### 核心数据结构
```go
type Clerk struct {
    servers []*labrpc.ClientEnd
    clientId int64
    seqNum   int64
    leaderId int
}
```

#### 请求重试逻辑
```go
func (ck *Clerk) Get(key string) string {
    ck.seqNum++
    args := GetArgs{
        Key:      key,
        ClientId: ck.clientId,
        SeqNum:   ck.seqNum,
    }

    for {
        reply := GetReply{}
        ok := ck.servers[ck.leaderId].Call("KVServer.Get", &args, &reply)

        if ok {
            switch reply.Err {
            case OK:
                return reply.Value
            case ErrNoKey:
                return ""
            case ErrWrongLeader:
                ck.leaderId = (ck.leaderId + 1) % len(ck.servers)
            case ErrTimeout:
                // Retry with same leader
            }
        } else {
            // RPC failed, try next server
            ck.leaderId = (ck.leaderId + 1) % len(ck.servers)
        }

        time.Sleep(100 * time.Millisecond)
    }
}
```

### 重复检测机制

#### 问题描述
- 网络分区可能导致客户端重发请求
- 服务器需要识别并过滤重复操作
- 确保每个操作只执行一次

#### 解决方案
1. **客户端标识**: 每个客户端有唯一的ID
2. **序列号**: 每个请求有递增的序列号
3. **服务器端缓存**: 记录每个客户端的最后序列号

```go
// Check if this operation has already been applied
if lastSeq, exists := kv.clientSeq[op.ClientId]; exists && op.SeqNum <= lastSeq {
    // Duplicate operation, return cached result
    if op.Type == "Get" {
        result.Value = kv.data[op.Key]
        result.Err = OK
    } else {
        result.Err = OK
    }
} else {
    // Apply the operation
    // ...
    kv.clientSeq[op.ClientId] = op.SeqNum
}
```

### 快照和日志压缩

#### 快照创建
```go
func (kv *KVServer) takeSnapshot(index int) {
    w := new(bytes.Buffer)
    e := gob.NewEncoder(w)
    e.Encode(kv.data)
    e.Encode(kv.clientSeq)
    e.Encode(kv.lastApplied)
    snapshot := w.Bytes()

    kv.rf.Snapshot(index, snapshot)
}
```

#### 快照恢复
```go
func (kv *KVServer) applySnapshot(msg raft.ApplyMsg) {
    kv.mu.Lock()
    defer kv.mu.Unlock()

    if msg.SnapshotIndex <= kv.lastApplied {
        return
    }

    r := bytes.NewBuffer(msg.Snapshot)
    d := gob.NewDecoder(r)

    var data map[string]string
    var clientSeq map[int64]int64
    var lastApplied int

    if d.Decode(&data) != nil ||
       d.Decode(&clientSeq) != nil ||
       d.Decode(&lastApplied) != nil {
        log.Fatal("Failed to decode snapshot")
    } else {
        kv.data = data
        kv.clientSeq = clientSeq
        kv.lastApplied = lastApplied
    }
}
```

## 关键设计决策

### 1. 线性化语义
- 所有操作通过Raft日志序列化
- 确保强一致性和线性化语义
- 客户端看到的是全局一致的状态

### 2. 领导者处理
- 只有领导者处理客户端请求
- 非领导者返回ErrWrongLeader
- 客户端自动重试其他服务器

### 3. 超时处理
- 设置合理的RPC超时（500ms）
- 避免长时间阻塞客户端
- 支持快速故障检测

### 4. 并发控制
- 使用互斥锁保护共享状态
- 通过通道进行异步通知
- 避免死锁和竞态条件

## 性能优化

### 1. 领导者缓存
```go
type Clerk struct {
    // ...
    leaderId int  // Cache the leader
}
```
- 客户端缓存当前领导者
- 减少不必要的重试
- 提高请求成功率

### 2. 批量操作
- Raft层支持批量日志条目
- 减少网络往返次数
- 提高吞吐量

### 3. 快照优化
- 定期创建快照压缩日志
- 减少内存使用
- 加速故障恢复

## 容错机制

### 1. 服务器故障
- Raft自动处理服务器故障
- 选举新的领导者
- 客户端自动重试

### 2. 网络分区
- 多数派继续提供服务
- 少数派拒绝客户端请求
- 分区恢复后自动同步

### 3. 消息丢失
- 客户端重试机制
- 服务器重复检测
- 确保操作幂等性

## 文件结构

```
lab3-kvraft/
├── kvraft/
│   ├── server.go        # KVServer实现
│   ├── client.go        # Clerk客户端实现
│   └── common.go        # 共享定义
└── README.md            # 本文档
```

## 测试

### 运行测试
```bash
cd lab3-kvraft/kvraft
go test -run 3A  # 测试基本功能
go test -run 3B  # 测试快照功能
```

### 测试覆盖
- **基本功能**: Get、Put、Append操作
- **并发性**: 多客户端并发访问
- **容错性**: 服务器故障和网络分区
- **一致性**: 线性化语义验证
- **快照**: 日志压缩和恢复

## 使用示例

### 启动服务器
```go
servers := make([]*labrpc.ClientEnd, 3)
for i := 0; i < 3; i++ {
    servers[i] = labrpc.MakeEnd(fmt.Sprintf("server-%d", i))
}

kv := StartKVServer(servers, 0, persister, 1000)
```

### 客户端操作
```go
ck := MakeClerk(servers)

// Put操作
ck.Put("key1", "value1")

// Get操作
value := ck.Get("key1")  // returns "value1"

// Append操作
ck.Append("key1", "_append")
value = ck.Get("key1")  // returns "value1_append"
```

## 性能特点

1. **强一致性**: 线性化语义保证
2. **高可用性**: 容忍少数派故障
3. **自动恢复**: 故障后自动恢复服务
4. **可扩展性**: 支持水平扩展

## 扩展可能

1. **读优化**: 实现只读请求的优化处理
2. **批量操作**: 支持批量Put/Get操作
3. **事务支持**: 添加多键事务功能
4. **性能监控**: 添加详细的性能指标
5. **压缩优化**: 改进快照压缩策略