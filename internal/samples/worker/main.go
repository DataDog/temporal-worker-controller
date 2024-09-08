// Unless explicitly stated otherwise all files in this repository are licensed under the MIT License.
//
// This product includes software developed at Datadog (https://www.datadoghq.com/). Copyright 2024 Datadog, Inc.

package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/metric"
	metricsdk "go.opentelemetry.io/otel/sdk/metric"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/contrib/datadog/tracing"
	"go.temporal.io/sdk/contrib/opentelemetry"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/worker"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

var (
	temporalHostPort  = os.Getenv("TEMPORAL_HOST_PORT")
	temporalNamespace = os.Getenv("TEMPORAL_NAMESPACE")
	temporalTaskQueue = os.Getenv("TEMPORAL_TASK_QUEUE")
	buildID           = os.Getenv("TEMPORAL_BUILD_ID")
)

func main() {
	tracer.Start(
		tracer.WithUniversalVersion(buildID),
		tracer.WithLogStartup(false),
		tracer.WithSampler(tracer.NewAllSampler()),
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
		"worker.buildID", buildID,
	)

	var metricMeter metric.Meter
	if metricExporter, err := otlpmetricgrpc.New(context.Background()); err != nil {
		l.Warn("Unable to create OTLP metric exporter", "error", err)
	} else {
		metricMeter = metricsdk.NewMeterProvider(
			metricsdk.WithReader(metricsdk.NewPeriodicReader(metricExporter,
				// Default is 1m. Set to 3s for demonstrative purposes.
				metricsdk.WithInterval(time.Second),
			)),
		).Meter("temporal_sdk")
	}

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
		MetricsHandler: opentelemetry.NewMetricsHandler(opentelemetry.MetricsHandlerOptions{
			Meter: metricMeter,
			InitialAttributes: attribute.NewSet(
				attribute.String("version", buildID),
			),
			OnError: nil,
		}),
	})
	if err != nil {
		l.Error("Unable to create Temporal client", "error", err)
		os.Exit(1)
	}

	w := worker.New(c, temporalTaskQueue, worker.Options{
		BuildID:                 buildID,
		UseBuildIDForVersioning: true,
		// Be nice to the dev server
		MaxConcurrentWorkflowTaskPollers: 2,
		MaxConcurrentActivityTaskPollers: 1,
	})
	defer w.Stop()

	// Register activities and workflows
	w.RegisterWorkflow(HelloWorld)
	w.RegisterActivity(Sleep)

	if err := w.Run(worker.InterruptCh()); err != nil {
		l.Error("Unable to start worker", "error", err)
		os.Exit(1)
	}
}
