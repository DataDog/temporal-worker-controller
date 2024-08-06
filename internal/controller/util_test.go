// Unless explicitly stated otherwise all files in this repository are licensed under the MIT License.
//
// This product includes software developed at Datadog (https://www.datadoghq.com/). Copyright 2024 Datadog, Inc.

package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"

	temporaliov1alpha1 "github.com/DataDog/temporal-worker-controller/api/v1alpha1"
)

func TestGroupIntoBatches(t *testing.T) {
	type testCase struct {
		items     []int
		batchSize uint
		expected  [][]int
	}

	for name, tc := range map[string]testCase{
		"empty": {},
		"single batch": {
			items:     []int{1, 2, 3},
			batchSize: 3,
			expected:  [][]int{{1, 2, 3}},
		},
		"multiple batches": {
			items:     []int{1, 2, 3, 4, 5, 6, 7},
			batchSize: 2,
			expected:  [][]int{{1, 2}, {3, 4}, {5, 6}, {7}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			actual := groupIntoBatches(tc.items, tc.batchSize)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestFindHighestPriorityStatus(t *testing.T) {
	type testCase struct {
		statuses []temporaliov1alpha1.ReachabilityStatus
		expected temporaliov1alpha1.ReachabilityStatus
	}

	for name, tc := range map[string]testCase{
		"empty": {},
		"single status": {
			statuses: []temporaliov1alpha1.ReachabilityStatus{temporaliov1alpha1.ReachabilityStatusNew},
			expected: temporaliov1alpha1.ReachabilityStatusNew,
		},
		"multiple statuses": {
			statuses: []temporaliov1alpha1.ReachabilityStatus{
				temporaliov1alpha1.ReachabilityStatusNotRegistered,
				temporaliov1alpha1.ReachabilityStatusExisting,
				temporaliov1alpha1.ReachabilityStatusNew,
				temporaliov1alpha1.ReachabilityStatusUnreachable,
				temporaliov1alpha1.ReachabilityStatusExisting,
			},
			expected: temporaliov1alpha1.ReachabilityStatusNew,
		},
	} {
		t.Run(name, func(t *testing.T) {
			actual := findHighestPriorityStatus(tc.statuses)
			assert.Equal(t, tc.expected, actual)
		})
	}
}
