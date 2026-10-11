package gatewaymux

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	productdtov1 "github.com/dangngochoainam/gm-core/application/product/v1/dto"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
)

func TestWrapSuccess(t *testing.T) {
	mux := New()
	req := httptest.NewRequest(http.MethodGet, "/v1/products", nil)
	rec := httptest.NewRecorder()
	ctx := runtime.NewServerMetadataContext(context.Background(), runtime.ServerMetadata{})
	_, marshaler := runtime.MarshalerForRequest(mux, req)

	runtime.ForwardResponseMessage(ctx, mux, marshaler, rec, req, &productdtov1.ListProductsResponse{
		Products:   []*productdtov1.ProductDetail{{Id: "p1", Name: "n", Code: "c"}},
		Pagination: &productdtov1.KeysetPagination{HasNext: true},
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		IsSuccess  *bool  `json:"isSuccess"`
		SystemCode *int32 `json:"systemCode"`
		Data       struct {
			Products   []map[string]any `json:"products"`
			Pagination map[string]any   `json:"pagination"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, rec.Body.String())
	}
	if body.IsSuccess == nil || !*body.IsSuccess {
		t.Errorf("isSuccess = %v, want true (%s)", body.IsSuccess, rec.Body.String())
	}
	if body.SystemCode == nil || *body.SystemCode != 1 {
		t.Errorf("systemCode = %v, want 1 (%s)", body.SystemCode, rec.Body.String())
	}
	if len(body.Data.Products) != 1 || body.Data.Products[0]["id"] != "p1" {
		t.Fatalf("data.products = %v, want one product p1", body.Data.Products)
	}
	if _, ok := body.Data.Products[0]["lastModifiedAt"]; !ok {
		t.Errorf("data.products[0] has no lastModifiedAt; data must be marshalled by protojson: %v", body.Data.Products[0])
	}
	if body.Data.Pagination["hasNext"] != true {
		t.Errorf("data.pagination.hasNext = %v, want true", body.Data.Pagination["hasNext"])
	}
}
