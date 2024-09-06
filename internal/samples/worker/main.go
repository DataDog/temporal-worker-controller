// Unless explicitly stated otherwise all files in this repository are licensed under the MIT License.
//
// This product includes software developed at Datadog (https://www.datadoghq.com/). Copyright 2024 Datadog, Inc.

package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

const helloWorldWorkflow = "hello_world"

var (
	temporalHostPort  = os.Getenv("TEMPORAL_HOST_PORT")
	temporalNamespace = os.Getenv("TEMPORAL_NAMESPACE")
	temporalTaskQueue = os.Getenv("TEMPORAL_TASK_QUEUE")
	workerBuildID     = os.Getenv("TEMPORAL_BUILD_ID")
)

func main() {
	// Read the value of the workflow-version flag.
	var workflowVersion string
	flag.StringVar(&workflowVersion, "workflow-version", "", "The workflow version to register with Temporal.")
	flag.Parse()

	if workflowVersion == "" {
		log.Fatal("workflow-version flag is required")
	}

	log.Println("Worker config is: ", temporalHostPort, temporalNamespace, temporalTaskQueue, workerBuildID)

	c, err := client.Dial(client.Options{
		HostPort:  temporalHostPort,
		Namespace: temporalNamespace,
	})
	if err != nil {
		log.Fatal(err)
	}

	w := worker.New(c, temporalTaskQueue, worker.Options{
		BuildID:                 workerBuildID,
		UseBuildIDForVersioning: true,
	})
	defer w.Stop()

	w.RegisterWorkflowWithOptions(HelloWorldV1, workflow.RegisterOptions{Name: helloWorldWorkflow})
	//w.RegisterWorkflowWithOptions(HelloWorldV2, workflow.RegisterOptions{Name: helloWorldWorkflow})
	//w.RegisterWorkflowWithOptions(HelloWorldV3, workflow.RegisterOptions{Name: helloWorldWorkflow})

	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatal(err)
	}
}

func HelloWorldV1(ctx workflow.Context) (string, error) {
	if err := workflow.Sleep(ctx, 30*time.Second); err != nil {
		return "", err
	}

	return "Hello World!", nil
}

func HelloWorldV2(ctx workflow.Context) (string, error) {
	workflow.SideEffect(ctx, func(ctx workflow.Context) interface{} {
		return nil
	})

	return HelloWorldV1(ctx)
}

func HelloWorldV3(ctx workflow.Context) (string, error) {
	if err := workflow.ExecuteLocalActivity(
		workflow.WithLocalActivityOptions(ctx, workflow.LocalActivityOptions{
			ScheduleToCloseTimeout: time.Second,
		}),
		func(ctx context.Context) error { return nil },
	).Get(ctx, nil); err != nil {
		return "", err
	}

	return HelloWorldV2(ctx)
}
