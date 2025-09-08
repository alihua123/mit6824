package mr


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

// 修改为返回TCP地址而不是Unix套接字路径
func coordinatorSock() string {
	return "localhost:1234"
}