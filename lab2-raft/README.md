# Lab 2: Raft 分布式共识算法实现

## 项目概述

本实验实现了Raft共识算法，这是一个用于分布式系统中管理复制日志的共识协议。Raft算法将共识问题分解为三个相对独立的子问题：**领导者选举**、**日志复制**和**安全性保证**。

## 实验分解

### Lab 2A: 领导者选举
- 实现Raft的领导者选举机制
- 处理选举超时和心跳机制
- 确保在网络分区情况下的正确性

### Lab 2B: 日志复制
- 实现日志条目的复制和提交
- 处理日志不一致的情况
- 实现客户端命令的处理

### Lab 2C: 持久化
- 实现关键状态的持久化存储
- 确保节点重启后的状态恢复
- 处理崩溃恢复场景

### Lab 2D: 日志压缩（快照）
- 实现日志快照机制
- 减少内存使用和恢复时间
- 实现快照的安装和传输

## 核心数据结构

### Raft 结构体
```go
type Raft struct {
    mu        sync.Mutex          // 保护共享状态的互斥锁
    peers     []*labrpc.ClientEnd // 所有节点的RPC端点
    persister *Persister          // 持久化存储对象
    me        int                 // 当前节点在peers中的索引
    dead      int32               // 节点是否被终止

    // 持久化状态（所有服务器）
    currentTerm int        // 当前任期号
    votedFor    int        // 当前任期投票给的候选者ID
    log         []LogEntry // 日志条目数组

    // 易失性状态（所有服务器）
    commitIndex int // 已知已提交的最高日志条目索引
    lastApplied int // 已应用到状态机的最高日志条目索引

    // 易失性状态（仅领导者）
    nextIndex  []int // 发送给每个服务器的下一个日志条目索引
    matchIndex []int // 已知已复制到每个服务器的最高日志条目索引

    // 附加状态
    state          ServerState   // 服务器状态（Follower/Candidate/Leader）
    electionTimer  *time.Timer   // 选举超时定时器
    heartbeatTimer *time.Timer   // 心跳定时器
    applyCh        chan ApplyMsg // 应用消息通道
    applyCond      *sync.Cond    // 应用条件变量

    // 快照相关（Lab 2D）
    lastIncludedIndex int // 快照中包含的最后一个日志条目索引
    lastIncludedTerm  int // 快照中包含的最后一个日志条目任期
}
```

### 日志条目结构
```go
type LogEntry struct {
    Term    int         // 日志条目的任期号
    Command interface{} // 状态机命令
}
```

### 服务器状态枚举
```go
type ServerState int

const (
    Follower  ServerState = iota // 跟随者
    Candidate                    // 候选者
    Leader                       // 领导者
)
```

## 核心RPC接口

### RequestVote RPC
用于候选者请求选票的RPC调用。

**请求参数 (RequestVoteArgs):**
- `Term`: 候选者的任期号
- `CandidateId`: 请求选票的候选者ID
- `LastLogIndex`: 候选者最后日志条目的索引
- `LastLogTerm`: 候选者最后日志条目的任期号

**响应参数 (RequestVoteReply):**
- `Term`: 当前任期号，用于候选者更新自己
- `VoteGranted`: 如果候选者获得选票则为true

### AppendEntries RPC
用于领导者复制日志条目和发送心跳的RPC调用。

**请求参数 (AppendEntriesArgs):**
- `Term`: 领导者的任期号
- `LeaderId`: 领导者ID
- `PrevLogIndex`: 新日志条目前一个条目的索引
- `PrevLogTerm`: 新日志条目前一个条目的任期号
- `Entries[]`: 要存储的日志条目（心跳时为空）
- `LeaderCommit`: 领导者的commitIndex

**响应参数 (AppendEntriesReply):**
- `Term`: 当前任期号，用于领导者更新自己
- `Success`: 如果跟随者包含匹配prevLogIndex和prevLogTerm的条目则为true
- `ConflictIndex`: 冲突条目的索引（用于快速回退）
- `ConflictTerm`: 冲突条目的任期号

### InstallSnapshot RPC (Lab 2D)
用于领导者向跟随者发送快照的RPC调用。

**请求参数 (InstallSnapshotArgs):**
- `Term`: 领导者的任期号
- `LeaderId`: 领导者ID
- `LastIncludedIndex`: 快照替换的最后一个日志条目索引
- `LastIncludedTerm`: 快照替换的最后一个日志条目任期
- `Data[]`: 快照数据

## 关键算法实现

### 领导者选举流程

1. **选举超时**: 使用随机化超时（150-300ms）避免选票分裂
2. **发起选举**: 增加任期号，转为候选者，为自己投票
3. **请求选票**: 并行向所有其他服务器发送RequestVote RPC
4. **处理结果**: 
   - 获得多数选票 → 成为领导者
   - 收到更高任期 → 转为跟随者
   - 选举超时 → 开始新一轮选举

### 日志复制机制

1. **接收客户端请求**: 领导者将命令追加到本地日志
2. **并行复制**: 向所有跟随者发送AppendEntries RPC
3. **等待多数确认**: 收到多数服务器的成功响应
4. **提交并应用**: 更新commitIndex并应用到状态机
5. **通知跟随者**: 在后续RPC中告知新的commitIndex

### 日志一致性保证

- **日志匹配属性**: 如果两个日志在某个索引处的条目有相同的任期号，则它们在该索引之前的所有条目都相同
- **领导者完整性**: 如果某个日志条目在给定任期号中已经被提交，那么这个条目必然出现在更大任期号的所有领导者的日志中
- **状态机安全性**: 如果服务器已经在给定索引处应用了日志条目，则其他服务器不会在该索引处应用不同的日志条目

## 持久化机制

### 需要持久化的状态
- `currentTerm`: 当前任期号
- `votedFor`: 投票记录
- `log[]`: 日志条目数组
- `lastIncludedIndex`: 快照最后索引
- `lastIncludedTerm`: 快照最后任期

### 持久化实现
```go
func (rf *Raft) persist() {
    w := new(bytes.Buffer)
    e := gob.NewEncoder(w)
    e.Encode(rf.currentTerm)
    e.Encode(rf.votedFor)
    e.Encode(rf.log)
    e.Encode(rf.lastIncludedIndex)
    e.Encode(rf.lastIncludedTerm)
    data := w.Bytes()
    rf.persister.SaveRaftState(data)
}
```

## 快照机制 (Lab 2D)

### 快照的作用
- 减少日志大小，节省存储空间
- 加快节点重启和新节点加入的速度
- 防止日志无限增长

### 快照实现要点
- 应用层触发快照创建
- 保留快照后的日志条目
- 通过InstallSnapshot RPC同步快照
- 正确处理快照与日志的边界

## 测试框架

### 测试配置 (config.go)
- 模拟网络分区和节点故障
- 提供不可靠网络环境测试
- 统计RPC调用次数和字节传输量
- 验证日志一致性和安全性

### RPC模拟 (labrpc.go)
- 模拟真实网络环境
- 支持消息丢失、延迟和重排序
- 提供可靠性控制接口

### 持久化模拟 (persister.go)
- 模拟磁盘存储
- 支持状态和快照的原子保存
- 提供崩溃恢复测试支持

## 运行测试

```bash
cd lab2-raft/raft

# 测试领导者选举
go test -run 2A

# 测试日志复制
go test -run 2B

# 测试持久化
go test -run 2C

# 测试快照
go test -run 2D

# 运行所有测试
go test

# 运行竞态检测
go test -race

# 多次运行测试确保稳定性
for i in {1..10}; do go test -run 2A; done
```

## 性能优化

### 1. 快速日志回退
- 使用冲突任期信息快速定位正确的nextIndex
- 避免逐个递减nextIndex的低效方式

### 2. 批量日志应用
- 使用单独的goroutine处理日志应用
- 批量应用多个已提交的日志条目
- 使用条件变量避免忙等待

### 3. 异步RPC发送
- 并行发送RPC避免阻塞
- 使用goroutine处理RPC响应
- 合理设置RPC超时时间

## 调试技巧

### 1. 日志记录
```go
// 添加详细的调试日志
fmt.Printf("[%d] Term %d: became leader\n", rf.me, rf.currentTerm)
```

### 2. 状态检查
- 定期检查不变量
- 验证日志一致性
- 监控选举和心跳超时

### 3. 竞态检测
```bash
# 使用竞态检测器
go test -race -run TestBasicAgree2B
```

## 常见问题

### 1. 选票分裂
**问题**: 多个候选者同时发起选举导致没有候选者获得多数选票
**解决**: 使用随机化的选举超时时间

### 2. 日志不一致
**问题**: 网络分区导致日志分叉
**解决**: 实现正确的日志回退和覆盖机制

### 3. 活锁问题
**问题**: 频繁的领导者变更导致系统无法取得进展
**解决**: 合理设置超时时间，实现领导者租约机制

### 4. 快照边界问题
**问题**: 快照与日志的索引边界处理错误
**解决**: 仔细处理lastIncludedIndex的边界情况

## 扩展思考

1. **成员变更**: 如何安全地添加或删除集群节点？
2. **只读优化**: 如何优化只读请求的处理？
3. **批量操作**: 如何支持批量客户端请求？
4. **性能监控**: 如何添加详细的性能指标？
5. **网络优化**: 如何减少网络通信开销？

## 参考资料

- [Raft论文](https://raft.github.io/raft.pdf)
- [Raft可视化](http://thesecretlivesofdata.com/raft/)
- [MIT 6.824课程](https://pdos.csail.mit.edu/6.824/)
- [Raft作者的博客](https://ramcloud.atlassian.net/wiki/display/RAM/Raft)