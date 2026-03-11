// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package topology

// reverseLevelOrder performs a bottom-up level order traversal of the topology tree.
//
// It returns a slice of domains starting from the bottom-most level up to the root.
// Within each level, domains are ordered left-to-right as they appear in the tree.
//
// Example:
//
//	       1
//	   /   |   \
//	  2    3    4
//	 / \        |
//	5   6       7
//
// reverseLevelOrder(root) returns: [5 6 7 2 3 4 1]
func reverseLevelOrder(root *DomainInfo) []*DomainInfo {
	if root == nil {
		return nil
	}

	// Perform level-order traversal and collect nodes by level
	var levels [][]*DomainInfo
	queue := []*DomainInfo{root}

	for len(queue) > 0 {
		levelSize := len(queue)
		currentLevel := make([]*DomainInfo, 0, levelSize)

		for i := 0; i < levelSize; i++ {
			curr := queue[0]
			queue = queue[1:]

			currentLevel = append(currentLevel, curr)

			// enqueue children
			for _, child := range curr.Children {
				queue = append(queue, child)
			}
		}

		levels = append(levels, currentLevel)
	}

	// Reverse the levels and flatten
	result := make([]*DomainInfo, 0)
	for i := len(levels) - 1; i >= 0; i-- {
		result = append(result, levels[i]...)
	}

	return result
}
