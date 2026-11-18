package httpapi

import (
	"errors"
	"net/http/httptest"
	"testing"

	"qimeng.local/next/server/internal/service"
)

func TestJSONReadRequiresRegisteredOperationMetadata(t *testing.T) {
	r := httptest.NewRequest("POST", "/test-only-unregistered", nil)
	var problem *service.Error
	if err := validateJSONRequest(r, []byte(`{}`)); !errors.As(err, &problem) || problem.Status != 500 || problem.Code != "request_contract_missing" {
		t.Fatal("missing operation metadata bypassed validation", err)
	}
}
