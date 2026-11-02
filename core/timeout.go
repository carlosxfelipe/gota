package core

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// TimeoutOptions configures the Timeout middleware.
type TimeoutOptions struct {
	// Message is the error message of the 503 response (default: "request timeout").
	Message string
}

// Timeout returns a middleware that bounds the time a request may take by attaching a deadline
// to Request.Context(). It panics if d is not positive.
//
// The timeout is cooperative: Go cannot kill a running handler, so the handler (and everything it
// calls: database queries, outbound HTTP calls, ...) must honor ctx.Request.Context() for the
// work to actually stop. If the deadline expired and the handler returned an error without having
// written a response, the client receives 503 Service Unavailable with an ErrorResponse body.
//
// Do not apply it to streaming routes (SSE / StreamFunc): the deadline would end the stream.
// Config.WriteTimeout/ReadTimeout still protect the connection itself.
func Timeout(d time.Duration, opts ...TimeoutOptions) MiddlewareFunc {
	if d <= 0 {
		panic("gota: Timeout duration must be positive")
	}
	message := "request timeout"
	if len(opts) > 0 && opts[0].Message != "" {
		message = opts[0].Message
	}

	return func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) error {
			parent := ctx.Request
			tctx, cancel := context.WithTimeout(parent.Context(), d)
			defer cancel()
			ctx.Request = parent.WithContext(tctx)
			defer func() { ctx.Request = parent }()

			err := next(ctx)
			if err != nil && !ctx.Written() &&
				(errors.Is(err, context.DeadlineExceeded) || errors.Is(tctx.Err(), context.DeadlineExceeded)) {
				return ctx.Status(http.StatusServiceUnavailable).Json(ErrorResponse{
					Error:     message,
					Status:    http.StatusServiceUnavailable,
					RequestID: ctx.RequestID(),
				})
			}
			return err
		}
	}
}
