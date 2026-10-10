// Package gatewaymux builds the grpc-gateway ServeMux: which client headers may
// cross into gRPC metadata, how the correlation ID is forwarded, and how a
// downstream error becomes an HTTP response.
package gatewaymux

import (
	"context"
	"net/http"
	"net/textproto"

	"github.com/dangngochoainam/gm-gateway/internal/middleware"
	"github.com/dangngochoainam/gopkg/commonhelper"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

// New returns the mux every downstream handler registers on.
func New() *runtime.ServeMux {
	return runtime.NewServeMux(
		runtime.WithIncomingHeaderMatcher(incomingHeaderMatcher),
		runtime.WithOutgoingHeaderMatcher(outgoingHeaderMatcher),
		runtime.WithMetadata(forwardTraceID),
		runtime.WithErrorHandler(errorHandler),
		runtime.WithRoutingErrorHandler(routingErrorHandler),
		runtime.WithForwardResponseOption(forwardLocationHeader),
	)
}

// incomingHeaderMatcher is an allowlist: a header not named here never reaches a
// downstream service. A passthrough default (including the grpc-metadata-
// prefix) would let a client inject any metadata it likes, which is the same
// spoofing hole that middleware.StripIdentityHeaders closes.
func incomingHeaderMatcher(key string) (string, bool) {
	switch textproto.CanonicalMIMEHeaderKey(key) {
	case "Authorization":
		// Forwarded as-is until the gateway verifies tokens itself. Once it
		// does, it sends the verified identity instead and stops passing the
		// raw credential downstream.
		return "authorization", true
	case "Cookie", "Content-Type", "Accept", "Accept-Language", "User-Agent":
		return runtime.DefaultHeaderMatcher(key)
	}
	return "", false
}

func outgoingHeaderMatcher(key string) (string, bool) {
	switch key {
	case "set-cookie":
		return "Set-Cookie", true
	case "location":
		return "Location", true
	}
	return runtime.DefaultHeaderMatcher(key)
}

// forwardTraceID carries the correlation ID into gRPC metadata under the key the
// services already read (grpcmiddlewarehelper.SetIDUnaryServerInterceptor), so
// one request ID spans gateway and every hop behind it.
func forwardTraceID(ctx context.Context, _ *http.Request) metadata.MD {
	id := middleware.RequestIDFromContext(ctx)
	if id == "" {
		return nil
	}
	return metadata.Pairs(string(commonhelper.TRACE_ID), id)
}

// forwardLocationHeader promotes a downstream location header to a real HTTP
// redirect.
func forwardLocationHeader(ctx context.Context, w http.ResponseWriter, _ proto.Message) error {
	md, ok := runtime.ServerMetadataFromContext(ctx)
	if !ok {
		return nil
	}
	location := md.HeaderMD.Get("location")
	if len(location) > 0 {
		w.Header().Set("Location", location[0])
		w.WriteHeader(http.StatusSeeOther)
	}
	return nil
}
