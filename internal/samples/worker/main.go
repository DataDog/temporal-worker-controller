// Unless explicitly stated otherwise all files in this repository are licensed under the MIT License.
//
// This product includes software developed at Datadog (https://www.datadoghq.com/). Copyright 2024 Datadog, Inc.

package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

var (
	temporalHostPort  = os.Getenv("TEMPORAL_HOST_PORT")
	temporalNamespace = os.Getenv("TEMPORAL_NAMESPACE")
	temporalTaskQueue = os.Getenv("TEMPORAL_TASK_QUEUE")
	workerBuildID     = os.Getenv("TEMPORAL_BUILD_ID")
)

func main() {
	l := log.NewStructuredLogger(slog.Default())
	l.Info("Worker config",
		"temporal.hostport", temporalHostPort,
		"temporal.namespace", temporalNamespace,
		"temporal.taskqueue", temporalTaskQueue,
		"worker.buildID", workerBuildID,
	)

	c, err := client.Dial(client.Options{
		HostPort:  temporalHostPort,
		Namespace: temporalNamespace,
		Logger:    l,
	})
	if err != nil {
		l.Error("Unable to create Temporal client", "error", err)
		os.Exit(1)
	}

	w := worker.New(c, temporalTaskQueue, worker.Options{
		BuildID:                 workerBuildID,
		UseBuildIDForVersioning: true,
	})
	defer w.Stop()

	w.RegisterWorkflowWithOptions(HelloWorldWorkflow, workflow.RegisterOptions{Name: "hello_world"})

	if err := w.Run(worker.InterruptCh()); err != nil {
		l.Error("Unable to start worker", "error", err)
		os.Exit(1)
	}
}

func HelloWorldWorkflow(ctx workflow.Context) (string, error) {
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
