// Unless explicitly stated otherwise all files in this repository are licensed under the MIT License.
//
// This product includes software developed at Datadog (https://www.datadoghq.com/). Copyright 2024 Datadog, Inc.

package main

import (
	"log/slog"
	"os"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/contrib/datadog/tracing"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

var (
	temporalHostPort  = os.Getenv("TEMPORAL_HOST_PORT")
	temporalNamespace = os.Getenv("TEMPORAL_NAMESPACE")
	temporalTaskQueue = os.Getenv("TEMPORAL_TASK_QUEUE")
	workerBuildID     = os.Getenv("TEMPORAL_BUILD_ID")
)

func main() {
	tracer.Start(
		tracer.WithUniversalVersion(workerBuildID),
		tracer.WithLogStartup(false),
	)
	defer tracer.Stop()

	l := log.NewStructuredLogger(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource:   true,
		Level:       slog.LevelDebug,
		ReplaceAttr: nil,
	})))

	l.Info("Worker config",
		"temporal.hostport", temporalHostPort,
		"temporal.namespace", temporalNamespace,
		"temporal.taskqueue", temporalTaskQueue,
		"worker.buildID", workerBuildID,
		"git.commit", os.Getenv("DD_GIT_COMMIT_SHA"),
		"git.repo", os.Getenv("DD_GIT_REPOSITORY_URL"),
	)

	c, err := client.Dial(client.Options{
		HostPort:  temporalHostPort,
		Namespace: temporalNamespace,
		Logger:    l,
		Interceptors: []interceptor.ClientInterceptor{
			tracing.NewTracingInterceptor(tracing.TracerOptions{
				DisableSignalTracing: false,
				DisableQueryTracing:  false,
			}),
		},
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
