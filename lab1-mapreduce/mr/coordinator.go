package mr

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"sync"
	"time"
)

type TaskType int

const (
	MapTask TaskType = iota
	ReduceTask
	WaitTask
	ExitTask
)

type TaskStatus int

const (
	Idle TaskStatus = iota
	InProgress
	Completed
)

type Task struct {
	TaskType   TaskType
	TaskId     int
	InputFiles []string
	OutputFile string
	NReduce    int
	NMap       int
}

type TaskInfo struct {
	Task      Task
	Status    TaskStatus
	StartTime time.Time
}

type Coordinator struct {
	mu           sync.Mutex
	mapTasks     []TaskInfo
	reduceTasks  []TaskInfo
	nReduce      int
	nMap         int
	mapFinished  bool
	allFinished  bool
	files        []string
}

// RPC handlers
func (c *Coordinator) GetTask(args *GetTaskArgs, reply *GetTaskReply) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check for timed out tasks
	c.checkTimeouts()

	// Assign map tasks first
	if !c.mapFinished {
		for i := range c.mapTasks {
			if c.mapTasks[i].Status == Idle {
				c.mapTasks[i].Status = InProgress
				c.mapTasks[i].StartTime = time.Now()
				reply.Task = c.mapTasks[i].Task
				return nil
			}
		}
		// No idle map tasks, return wait task
		reply.Task = Task{TaskType: WaitTask}
		return nil
	}

	// Map tasks finished, assign reduce tasks
	for i := range c.reduceTasks {
		if c.reduceTasks[i].Status == Idle {
			c.reduceTasks[i].Status = InProgress
			c.reduceTasks[i].StartTime = time.Now()
			reply.Task = c.reduceTasks[i].Task
			return nil
		}
	}

	// All tasks assigned or completed
	if c.allFinished {
		reply.Task = Task{TaskType: ExitTask}
	} else {
		reply.Task = Task{TaskType: WaitTask}
	}
	return nil
}

func (c *Coordinator) TaskCompleted(args *TaskCompletedArgs, reply *TaskCompletedReply) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch args.TaskType {
	case MapTask:
		if args.TaskId < len(c.mapTasks) && c.mapTasks[args.TaskId].Status == InProgress {
			c.mapTasks[args.TaskId].Status = Completed
			c.checkMapCompletion()
		}
	case ReduceTask:
		if args.TaskId < len(c.reduceTasks) && c.reduceTasks[args.TaskId].Status == InProgress {
			c.reduceTasks[args.TaskId].Status = Completed
			c.checkAllCompletion()
		}
	}
	return nil
}

func (c *Coordinator) checkTimeouts() {
	now := time.Now()
	timeout := 10 * time.Second

	// Check map task timeouts
	for i := range c.mapTasks {
		if c.mapTasks[i].Status == InProgress && now.Sub(c.mapTasks[i].StartTime) > timeout {
			c.mapTasks[i].Status = Idle
		}
	}

	// Check reduce task timeouts
	for i := range c.reduceTasks {
		if c.reduceTasks[i].Status == InProgress && now.Sub(c.reduceTasks[i].StartTime) > timeout {
			c.reduceTasks[i].Status = Idle
		}
	}
}

func (c *Coordinator) checkMapCompletion() {
	for _, task := range c.mapTasks {
		if task.Status != Completed {
			return
		}
	}
	c.mapFinished = true
}

func (c *Coordinator) checkAllCompletion() {
	for _, task := range c.reduceTasks {
		if task.Status != Completed {
			return
		}
	}
	c.allFinished = true
}

// start a thread that listens for RPCs from worker.go
func (c *Coordinator) server() {
	rpc.Register(c)
	rpc.HandleHTTP()
	l, e := net.Listen("tcp", ":1234")
	sockname := coordinatorSock()
	os.Remove(sockname)
	l, e = net.Listen("unix", sockname)
	if e != nil {
		log.Fatal("listen error:", e)
	}
	go http.Serve(l, nil)
}

// main/mrcoordinator.go calls Done() periodically to find out
// if the entire job has finished.
func (c *Coordinator) Done() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.allFinished
}

// create a Coordinator.
// main/mrcoordinator.go calls this function.
// nReduce is the number of reduce tasks to use.
func MakeCoordinator(files []string, nReduce int) *Coordinator {
	c := Coordinator{
		nReduce: nReduce,
		nMap:    len(files),
		files:  files,
	}

	// Initialize map tasks
	c.mapTasks = make([]TaskInfo, len(files))
	for i, file := range files {
		c.mapTasks[i] = TaskInfo{
			Task: Task{
				TaskType:   MapTask,
				TaskId:     i,
				InputFiles: []string{file},
				NReduce:    nReduce,
				NMap:       len(files),
			},
			Status: Idle,
		}
	}

	// Initialize reduce tasks
	c.reduceTasks = make([]TaskInfo, nReduce)
	for i := 0; i < nReduce; i++ {
		c.reduceTasks[i] = TaskInfo{
			Task: Task{
				TaskType:   ReduceTask,
				TaskId:     i,
				OutputFile: fmt.Sprintf("mr-out-%d", i),
				NReduce:    nReduce,
				NMap:       len(files),
			},
			Status: Idle,
		}
	}

	c.server()
	return &c
}