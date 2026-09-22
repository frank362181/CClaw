package common

// AllSame 用来判断数组中所有元素都相同
// 这里要注意的是：T的成员必须支持 ==/!=的判断操作
func AllSame[T comparable](a []T) bool {
	for i := 1; i < len(a); i++ {
		if a[i] != a[0] {
			return false
		}
	}

	return true
}

// IsAlternative 用来判断调用签名序列是否为二元交替(A,B,A,B...)
func IsAlternative[T comparable](a []T) bool {
	if len(a) < 2 || a[0] == a[1] {
		return false
	}

	for i := 2; i < len(a); i++ {
		if a[i] != a[i-2] {
			return false
		}
	}

	return true
}

// RemoveInB 从a中删除出现在b中元素
func RemoveInB[T comparable](a, b []T) []T {
	set := make(map[T]struct{}, len(b))

	for _, v := range b {
		set[v] = struct{}{}
	}

	result := make([]T, 0, len(a))
	for _, v := range a {
		if _, ok := set[v]; !ok {
			result = append(result, v)
		}
	}

	return result
}
