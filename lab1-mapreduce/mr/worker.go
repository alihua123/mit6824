package mr

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io/ioutil"
	"log"
	"net/rpc"
	"os"
	"sort"
	_ "strconv"
	"time"
)

// Map functions return a slice of KeyValue.
type KeyValue struct {
	Key   string
	Value string
}

// for sorting by key.
type ByKey []KeyValue

func (a ByKey) Len() int           { return len(a) }
func (a ByKey) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByKey) Less(i, j int) bool { return a[i].Key < a[j].Key }

// use ihash(key) % NReduce to choose the reduce
// task number for each KeyValue emitted by Map.
func ihash(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() & 0x7fffffff)
}

// main/mrworker.go calls this function.
func Worker(mapf func(string, string) []KeyValue,
	reducef func(string, []string) string) {

	for {
		// Request a task from coordinator
		task := requestTask()

		switch task.TaskType {
		case MapTask:
			performMapTask(task, mapf)
		case ReduceTask:
			performReduceTask(task, reducef)
		case WaitTask:
			time.Sleep(time.Second)
		case ExitTask:
			return
		}
	}
}

func requestTask() Task {
	args := GetTaskArgs{}
	reply := GetTaskReply{}

	ok := Call("Coordinator.GetTask", &args, &reply)
	if !ok {
		log.Fatal("Failed to contact coordinator")
	}

	return reply.Task
}

func performMapTask(task Task, mapf func(string, string) []KeyValue) {
	// Read input file
	filename := task.InputFiles[0]
	file, err := os.Open(filename)
	if err != nil {
		log.Fatalf("cannot open %v", filename)
	}
	content, err := ioutil.ReadAll(file)
	if err != nil {
		log.Fatalf("cannot read %v", filename)
	}
	file.Close()

	// Apply map function
	kva := mapf(filename, string(content))

	// Create intermediate files for each reduce task
	intermediate := make([][]KeyValue, task.NReduce)
	for _, kv := range kva {
		reduceTaskNum := ihash(kv.Key) % task.NReduce
		intermediate[reduceTaskNum] = append(intermediate[reduceTaskNum], kv)
	}

	// Write intermediate files
	for i := 0; i < task.NReduce; i++ {
		intermediateFilename := fmt.Sprintf("mr-%d-%d", task.TaskId, i)
		file, err := os.Create(intermediateFilename)
		if err != nil {
			log.Fatalf("cannot create %v", intermediateFilename)
		}

		enc := json.NewEncoder(file)
		for _, kv := range intermediate[i] {
			err := enc.Encode(&kv)
			if err != nil {
				log.Fatalf("cannot encode %v", kv)
			}
		}
		file.Close()
	}

	// Notify coordinator of completion
	notifyTaskCompletion(task.TaskType, task.TaskId)
}

func performReduceTask(task Task, reducef func(string, []string) string) {
	// Read all intermediate files for this reduce task
	var kva []KeyValue
	for i := 0; i < task.NMap; i++ {
		intermediateFilename := fmt.Sprintf("mr-%d-%d", i, task.TaskId)
		file, err := os.Open(intermediateFilename)
		if err != nil {
			continue // File might not exist if map task hasn't completed
		}

		dec := json.NewDecoder(file)
		for {
			var kv KeyValue
			if err := dec.Decode(&kv); err != nil {
				break
			}
			kva = append(kva, kv)
		}
		file.Close()
	}

	// Sort by key
	sort.Sort(ByKey(kva))

	// Create output file
	ofile, err := os.Create(task.OutputFile)
	if err != nil {
		log.Fatalf("cannot create %v", task.OutputFile)
	}

	// Apply reduce function to each unique key
	i := 0
	for i < len(kva) {
		j := i + 1
		for j < len(kva) && kva[j].Key == kva[i].Key {
			j++
		}
		values := []string{}
		for k := i; k < j; k++ {
			values = append(values, kva[k].Value)
		}
		output := reducef(kva[i].Key, values)

		// Write to output file
		fmt.Fprintf(ofile, "%v %v\n", kva[i].Key, output)

		i = j
	}
	ofile.Close()

	// Clean up intermediate files
	for i := 0; i < task.NMap; i++ {
		intermediateFilename := fmt.Sprintf("mr-%d-%d", i, task.TaskId)
		os.Remove(intermediateFilename)
	}

	// Notify coordinator of completion
	notifyTaskCompletion(task.TaskType, task.TaskId)
}

func notifyTaskCompletion(taskType TaskType, taskId int) {
	args := TaskCompletedArgs{
		TaskType: taskType,
		TaskId:   taskId,
	}
	reply := TaskCompletedReply{}

	Call("Coordinator.TaskCompleted", &args, &reply)
}

// send an RPC request to the coordinator, wait for the response.
// usually returns true.
// returns false if something goes wrong.
func Call(rpcname string, args interface{}, reply interface{}) bool {
	// 使用TCP协议连接
	sockname := coordinatorSock()
	c, err := rpc.DialHTTP("tcp", sockname)
	if err != nil {
		log.Fatal("dialing:", err)
	}
	defer c.Close()

	err = c.Call(rpcname, args, reply)
	if err == nil {
		return true
	}

	fmt.Println(err)
	return false
}
