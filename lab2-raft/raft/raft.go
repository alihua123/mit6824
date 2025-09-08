//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"log"
	"math/rand"
	"mit6824/lab2-raft/labrpc"
	"sync"
	"sync/atomic"
	"time"
)

// ApplyMsg - 应用消息结构体，用于向上层应用传递已提交的日志条目
// ApplyMsg - Apply message struct for delivering committed log entries to upper application
type ApplyMsg struct {
	CommandValid bool        // 命令是否有效 / Whether the command is valid
	Command      interface{} // 要应用的命令 / Command to be applied
	CommandIndex int         // 命令在日志中的索引 / Index of command in log

	// For 2D:
	SnapshotValid bool   // 快照是否有效 / Whether the snapshot is valid
	Snapshot      []byte // 快照数据 / Snapshot data
	SnapshotTerm  int    // 快照对应的任期 / Term corresponding to snapshot
	SnapshotIndex int    // 快照对应的索引 / Index corresponding to snapshot
}

// Raft - Raft节点结构体，包含所有必要的状态信息
// Raft - Raft node struct containing all necessary state information
type Raft struct {
	mu        sync.Mutex          // 保护共享状态的互斥锁 / Mutex to protect shared state
	peers     []*labrpc.ClientEnd // 所有节点的RPC端点 / RPC endpoints of all peers
	persister *Persister          // 持久化对象 / Persister object
	me        int                 // 当前节点在peers数组中的索引 / Index of this peer in peers array
	dead      int32               // 节点是否已终止，由Kill()设置 / Whether peer is killed, set by Kill()

	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.

	// 持久化状态 (在响应RPC之前必须更新到稳定存储)
	// Persistent state (must be updated to stable storage before responding to RPCs)
	currentTerm int        // 服务器已知的最新任期 / Latest term server has seen
	votedFor    int        // 当前任期内收到选票的候选人ID / CandidateId that received vote in current term
	log         []LogEntry // 日志条目数组 / Log entries array

	// 易失性状态 (所有服务器)
	// Volatile state (all servers)
	commitIndex int // 已知已提交的最高日志条目索引 / Index of highest log entry known to be committed
	lastApplied int // 已应用到状态机的最高日志条目索引 / Index of highest log entry applied to state machine

	// 易失性状态 (仅领导者)
	// Volatile state (leaders only)
	nextIndex  []int // 发送给每个服务器的下一个日志条目索引 / Index of next log entry to send to each server
	matchIndex []int // 已知在每个服务器上复制的最高日志条目索引 / Index of highest log entry known to be replicated on each server

	// 额外状态
	// Additional state
	state          ServerState   // 服务器当前状态 / Current server state
	electionTimer  *time.Timer   // 选举超时定时器 / Election timeout timer
	heartbeatTimer *time.Timer   // 心跳定时器 / Heartbeat timer
	applyCh        chan ApplyMsg // 应用消息通道 / Apply message channel
	applyCond      *sync.Cond    // 应用条件变量 / Apply condition variable

	// 快照相关状态 (2D)
	// Snapshot related state (2D)
	lastIncludedIndex int // 快照中包含的最后一个日志条目的索引 / Index of last log entry included in snapshot
	lastIncludedTerm  int // 快照中包含的最后一个日志条目的任期 / Term of last log entry included in snapshot
}

// LogEntry - 日志条目结构体
// LogEntry - Log entry struct
type LogEntry struct {
	Term    int         // 条目被创建时的任期 / Term when entry was created
	Command interface{} // 状态机命令 / State machine command
}

// ServerState - 服务器状态枚举
// ServerState - Server state enumeration
type ServerState int

const (
	Follower  ServerState = iota // 跟随者状态 / Follower state
	Candidate                    // 候选人状态 / Candidate state
	Leader                       // 领导者状态 / Leader state
)

// GetState - 返回当前任期和是否为领导者
// GetState - Return currentTerm and whether this server believes it is the leader.
func (rf *Raft) GetState() (int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.currentTerm, rf.state == Leader
}

// persist - 保存Raft的持久化状态到稳定存储
// persist - Save Raft's persistent state to stable storage
func (rf *Raft) persist() {
	// Your code here (2C).
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

// readPersist - 从之前持久化的状态恢复
// readPersist - Restore previously persisted state
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (2C).
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
		rf.lastApplied = lastIncludedIndex
		rf.commitIndex = lastIncludedIndex
	}
}

// CondInstallSnapshot - 条件安装快照的服务API
// CondInstallSnapshot - A service wants to switch to snapshot. Only do so if Raft hasn't
// have more recent info since it communicate the snapshot on applyCh.
func (rf *Raft) CondInstallSnapshot(lastIncludedTerm int, lastIncludedIndex int, snapshot []byte) bool {
	// Your code here (2D).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// 如果快照过时，拒绝安装 / Reject if snapshot is outdated
	if lastIncludedIndex <= rf.commitIndex {
		return false
	}

	// 截断日志 / Truncate log
	if lastIncludedIndex < rf.getLastLogIndex() && rf.getLogTerm(lastIncludedIndex) == lastIncludedTerm {
		// 保留快照后的日志 / Keep log entries after snapshot
		rf.log = rf.log[lastIncludedIndex-rf.lastIncludedIndex:]
	} else {
		// 丢弃所有日志 / Discard all log entries
		rf.log = []LogEntry{}
	}

	rf.lastIncludedIndex = lastIncludedIndex
	rf.lastIncludedTerm = lastIncludedTerm
	rf.commitIndex = lastIncludedIndex
	rf.lastApplied = lastIncludedIndex

	rf.persister.SaveStateAndSnapshot(rf.encodeState(), snapshot)
	return true
}

// Snapshot - 服务调用Snapshot()创建快照
// Snapshot - The service says it has created a snapshot that has
// all info up to and including index. This means the
// service no longer needs the log through (and including)
// that index. Raft should now trim its log as much as possible.
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// Your code here (2D).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// 检查快照索引是否有效 / Check if snapshot index is valid
	if index <= rf.lastIncludedIndex || index > rf.commitIndex {
		return
	}

	// 截断日志并更新快照状态 / Truncate log and update snapshot state
	rf.log = rf.log[index-rf.lastIncludedIndex:]
	rf.lastIncludedTerm = rf.getLogTerm(index)
	rf.lastIncludedIndex = index

	rf.persister.SaveStateAndSnapshot(rf.encodeState(), snapshot)
}

// RequestVoteArgs - 请求投票RPC参数
// RequestVoteArgs - RequestVote RPC arguments structure
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	Term         int // 候选人的任期 / Candidate's term
	CandidateId  int // 请求投票的候选人ID / Candidate requesting vote
	LastLogIndex int // 候选人最后日志条目的索引 / Index of candidate's last log entry
	LastLogTerm  int // 候选人最后日志条目的任期 / Term of candidate's last log entry
}

// RequestVoteReply - 请求投票RPC回复
// RequestVoteReply - RequestVote RPC reply structure
type RequestVoteReply struct {
	// Your data here (2A).
	Term        int  // 当前任期，用于候选人更新自己 / Current term, for candidate to update itself
	VoteGranted bool // 如果候选人收到选票则为true / True means candidate received vote
}

// RequestVote - 请求投票RPC处理器
// RequestVote - RequestVote RPC handler
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	reply.Term = rf.currentTerm
	reply.VoteGranted = false

	// 如果请求的任期小于当前任期，拒绝投票 / Reject if term is outdated
	if args.Term < rf.currentTerm {
		return
	}

	// 如果请求的任期更大，更新当前任期并转为跟随者 / Update term and become follower if term is newer
	if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.votedFor = -1
		rf.state = Follower
		rf.persist()
	}

	// 检查是否可以投票给候选人 / Check if can vote for candidate
	if (rf.votedFor == -1 || rf.votedFor == args.CandidateId) &&
		rf.isLogUpToDate(args.LastLogIndex, args.LastLogTerm) {
		reply.VoteGranted = true
		rf.votedFor = args.CandidateId
		rf.resetElectionTimer()
		rf.persist()
	}
}

// AppendEntriesArgs - 追加条目RPC参数
// AppendEntriesArgs - AppendEntries RPC arguments structure
type AppendEntriesArgs struct {
	Term         int        // 领导者的任期 / Leader's term
	LeaderId     int        // 领导者ID，跟随者可以重定向客户端 / Leader ID, so follower can redirect clients
	PrevLogIndex int        // 新条目前一个日志条目的索引 / Index of log entry immediately preceding new ones
	PrevLogTerm  int        // 前一个日志条目的任期 / Term of prevLogIndex entry
	Entries      []LogEntry // 要存储的日志条目（心跳时为空） / Log entries to store (empty for heartbeat)
	LeaderCommit int        // 领导者的提交索引 / Leader's commitIndex
}

// AppendEntriesReply - 追加条目RPC回复
// AppendEntriesReply - AppendEntries RPC reply structure
type AppendEntriesReply struct {
	Term    int  // 当前任期，用于领导者更新自己 / Current term, for leader to update itself
	Success bool // 如果跟随者包含匹配prevLogIndex和prevLogTerm的条目则为true / True if follower contained entry matching prevLogIndex and prevLogTerm

	// 优化回退的冲突信息 / Conflict information for optimized backtracking
	ConflictIndex int // 冲突条目的索引 / Index of conflicting entry
	ConflictTerm  int // 冲突条目的任期 / Term of conflicting entry
}

// AppendEntries - 追加条目RPC处理器
// AppendEntries - AppendEntries RPC handler
func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	reply.Term = rf.currentTerm
	reply.Success = false

	// 如果任期小于当前任期，拒绝请求 / Reject if term is outdated
	if args.Term < rf.currentTerm {
		return
	}

	// 重置选举定时器 / Reset election timer
	rf.resetElectionTimer()

	// 如果任期更大，更新当前任期并转为跟随者 / Update term and become follower if term is newer
	if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.votedFor = -1
		rf.persist()
	}
	rf.state = Follower

	// 检查日志一致性 / Check log consistency
	if args.PrevLogIndex > rf.getLastLogIndex() {
		// 日志太短 / Log too short
		reply.ConflictIndex = rf.getLastLogIndex() + 1
		reply.ConflictTerm = -1
		return
	}

	if args.PrevLogIndex >= rf.lastIncludedIndex+1 && rf.getLogTerm(args.PrevLogIndex) != args.PrevLogTerm {
		// 日志不匹配 / Log doesn't match
		reply.ConflictTerm = rf.getLogTerm(args.PrevLogIndex)
		reply.ConflictIndex = rf.findFirstIndexOfTerm(reply.ConflictTerm)
		return
	}

	// 追加新条目 / Append new entries
	reply.Success = true
	if len(args.Entries) > 0 {
		// 删除冲突的条目并追加新条目 / Delete conflicting entries and append new ones
		insertIndex := args.PrevLogIndex + 1
		for i, entry := range args.Entries {
			index := insertIndex + i
			if index <= rf.getLastLogIndex() && rf.getLogTerm(index) != entry.Term {
				// 删除从这里开始的所有条目 / Delete all entries from here
				rf.log = rf.log[:index-rf.lastIncludedIndex-1]
				break
			}
		}
		// 追加新条目 / Append new entries
		for i, entry := range args.Entries {
			index := insertIndex + i
			if index > rf.getLastLogIndex() {
				rf.log = append(rf.log, entry)
			}
		}
		rf.persist()
	}

	// 更新提交索引 / Update commit index
	if args.LeaderCommit > rf.commitIndex {
		rf.commitIndex = min(args.LeaderCommit, rf.getLastLogIndex())
		rf.applyCond.Signal()
	}
}

// InstallSnapshotArgs - 安装快照RPC参数
// InstallSnapshotArgs - InstallSnapshot RPC arguments structure
type InstallSnapshotArgs struct {
	Term              int    // 领导者的任期 / Leader's term
	LeaderId          int    // 领导者ID / Leader ID
	LastIncludedIndex int    // 快照替换的最后一个条目的索引 / Index of last entry in snapshot
	LastIncludedTerm  int    // 快照替换的最后一个条目的任期 / Term of last entry in snapshot
	Data              []byte // 快照数据 / Snapshot data
}

// InstallSnapshotReply - 安装快照RPC回复
// InstallSnapshotReply - InstallSnapshot RPC reply structure
type InstallSnapshotReply struct {
	Term int // 当前任期，用于领导者更新自己 / Current term, for leader to update itself
}

// InstallSnapshot - 安装快照RPC处理器
// InstallSnapshot - InstallSnapshot RPC handler
func (rf *Raft) InstallSnapshot(args *InstallSnapshotArgs, reply *InstallSnapshotReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	reply.Term = rf.currentTerm

	// 如果任期小于当前任期，拒绝请求 / Reject if term is outdated
	if args.Term < rf.currentTerm {
		return
	}

	// 重置选举定时器 / Reset election timer
	rf.resetElectionTimer()

	// 如果任期更大，更新当前任期并转为跟随者 / Update term and become follower if term is newer
	if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.votedFor = -1
		rf.state = Follower
		rf.persist()
	}

	// 如果快照过时，忽略 / Ignore if snapshot is outdated
	if args.LastIncludedIndex <= rf.lastIncludedIndex {
		return
	}

	// 发送快照到应用层 / Send snapshot to application
	go func() {
		rf.applyCh <- ApplyMsg{
			SnapshotValid: true,
			Snapshot:      args.Data,
			SnapshotTerm:  args.LastIncludedTerm,
			SnapshotIndex: args.LastIncludedIndex,
		}
	}()
}

// sendRequestVote - 发送请求投票RPC
// sendRequestVote - Send RequestVote RPC
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

// sendAppendEntries - 发送追加条目RPC
// sendAppendEntries - Send AppendEntries RPC
func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
}

// sendInstallSnapshot - 发送安装快照RPC
// sendInstallSnapshot - Send InstallSnapshot RPC
func (rf *Raft) sendInstallSnapshot(server int, args *InstallSnapshotArgs, reply *InstallSnapshotReply) bool {
	ok := rf.peers[server].Call("Raft.InstallSnapshot", args, reply)
	return ok
}

// Start - 开始对新日志条目达成一致
// Start - The service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. If this
// server isn't the leader, returns false. Otherwise start the
// agreement and return immediately. There is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election. Even if the Raft instance has been killed,
// this function should return gracefully.
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// 检查是否为领导者 / Check if leader
	if rf.state != Leader {
		return -1, -1, false
	}

	// 追加新条目到日志 / Append new entry to log
	index := rf.getLastLogIndex() + 1
	term := rf.currentTerm
	rf.log = append(rf.log, LogEntry{Term: term, Command: command})
	rf.persist()

	// 立即发送心跳以快速复制 / Send heartbeats immediately for fast replication
	rf.sendHeartbeats(false)

	return index, term, true
}

// Kill - 测试器不会停止由Make()创建的Raft实例，但会调用Kill()方法
// Kill - The tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. Your code can use killed() to
// check whether Kill() has been called. The use of atomic avoids the
// need for a lock.
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
}

// killed - 检查节点是否已被终止
// killed - Check if the peer has been killed
func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

// 辅助函数 / Utility functions

// getLastLogIndex - 获取最后一个日志条目的索引
// getLastLogIndex - Get index of last log entry
func (rf *Raft) getLastLogIndex() int {
	return rf.lastIncludedIndex + len(rf.log)
}

// getLastLogTerm - 获取最后一个日志条目的任期
// getLastLogTerm - Get term of last log entry
func (rf *Raft) getLastLogTerm() int {
	if len(rf.log) == 0 {
		return rf.lastIncludedTerm
	}
	return rf.log[len(rf.log)-1].Term
}

// getLogTerm - 获取指定索引处日志条目的任期
// getLogTerm - Get term of log entry at given index
func (rf *Raft) getLogTerm(index int) int {
	if index == rf.lastIncludedIndex {
		return rf.lastIncludedTerm
	}
	return rf.log[index-rf.lastIncludedIndex-1].Term
}

// isLogUpToDate - 检查候选人的日志是否至少和当前节点一样新
// isLogUpToDate - Check if candidate's log is at least as up-to-date as current node
func (rf *Raft) isLogUpToDate(lastLogIndex, lastLogTerm int) bool {
	myLastLogTerm := rf.getLastLogTerm()
	myLastLogIndex := rf.getLastLogIndex()

	if lastLogTerm != myLastLogTerm {
		return lastLogTerm > myLastLogTerm
	}
	return lastLogIndex >= myLastLogIndex
}

// findFirstIndexOfTerm - 查找指定任期的第一个日志条目索引
// findFirstIndexOfTerm - Find first index of given term
func (rf *Raft) findFirstIndexOfTerm(term int) int {
	for i := rf.lastIncludedIndex + 1; i <= rf.getLastLogIndex(); i++ {
		if rf.getLogTerm(i) == term {
			return i
		}
	}
	return -1
}

// encodeState - 编码状态用于持久化
// encodeState - Encode state for persistence
func (rf *Raft) encodeState() []byte {
	w := new(bytes.Buffer)
	e := gob.NewEncoder(w)
	e.Encode(rf.currentTerm)
	e.Encode(rf.votedFor)
	e.Encode(rf.log)
	e.Encode(rf.lastIncludedIndex)
	e.Encode(rf.lastIncludedTerm)
	return w.Bytes()
}

// 选举和心跳相关函数 / Election and heartbeat related functions

// resetElectionTimer - 重置选举超时定时器
// resetElectionTimer - Reset election timeout timer
func (rf *Raft) resetElectionTimer() {
	timeout := time.Duration(300+rand.Intn(200)) * time.Millisecond
	if rf.electionTimer != nil {
		rf.electionTimer.Stop()
	}
	rf.electionTimer = time.AfterFunc(timeout, rf.startElection)
}

// startElection - 开始选举过程
// startElection - Start election process
func (rf *Raft) startElection() {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// 检查是否仍需要选举 / Check if still need election
	if rf.state == Leader || rf.killed() {
		return
	}

	// 转为候选人状态 / Become candidate
	rf.state = Candidate
	rf.currentTerm++
	rf.votedFor = rf.me
	rf.persist()
	rf.resetElectionTimer()

	// 发送请求投票RPC / Send RequestVote RPCs
	votes := 1
	args := &RequestVoteArgs{
		Term:         rf.currentTerm,
		CandidateId:  rf.me,
		LastLogIndex: rf.getLastLogIndex(),
		LastLogTerm:  rf.getLastLogTerm(),
	}

	for i := range rf.peers {
		if i != rf.me {
			go func(server int) {
				reply := &RequestVoteReply{}
				if rf.sendRequestVote(server, args, reply) {
					rf.mu.Lock()
					defer rf.mu.Unlock()

					// 检查状态是否仍然有效 / Check if state is still valid
					if rf.state != Candidate || rf.currentTerm != args.Term {
						return
					}

					// 处理回复 / Handle reply
					if reply.Term > rf.currentTerm {
						rf.currentTerm = reply.Term
						rf.votedFor = -1
						rf.state = Follower
						rf.persist()
						return
					}

					if reply.VoteGranted {
						votes++
						if votes > len(rf.peers)/2 {
							rf.becomeLeader()
						}
					}
				}
			}(i)
		}
	}
}

// becomeLeader - 成为领导者
// becomeLeader - Become leader
func (rf *Raft) becomeLeader() {
	if rf.state != Candidate {
		return
	}

	rf.state = Leader

	// 初始化领导者状态 / Initialize leader state
	rf.nextIndex = make([]int, len(rf.peers))
	rf.matchIndex = make([]int, len(rf.peers))
	for i := range rf.peers {
		rf.nextIndex[i] = rf.getLastLogIndex() + 1
		rf.matchIndex[i] = 0
	}

	// 立即发送心跳 / Send heartbeats immediately
	rf.sendHeartbeats(true)

	// 启动心跳定时器 / Start heartbeat timer
	if rf.heartbeatTimer != nil {
		rf.heartbeatTimer.Stop()
	}
	rf.heartbeatTimer = time.AfterFunc(100*time.Millisecond, func() { rf.sendHeartbeats(true) })
}

// sendHeartbeats - 发送心跳给所有跟随者
// sendHeartbeats - Send heartbeats to all followers
func (rf *Raft) sendHeartbeats(isHeartbeat bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.state != Leader || rf.killed() {
		return
	}

	// 发送给所有其他节点 / Send to all other peers
	for i := range rf.peers {
		if i != rf.me {
			go rf.sendAppendEntriesToPeer(i, isHeartbeat)
		}
	}

	// 重置心跳定时器 / Reset heartbeat timer
	if isHeartbeat && rf.heartbeatTimer != nil {
		rf.heartbeatTimer.Reset(100 * time.Millisecond)
	}
}

// sendAppendEntriesToPeer - 向指定节点发送追加条目RPC
// sendAppendEntriesToPeer - Send AppendEntries RPC to specific peer
func (rf *Raft) sendAppendEntriesToPeer(server int, isHeartbeat bool) {
	rf.mu.Lock()

	if rf.state != Leader || rf.killed() {
		rf.mu.Unlock()
		return
	}

	nextIndex := rf.nextIndex[server]

	// 检查是否需要发送快照 / Check if need to send snapshot
	if nextIndex <= rf.lastIncludedIndex {
		// 发送快照 / Send snapshot
		args := &InstallSnapshotArgs{
			Term:              rf.currentTerm,
			LeaderId:          rf.me,
			LastIncludedIndex: rf.lastIncludedIndex,
			LastIncludedTerm:  rf.lastIncludedTerm,
			Data:              rf.persister.ReadSnapshot(),
		}
		rf.mu.Unlock()

		go func() {
			reply := &InstallSnapshotReply{}
			if rf.sendInstallSnapshot(server, args, reply) {
				rf.mu.Lock()
				defer rf.mu.Unlock()

				if reply.Term > rf.currentTerm {
					rf.currentTerm = reply.Term
					rf.votedFor = -1
					rf.state = Follower
					rf.persist()
					return
				}

				rf.nextIndex[server] = rf.lastIncludedIndex + 1
				rf.matchIndex[server] = rf.lastIncludedIndex
			}
		}()
		return
	}

	// 准备追加条目参数 / Prepare AppendEntries arguments
	prevLogIndex := nextIndex - 1
	prevLogTerm := rf.lastIncludedTerm
	if prevLogIndex > rf.lastIncludedIndex {
		prevLogTerm = rf.getLogTerm(prevLogIndex)
	}

	entries := []LogEntry{}
	if nextIndex <= rf.getLastLogIndex() {
		entries = rf.log[nextIndex-rf.lastIncludedIndex-1:]
	}

	args := &AppendEntriesArgs{
		Term:         rf.currentTerm,
		LeaderId:     rf.me,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  prevLogTerm,
		Entries:      entries,
		LeaderCommit: rf.commitIndex,
	}
	rf.mu.Unlock()

	// 发送RPC / Send RPC
	go func() {
		reply := &AppendEntriesReply{}
		if rf.sendAppendEntries(server, args, reply) {
			rf.mu.Lock()
			defer rf.mu.Unlock()

			// 检查状态是否仍然有效 / Check if state is still valid
			if rf.state != Leader || rf.currentTerm != args.Term {
				return
			}

			// 处理任期更新 / Handle term update
			if reply.Term > rf.currentTerm {
				rf.currentTerm = reply.Term
				rf.votedFor = -1
				rf.state = Follower
				rf.persist()
				return
			}

			if reply.Success {
				// 更新nextIndex和matchIndex / Update nextIndex and matchIndex
				rf.nextIndex[server] = args.PrevLogIndex + len(args.Entries) + 1
				rf.matchIndex[server] = args.PrevLogIndex + len(args.Entries)
				rf.updateCommitIndex()
			} else {
				// 优化nextIndex回退 / Optimize nextIndex decrement
				if reply.ConflictTerm == -1 {
					// 跟随者日志太短 / Follower's log is too short
					rf.nextIndex[server] = reply.ConflictIndex
				} else {
					// 查找冲突任期的最后一个条目 / Find last entry with ConflictTerm
					lastIndex := -1
					for i := args.PrevLogIndex; i >= rf.lastIncludedIndex+1; i-- {
						if rf.getLogTerm(i) == reply.ConflictTerm {
							lastIndex = i
							break
						}
					}
					if lastIndex != -1 {
						rf.nextIndex[server] = lastIndex + 1
					} else {
						rf.nextIndex[server] = reply.ConflictIndex
					}
				}
				rf.nextIndex[server] = max(1, rf.nextIndex[server])
			}
		}
	}()
}

// updateCommitIndex - 更新提交索引
// updateCommitIndex - Update commit index
func (rf *Raft) updateCommitIndex() {
	// 从最后一个日志条目开始向前查找可以提交的条目 / Search backwards from last log entry for committable entries
	for n := rf.getLastLogIndex(); n > rf.commitIndex; n-- {
		count := 1
		for i := range rf.peers {
			if i != rf.me && rf.matchIndex[i] >= n {
				count++
			}
		}
		// 如果大多数节点都有该条目且是当前任期的条目，则可以提交 / Commit if majority has entry and it's from current term
		if count > len(rf.peers)/2 && rf.getLogTerm(n) == rf.currentTerm {
			rf.commitIndex = n
			rf.applyCond.Signal() // 通知应用协程 / Notify applier goroutine
			break
		}
	}
}

// applier - 应用已提交条目的协程
// applier - Goroutine to apply committed entries
func (rf *Raft) applier() {
	for !rf.killed() {
		rf.mu.Lock()
		// 等待有新的条目需要应用 / Wait for new entries to apply
		for rf.lastApplied >= rf.commitIndex {
			rf.applyCond.Wait()
			if rf.killed() {
				rf.mu.Unlock()
				return
			}
		}

		// 批量应用条目以提高性能 / Batch apply entries for better performance
		commitIndex := rf.commitIndex
		lastApplied := rf.lastApplied
		entries := make([]LogEntry, commitIndex-lastApplied)
		copy(entries, rf.log[lastApplied+1-rf.lastIncludedIndex-1:commitIndex-rf.lastIncludedIndex])
		rf.mu.Unlock()

		// 发送应用消息 / Send apply messages
		for i, entry := range entries {
			rf.applyCh <- ApplyMsg{
				CommandValid: true,
				Command:      entry.Command,
				CommandIndex: lastApplied + 1 + i,
			}
		}

		// 更新lastApplied / Update lastApplied
		rf.mu.Lock()
		rf.lastApplied = commitIndex
		rf.mu.Unlock()
	}
}

// 工具函数 / Utility functions
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Make - 创建Raft服务器实例
// Make - The service or tester wants to create a Raft server. The ports
// of all the Raft servers (including this one) are in peers[]. This
// server's port is peers[me]. All the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me
	rf.applyCh = applyCh

	// Your initialization code here (2A, 2B, 2C).
	rf.currentTerm = 0
	rf.votedFor = -1
	rf.log = []LogEntry{}
	rf.commitIndex = 0
	rf.lastApplied = 0
	rf.state = Follower
	rf.lastIncludedIndex = 0
	rf.lastIncludedTerm = 0

	rf.applyCond = sync.NewCond(&rf.mu)

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	// start ticker goroutine to start elections
	rf.resetElectionTimer()

	// start applier goroutine
	go rf.applier()

	return rf
}