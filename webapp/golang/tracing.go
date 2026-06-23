package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"

	// pprof registers its handlers on http.DefaultServeMux via init().
	_ "net/http/pprof"
	"runtime/pprof"

	"github.com/XSAM/otelsql"
	"github.com/jmoiron/sqlx"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// tracingEnabled reports whether OpenTelemetry tracing should be active.
// Tracing adds overhead, so it is OFF by default and only enabled when
// ENABLE_TRACING=1 — turn it on while investigating, off for scoring runs.
func tracingEnabled() bool {
	return os.Getenv("ENABLE_TRACING") == "1"
}

// startPprof starts a localhost-only pprof/debug HTTP server on :6060.
// It is always on (negligible overhead) and not reachable from outside the host.
//
//	go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30
//	go tool pprof http://localhost:6060/debug/pprof/heap
func startPprof() {
	go func() {
		log.Println("pprof listening on localhost:6060")
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()
}

// initTracer wires up the OTLP exporter -> Jaeger (localhost:4318 by default,
// override with OTEL_EXPORTER_OTLP_ENDPOINT). Returns a shutdown func.
// Only call when tracingEnabled().
func initTracer(ctx context.Context) (func(context.Context) error, error) {
	exp, err := otlptracehttp.New(ctx, otlptracehttp.WithInsecure())
	if err != nil {
		return nil, err
	}

	res, err := resource.New(ctx, resource.WithAttributes(
		semconv.ServiceName("private-isu-go"),
	))
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

// openDB opens the MySQL connection. When tracing is enabled it wraps the
// driver with otelsql so every query becomes a span nested under its request.
// When disabled it opens a plain sqlx connection (zero tracing overhead).
func openDB(dsn string) (*sqlx.DB, error) {
	if tracingEnabled() {
		sqlDB, err := otelsql.Open("mysql", dsn, otelsql.WithAttributes(semconv.DBSystemMySQL))
		if err != nil {
			return nil, err
		}
		return sqlx.NewDb(sqlDB, "mysql"), nil
	}
	return sqlx.Open("mysql", dsn)
}

// pprofLabelMiddleware tags each request's CPU/goroutine samples with its route
// pattern so pprof can be sliced per endpoint:
//
//	go tool pprof -tagfocus='endpoint:GET /' app cpu.pprof
//	(or the `tags` command inside go tool pprof)
func normalizeEndpoint(p string) string {
	switch {
	case p == "/":
		return "/"
	case strings.HasPrefix(p, "/posts/"):
		return "/posts/:id"
	case strings.HasPrefix(p, "/image/"):
		return "/image/:id"
	case strings.HasPrefix(p, "/@"):
		return "/@:name"
	default:
		return p
	}
}

func pprofLabelMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ep := r.Method + " " + normalizeEndpoint(r.URL.Path)
		pprof.Do(r.Context(), pprof.Labels("endpoint", ep), func(ctx context.Context) {
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
}

// wrapHandler adds the otelhttp middleware when tracing is enabled.
func wrapHandler(h http.Handler) http.Handler {
	if tracingEnabled() {
		return otelhttp.NewHandler(h, "http")
	}
	return h
}
