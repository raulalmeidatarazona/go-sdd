// Package cqrs defines the single shape every use case follows: a Handler that
// takes one input and returns one output. Commands change state and return
// identifiers; queries read projections and never change state.
package cqrs

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"

	"github.com/raulalmeidatarazona/go-sdd/internal/platform/apperr"
)

// Handler is a command or query handler.
type Handler[In, Out any] interface {
	Handle(ctx context.Context, in In) (Out, error)
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc[In, Out any] func(ctx context.Context, in In) (Out, error)

func (f HandlerFunc[In, Out]) Handle(ctx context.Context, in In) (Out, error) { return f(ctx, in) }

// Kind distinguishes commands from queries in telemetry.
type Kind string

const (
	KindCommand Kind = "command"
	KindQuery   Kind = "query"
)

var (
	tracer   = otel.Tracer("github.com/raulalmeidatarazona/go-sdd/internal/platform/cqrs")
	meter    = otel.Meter("github.com/raulalmeidatarazona/go-sdd/internal/platform/cqrs")
	duration metric.Float64Histogram
)

func init() {
	var err error
	duration, err = meter.Float64Histogram(
		"oms.cqrs.handler.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of command and query handlers"),
	)
	if err != nil {
		otel.Handle(err)
	}
}

// Observe wraps a handler with a span, a duration metric and a log line.
// Every handler is registered through it, so all use cases share the same telemetry.
func Observe[In, Out any](kind Kind, name string, logger *slog.Logger, next Handler[In, Out]) Handler[In, Out] {
	return HandlerFunc[In, Out](func(ctx context.Context, in In) (Out, error) {
		ctx, span := tracer.Start(ctx, string(kind)+" "+name)
		defer span.End()
		start := time.Now()

		out, err := next.Handle(ctx, in)

		outcome := "ok"
		level := slog.LevelDebug
		if err != nil {
			outcome = outcomeOf(err)
			if outcome == "internal_error" {
				level = slog.LevelError
				span.SetStatus(codes.Error, err.Error())
			} else {
				level = slog.LevelInfo
			}
			span.RecordError(err)
		}
		attrs := attribute.NewSet(
			attribute.String("handler", name),
			attribute.String("kind", string(kind)),
			attribute.String("outcome", outcome),
		)
		duration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributeSet(attrs))
		logger.Log(ctx, level, "handled "+string(kind),
			slog.String("handler", name),
			slog.String("outcome", outcome),
			slog.Duration("duration", time.Since(start)),
			slog.Any("error", err),
		)
		return out, err
	})
}

func outcomeOf(err error) string {
	switch apperr.KindOf(err) {
	case apperr.KindInternal:
		return "internal_error"
	default:
		return "rejected"
	}
}
