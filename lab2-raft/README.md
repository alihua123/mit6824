# Lab 2: Raft共识算法实现

## 概述

本实验实现了Raft共识算法，这是一个用于管理复制日志的共识算法。Raft算法被设计为易于理解，它将共识问题分解为相对独立的子问题：领导者选举、日志复制和安全性。

## 架构设计

### 核心组件

1. **Raft节点状态**
   - **Follower（跟随者）**: 被动接收来自领导者和候选者的RPC
   - **Candidate（候选者）**: 用于选举新的领导者
   - **Leader（领导者）**: 处理所有客户端请求，管理日志复制

2. **持久化状态**
   - `currentTerm`: 服务器已知的最新任期
   - `votedFor`: 在当前任期内收到选票的候选者ID
   - `log[]`: 日志条目数组

3. **易失性状态**
   - `commitIndex`: 已知已提交的最高日志条目索引
   - `lastApplied`: 已应用到状态机的最高日志条目索引

4. **领导者易失性状态**
   - `nextIndex[]`: 对于每个服务器，发送到该服务器的下一个日志条目索引
   - `matchIndex[]`: 对于每个服务器，已知已复制到该服务器的最高日志条目索引

## 实现细节

### 领导者选举（Lab 2A）

#### 选举超时机制
```go
func (rf *Raft) resetElectionTimer() {
    if rf.electionTimer != nil {
        rf.electionTimer.Stop()
    }
    timeout := time.Duration(150+rand.Intn(150)) * time.Millisecond
    rf.electionTimer = time.AfterFunc(timeout, rf.startElection)
}
```

- 使用随机化的选举超时（150-300ms）避免选票分裂
- 当收到有效的AppendEntries或投票给候选者时重置定时器

#### 选举过程
1. **发起选举**: 增加当前任期，转换为候选者状态，为自己投票
2. **请求选票**: 并行向所有其他服务器发送RequestVote RPC
3. **收集选票**: 如果收到大多数选票，成为领导者
4. **处理失败**: 如果选举超时或发现更高任期，回到跟随者状态

#### RequestVote RPC实现
```go
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
    rf.mu.Lock()
    defer rf.mu.Unlock()

    reply.Term = rf.currentTerm
    reply.VoteGranted = false

    // 拒绝过时的请求
    if args.Term < rf.currentTerm {
        return
    }

    // 更新任期并转为跟随者
    if args.Term > rf.currentTerm {
        rf.currentTerm = args.Term
        rf.votedFor = -1
        rf.state = Follower
        rf.persist()
    }

    // 投票条件：未投票且候选者日志至少和自己一样新
    if (rf.votedFor == -1 || rf.votedFor == args.CandidateId) && 
       rf.isLogUpToDate(args.LastLogIndex, args.LastLogTerm) {
        rf.votedFor = args.CandidateId
        reply.VoteGranted = true
        rf.resetElectionTimer()
        rf.persist()
    }
}
```

### 日志复制（Lab 2B）

#### AppendEntries RPC
```go
func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
    rf.mu.Lock()
    defer rf.mu.Unlock()

    reply.Term = rf.currentTerm
    reply.Success = false

    // 拒绝过时的请求
    if args.Term < rf.currentTerm {
        return
    }

    // 更新任期并转为跟随者
    if args.Term > rf.currentTerm {
        rf.currentTerm = args.Term
        rf.votedFor = -1
        rf.persist()
    }

    rf.state = Follower
    rf.resetElectionTimer()

    // 日志一致性检查
    if args.PrevLogIndex > rf.getLastLogIndex() {
        reply.ConflictIndex = rf.getLastLogIndex() + 1
        reply.ConflictTerm = -1
        return
    }

    if args.PrevLogIndex >= rf.lastIncludedIndex && 
       rf.getLogTerm(args.PrevLogIndex) != args.PrevLogTerm {
        reply.ConflictTerm = rf.getLogTerm(args.PrevLogIndex)
        reply.ConflictIndex = rf.findFirstIndexOfTerm(reply.ConflictTerm)
        return
    }

    // 处理日志条目冲突
    for i, entry := range args.Entries {
        index := args.PrevLogIndex + 1 + i
        if index <= rf.getLastLogIndex() && rf.getLogTerm(index) != entry.Term {
            rf.log = rf.log[:index-rf.lastIncludedIndex-1]
            break
        }
    }

    // 追加新条目
    for i, entry := range args.Entries {
        index := args.PrevLogIndex + 1 + i
        if index > rf.getLastLogIndex() {
            rf.log = append(rf.log, entry)
        }
    }

    rf.persist()

    // 更新提交索引
    if args.LeaderCommit > rf.commitIndex {
        rf.commitIndex = min(args.LeaderCommit, rf.getLastLogIndex())
        rf.applyCond.Signal()
    }

    reply.Success = true
}
```

#### 心跳和日志复制
- 领导者定期发送心跳（100ms间隔）维持权威
- 当有新的客户端请求时，立即发送AppendEntries
- 使用优化的回退策略加速日志修复

### 持久化（Lab 2C）

#### 状态持久化
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

#### 状态恢复
```go
func (rf *Raft) readPersist(data []byte) {
    if data == nil || len(data) < 1 {
        return
    }
    r := bytes.NewBuffer(data)
    d := gob.NewDecoder(r)
    var currentTerm int
    var votedFor int
    var logEntries []LogEntry
    var lastIncludedIndex int
    var lastIncludedTerm int

    if d.Decode(&currentTerm) != nil ||
       d.Decode(&votedFor) != nil ||
       d.Decode(&logEntries) != nil ||
       d.Decode(&lastIncludedIndex) != nil ||
       d.Decode(&lastIncludedTerm) != nil {
        log.Fatal("Failed to decode persisted state")
    } else {
        rf.currentTerm = currentTerm
        rf.votedFor = votedFor
        rf.log = logEntries
        rf.lastIncludedIndex = lastIncludedIndex
        rf.lastIncludedTerm = lastIncludedTerm
    }
}
```

### 日志压缩（Lab 2D）

#### 快照机制
```go
func (rf *Raft) Snapshot(index int, snapshot []byte) {
    rf.mu.Lock()
    defer rf.mu.Unlock()

    if index <= rf.lastIncludedIndex {
        return
    }

    // 截断日志
    rf.log = rf.log[index-rf.lastIncludedIndex:]
    rf.lastIncludedTerm = rf.getLogTerm(index)
    rf.lastIncludedIndex = index

    rf.persister.SaveStateAndSnapshot(rf.encodeState(), snapshot)
}
```

#### InstallSnapshot RPC
```go
func (rf *Raft) InstallSnapshot(args *InstallSnapshotArgs, reply *InstallSnapshotReply) {
    rf.mu.Lock()
    defer rf.mu.Unlock()

    reply.Term = rf.currentTerm

    if args.Term < rf.currentTerm {
        return
    }

    if args.Term > rf.currentTerm {
        rf.currentTerm = args.Term
        rf.votedFor = -1
        rf.persist()
    }

    rf.state = Follower
    rf.resetElectionTimer()

    // 忽略过时的快照
    if args.LastIncludedIndex <= rf.lastIncludedIndex {
        return
    }

    // 应用快照
    go func() {
        rf.applyCh <- ApplyMsg{
            SnapshotValid: true,
            Snapshot:      args.Data,
            SnapshotTerm:  args.LastIncludedTerm,
            SnapshotIndex: args.LastIncludedIndex,
        }
    }()
}
```

## 关键优化

### 1. 快速日志回退
- 当AppendEntries失败时，使用冲突任期信息快速定位正确的nextIndex
- 避免逐个递减nextIndex的低效方式

### 2. 批量应用
```go
func (rf *Raft) applier() {
    for !rf.killed() {
        rf.mu.Lock()
        for rf.lastApplied >= rf.commitIndex {
            rf.applyCond.Wait()
            if rf.killed() {
                rf.mu.Unlock()
                return
            }
        }

        commitIndex := rf.commitIndex
        lastApplied := rf.lastApplied
        entries := make([]LogEntry, commitIndex-lastApplied)
        copy(entries, rf.log[lastApplied+1-rf.lastIncludedIndex-1:commitIndex-rf.lastIncludedIndex])
        rf.mu.Unlock()

        for i, entry := range entries {
            rf.applyCh <- ApplyMsg{
                CommandValid: true,
                Command:      entry.Command,
                CommandIndex: lastApplied + 1 + i,
            }
        }

        rf.mu.Lock()
        rf.lastApplied = commitIndex
        rf.mu.Unlock()
    }
}
```

### 3. 提交索引更新
```go
func (rf *Raft) updateCommitIndex() {
    for n := rf.getLastLogIndex(); n > rf.commitIndex; n-- {
        count := 1
        for i := range rf.peers {
            if i != rf.me && rf.matchIndex[i] >= n {
                count++
            }
        }
        if count > len(rf.peers)/2 && rf.getLogTerm(n) == rf.currentTerm {
            rf.commitIndex = n
            rf.applyCond.Signal()
            break
        }
    }
}
```

## 文件结构

```
lab2-raft/
├── raft/
│   ├── raft.go          # 主要Raft实现
│   ├── persister.go     # 持久化管理
│   └── config.go        # 测试配置
├── labrpc/
│   └── labrpc.go        # RPC模拟框架
└── README.md            # 本文档
```

## 测试

### 运行测试
```bash
cd lab2-raft/raft
go test -run 2A  # 测试领导者选举
go test -run 2B  # 测试日志复制
go test -run 2C  # 测试持久化
go test -run 2D  # 测试快照
```

### 测试覆盖
- **2A**: 初始选举、重新选举、多个选举
- **2B**: 基本一致性、失败后一致性、失败和恢复
- **2C**: 持久化状态、图8不可靠性测试
- **2D**: 快照基础功能、快照安装、不可靠快照

## 性能特点

1. **选举效率**: 随机化超时减少选票分裂
2. **日志复制**: 批量发送和快速回退优化
3. **容错性**: 处理网络分区、节点故障和消息丢失
4. **一致性**: 强一致性保证，线性化语义

## 关键设计决策

1. **超时设置**: 选举超时150-300ms，心跳间隔100ms
2. **并发控制**: 使用互斥锁保护共享状态
3. **RPC优化**: 异步发送RPC避免阻塞
4. **状态机**: 清晰的状态转换逻辑

## 调试技巧

1. **日志记录**: 详细记录状态变化和RPC交互
2. **竞态检测**: 使用`go test -race`检测竞态条件
3. **压力测试**: 长时间运行测试验证稳定性
4. **可视化**: 使用日志分析工具理解执行流程

## 扩展可能

1. **成员变更**: 支持动态添加/删除节点
2. **只读优化**: 实现只读请求的优化处理
3. **批量操作**: 支持批量客户端请求
4. **性能监控**: 添加详细的性能指标收集