package main

import (
	"context"
	"time"

	"go.temporal.io/sdk/workflow"
)

func HelloWorld(ctx workflow.Context) (string, error) {
	workflow.GetLogger(ctx).Info("HelloWorld workflow started")

	//if err := workflow.ExecuteActivity(
	//	workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
	//		ScheduleToCloseTimeout: time.Minute,
	//	}),
	//	Sleep, 30,
	//).Get(ctx, nil); err != nil {
	//	return "", err
	//}

	if err := workflow.Sleep(ctx, 10*time.Second); err != nil {
		return "", err
	}

	return "Hello World!", nil
}

func Sleep(ctx context.Context, seconds uint) error {
	time.Sleep(time.Duration(seconds) * time.Second)
	return nil
}
