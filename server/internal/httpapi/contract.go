package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"

	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/transport/generated"
)

// The generated router registers every method and path. Authentication wraps
// each protected operation before parameter binding, including rejected input.
type authenticatedRouter struct {
	*http.ServeMux
	api        *API
	contract   *requestContract
	registered int
}

type requestRuleKey struct{}

func (router *authenticatedRouter) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	rule, ok := router.contract.operations[pattern]
	if !ok {
		panic("generated route has no OpenAPI operation: " + pattern)
	}
	router.registered++
	bound := func(w http.ResponseWriter, r *http.Request) {
		handler(w, r.WithContext(context.WithValue(r.Context(), requestRuleKey{}, rule)))
	}
	if !rule.public {
		bound = router.api.authScope("", bound)
	}
	router.ServeMux.HandleFunc(pattern, bound)
}

type requestContract struct {
	baseURL    string
	operations map[string]*requestRule
}

type requestRule struct {
	public   bool
	required bool
	json     *openapi3.Schema
}

var embeddedContract = sync.OnceValue(func() *requestContract {
	spec, err := generated.GetSwagger()
	if err != nil || spec.Paths == nil || len(spec.Servers) != 1 {
		panic("cannot load the generated OpenAPI contract")
	}
	contract := &requestContract{baseURL: spec.Servers[0].URL, operations: map[string]*requestRule{}}
	shapes := requestShapes{}
	for path, item := range spec.Paths.Map() {
		for method, operation := range item.Operations() {
			policy := spec.Security
			if operation.Security != nil {
				policy = *operation.Security
			}
			rule := &requestRule{public: len(policy) == 0 || slices.ContainsFunc(policy, func(requirement openapi3.SecurityRequirement) bool { return len(requirement) == 0 })}
			if body := operation.RequestBody; body != nil {
				if body.Value == nil {
					panic("unresolved OpenAPI request body: " + operation.OperationID)
				}
				if media, ok := body.Value.Content["application/json"]; ok {
					if media == nil || media.Schema == nil {
						panic("missing OpenAPI JSON schema: " + operation.OperationID)
					}
					rule.required = body.Value.Required
					rule.json = shapes.ref(media.Schema).Value
				}
			}
			contract.operations[method+" "+contract.baseURL+path] = rule
		}
	}
	return contract
})

// Preserve JSON shape at any depth while leaving business limits and their
// established responses to the service. Exclusive and alternative branches
// keep their original predicates so overlapping branches are never flattened.
type requestShapes map[*openapi3.Schema]*openapi3.Schema

func (shapes requestShapes) ref(ref *openapi3.SchemaRef) *openapi3.SchemaRef {
	if ref == nil {
		return nil
	}
	src := ref.Value
	if src == nil {
		panic("unresolved OpenAPI request schema")
	}
	if existing := shapes[src]; existing != nil {
		return &openapi3.SchemaRef{Value: existing}
	}
	shape := &openapi3.Schema{Type: src.Type, Nullable: src.Nullable, Required: src.Required, ReadOnly: src.ReadOnly,
		WriteOnly: src.WriteOnly, AnyOf: src.AnyOf, OneOf: src.OneOf, Not: src.Not, Discriminator: src.Discriminator}
	shapes[src] = shape
	if len(src.Properties) > 0 {
		shape.Properties = openapi3.Schemas{}
		for name, property := range src.Properties {
			shape.Properties[name] = shapes.ref(property)
		}
	}
	shape.Items = shapes.ref(src.Items)
	shape.AdditionalProperties = openapi3.AdditionalProperties{Has: src.AdditionalProperties.Has, Schema: shapes.ref(src.AdditionalProperties.Schema)}
	for _, member := range src.AllOf {
		shape.AllOf = append(shape.AllOf, shapes.ref(member))
	}
	return &openapi3.SchemaRef{Value: shape}
}

func allowScope(w http.ResponseWriter, r *http.Request, scope string) bool {
	if scope != "" && !principal(r).Has(scope) {
		fail(w, service.Fail(403, "scope_denied", "凭据没有此操作权限。"))
		return false
	}
	return true
}

func parameterError(w http.ResponseWriter, _ *http.Request, err error) {
	var header *generated.RequiredHeaderError
	var invalid *generated.InvalidParamFormatError
	var required *generated.RequiredParamError
	var repeated *generated.TooManyValuesForParamError
	var name string
	switch {
	case errors.As(err, &header):
		name = header.ParamName
	case errors.As(err, &invalid):
		name = invalid.ParamName
	case errors.As(err, &required):
		name = required.ParamName
	case errors.As(err, &repeated):
		name = repeated.ParamName
	}
	code := "invalid_parameter"
	switch name {
	case "Idempotency-Key":
		code = "idempotency_required"
	case "Upload-Offset":
		code = "offset_required"
	case "expectedRevision":
		code = "revision_required"
	case "version":
		code = "invalid_version"
	case "limit":
		code = "invalid_limit"
	}
	fail(w, service.Fail(400, code, "请求参数缺失或格式无效。"))
}

// Go's zero values otherwise conflate an absent or null property with an
// explicit false or zero. Binary and multipart handlers own their streams.
func validateJSONRequest(r *http.Request, raw []byte) error {
	rule, ok := r.Context().Value(requestRuleKey{}).(*requestRule)
	if !ok || rule == nil {
		return service.Fail(500, "request_contract_missing", "请求缺少协议绑定。")
	}
	if len(raw) == 0 {
		if rule.required {
			return service.Fail(400, "invalid_json", "请求必须包含 JSON 正文。")
		}
		return nil
	}
	if rule.json == nil {
		return nil
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil || rule.json.VisitJSON(value, openapi3.VisitAsRequest(), openapi3.FailFast()) != nil {
		return service.Fail(400, "invalid_json", "请求 JSON 的结构、必填字段或空值不符合协议。")
	}
	return nil
}
