package main

import (
	"context"
	"time"

	"go.temporal.io/sdk/workflow"
)

func HelloWorld(ctx workflow.Context) (string, error) {
	workflow.GetLogger(ctx).Info("HelloWorld workflow started")

	if err := workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			ScheduleToCloseTimeout: time.Minute,
		}),
		Sleep, 30,
	).Get(ctx, nil); err != nil {
		return "", err
	}

	return "Hello World!", nil
}

func Sleep(ctx context.Context, seconds uint) error {
	time.Sleep(time.Duration(seconds) * time.Second)
	return nil
}

//http://localhost:8233/namespaces/default/workflows?query=BuildIds+%3D+%22versioned%3A6689d9b994%22

//temporal workflow reset \
//  --type FirstWorkflowTask \
//  --reason "bad version: workflow panic" \
//  --query 'BuildIds = "versioned:6689d9b994"'

// if v := workflow.GetVersion(ctx, "sleep-without-activity", workflow.DefaultVersion, 1); v == 1 {
//		if err := workflow.Sleep(ctx, 10*time.Second); err != nil {
//			return "", err
//		}
//	} else {
//		if err := workflow.ExecuteActivity(
//			workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
//				ScheduleToCloseTimeout: time.Minute,
//			}),
//			Sleep, 30,
//		).Get(ctx, nil); err != nil {
//			return "", err
//		}
//	}
