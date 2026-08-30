package di

import (
	"context"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace"

	"github.com/NdoleStudio/stacktrace"
	"github.com/hirosassa/zerodriver"
)

// CUSTOM FILE — not upstream. Keep all local telemetry changes here so that
// pulling a new httpSMS release touches container.go in one place only: the body
// of InitializeTraceProvider, which delegates to initializeTraceProviderCustom
// below.
//
// WHY THIS EXISTS
//
// Upstream's InitializeTraceProvider calls initializeAxiomTraceProvider
// unconditionally. That builds an OTLP exporter with the header
// "Authorization: Bearer " + AXIOM_TOKEN whether or not the token is set, so a
// self-hosted deployment with no Axiom account emits
//
//	traces export: failed to send to https://us-east-1.aws.edge.axiom.co/v1/traces:
//	401 Unauthorized (body: {"code":401,"message":"auth token not provided"})
//
// on every batch, roughly every five seconds. Nothing breaks, but it churns the
// container's json-file log (max-size 10m, max-file 3) and rotates away the
// startup errors that are actually worth keeping.
//
// Note this is NOT reachable through OTEL_SDK_DISABLED: that variable is only
// honoured by the SDK's autoconfiguration package, and the providers here are
// constructed by hand.

// logDriverCustom picks the LOG driver. Called from logDriver in container.go.
//
// THIS IS WHAT STOPPED PRODUCTION BOOTING, and it is a different code path from
// the trace provider below — patching one does not fix the other.
//
// Upstream:
//
//	if isLocal() { return consoleLogger(...) }
//	return axiomLogger(...)          // log.Fatal on error
//
// isLocal() is `ENV == "local"`, so dev took the console branch and looked fine
// while every non-local deployment went to axiomLogger — which calls log.Fatal
// with "cannot create axiom zerolog writer / Caused by: missing token" when
// AXIOM_TOKEN is unset. log.Fatal is os.Exit(1): the container died at boot, the
// deploy's /health gate waited out its full 60 retries, and the only clue was a
// message that reads like a warning.
//
// Falling back to the console driver is also simply correct for a container:
// stdout/stderr is where docker collects logs from.
func logDriverCustom(skipFrameCount int) *zerodriver.Logger {
	if isLocal() || os.Getenv("AXIOM_TOKEN") == "" {
		// The LOG LEVEL is deliberately NOT set here. telemetry.NewZerologLogger
		// calls SetGlobalLevel on every construction and would overwrite anything
		// chosen at this point — which is what made an earlier version of this
		// fix look correct while changing nothing. The single source of truth is
		// telemetry.GlobalLevelCustom, called from there.
		return consoleLogger(skipFrameCount)
	}
	return axiomLogger(skipFrameCount)
}

// initializeTraceProviderCustom picks a telemetry backend from the environment.
//
// The selection is by CREDENTIAL, not by a mode flag: a backend is used when its
// token is present, which means an unconfigured deployment silently does the
// right thing instead of needing a switch set correctly.
func (container *Container) initializeTraceProviderCustom(version string, namespace string) func() {
	switch {
	case os.Getenv("AXIOM_TOKEN") != "":
		return container.initializeAxiomTraceProvider(version, namespace)

	case os.Getenv("POSTHOG_API_KEY") != "":
		return container.initializePosthogTraceProvider(version, namespace)

	default:
		container.logger.Info("no telemetry backend configured (AXIOM_TOKEN and POSTHOG_API_KEY are both empty); traces and metrics are disabled")
		return container.initializeNoopTraceProvider(version, namespace)
	}
}

// initializeNoopTraceProvider installs a TracerProvider with NO span processor.
//
// Spans are still created, so every tracer.Start call site keeps working and
// nothing has to become conditional — they are simply never exported, and no
// network call is ever attempted. This is deliberately the SDK provider rather
// than trace/noop: the SDK one still carries the resource attributes, so if a
// backend is configured later the span shape does not change.
func (container *Container) initializeNoopTraceProvider(version string, namespace string) func() {
	tp := trace.NewTracerProvider(
		trace.WithResource(container.OtelResources(version, namespace)),
	)
	otel.SetTracerProvider(tp)

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			container.logger.Error(stacktrace.Propagatef(err, "cannot shutdown noop trace provider"))
		}
	}
}

// initializePosthogTraceProvider is a PLACEHOLDER — it does not export anything
// yet, and says so rather than pretending to work.
//
// It exists so that turning PostHog on later is a change in ONE function with a
// wiring point that already resolves, instead of an edit to upstream's
// container.go. To implement it:
//
//  1. Set POSTHOG_HOST to the OTLP ingest endpoint, HOST ONLY, no scheme and no
//     /v1/traces suffix — otlptracehttp.WithEndpoint expects "host:port", and
//     passing a URL is the usual reason a first attempt 404s. EU projects and
//     US projects have different hosts.
//  2. Swap the body for an otlptracehttp/otlpmetrichttp pair modelled on
//     initializeAxiomTraceProvider, with the project API key as the auth header
//     PostHog documents at the time.
//  3. Do NOT copy Axiom's Fatal-on-error handling. Telemetry is optional here;
//     a misconfigured observability backend must never stop the API from
//     booting. Log and fall through to noop instead.
//
// Until then any POSTHOG_API_KEY value degrades to the no-op provider, so
// setting it early is harmless.
func (container *Container) initializePosthogTraceProvider(version string, namespace string) func() {
	container.logger.Warn(stacktrace.NewError("POSTHOG_API_KEY is set but the PostHog exporter is not implemented; falling back to no-op telemetry. See initializePosthogTraceProvider in container_telemetry_custom.go"))
	return container.initializeNoopTraceProvider(version, namespace)
}
