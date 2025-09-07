package kvraft

import (
	"bytes"
	"encoding/gob"
	"log"
	"mit6824/lab2-raft/labrpc"
	"mit6824/lab2-raft/raft"
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
	Type     string // "Get", "Put", "Append"
	Key      string
	Value    string
	ClientId int64
	SeqNum   int64
}

type KVServer struct {
	mu      sync.Mutex
	me      int
	rf      *raft.Raft
	applyCh chan raft.ApplyMsg
	dead    int32 // set by Kill()

	maxraftstate int // snapshot if log grows this big

	// Your definitions here.
	data         map[string]string    // key-value store
	clientSeq    map[int64]int64      // client -> last sequence number
	notifyCh     map[int]chan OpResult // index -> notification channel
	lastApplied  int                  // last applied log index
	persister    *raft.Persister
}

type OpResult struct {
	Err   Err
	Value string
}

func (kv *KVServer) Get(args *GetArgs, reply *GetReply) {
	// Your code here.
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

func (kv *KVServer) PutAppend(args *PutAppendArgs, reply *PutAppendReply) {
	// Your code here.
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

func (kv *KVServer) applier() {
	for !kv.killed() {
		msg := <-kv.applyCh
		if msg.CommandValid {
			kv.applyCommand(msg)
		} else if msg.SnapshotValid {
			kv.applySnapshot(msg)
		}
	}
}

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

func (kv *KVServer) takeSnapshot(index int) {
	w := new(bytes.Buffer)
	e := gob.NewEncoder(w)
	e.Encode(kv.data)
	e.Encode(kv.clientSeq)
	e.Encode(kv.lastApplied)
	snapshot := w.Bytes()

	kv.rf.Snapshot(index, snapshot)
}

// the tester calls Kill() when a KVServer instance won't
// be needed again. for your convenience, we supply
// code to set rf.dead (without needing a lock),
// and a killed() method to test rf.dead in
// long-running loops. you can also add your own
// code to Kill(). you're not required to do anything
// about this, but it may be convenient (for example
// to turn off debug output from a Kill()ed instance).
func (kv *KVServer) Kill() {
	atomic.StoreInt32(&kv.dead, 1)
	kv.rf.Kill()
	// Your code here, if desired.
}

func (kv *KVServer) killed() bool {
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
// StartKVServer() must return quickly, so it should start goroutines
// for any long-running work.
func StartKVServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister, maxraftstate int) *KVServer {
	// call labgob.Register on structures you want
	// Go's RPC library to marshall/unmarshall.
	gob.Register(Op{})

	kv := new(KVServer)
	kv.me = me
	kv.maxraftstate = maxraftstate
	kv.persister = persister

	// You may need initialization code here.
	kv.data = make(map[string]string)
	kv.clientSeq = make(map[int64]int64)
	kv.notifyCh = make(map[int]chan OpResult)
	kv.lastApplied = 0

	kv.applyCh = make(chan raft.ApplyMsg)
	kv.rf = raft.Make(servers, me, persister, kv.applyCh)

	// You may need initialization code here.
	go kv.applier()

	return kv
}