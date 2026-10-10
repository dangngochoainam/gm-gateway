package gatewaymux

import (
	"context"
	"net/http"
	"strings"

	"github.com/dangngochoainam/gm-gateway/internal/apierror"
	"github.com/dangngochoainam/gm-gateway/internal/middleware"
	"github.com/dangngochoainam/gopkg/loghelper"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// mapping is how one gRPC code is published to a client. An empty message means
// the downstream message is passed through, which is only done for codes the
// caller itself caused (validation, not found, permission). Server-side codes
// get a fixed message so stack traces, SQL and internal host names stay in the
// log.
type mapping struct {
	httpStatus int
	code       string
	message    string
}

// statusClientClosedRequest is nginx's code for a caller that hung up. There is
// no RFC status for it and nobody is left to read the response anyway.
const statusClientClosedRequest = 499

var mappings = map[codes.Code]mapping{
	codes.OK:                 {http.StatusOK, "OK", ""},
	codes.Canceled:           {statusClientClosedRequest, "CANCELED", "Request canceled by the client"},
	codes.InvalidArgument:    {http.StatusBadRequest, "INVALID_ARGUMENT", ""},
	codes.NotFound:           {http.StatusNotFound, "NOT_FOUND", ""},
	codes.AlreadyExists:      {http.StatusConflict, "ALREADY_EXISTS", ""},
	codes.PermissionDenied:   {http.StatusForbidden, "PERMISSION_DENIED", ""},
	codes.Unauthenticated:    {http.StatusUnauthorized, "UNAUTHENTICATED", ""},
	codes.ResourceExhausted:  {http.StatusTooManyRequests, "RESOURCE_EXHAUSTED", ""},
	codes.FailedPrecondition: {http.StatusBadRequest, "FAILED_PRECONDITION", ""},
	codes.Aborted:            {http.StatusConflict, "ABORTED", ""},
	codes.OutOfRange:         {http.StatusBadRequest, "OUT_OF_RANGE", ""},
	codes.DeadlineExceeded:   {http.StatusGatewayTimeout, "DEADLINE_EXCEEDED", "Upstream timeout"},
	codes.Unimplemented:      {http.StatusNotImplemented, "UNIMPLEMENTED", "Not implemented"},
	codes.Unavailable:        {http.StatusServiceUnavailable, "UNAVAILABLE", "Service temporarily unavailable"},
	codes.Internal:           {http.StatusInternalServerError, "INTERNAL", "Internal server error"},
	codes.DataLoss:           {http.StatusInternalServerError, "INTERNAL", "Internal server error"},
	codes.Unknown:            {http.StatusInternalServerError, "INTERNAL", "Internal server error"},
}

var fallbackMapping = mapping{http.StatusInternalServerError, "INTERNAL", "Internal server error"}

// errorHandler replaces the grpc-gateway default, which returns the downstream
// error message verbatim and so leaks internal detail to clients.
func errorHandler(ctx context.Context, _ *runtime.ServeMux, _ runtime.Marshaler, w http.ResponseWriter, r *http.Request, err error) {
	st := status.Convert(err)
	m, ok := mappings[st.Code()]
	if !ok {
		m = fallbackMapping
	}

	message := m.message
	if message == "" {
		message = st.Message()
	}

	logger := loghelper.Logger.WithContext(ctx).With(
		"path", r.URL.Path,
		"grpc_code", st.Code().String(),
		"http_status", m.httpStatus,
		"upstream_error", st.Message(),
	)
	if m.httpStatus >= http.StatusInternalServerError {
		logger.Error("upstream call failed")
	} else {
		logger.Warn("upstream call rejected")
	}

	if m.httpStatus == statusClientClosedRequest {
		// The connection is already gone; writing a body only logs noise.
		w.WriteHeader(statusClientClosedRequest)
		return
	}

	apierror.Write(w, m.httpStatus, apierror.Body{
		Code:      m.code,
		Message:   message,
		RequestID: middleware.RequestIDFromContext(ctx),
	})
}

// routingErrorHandler answers requests that match no route. The grpc-gateway
// default turns 405 into Unimplemented, which this mux would then publish as
// 501; keeping the routing status is more honest.
func routingErrorHandler(ctx context.Context, _ *runtime.ServeMux, _ runtime.Marshaler, w http.ResponseWriter, r *http.Request, httpStatus int) {
	loghelper.Logger.WithContext(ctx).Infow("no route matched",
		"method", r.Method,
		"path", r.URL.Path,
		"http_status", httpStatus,
	)

	apierror.Write(w, httpStatus, apierror.Body{
		Code:      strings.ToUpper(strings.ReplaceAll(http.StatusText(httpStatus), " ", "_")),
		Message:   http.StatusText(httpStatus),
		RequestID: middleware.RequestIDFromContext(ctx),
	})
}
