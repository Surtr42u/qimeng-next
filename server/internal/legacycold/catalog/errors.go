package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"qimeng.local/next/server/internal/service"
)

// Only package-defined codes can be emitted as source input problems.
type sourceProblem string

func (p sourceProblem) Error() string { return string(p) }

type projectionError struct {
	stage  string
	reason string
	cause  error
}

func (e *projectionError) Error() string { return "cold catalog: " + e.stage + "/" + e.reason }
func (e *projectionError) Unwrap() error { return e.cause }

// ColdDiagnostic returns fixed codes suitable for an offline coverage report.
// Neither a source ID/value nor a raw SQLite/decoder error is interpolated.
func (e *projectionError) ColdDiagnostic() (string, string) { return e.stage, e.reason }

func diagnostic(stage string, err error) error {
	var problem sourceProblem
	var contract *service.Error
	var syntax *json.SyntaxError
	var shape *json.UnmarshalTypeError
	reason := "projection_operation_failed"
	switch {
	case errors.Is(err, context.Canceled):
		reason = "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		reason = "deadline_exceeded"
	case errors.As(err, &problem):
		reason = string(problem)
	case errors.Is(err, sql.ErrNoRows):
		reason = "native_reference_missing"
	case errors.As(err, &syntax), errors.As(err, &shape):
		reason = "business_json_invalid"
	case errors.As(err, &contract):
		switch contract.Code {
		case "invalid_imported_profile", "imported_profile_identity", "invalid_imported_x_profile", "imported_avatar_binding",
			"invalid_avatar_url", "avatar_address_denied", "invalid_feed_subscription", "feed_identity_unverified", "invalid_author_source":
			reason = contract.Code
		default:
			reason = "native_contract_refused"
		}
	}
	return &projectionError{stage: stage, reason: reason, cause: err}
}
