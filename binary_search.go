package main

import "fmt"

// 704. 二分查找
// 时间复杂度: O(log n)
// 空间复杂度: O(1)
func search(nums []int, target int) int {
    left, right := 0, len(nums)-1
    
    for left <= right {
        mid := (left + right) / 2
        
        if nums[mid] == target {
            return mid
        }
        
        if nums[mid] < target {
            left = mid + 1
        } else {
            right = mid - 1
        }
    }
    
    return -1
}

// 测试函数
func main() {
    // 示例 1
    nums1 := []int{-1, 0, 3, 5, 9, 12}
    target1 := 9
    result1 := search(nums1, target1)
    fmt.Printf("示例 1: nums = %v, target = %d, 输出: %d\n", nums1, target1, result1)
    
    // 示例 2
    nums2 := []int{-1, 0, 3, 5, 9, 12}
    target2 := 2
    result2 := search(nums2, target2)
    fmt.Printf("示例 2: nums = %v, target = %d, 输出: %d\n", nums2, target2, result2)
    
    // 额外测试用例
    nums3 := []int{5}
    target3 := 5
    result3 := search(nums3, target3)
    fmt.Printf("额外测试: nums = %v, target = %d, 输出: %d\n", nums3, target3, result3)
}