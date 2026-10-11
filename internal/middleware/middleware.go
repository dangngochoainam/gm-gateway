// Package middleware holds the HTTP middleware chain the gateway wraps around
// the grpc-gateway mux. Order matters; see Chain in cmd/server/main.go.
package middleware

import (
	"context"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	commondtov1 "github.com/dangngochoainam/gm-core/application/common/v1/dto"
	"github.com/dangngochoainam/gm-gateway/internal/apierror"
	"github.com/dangngochoainam/gopkg/commonhelper"
	"github.com/dangngochoainam/gopkg/loghelper"
	"github.com/google/uuid"
)

// RequestIDHeader is the correlation ID header, read from the client when
// present and echoed back on the response.
const RequestIDHeader = "X-Request-ID"

// identityHeaders are set by the gateway after authentication and are the
// reason downstream services may trust the caller's identity without verifying
// the token again. Anything a client sends under these names is forged, so it
// is dropped before the request is forwarded.
var identityHeaders = []string{
	"X-User-ID",
	"X-User-Email",
	"X-Roles",
	"X-Scopes",
	"X-Tenant-ID",
}

// identityHeaderPrefixes are dropped the same way, by prefix.
var identityHeaderPrefixes = []string{
	"X-Internal-",
}

type Middleware func(http.Handler) http.Handler

// Chain wraps h with mws, the first entry being the outermost.
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// RequestID puts a correlation ID on every request: the client's
// X-Request-ID when it sends one, a fresh UUID otherwise. It lands in the
// context under commonhelper.TRACE_ID, which is what loghelper.WithContext
// reads and what the metadata annotator forwards to the downstream service.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set(RequestIDHeader, id)
		ctx := context.WithValue(r.Context(), commonhelper.TRACE_ID, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFromContext returns the correlation ID placed by RequestID.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(commonhelper.TRACE_ID).(string)
	return id
}

// AccessLog writes one structured line per request. Bodies, tokens and cookies
// are never logged.
func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &recorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		loghelper.Logger.WithContext(r.Context()).Infow("http access",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.written,
			"latency_ms", time.Since(start).Milliseconds(),
			"remote_ip", clientIP(r),
			"user_agent", r.UserAgent(),
		)
	})
}

// Recovery turns a panic anywhere below it into a generic 500. Without it a
// panic in the middleware chain kills the whole gateway process.
func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				loghelper.Logger.WithContext(r.Context()).Errorw("panic recovered",
					"panic", rec,
					"path", r.URL.Path,
					"stack", string(debug.Stack()),
				)
				apierror.Write(w, int32(commondtov1.SystemCode_GATEWAY_INTERNAL_ERROR), apierror.Error{
					Code:      http.StatusInternalServerError,
					Status:    "INTERNAL",
					Message:   "Internal server error",
					RequestID: RequestIDFromContext(r.Context()),
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// StripIdentityHeaders removes every identity header a client may have sent, so
// the only source of identity downstream is the gateway itself.
func StripIdentityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, h := range identityHeaders {
			r.Header.Del(h)
		}
		for name := range r.Header {
			for _, prefix := range identityHeaderPrefixes {
				if strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
					r.Header.Del(name)
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// MaxBodyBytes caps the request body so an oversized upload cannot exhaust
// gateway memory. A declared Content-Length over the cap is rejected before the
// body is read; MaxBytesReader is the backstop for chunked requests, where the
// size is unknown up front and grpc-gateway reports the failure as a 400.
func MaxBodyBytes(limit int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > limit {
				apierror.Write(w, int32(commondtov1.SystemCode_REQUEST_TOO_LARGE), apierror.Error{
					Code:      http.StatusRequestEntityTooLarge,
					Status:    "REQUEST_TOO_LARGE",
					Message:   "Request body too large",
					RequestID: RequestIDFromContext(r.Context()),
				})
				return
			}
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Timeout gives every request a deadline. grpc-gateway passes the request
// context straight into the gRPC call, so this deadline reaches the downstream
// service and comes back as DeadlineExceeded (504) instead of hanging.
func Timeout(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// recorder captures the status code and size for the access log.
type recorder struct {
	http.ResponseWriter
	status  int
	written int
}

func (r *recorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.written += n
	return n, err
}

// Flush keeps server-streaming responses flowing through the recorder.
func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *recorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if i := strings.IndexByte(fwd, ','); i > 0 {
			return strings.TrimSpace(fwd[:i])
		}
		return strings.TrimSpace(fwd)
	}
	return r.RemoteAddr
}
