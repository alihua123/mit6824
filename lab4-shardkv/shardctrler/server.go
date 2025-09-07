package shardctrler

import (
	"mit6824/lab2-raft/raft"
	"sort"
	"sync"
	"time"
)
import "mit6824/lab2-raft/labrpc"
import "sync/atomic"

type ShardCtrler struct {
	mu      sync.Mutex
	me      int
	rf      *raft.Raft
	applyCh chan raft.ApplyMsg
	dead    int32 // set by Kill()

	// Your data here.
	configs   []Config                 // indexed by config num
	notifyCh  map[int]chan OpResult    // index -> notification channel
	clientSeq map[int64]int64          // client -> last sequence number
	lastApplied int
}

type Op struct {
	// Your data here.
	Type     string // "Join", "Leave", "Move", "Query"
	Servers  map[int][]string // for Join
	GIDs     []int            // for Leave
	Shard    int              // for Move
	GID      int              // for Move
	Num      int              // for Query
	ClientId int64
	SeqNum   int64
}

type OpResult struct {
	Err    Err
	Config Config
}

func (sc *ShardCtrler) Join(args *JoinArgs, reply *JoinReply) {
	// Your code here.
	op := Op{
		Type:     "Join",
		Servers:  args.Servers,
		ClientId: args.ClientId,
		SeqNum:   args.SeqNum,
	}

	result := sc.executeOp(op)
	reply.Err = result.Err
}

func (sc *ShardCtrler) Leave(args *LeaveArgs, reply *LeaveReply) {
	// Your code here.
	op := Op{
		Type:     "Leave",
		GIDs:     args.GIDs,
		ClientId: args.ClientId,
		SeqNum:   args.SeqNum,
	}

	result := sc.executeOp(op)
	reply.Err = result.Err
}

func (sc *ShardCtrler) Move(args *MoveArgs, reply *MoveReply) {
	// Your code here.
	op := Op{
		Type:     "Move",
		Shard:    args.Shard,
		GID:      args.GID,
		ClientId: args.ClientId,
		SeqNum:   args.SeqNum,
	}

	result := sc.executeOp(op)
	reply.Err = result.Err
}

func (sc *ShardCtrler) Query(args *QueryArgs, reply *QueryReply) {
	// Your code here.
	op := Op{
		Type:     "Query",
		Num:      args.Num,
		ClientId: args.ClientId,
		SeqNum:   args.SeqNum,
	}

	result := sc.executeOp(op)
	reply.Err = result.Err
	reply.Config = result.Config
}

func (sc *ShardCtrler) executeOp(op Op) OpResult {
	index, _, isLeader := sc.rf.Start(op)
	if !isLeader {
		return OpResult{Err: ErrWrongLeader}
	}

	sc.mu.Lock()
	notifyCh := make(chan OpResult, 1)
	sc.notifyCh[index] = notifyCh
	sc.mu.Unlock()

	select {
	case result := <-notifyCh:
		return result
	case <-time.After(500 * time.Millisecond):
		return OpResult{Err: ErrTimeout}
	}
}

func (sc *ShardCtrler) applier() {
	for !sc.killed() {
		msg := <-sc.applyCh
		if msg.CommandValid {
			sc.applyCommand(msg)
		}
	}
}

func (sc *ShardCtrler) applyCommand(msg raft.ApplyMsg) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if msg.CommandIndex <= sc.lastApplied {
		return
	}

	op := msg.Command.(Op)
	result := OpResult{}

	// Check if this operation has already been applied
	if lastSeq, exists := sc.clientSeq[op.ClientId]; exists && op.SeqNum <= lastSeq {
		// Duplicate operation
		if op.Type == "Query" {
			result.Config = sc.queryConfig(op.Num)
		}
		result.Err = OK
	} else {
		// Apply the operation
		switch op.Type {
		case "Join":
			sc.joinGroups(op.Servers)
			result.Err = OK
		case "Leave":
			sc.leaveGroups(op.GIDs)
			result.Err = OK
		case "Move":
			sc.moveShard(op.Shard, op.GID)
			result.Err = OK
		case "Query":
			result.Config = sc.queryConfig(op.Num)
			result.Err = OK
		}
		sc.clientSeq[op.ClientId] = op.SeqNum
	}

	sc.lastApplied = msg.CommandIndex

	// Notify waiting RPC handler
	if notifyCh, exists := sc.notifyCh[msg.CommandIndex]; exists {
		select {
		case notifyCh <- result:
		default:
		}
		delete(sc.notifyCh, msg.CommandIndex)
	}
}

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

func (sc *ShardCtrler) leaveGroups(gids []int) {
	lastConfig := sc.configs[len(sc.configs)-1]
	newConfig := Config{
		Num:    lastConfig.Num + 1,
		Shards: lastConfig.Shards,
		Groups: make(map[int][]string),
	}

	// Copy existing groups except those leaving
	leaveSet := make(map[int]bool)
	for _, gid := range gids {
		leaveSet[gid] = true
	}

	for gid, servers := range lastConfig.Groups {
		if !leaveSet[gid] {
			newConfig.Groups[gid] = servers
		}
	}

	// Reassign shards from leaving groups
	for i, gid := range newConfig.Shards {
		if leaveSet[gid] {
			newConfig.Shards[i] = 0
		}
	}

	// Rebalance shards
	sc.rebalance(&newConfig)
	sc.configs = append(sc.configs, newConfig)
}

func (sc *ShardCtrler) moveShard(shard int, gid int) {
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

	// Move the shard
	newConfig.Shards[shard] = gid
	sc.configs = append(sc.configs, newConfig)
}

func (sc *ShardCtrler) queryConfig(num int) Config {
	if num < 0 || num >= len(sc.configs) {
		return sc.configs[len(sc.configs)-1]
	}
	return sc.configs[num]
}

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

	// Collect shards that need to be reassigned
	unassigned := make([]int, 0)
	for i, gid := range config.Shards {
		if gid == 0 || config.Groups[gid] == nil {
			unassigned = append(unassigned, i)
			config.Shards[i] = 0
		}
	}

	// Move excess shards to unassigned
	for _, gid := range gids {
		maxShards := target
		if extra > 0 {
			maxShards++
			extra--
		}

		for shardCount[gid] > maxShards {
			// Find a shard assigned to this group and move it to unassigned
			for i, assignedGid := range config.Shards {
				if assignedGid == gid {
					config.Shards[i] = 0
					unassigned = append(unassigned, i)
					shardCount[gid]--
					break
				}
			}
		}
	}

	// Assign unassigned shards
	extra = NShards % len(gids)
	for _, shard := range unassigned {
		// Find group with minimum shards
		minGid := gids[0]
		minCount := shardCount[minGid]
		for _, gid := range gids {
			if shardCount[gid] < minCount {
				minGid = gid
				minCount = shardCount[gid]
			}
		}

		config.Shards[shard] = minGid
		shardCount[minGid]++
	}
}

// the tester calls Kill() when a ShardCtrler instance won't
// be needed again. you are not required to do anything
// in Kill(), but it might be convenient to (for example)
// turn off debug output from this instance.
func (sc *ShardCtrler) Kill() {
	atomic.StoreInt32(&sc.dead, 1)
	sc.rf.Kill()
	// Your code here, if desired.
}

func (sc *ShardCtrler) killed() bool {
	z := atomic.LoadInt32(&sc.dead)
	return z == 1
}

// needed by shardkv tester
func (sc *ShardCtrler) Raft() *raft.Raft {
	return sc.rf
}

// servers[] contains the ports of the set of
// servers that will cooperate via Raft to
// form the fault-tolerant shardctrler service.
// me is the index of the current server in servers[].
func StartServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister) *ShardCtrler {
	sc := new(ShardCtrler)
	sc.me = me

	sc.configs = make([]Config, 1)
	sc.configs[0].Groups = map[int][]string{}

	sc.applyCh = make(chan raft.ApplyMsg)
	sc.rf = raft.Make(servers, me, persister, sc.applyCh)

	// Your code here.
	sc.notifyCh = make(map[int]chan OpResult)
	sc.clientSeq = make(map[int64]int64)
	sc.lastApplied = 0

	go sc.applier()

	return sc
}