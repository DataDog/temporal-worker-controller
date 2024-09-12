package main

import (
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/otel/exporters/metric/prometheus"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/contrib/datadog/tracing"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/workflow"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

func setActivityTimeout(ctx workflow.Context, d time.Duration) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		ScheduleToCloseTimeout: d,
	})
}

func newClient(l log.Logger) (client.Client, error) {
	//var metricMeter metric.Meter
	//if metricExporter, err := otlpmetricgrpc.New(context.Background()); err != nil {
	//	l.Warn("Unable to create OTLP metric exporter", "error", err)
	//} else {
	//	metricMeter = metricsdk.NewMeterProvider(
	//		metricsdk.WithReader(metricsdk.NewPeriodicReader(metricExporter,
	//			// Default is 1m. Set to 3s for demonstrative purposes.
	//			metricsdk.WithInterval(time.Second),
	//		)),
	//	).Meter("temporal_sdk")
	//}

	prometheus.New(prometheus.Config{}, nil)

	return client.Dial(client.Options{
		HostPort:  temporalHostPort,
		Namespace: temporalNamespace,
		Logger:    l,
		Interceptors: []interceptor.ClientInterceptor{
			tracing.NewTracingInterceptor(tracing.TracerOptions{
				DisableSignalTracing: false,
				DisableQueryTracing:  false,
			}),
		},
		//MetricsHandler: opentelemetry.NewMetricsHandler(opentelemetry.MetricsHandlerOptions{
		//	Meter: metricMeter,
		//	InitialAttributes: attribute.NewSet(
		//		attribute.String("version", buildID),
		//	),
		//	OnError: nil,
		//}),
	})
}

func newLoggerAndTracer() (l log.Logger, stopTracerFunc func()) {
	tracer.Start(
		tracer.WithUniversalVersion(buildID),
		tracer.WithLogStartup(false),
		tracer.WithSampler(tracer.NewAllSampler()),
	)
	l = log.NewStructuredLogger(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource:   true,
		Level:       slog.LevelDebug,
		ReplaceAttr: nil,
	})))
	return l, tracer.Stop
}
