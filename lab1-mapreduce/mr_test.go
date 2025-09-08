package mr

import (
	"fmt"
	"io/ioutil"
	"mit6824/lab1-mapreduce/mr"
"os"
	"path/filepath"
	_ "sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"
)

// 测试用的Map函数 - 词频统计
func testMapFunction(filename string, contents string) []mr.KeyValue {
	ff := func(r rune) bool { return !unicode.IsLetter(r) }
	words := strings.FieldsFunc(contents, ff)
	kva := []mr.KeyValue{}
	for _, w := range words {
		kv := mr.KeyValue{w, "1"}
		kva = append(kva, kv)
	}
	return kva
}

// 测试用的Reduce函数 - 计数
func testReduceFunction(key string, values []string) string {
	return strconv.Itoa(len(values))
}

// 创建测试输入文件
func createTestFiles(t *testing.T, contents []string) []string {
	var files []string
	for i, content := range contents {
		filename := fmt.Sprintf("test-input-%d.txt", i)
		err := ioutil.WriteFile(filename, []byte(content), 0644)
		if err != nil {
			t.Fatalf("Failed to create test file %s: %v", filename, err)
		}
		files = append(files, filename)
	}
	return files
}

// 清理测试文件
func cleanupFiles(files []string) {
	for _, file := range files {
		os.Remove(file)
	}
	// 清理可能的中间文件和输出文件
	glob, _ := filepath.Glob("mr-*")
	for _, file := range glob {
		os.Remove(file)
	}
}

// 读取并解析输出文件
func readOutput(nReduce int) map[string]int {
	result := make(map[string]int)
	for i := 0; i < nReduce; i++ {
		filename := fmt.Sprintf("mr-out-%d", i)
		content, err := ioutil.ReadFile(filename)
		if err != nil {
			continue
		}
		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			if line == "" {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) == 2 {
				key := parts[0]
				count, _ := strconv.Atoi(parts[1])
				result[key] += count
			}
		}
	}
	return result
}

// 测试基本的MapReduce功能
func TestBasicMapReduce(t *testing.T) {
	// 创建测试输入
	testContents := []string{
		"hello world hello",
		"world test hello",
		"test world",
	}
	files := createTestFiles(t, testContents)
	defer cleanupFiles(files)

	// 创建coordinator
	nReduce := 3
	c := mr.MakeCoordinator(files, nReduce)
	defer func() {
		// 等待coordinator完成
		for !c.Done() {
			time.Sleep(100 * time.Millisecond)
		}
	}()

	// 启动多个worker
	nWorkers := 3
	var wg sync.WaitGroup
	for i := 0; i < nWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mr.Worker(testMapFunction, testReduceFunction)
		}()
	}

	// 等待所有任务完成
	for !c.Done() {
		time.Sleep(100 * time.Millisecond)
	}

	// 验证结果
	result := readOutput(nReduce)
	expected := map[string]int{
		"hello": 3,
		"world": 3,
		"test":  2,
	}

	for word, expectedCount := range expected {
		if actualCount, exists := result[word]; !exists || actualCount != expectedCount {
			t.Errorf("Word '%s': expected %d, got %d", word, expectedCount, actualCount)
		}
	}

	t.Logf("Basic MapReduce test passed. Results: %v", result)
}

// 测试单个文件的处理
func TestSingleFile(t *testing.T) {
	testContents := []string{"apple banana apple cherry banana apple"}
	files := createTestFiles(t, testContents)
	defer cleanupFiles(files)

	nReduce := 2
	c := mr.MakeCoordinator(files, nReduce)

	// 启动单个worker
	go mr.Worker(testMapFunction, testReduceFunction)

	// 等待完成
	for !c.Done() {
		time.Sleep(100 * time.Millisecond)
	}

	result := readOutput(nReduce)
	expected := map[string]int{
		"apple":  3,
		"banana": 2,
		"cherry": 1,
	}

	for word, expectedCount := range expected {
		if actualCount := result[word]; actualCount != expectedCount {
			t.Errorf("Word '%s': expected %d, got %d", word, expectedCount, actualCount)
		}
	}

	t.Logf("Single file test passed. Results: %v", result)
}

// 测试空文件处理
func TestEmptyFiles(t *testing.T) {
	testContents := []string{"", "hello world", ""}
	files := createTestFiles(t, testContents)
	defer cleanupFiles(files)

	nReduce := 2
	c := mr.MakeCoordinator(files, nReduce)

	go mr.Worker(testMapFunction, testReduceFunction)

	for !c.Done() {
		time.Sleep(100 * time.Millisecond)
	}

	result := readOutput(nReduce)
	expected := map[string]int{
		"hello": 1,
		"world": 1,
	}

	for word, expectedCount := range expected {
		if actualCount := result[word]; actualCount != expectedCount {
			t.Errorf("Word '%s': expected %d, got %d", word, expectedCount, actualCount)
		}
	}

	t.Logf("Empty files test passed. Results: %v", result)
}

// 测试并发worker
func TestConcurrentWorkers(t *testing.T) {
	// 创建更多的测试文件
	testContents := make([]string, 10)
	for i := 0; i < 10; i++ {
		testContents[i] = fmt.Sprintf("file%d word%d test common", i, i)
	}
	files := createTestFiles(t, testContents)
	defer cleanupFiles(files)

	nReduce := 5
	c := mr.MakeCoordinator(files, nReduce)

	// 启动多个并发worker
	nWorkers := 5
	var wg sync.WaitGroup
	for i := 0; i < nWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			t.Logf("Starting worker %d", workerID)
			mr.Worker(testMapFunction, testReduceFunction)
			t.Logf("Worker %d finished", workerID)
		}(i)
	}

	// 等待完成
	for !c.Done() {
		time.Sleep(100 * time.Millisecond)
	}

	result := readOutput(nReduce)
	
	// 验证common词出现10次（每个文件一次）
	if count := result["common"]; count != 10 {
		t.Errorf("Expected 'common' to appear 10 times, got %d", count)
	}

	// 验证test词出现10次
	if count := result["test"]; count != 10 {
		t.Errorf("Expected 'test' to appear 10 times, got %d", count)
	}

	t.Logf("Concurrent workers test passed. Total unique words: %d", len(result))
}

// 测试coordinator的Done()方法
func TestCoordinatorDone(t *testing.T) {
	testContents := []string{"quick test"}
	files := createTestFiles(t, testContents)
	defer cleanupFiles(files)

	c := mr.MakeCoordinator(files, 1)

	// 初始状态应该是未完成
	if c.Done() {
		t.Error("Coordinator should not be done initially")
	}

	// 启动worker
	go mr.Worker(testMapFunction, testReduceFunction)

	// 等待完成
	start := time.Now()
	for !c.Done() && time.Since(start) < 10*time.Second {
		time.Sleep(100 * time.Millisecond)
	}

	if !c.Done() {
		t.Error("Coordinator should be done after all tasks complete")
	}

	t.Log("Coordinator Done() test passed")
}

// 测试任务超时和重试机制
func TestTaskTimeout(t *testing.T) {
	testContents := []string{"timeout test data"}
	files := createTestFiles(t, testContents)
	defer cleanupFiles(files)

	c := mr.MakeCoordinator(files, 1)

	// 模拟一个会超时的worker
	go func() {
		task := requestTask()
		if task.TaskType == mr.MapTask {
			// 故意不完成任务，让它超时
			time.Sleep(12 * time.Second) // 超过10秒超时时间
		}
	}()

	// 等待一段时间让任务超时
	time.Sleep(11 * time.Second)

	// 启动正常的worker来完成任务
	go mr.Worker(testMapFunction, testReduceFunction)

	// 等待完成
	start := time.Now()
	for !c.Done() && time.Since(start) < 15*time.Second {
		time.Sleep(100 * time.Millisecond)
	}

	if !c.Done() {
		t.Error("Tasks should complete even after timeout and retry")
	}

	t.Log("Task timeout test passed")
}

// 测试不同的reduce数量
func TestDifferentReduceCounts(t *testing.T) {
	testContents := []string{"a b c d e f g h i j"}
	files := createTestFiles(t, testContents)
	defer cleanupFiles(files)

	reduceCounts := []int{1, 3, 5, 10}

	for _, nReduce := range reduceCounts {
		t.Run(fmt.Sprintf("nReduce=%d", nReduce), func(t *testing.T) {
			c := mr.MakeCoordinator(files, nReduce)
			go mr.Worker(testMapFunction, testReduceFunction)

			for !c.Done() {
				time.Sleep(100 * time.Millisecond)
			}

			result := readOutput(nReduce)
			
			// 验证每个字母都出现一次
			letters := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
			for _, letter := range letters {
				if count := result[letter]; count != 1 {
					t.Errorf("Letter '%s': expected 1, got %d", letter, count)
				}
			}

			// 清理输出文件
			for i := 0; i < nReduce; i++ {
				os.Remove(fmt.Sprintf("mr-out-%d", i))
			}
		})
	}

	t.Log("Different reduce counts test passed")
}

// 基准测试 - 测试性能
func BenchmarkMapReduce(b *testing.B) {
	// 创建较大的测试数据
	testContents := make([]string, 20)
	for i := 0; i < 20; i++ {
		content := strings.Repeat(fmt.Sprintf("word%d ", i%10), 1000)
		testContents[i] = content
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		files := createTestFiles(nil, testContents) // 不传testing.T，避免测试失败
		
		c := mr.MakeCoordinator(files, 5)
		
		// 启动多个worker
		for j := 0; j < 3; j++ {
			go mr.Worker(testMapFunction, testReduceFunction)
		}

		// 等待完成
		for !c.Done() {
			time.Sleep(10 * time.Millisecond)
		}

		cleanupFiles(files)
	}
}

// 辅助函数：请求任务（用于测试）
func requestTask() mr.Task {
	args := mr.GetTaskArgs{}
	reply := mr.GetTaskReply{}
	mr.Call("Coordinator.GetTask", &args, &reply)
	return reply.Task
}