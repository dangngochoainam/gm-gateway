package gatewaymux

import (
	"context"

	commondtov1 "github.com/dangngochoainam/gm-core/application/common/v1/dto"
	"google.golang.org/protobuf/proto"
)

// wrapSuccess is the mux's ForwardResponseRewriter: every successful unary
// response reaches the client as {"isSuccess": true, "systemCode": 1, "data": resp}.
// It must return a map, not a struct: JSONPb marshals each map value through
// protojson (camelCase names), but a struct goes through encoding/json.
func wrapSuccess(_ context.Context, resp proto.Message) (any, error) {
	return map[string]any{
		"isSuccess":  true,
		"systemCode": int32(commondtov1.SystemCode_SUCCESS),
		"data":       resp,
	}, nil
}
