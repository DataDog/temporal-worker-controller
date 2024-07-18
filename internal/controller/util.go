package controller

import (
	"golang.org/x/exp/slices"

	temporaliov1alpha1 "github.com/DataDog/temporal-worker-controller/api/v1alpha1"
)

// groupIntoBatches splits a slice of items into batches of the given size.
func groupIntoBatches[T any](items []T, batchSize uint) [][]T {
	var batches [][]T
	for i := 0; i < len(items); i += int(batchSize) {
		end := i + int(batchSize)
		if end > len(items) {
			end = len(items)
		}
		batches = append(batches, items[i:end])
	}
	return batches
}

func findHighestPriorityStatus(statuses []temporaliov1alpha1.ReachabilityStatus) temporaliov1alpha1.ReachabilityStatus {
	if len(statuses) == 0 {
		return ""
	}
	slices.SortFunc(statuses, func(a, b temporaliov1alpha1.ReachabilityStatus) int {
		return getStatusPriority(a) - getStatusPriority(b)
	})
	return statuses[len(statuses)-1]
}

func getStatusPriority(s temporaliov1alpha1.ReachabilityStatus) int {
	switch s {
	case temporaliov1alpha1.ReachabilityStatusActive:
		return 4
	case temporaliov1alpha1.ReachabilityStatusQueryable:
		return 3
	case temporaliov1alpha1.ReachabilityStatusUnreachable:
		return 2
	case temporaliov1alpha1.ReachabilityStatusNotRegistered:
		return 1
	}
	return 0
}
