package main

import (
	"context"
	"time"

	"go.temporal.io/sdk/workflow"
)

func HelloWorld(ctx workflow.Context) (string, error) {
	workflow.GetLogger(ctx).Info("HelloWorldWorkflow started")

	workflow.SideEffect(ctx, func(ctx workflow.Context) interface{} {
		return nil
	})

	//if err := workflow.ExecuteActivity(
	//	workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
	//		ScheduleToCloseTimeout: time.Minute,
	//	}),
	//	Sleep, 15,
	//).Get(ctx, nil); err != nil {
	//	return "", err
	//}

	//if err := workflow.Sleep(ctx, 30*time.Second); err != nil {
	//	return "", err
	//}

	return "Hello World!!!!", nil
}

func Sleep(ctx context.Context, seconds uint) error {
	time.Sleep(time.Duration(seconds) * time.Second)
	return nil
}

func executeLocalActivity(ctx workflow.Context) error {
	return workflow.ExecuteLocalActivity(
		workflow.WithLocalActivityOptions(ctx, workflow.LocalActivityOptions{
			ScheduleToCloseTimeout: time.Second,
		}),
		func(ctx context.Context) error {
			time.Sleep(time.Second)
			return nil
		},
	).Get(ctx, nil)
}
