// Unless explicitly stated otherwise all files in this repository are licensed under the MIT License.
//
// This product includes software developed at Datadog (https://www.datadoghq.com/). Copyright 2024 Datadog, Inc.

package main

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/sdk/workflow"
)

func HelloWorld(ctx workflow.Context) (string, error) {
	workflow.GetLogger(ctx).Info("HelloWorld workflow started")
	ctx = setActivityTimeout(ctx, 5*time.Minute)

	// Compute a subject
	var subject string
	if err := workflow.ExecuteActivity(ctx, GetSubject).Get(ctx, &subject); err != nil {
		return "", err
	}

	// Sleep for a while
	if err := workflow.Sleep(ctx, 2*time.Minute); err != nil {
		return "", err
	}

	// Return the greeting
	return fmt.Sprintf("Hello %s", subject), nil
}

func GetSubject(ctx context.Context) (string, error) {
	return "World", nil
}

func Sleep(ctx context.Context, seconds uint) error {
	time.Sleep(time.Duration(seconds) * time.Second)
	return nil
}
