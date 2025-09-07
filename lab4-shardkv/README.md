# Lab 4: 分片键值服务系统

## 概述

本实验构建了一个分片键值存储系统，将键值对分布在多个副本组中以提高性能和可扩展性。系统包含两个主要组件：分片控制器（Shard Controller）和分片键值服务器（Sharded Key/Value Server）。

## 架构设计

### 系统组件

1. **分片控制器（Shard Controller）**
   - 管理分片到副本组的分配
   - 处理配置变更（Join、Leave、Move、Query）
   - 使用Raft确保配置的一致性

2. **分片键值服务器（Sharded KV Server）**
   - 每个副本组负责一部分分片
   - 处理客户端的Get、Put、Append请求
   - 在配置变更时进行分片迁移

3. **客户端（Clerk）**
   - 查询分片控制器获取最新配置
   - 向正确的副本组发送请求
   - 处理配置变更和重试逻辑

### 分片机制

```go
// 分片数量
const NShards = 10

// 键到分片的映射
func key2shard(key string) int {
    shard := 0
    if len(key) > 0 {
        shard = int(key[0])
    }
    shard %= shardctrler.NShards
    return shard
}
```

## 分片控制器实现

### 核心数据结构

```go
type ShardCtrler struct {
    mu      sync.Mutex
    me      int
    rf      *raft.Raft
    applyCh chan raft.ApplyMsg
    dead    int32

    configs   []Config                 // indexed by config num
    notifyCh  map[int]chan OpResult    // index -> notification channel
    clientSeq map[int64]int64          // client -> last sequence number
    lastApplied int
}

type Config struct {
    Num    int              // config number
    Shards [NShards]int     // shard -> gid
    Groups map[int][]string // gid -> servers[]
}
```

### 配置操作

#### Join操作
```go
func (sc *ShardCtrler) joinGroups(servers map[int][]string) {
    lastConfig := sc.configs[len(sc.configs)-1]
    newConfig := Config{
        Num:    lastConfig.Num + 1,
        Shards: lastConfig.Shards,
        Groups: make(map[int][]string),
    }

    // Copy existing groups
    for gid, servers := range lastConfig.Groups {
        newConfig.Groups[gid] = servers
    }

    // Add new groups
    for gid, servers := range servers {
        newConfig.Groups[gid] = servers
    }

    // Rebalance shards
    sc.rebalance(&newConfig)
    sc.configs = append(sc.configs, newConfig)
}
```

#### 负载均衡算法
```go
func (sc *ShardCtrler) rebalance(config *Config) {
    if len(config.Groups) == 0 {
        // No groups, assign all shards to group 0
        for i := range config.Shards {
            config.Shards[i] = 0
        }
        return
    }

    // Count shards per group
    shardCount := make(map[int]int)
    for _, gid := range config.Shards {
        if gid != 0 {
            shardCount[gid]++
        }
    }

    // Get sorted list of group IDs
    gids := make([]int, 0, len(config.Groups))
    for gid := range config.Groups {
        gids = append(gids, gid)
        if _, exists := shardCount[gid]; !exists {
            shardCount[gid] = 0
        }
    }
    sort.Ints(gids)

    // Calculate target shard count per group
    target := NShards / len(gids)
    extra := NShards % len(gids)

    // Rebalance logic...
}
```

## 分片键值服务器实现

### 核心数据结构

```go
type ShardKV struct {
    mu           sync.Mutex
    me           int
    rf           *raft.Raft
    applyCh      chan raft.ApplyMsg
    make_end     func(string) *labrpc.ClientEnd
    gid          int
    ctrlers      []*labrpc.ClientEnd
    maxraftstate int
    dead         int32

    config       shardctrler.Config
    lastConfig   shardctrler.Config
    shards       map[int]map[string]string // shard -> key-value data
    clientSeq    map[int64]int64           // client -> last sequence number
    notifyCh     map[int]chan OpResult     // index -> notification channel
    lastApplied  int
    mck          *shardctrler.Clerk
    persister    *raft.Persister

    // For shard migration
    shardStatus map[int]ShardStatus // shard -> status
}

type ShardStatus int

const (
    Serving ShardStatus = iota
    Pulling
    BePulling
    GCing
)
```

### 分片状态管理

#### 分片状态转换
- **Serving**: 正常服务状态
- **Pulling**: 正在拉取分片数据
- **BePulling**: 等待其他组拉取分片数据
- **GCing**: 垃圾回收状态

#### 配置变更处理
```go
func (kv *ShardKV) applyReconfigure(op Op) {
    if op.Config.Num <= kv.config.Num {
        return
    }

    kv.lastConfig = kv.config
    kv.config = op.Config

    // Update shard status
    for shard := 0; shard < shardctrler.NShards; shard++ {
        if kv.lastConfig.Shards[shard] == kv.gid && kv.config.Shards[shard] != kv.gid {
            // We no longer own this shard
            kv.shardStatus[shard] = BePulling
        } else if kv.lastConfig.Shards[shard] != kv.gid && kv.config.Shards[shard] == kv.gid {
            // We now own this shard
            if kv.lastConfig.Shards[shard] == 0 {
                // New shard, no migration needed
                kv.shardStatus[shard] = Serving
            } else {
                // Need to pull this shard
                kv.shardStatus[shard] = Pulling
            }
        }
    }
}
```

### 分片迁移机制

#### 分片拉取
```go
func (kv *ShardKV) pullShard(shard, gid, configNum int) {
    kv.mu.Lock()
    servers := kv.lastConfig.Groups[gid]
    kv.mu.Unlock()

    args := &PullShardArgs{
        Shard:     shard,
        ConfigNum: configNum,
    }

    for _, server := range servers {
        srv := kv.make_end(server)
        reply := &PullShardReply{}
        ok := srv.Call("ShardKV.PullShard", args, reply)
        if ok && reply.Err == OK {
            op := Op{
                Type:      "InstallShard",
                Shard:     shard,
                ShardData: reply.ShardData,
                ClientSeq: reply.ClientSeq,
                ConfigNum: configNum,
            }
            kv.rf.Start(op)
            return
        }
    }
}
```

#### 分片安装
```go
func (kv *ShardKV) applyInstallShard(op Op) {
    if op.ConfigNum != kv.config.Num {
        return
    }

    if kv.shardStatus[op.Shard] == Pulling {
        kv.shards[op.Shard] = make(map[string]string)
        for k, v := range op.ShardData {
            kv.shards[op.Shard][k] = v
        }

        // Update client sequences
        for clientId, seq := range op.ClientSeq {
            if kv.clientSeq[clientId] < seq {
                kv.clientSeq[clientId] = seq
            }
        }

        kv.shardStatus[op.Shard] = Serving
    }
}
```

### 后台监控进程

#### 配置监控
```go
func (kv *ShardKV) configMonitor() {
    for !kv.killed() {
        if _, isLeader := kv.rf.GetState(); isLeader {
            kv.mu.Lock()
            currentConfigNum := kv.config.Num
            kv.mu.Unlock()

            nextConfig := kv.mck.Query(currentConfigNum + 1)
            if nextConfig.Num == currentConfigNum+1 {
                // Check if we can reconfigure
                if kv.canReconfigure() {
                    op := Op{
                        Type:   "Reconfigure",
                        Config: nextConfig,
                    }
                    kv.rf.Start(op)
                }
            }
        }
        time.Sleep(100 * time.Millisecond)
    }
}
```

#### 迁移监控
```go
func (kv *ShardKV) migrationMonitor() {
    for !kv.killed() {
        if _, isLeader := kv.rf.GetState(); isLeader {
            kv.pullShards()
            kv.deleteShards()
        }
        time.Sleep(50 * time.Millisecond)
    }
}
```

## 客户端实现

### 核心数据结构
```go
type Clerk struct {
    sm       *shardctrler.Clerk
    config   shardctrler.Config
    make_end func(string) *labrpc.ClientEnd
    clientId int64
    seqNum   int64
    leaders  map[int]int // gid -> leader index
}
```

### 请求处理流程
```go
func (ck *Clerk) Get(key string) string {
    ck.seqNum++
    args := GetArgs{
        Key:      key,
        ClientId: ck.clientId,
        SeqNum:   ck.seqNum,
    }

    for {
        shard := key2shard(key)
        gid := ck.config.Shards[shard]
        if servers, ok := ck.config.Groups[gid]; ok {
            // try each server for the shard.
            leaderIdx := ck.leaders[gid]
            for i := 0; i < len(servers); i++ {
                srv := ck.make_end(servers[leaderIdx])
                reply := GetReply{}
                ok := srv.Call("ShardKV.Get", &args, &reply)
                if ok && (reply.Err == OK || reply.Err == ErrNoKey) {
                    return reply.Value
                }
                if ok && reply.Err == ErrWrongGroup {
                    break
                }
                // ... not ok, or ErrWrongLeader
                leaderIdx = (leaderIdx + 1) % len(servers)
                ck.leaders[gid] = leaderIdx
            }
        }
        time.Sleep(100 * time.Millisecond)
        // ask controler for the latest configuration.
        ck.config = ck.sm.Query(-1)
    }
}
```

## 关键设计决策

### 1. 分片迁移的原子性
- 使用Raft日志确保分片迁移的原子性
- 分片状态变更通过Raft同步
- 避免分片数据的丢失或重复

### 2. 配置变更的顺序性
- 严格按照配置版本号顺序处理
- 确保所有副本组看到相同的配置序列
- 避免配置不一致导致的数据丢失

### 3. 分片状态管理
- 明确的分片状态转换
- 防止在迁移过程中服务请求
- 确保数据一致性

### 4. 垃圾回收机制
- 及时清理已迁移的分片数据
- 避免内存泄漏
- 确保系统的长期稳定性

## 容错机制

### 1. 网络分区处理
- 分片控制器使用Raft确保配置一致性
- 客户端自动重试和配置更新
- 副本组内部使用Raft处理分区

### 2. 服务器故障
- Raft自动处理副本组内的故障
- 客户端自动切换到其他副本
- 分片迁移过程中的故障恢复

### 3. 配置不一致
- 严格的配置版本控制
- 分片迁移的幂等性
- 重复检测和去重

## 性能优化

### 1. 并行分片迁移
```go
func (kv *ShardKV) pullShards() {
    kv.mu.Lock()
    pullTasks := make(map[int]int) // shard -> gid
    for shard, status := range kv.shardStatus {
        if status == Pulling {
            pullTasks[shard] = kv.lastConfig.Shards[shard]
        }
    }
    configNum := kv.config.Num
    kv.mu.Unlock()

    for shard, gid := range pullTasks {
        go kv.pullShard(shard, gid, configNum)
    }
}
```

### 2. 领导者缓存
- 客户端缓存每个副本组的领导者
- 减少不必要的重试
- 提高请求成功率

### 3. 批量操作
- 支持批量分片迁移
- 减少网络往返次数
- 提高迁移效率

## 文件结构

```
lab4-shardkv/
├── shardctrler/
│   ├── server.go        # 分片控制器服务器
│   ├── client.go        # 分片控制器客户端
│   └── common.go        # 共享定义
├── shardkv/
│   ├── server.go        # 分片KV服务器
│   ├── client.go        # 分片KV客户端
│   └── common.go        # 共享定义
└── README.md            # 本文档
```

## 测试

### 运行测试
```bash
# 测试分片控制器
cd lab4-shardkv/shardctrler
go test

# 测试分片键值服务
cd lab4-shardkv/shardkv
go test
```

### 测试覆盖
- **基本功能**: Join、Leave、Move、Query操作
- **负载均衡**: 分片的均匀分布
- **分片迁移**: 配置变更时的数据迁移
- **容错性**: 网络分区和服务器故障
- **并发性**: 多客户端并发访问
- **一致性**: 强一致性保证

## 使用示例

### 启动分片控制器
```go
ctrlers := make([]*labrpc.ClientEnd, 3)
for i := 0; i < 3; i++ {
    ctrlers[i] = labrpc.MakeEnd(fmt.Sprintf("ctrler-%d", i))
}

sc := StartServer(ctrlers, 0, persister)
```

### 启动分片键值服务
```go
servers := make([]*labrpc.ClientEnd, 3)
for i := 0; i < 3; i++ {
    servers[i] = labrpc.MakeEnd(fmt.Sprintf("server-%d", i))
}

kv := StartServer(servers, 0, persister, 1000, 1, ctrlers, make_end)
```

### 客户端操作
```go
ck := MakeClerk(ctrlers, make_end)

// Put操作
ck.Put("key1", "value1")

// Get操作
value := ck.Get("key1")  // returns "value1"

// Append操作
ck.Append("key1", "_append")
value = ck.Get("key1")  // returns "value1_append"
```

## 性能特点

1. **水平扩展**: 支持动态添加副本组
2. **负载均衡**: 自动分片重新分布
3. **高可用性**: 容忍副本组故障
4. **强一致性**: 线性化语义保证
5. **高吞吐量**: 并行处理不同分片

## 扩展可能

1. **动态分片**: 支持分片数量的动态调整
2. **智能负载均衡**: 基于负载的分片分配
3. **跨数据中心**: 支持地理分布式部署
4. **压缩优化**: 改进分片迁移的压缩策略
5. **监控系统**: 添加详细的性能和健康监控
6. **热点检测**: 自动检测和处理热点分片