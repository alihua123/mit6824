package mr

import (
	"os"
	"strconv"
)

// RPC definitions.

// GetTask RPC
type GetTaskArgs struct {
}

type GetTaskReply struct {
	Task Task
}

// TaskCompleted RPC
type TaskCompletedArgs struct {
	TaskType TaskType
	TaskId   int
}

type TaskCompletedReply struct {
}

// Cook up a unique-ish UNIX-domain socket name
// in /var/tmp, for the coordinator.
// Can't use the current directory since
// Athena AFS doesn't support UNIX-domain sockets.
func coordinatorSock() string {
	s := "/var/tmp/824-mr-"
	s += strconv.Itoa(os.Getuid())
	return s
}