package shardctrler

//
// Shardctrler clerk.
//

import (
	"crypto/rand"
	"math/big"
	"mit6824/lab2-raft/labrpc"
	"time"
)

type Clerk struct {
	servers []*labrpc.ClientEnd
	// Your data here.
	clientId int64
	seqNum   int64
	leaderId int
}

func nrand() int64 {
	max := big.NewInt(int64(1) << 62)
	bigx, _ := rand.Int(rand.Reader, max)
	x := bigx.Int64()
	return x
}

func MakeClerk(servers []*labrpc.ClientEnd) *Clerk {
	ck := new(Clerk)
	ck.servers = servers
	// Your code here.
	ck.clientId = nrand()
	ck.seqNum = 0
	ck.leaderId = 0
	return ck
}

func (ck *Clerk) Query(num int) Config {
	ck.seqNum++
	args := &QueryArgs{
		Num:      num,
		ClientId: ck.clientId,
		SeqNum:   ck.seqNum,
	}

	for {
		// try each known server.
		reply := &QueryReply{}
		ok := ck.servers[ck.leaderId].Call("ShardCtrler.Query", args, reply)
		if ok && reply.Err == OK {
			return reply.Config
		}
		if ok && reply.Err == ErrWrongLeader {
			ck.leaderId = (ck.leaderId + 1) % len(ck.servers)
		} else if !ok {
			ck.leaderId = (ck.leaderId + 1) % len(ck.servers)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (ck *Clerk) Join(servers map[int][]string) {
	ck.seqNum++
	args := &JoinArgs{
		Servers:  servers,
		ClientId: ck.clientId,
		SeqNum:   ck.seqNum,
	}

	for {
		// try each known server.
		reply := &JoinReply{}
		ok := ck.servers[ck.leaderId].Call("ShardCtrler.Join", args, reply)
		if ok && reply.Err == OK {
			return
		}
		if ok && reply.Err == ErrWrongLeader {
			ck.leaderId = (ck.leaderId + 1) % len(ck.servers)
		} else if !ok {
			ck.leaderId = (ck.leaderId + 1) % len(ck.servers)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (ck *Clerk) Leave(gids []int) {
	ck.seqNum++
	args := &LeaveArgs{
		GIDs:     gids,
		ClientId: ck.clientId,
		SeqNum:   ck.seqNum,
	}

	for {
		// try each known server.
		reply := &LeaveReply{}
		ok := ck.servers[ck.leaderId].Call("ShardCtrler.Leave", args, reply)
		if ok && reply.Err == OK {
			return
		}
		if ok && reply.Err == ErrWrongLeader {
			ck.leaderId = (ck.leaderId + 1) % len(ck.servers)
		} else if !ok {
			ck.leaderId = (ck.leaderId + 1) % len(ck.servers)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (ck *Clerk) Move(shard int, gid int) {
	ck.seqNum++
	args := &MoveArgs{
		Shard:    shard,
		GID:      gid,
		ClientId: ck.clientId,
		SeqNum:   ck.seqNum,
	}

	for {
		// try each known server.
		reply := &MoveReply{}
		ok := ck.servers[ck.leaderId].Call("ShardCtrler.Move", args, reply)
		if ok && reply.Err == OK {
			return
		}
		if ok && reply.Err == ErrWrongLeader {
			ck.leaderId = (ck.leaderId + 1) % len(ck.servers)
		} else if !ok {
			ck.leaderId = (ck.leaderId + 1) % len(ck.servers)
		}
		time.Sleep(100 * time.Millisecond)
	}
}