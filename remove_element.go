package main

import "fmt"

// 27. 移除元素
// 双指针法实现
func removeElement(nums []int, val int) int {
    left := 0
    right := len(nums) - 1
    
    for left <= right {
        // 从左边找到等于val的元素
        for left <= right && nums[left] != val {
            left++
        }
        
        // 从右边找到不等于val的元素
        for left <= right && nums[right] == val {
            right--
        }
        
        // 如果左指针还在右指针左边，交换元素
        if left < right {
            nums[left], nums[right] = nums[right], nums[left]
            left++
            right--
        }
    }
    
    return left
}

// 更简单的双指针实现（快慢指针）
func removeElementSimple(nums []int, val int) int {
    slow := 0
    for fast := 0; fast < len(nums); fast++ {
        if nums[fast] != val {
            nums[slow] = nums[fast]
            slow++
        }
    }
    return slow
}

// 测试函数
func main() {
    // 测试用例1
    nums1 := []int{3, 2, 2, 3}
    val1 := 3
    result1 := removeElement(nums1, val1)
    fmt.Printf("测试1: nums = [3,2,2,3], val = %d, 结果长度 = %d, 数组 = %v\n", val1, result1, nums1[:result1])
    
    // 测试用例2
    nums2 := []int{0, 1, 2, 2, 3, 0, 4, 2}
    val2 := 2
    result2 := removeElementSimple(nums2, val2)
    fmt.Printf("测试2: nums = [0,1,2,2,3,0,4,2], val = %d, 结果长度 = %d, 数组 = %v\n", val2, result2, nums2[:result2])
}