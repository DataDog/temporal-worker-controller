package main

import (
	"context"
	"time"

	"go.temporal.io/sdk/workflow"
)

func HelloWorld(ctx workflow.Context) (string, error) {
	workflow.GetLogger(ctx).Info("HelloWorld workflow started")

	workflow.GetMetricsHandler(ctx).WithTags(map[string]string{
		"workflow_type": "HelloWorld",
	}).Counter("demo_temporal_workflow_start").Inc(1)

	workflow.SideEffect(ctx, func(ctx workflow.Context) interface{} {
		return nil
	})

	if err := workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			ScheduleToCloseTimeout: time.Minute,
		}),
		Sleep, 5,
	).Get(ctx, nil); err != nil {
		return "", err
	}

	if err := workflow.Sleep(ctx, 30*time.Second); err != nil {
		return "", err
	}

	return "Hello World!", nil
}

func Sleep(ctx context.Context, seconds uint) error {
	time.Sleep(time.Duration(seconds) * time.Second)
	return nil
}
