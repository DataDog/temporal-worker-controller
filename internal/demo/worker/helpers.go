package main

import (
	"log/slog"
	"os"
	"time"

	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/uber-go/tally/v4"
	"github.com/uber-go/tally/v4/prometheus"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/contrib/datadog/tracing"
	sdktally "go.temporal.io/sdk/contrib/tally"
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
	promScope, err := newPrometheusScope(l, prometheus.Configuration{
		ListenAddress: "0.0.0.0:9090",
		HandlerPath:   "/metrics",
		TimerType:     "histogram",
	})
	if err != nil {
		return nil, err
	}

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
		MetricsHandler: sdktally.NewMetricsHandler(promScope),
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

func newPrometheusScope(l log.Logger, c prometheus.Configuration) (tally.Scope, error) {
	reporter, err := c.NewReporter(
		prometheus.ConfigurationOptions{
			Registry: prom.NewRegistry(),
			OnError: func(err error) {
				l.Error("Error in prometheus reporter", "error", err)
			},
		},
	)
	if err != nil {
		return nil, err
	}
	scopeOpts := tally.ScopeOptions{
		CachedReporter:  reporter,
		Separator:       prometheus.DefaultSeparator,
		SanitizeOptions: &sdktally.PrometheusSanitizeOptions,
		Prefix:          "temporal_samples",
	}
	scope, _ := tally.NewRootScope(scopeOpts, time.Second)
	scope = sdktally.NewPrometheusNamingScope(scope)

	return scope, nil
}
