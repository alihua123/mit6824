package shardkv

import (
	"bytes"
	"encoding/gob"
	"log"
	"mit6824/lab2-raft/labrpc"
	"mit6824/lab2-raft/raft"
	"mit6824/lab4-shardkv/shardctrler"
	"sync"
	"sync/atomic"
	"time"
)

const Debug = false

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug {
		log.Printf(format, a...)
	}
	return
}

type Op struct {
	// Your definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
	Type     string // "Get", "Put", "Append", "Reconfigure", "InstallShard", "DeleteShard"
	Key      string
	Value    string
	ClientId int64
	SeqNum   int64

	// For reconfiguration
	Config shardctrler.Config

	// For shard migration
	Shard     int
	ShardData map[string]string
	ClientSeq map[int64]int64
	ConfigNum int
}

type ShardKV struct {
	mu           sync.Mutex
	me           int
	rf           *raft.Raft
	applyCh      chan raft.ApplyMsg
	make_end     func(string) *labrpc.ClientEnd
	gid          int
	ctrlers      []*labrpc.ClientEnd
	maxraftstate int // snapshot if log grows this big
	dead         int32

	// Your definitions here.
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
	pendingShards map[int]map[string]string // shards waiting to be installed
	pendingClientSeq map[int]map[int64]int64 // pending client sequences
}

type ShardStatus int

const (
	Serving ShardStatus = iota
	Pulling
	BePulling
	GCing
)

type OpResult struct {
	Err   Err
	Value string
}

func (kv *ShardKV) Get(args *GetArgs, reply *GetReply) {
	// Your code here.
	if !kv.canServe(args.Key) {
		reply.Err = ErrWrongGroup
		return
	}

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

func (kv *ShardKV) PutAppend(args *PutAppendArgs, reply *PutAppendReply) {
	// Your code here.
	if !kv.canServe(args.Key) {
		reply.Err = ErrWrongGroup
		return
	}

	op := Op{
		Type:     args.Op,
		Key:      args.Key,
		Value:    args.Value,
		ClientId: args.ClientId,
		SeqNum:   args.SeqNum,
	}

	result := kv.executeOp(op)
	reply.Err = result.Err
}

func (kv *ShardKV) canServe(key string) bool {
	kv.mu.Lock()
	defer kv.mu.Unlock()

	shard := key2shard(key)
	return kv.config.Shards[shard] == kv.gid && kv.shardStatus[shard] == Serving
}

func (kv *ShardKV) executeOp(op Op) OpResult {
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

func (kv *ShardKV) applier() {
	for !kv.killed() {
		msg := <-kv.applyCh
		if msg.CommandValid {
			kv.applyCommand(msg)
		} else if msg.SnapshotValid {
			kv.applySnapshot(msg)
		}
	}
}

func (kv *ShardKV) applyCommand(msg raft.ApplyMsg) {
	kv.mu.Lock()
	defer kv.mu.Unlock()

	if msg.CommandIndex <= kv.lastApplied {
		return
	}

	op := msg.Command.(Op)
	result := OpResult{}

	switch op.Type {
	case "Get", "Put", "Append":
		result = kv.applyClientOp(op)
	case "Reconfigure":
		kv.applyReconfigure(op)
		result.Err = OK
	case "InstallShard":
		kv.applyInstallShard(op)
		result.Err = OK
	case "DeleteShard":
		kv.applyDeleteShard(op)
		result.Err = OK
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

func (kv *ShardKV) applyClientOp(op Op) OpResult {
	shard := key2shard(op.Key)
	result := OpResult{}

	// Check if we can serve this shard
	if kv.config.Shards[shard] != kv.gid || kv.shardStatus[shard] != Serving {
		result.Err = ErrWrongGroup
		return result
	}

	// Check if this operation has already been applied
	if lastSeq, exists := kv.clientSeq[op.ClientId]; exists && op.SeqNum <= lastSeq {
		// Duplicate operation
		if op.Type == "Get" {
			if kv.shards[shard] != nil {
				result.Value = kv.shards[shard][op.Key]
			}
			result.Err = OK
		} else {
			result.Err = OK
		}
	} else {
		// Apply the operation
		if kv.shards[shard] == nil {
			kv.shards[shard] = make(map[string]string)
		}

		switch op.Type {
		case "Get":
			value, exists := kv.shards[shard][op.Key]
			if exists {
				result.Value = value
				result.Err = OK
			} else {
				result.Err = ErrNoKey
			}
		case "Put":
			kv.shards[shard][op.Key] = op.Value
			result.Err = OK
		case "Append":
			kv.shards[shard][op.Key] += op.Value
			result.Err = OK
		}
		kv.clientSeq[op.ClientId] = op.SeqNum
	}

	return result
}

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

func (kv *ShardKV) applyDeleteShard(op Op) {
	if op.ConfigNum != kv.lastConfig.Num {
		return
	}

	if kv.shardStatus[op.Shard] == BePulling {
		delete(kv.shards, op.Shard)
		kv.shardStatus[op.Shard] = GCing
	}
}

func (kv *ShardKV) applySnapshot(msg raft.ApplyMsg) {
	kv.mu.Lock()
	defer kv.mu.Unlock()

	if msg.SnapshotIndex <= kv.lastApplied {
		return
	}

	r := bytes.NewBuffer(msg.Snapshot)
	d := gob.NewDecoder(r)

	var config shardctrler.Config
	var lastConfig shardctrler.Config
	var shards map[int]map[string]string
	var clientSeq map[int64]int64
	var shardStatus map[int]ShardStatus
	var lastApplied int

	if d.Decode(&config) != nil ||
		d.Decode(&lastConfig) != nil ||
		d.Decode(&shards) != nil ||
		d.Decode(&clientSeq) != nil ||
		d.Decode(&shardStatus) != nil ||
		d.Decode(&lastApplied) != nil {
		log.Fatal("Failed to decode snapshot")
	} else {
		kv.config = config
		kv.lastConfig = lastConfig
		kv.shards = shards
		kv.clientSeq = clientSeq
		kv.shardStatus = shardStatus
		kv.lastApplied = lastApplied
	}
}

func (kv *ShardKV) takeSnapshot(index int) {
	w := new(bytes.Buffer)
	e := gob.NewEncoder(w)
	e.Encode(kv.config)
	e.Encode(kv.lastConfig)
	e.Encode(kv.shards)
	e.Encode(kv.clientSeq)
	e.Encode(kv.shardStatus)
	e.Encode(kv.lastApplied)
	snapshot := w.Bytes()

	kv.rf.Snapshot(index, snapshot)
}

// Configuration monitor
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

func (kv *ShardKV) canReconfigure() bool {
	kv.mu.Lock()
	defer kv.mu.Unlock()

	for _, status := range kv.shardStatus {
		if status != Serving && status != GCing {
			return false
		}
	}
	return true
}

// Shard migration
func (kv *ShardKV) migrationMonitor() {
	for !kv.killed() {
		if _, isLeader := kv.rf.GetState(); isLeader {
			kv.pullShards()
			kv.deleteShards()
		}
		time.Sleep(50 * time.Millisecond)
	}
}

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

func (kv *ShardKV) deleteShards() {
	kv.mu.Lock()
	deleteTasks := make(map[int]int) // shard -> gid
	for shard, status := range kv.shardStatus {
		if status == GCing {
			deleteTasks[shard] = kv.lastConfig.Shards[shard]
		}
	}
	configNum := kv.lastConfig.Num
	kv.mu.Unlock()

	for shard, gid := range deleteTasks {
		go kv.deleteShard(shard, gid, configNum)
	}
}

func (kv *ShardKV) deleteShard(shard, gid, configNum int) {
	kv.mu.Lock()
	servers := kv.lastConfig.Groups[gid]
	kv.mu.Unlock()

	args := &DeleteShardArgs{
		Shard:     shard,
		ConfigNum: configNum,
	}

	for _, server := range servers {
		srv := kv.make_end(server)
		reply := &DeleteShardReply{}
		ok := srv.Call("ShardKV.DeleteShard", args, reply)
		if ok && reply.Err == OK {
			op := Op{
				Type:      "DeleteShard",
				Shard:     shard,
				ConfigNum: configNum,
			}
			kv.rf.Start(op)
			return
		}
	}
}

// RPC handlers for shard migration
func (kv *ShardKV) PullShard(args *PullShardArgs, reply *PullShardReply) {
	if _, isLeader := kv.rf.GetState(); !isLeader {
		reply.Err = ErrWrongLeader
		return
	}

	kv.mu.Lock()
	defer kv.mu.Unlock()

	if args.ConfigNum != kv.config.Num {
		reply.Err = ErrWrongGroup
		return
	}

	if kv.shardStatus[args.Shard] != BePulling {
		reply.Err = ErrWrongGroup
		return
	}

	reply.Err = OK
	reply.ShardData = make(map[string]string)
	if kv.shards[args.Shard] != nil {
		for k, v := range kv.shards[args.Shard] {
			reply.ShardData[k] = v
		}
	}
	reply.ClientSeq = make(map[int64]int64)
	for clientId, seq := range kv.clientSeq {
		reply.ClientSeq[clientId] = seq
	}
}

func (kv *ShardKV) DeleteShard(args *DeleteShardArgs, reply *DeleteShardReply) {
	if _, isLeader := kv.rf.GetState(); !isLeader {
		reply.Err = ErrWrongLeader
		return
	}

	kv.mu.Lock()
	defer kv.mu.Unlock()

	if args.ConfigNum != kv.lastConfig.Num {
		reply.Err = ErrWrongGroup
		return
	}

	reply.Err = OK
}

// the tester calls Kill() when a ShardKV instance won't
// be needed again. you are not required to do anything
// in Kill(), but it might be convenient to (for example)
// turn off debug output from this instance.
func (kv *ShardKV) Kill() {
	atomic.StoreInt32(&kv.dead, 1)
	kv.rf.Kill()
	// Your code here, if desired.
}

func (kv *ShardKV) killed() bool {
	z := atomic.LoadInt32(&kv.dead)
	return z == 1
}

// servers[] contains the ports of the set of
// servers that will cooperate via Raft to
// form the fault-tolerant key/value service.
// me is the index of the current server in servers[].
// the k/v server should store snapshots through the underlying Raft
// implementation, which should call persister.SaveStateAndSnapshot() to
// atomically save the Raft state along with the snapshot.
// the k/v server should snapshot when Raft's saved state exceeds maxraftstate bytes,
// in order to allow Raft to garbage-collect its log. if maxraftstate is -1,
// you don't need to snapshot.
// StartServer() must return quickly, so it should start goroutines
// for any long-running work.
func StartServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister, maxraftstate int, gid int, ctrlers []*labrpc.ClientEnd, make_end func(string) *labrpc.ClientEnd) *ShardKV {
	// call labgob.Register on structures you want
	// Go's RPC library to marshall/unmarshall.
	gob.Register(Op{})

	kv := new(ShardKV)
	kv.me = me
	kv.maxraftstate = maxraftstate
	kv.make_end = make_end
	kv.gid = gid
	kv.ctrlers = ctrlers
	kv.persister = persister

	// Your initialization code here.
	kv.shards = make(map[int]map[string]string)
	kv.clientSeq = make(map[int64]int64)
	kv.notifyCh = make(map[int]chan OpResult)
	kv.shardStatus = make(map[int]ShardStatus)
	kv.pendingShards = make(map[int]map[string]string)
	kv.pendingClientSeq = make(map[int]map[int64]int64)
	kv.lastApplied = 0

	// Initialize shard status
	for i := 0; i < shardctrler.NShards; i++ {
		kv.shardStatus[i] = Serving
	}

	kv.applyCh = make(chan raft.ApplyMsg)
	kv.rf = raft.Make(servers, me, persister, kv.applyCh)

	kv.mck = shardctrler.MakeClerk(kv.ctrlers)

	// Start background goroutines
	go kv.applier()
	go kv.configMonitor()
	go kv.migrationMonitor()

	return kv
}