// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package topology

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReverseLevelOrder(t *testing.T) {
	tests := []struct {
		name     string
		root     *DomainInfo
		expected []DomainID
	}{
		{
			name:     "nil root returns nil",
			root:     nil,
			expected: nil,
		},
		{
			name: "single node",
			root: &DomainInfo{
				ID:    "1",
				Level: "zone",
			},
			expected: []DomainID{"1"},
		},
		{
			name: "simple tree - documentation example",
			// Tree structure:
			//        1
			//    /   |   \
			//   2    3    4
			//  / \        |
			// 5   6       7
			// Bottom-up: [5 6 7 2 3 4 1]
			root: &DomainInfo{
				ID:    "1",
				Level: "zone",
				Children: []*DomainInfo{
					{
						ID:    "2",
						Level: "spine",
						Children: []*DomainInfo{
							{ID: "5", Level: "rack"},
							{ID: "6", Level: "rack"},
						},
					},
					{
						ID:    "3",
						Level: "spine",
					},
					{
						ID:    "4",
						Level: "spine",
						Children: []*DomainInfo{
							{ID: "7", Level: "rack"},
						},
					},
				},
			},
			expected: []DomainID{"5", "6", "7", "2", "3", "4", "1"},
		},
		{
			name: "binary tree",
			// Tree structure:
			//       A
			//      / \
			//     B   C
			//    / \
			//   D   E
			// Bottom-up: [D E] [B C] [A]
			root: &DomainInfo{
				ID:    "A",
				Level: "zone",
				Children: []*DomainInfo{
					{
						ID:    "B",
						Level: "spine",
						Children: []*DomainInfo{
							{ID: "D", Level: "rack"},
							{ID: "E", Level: "rack"},
						},
					},
					{
						ID:    "C",
						Level: "spine",
					},
				},
			},
			expected: []DomainID{"D", "E", "B", "C", "A"},
		},
		{
			name: "linear tree (linked list)",
			// Tree structure:
			//   1
			//   |
			//   2
			//   |
			//   3
			root: &DomainInfo{
				ID:    "1",
				Level: "zone",
				Children: []*DomainInfo{
					{
						ID:    "2",
						Level: "spine",
						Children: []*DomainInfo{
							{ID: "3", Level: "rack"},
						},
					},
				},
			},
			expected: []DomainID{"3", "2", "1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reverseLevelOrder(tt.root)

			if tt.expected == nil {
				assert.Nil(t, result)
				return
			}

			assert.Equal(t, len(tt.expected), len(result), "length mismatch")

			var resultIDs []DomainID
			for _, domain := range result {
				resultIDs = append(resultIDs, domain.ID)
			}

			assert.Equal(t, tt.expected, resultIDs, "traversal order mismatch")
		})
	}
}
