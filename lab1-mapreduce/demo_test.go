package mr

import (
	"fmt"
	"io/ioutil"
	"mit6824/lab1-mapreduce/mr"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"
)

// 演示用的Map函数 - 词频统计
func demoMapFunction(filename string, contents string) []mr.KeyValue {
	ff := func(r rune) bool { return !unicode.IsLetter(r) }
	words := strings.FieldsFunc(contents, ff)
	kva := []mr.KeyValue{}
	for _, w := range words {
		// 转换为小写以便统计
		kv := mr.KeyValue{strings.ToLower(w), "1"}
		kva = append(kva, kv)
	}
	return kva
}

// 演示用的Reduce函数 - 计数
func demoReduceFunction(key string, values []string) string {
	return strconv.Itoa(len(values))
}

// 创建5个测试输入文件
func createDemoInputFiles() []string {
	// 5个不同主题的文本内容
	testContents := []string{
		// 文件1: 动物主题
		"cat dog bird fish cat dog elephant tiger lion cat bird fish",
		// 文件2: 水果主题  
		"apple banana orange apple grape banana cherry apple orange grape",
		// 文件3: 颜色主题
		"red blue green yellow red blue purple pink red green yellow blue",
		// 文件4: 数字主题
		"one two three four five one two six seven eight one two three",
		// 文件5: 混合主题
		"hello world test data processing hello world computer science hello",
	}

	var files []string
	for i, content := range testContents {
		filename := fmt.Sprintf("demo-input-%d.txt", i+1)
		err := ioutil.WriteFile(filename, []byte(content), 0644)
		if err != nil {
			panic(fmt.Sprintf("Failed to create demo file %s: %v", filename, err))
		}
		files = append(files, filename)
		fmt.Printf("创建输入文件: %s\n内容: %s\n\n", filename, content)
	}
	return files
}

// 清理演示文件
func cleanupDemoFiles(files []string) {
	fmt.Println("清理临时文件...")
	for _, file := range files {
		os.Remove(file)
	}
	// 清理中间文件和输出文件
	glob, _ := filepath.Glob("mr-*")
	for _, file := range glob {
		os.Remove(file)
	}
	glob, _ = filepath.Glob("demo-*")
	for _, file := range glob {
		os.Remove(file)
	}
}

// 读取并显示输出结果
func readAndDisplayOutput(nReduce int) map[string]int {
	result := make(map[string]int)
	fmt.Println("\n=== 读取输出文件 ===")
	
	for i := 0; i < nReduce; i++ {
		filename := fmt.Sprintf("mr-out-%d", i)
		content, err := ioutil.ReadFile(filename)
		if err != nil {
			fmt.Printf("无法读取输出文件 %s: %v\n", filename, err)
			continue
		}
		
		fmt.Printf("\n输出文件 %s 内容:\n", filename)
		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			if line == "" {
				continue
			}
			fmt.Printf("  %s\n", line)
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

// 主要的演示函数
func TestMapReduceDemo(t *testing.T) {
	fmt.Println("=== MapReduce 演示程序 ===")
	fmt.Println("配置: 5个输入文件, 5个Map任务, 3个Reduce任务")
	fmt.Println()

	// 1. 创建5个输入文件
	files := createDemoInputFiles()
	defer cleanupDemoFiles(files)

	// 2. 配置MapReduce参数
	nReduce := 3 // 3个Reduce任务
	nMap := len(files) // 5个Map任务（每个文件一个）

	fmt.Printf("MapReduce配置:\n")
	fmt.Printf("- 输入文件数量: %d\n", nMap)
	fmt.Printf("- Map任务数量: %d\n", nMap)
	fmt.Printf("- Reduce任务数量: %d\n\n", nReduce)

	// 3. 创建Coordinator
	fmt.Println("=== 启动Coordinator ===")
	c := mr.MakeCoordinator(files, nReduce)
	defer func() {
		for !c.Done() {
			time.Sleep(100 * time.Millisecond)
		}
	}()

	// 4. 启动多个Worker进程
	nWorkers := 3
	fmt.Printf("启动 %d 个Worker进程...\n\n", nWorkers)
	
	var wg sync.WaitGroup
	for i := 0; i < nWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			fmt.Printf("Worker %d 开始工作\n", workerID)
			mr.Worker(demoMapFunction, demoReduceFunction)
			fmt.Printf("Worker %d 完成工作\n", workerID)
		}(i)
	}

	// 5. 等待所有任务完成
	fmt.Println("\n=== 等待任务完成 ===")
	start := time.Now()
	for !c.Done() {
		time.Sleep(500 * time.Millisecond)
		fmt.Print(".")
	}
	duration := time.Since(start)
	fmt.Printf("\n所有任务完成! 耗时: %v\n", duration)

	// 6. 读取并显示结果
	result := readAndDisplayOutput(nReduce)

	// 7. 统计和分析结果
	fmt.Println("\n=== 最终统计结果 ===")
	fmt.Printf("总共统计了 %d 个不同的单词\n\n", len(result))

	// 按词频排序显示
	type wordCount struct {
		word  string
		count int
	}
	var sortedResults []wordCount
	for word, count := range result {
		sortedResults = append(sortedResults, wordCount{word, count})
	}

	// 简单排序（按计数降序）
	for i := 0; i < len(sortedResults)-1; i++ {
		for j := i + 1; j < len(sortedResults); j++ {
			if sortedResults[j].count > sortedResults[i].count {
				sortedResults[i], sortedResults[j] = sortedResults[j], sortedResults[i]
			}
		}
	}

	fmt.Println("词频统计结果（按频率降序）:")
	for i, wc := range sortedResults {
		fmt.Printf("%2d. %-12s: %d次\n", i+1, wc.word, wc.count)
	}

	// 8. 验证一些预期结果
	fmt.Println("\n=== 结果验证 ===")
	expectedWords := map[string]int{
		"hello": 3, // 在文件5中出现3次
		"world": 2, // 在文件5中出现2次
		"cat":   3, // 在文件1中出现3次
		"apple": 3, // 在文件2中出现3次
		"red":   3, // 在文件3中出现3次
		"one":   3, // 在文件4中出现3次
	}

	allCorrect := true
	for word, expectedCount := range expectedWords {
		if actualCount, exists := result[word]; exists {
			if actualCount == expectedCount {
				fmt.Printf("✓ '%s': 预期 %d, 实际 %d\n", word, expectedCount, actualCount)
			} else {
				fmt.Printf("✗ '%s': 预期 %d, 实际 %d\n", word, expectedCount, actualCount)
				allCorrect = false
			}
		} else {
			fmt.Printf("✗ '%s': 预期 %d, 但未找到\n", word, expectedCount)
			allCorrect = false
		}
	}

	if allCorrect {
		fmt.Println("\n🎉 所有验证都通过了！MapReduce演示成功！")
	} else {
		fmt.Println("\n⚠️  部分验证失败，请检查实现")
		t.Fail()
	}

	fmt.Println("\n=== 演示完成 ===")
}

// 简化版演示（不包含详细输出）
func TestSimpleDemo(t *testing.T) {
	files := createDemoInputFiles()
	defer cleanupDemoFiles(files)

	c := mr.MakeCoordinator(files, 3)
	
	// 启动worker
	go mr.Worker(demoMapFunction, demoReduceFunction)
	go mr.Worker(demoMapFunction, demoReduceFunction)

	// 等待完成
	for !c.Done() {
		time.Sleep(100 * time.Millisecond)
	}

	result := readAndDisplayOutput(3)
	fmt.Printf("简化演示完成，统计了 %d 个单词\n", len(result))
}