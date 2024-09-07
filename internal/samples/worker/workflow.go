package main

import (
	"context"
	"time"

	"go.temporal.io/sdk/workflow"
)

func HelloWorldWorkflow(ctx workflow.Context) (string, error) {
	workflow.GetLogger(ctx).Info("HelloWorldWorkflow started")

	if err := executeLocalActivity(ctx); err != nil {
		return "", err
	}

	workflow.SideEffect(ctx, func(ctx workflow.Context) interface{} {
		return nil
	})

	if err := workflow.Sleep(ctx, 30*time.Second); err != nil {
		return "", err
	}

	return "Hello World!", nil
}

func executeLocalActivity(ctx workflow.Context) error {
	return workflow.ExecuteLocalActivity(
		workflow.WithLocalActivityOptions(ctx, workflow.LocalActivityOptions{
			ScheduleToCloseTimeout: time.Second,
		}),
		func(ctx context.Context) error { return nil },
	).Get(ctx, nil)
}
