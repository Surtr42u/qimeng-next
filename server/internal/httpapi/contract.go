package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/transport/generated"
)

// The generated router registers every method and path. Authentication wraps
// each protected operation before parameter binding, including rejected input.
type authenticatedRouter struct {
	*http.ServeMux
	api *API
}

func (router authenticatedRouter) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	if !generated.HTTPRequestRules[pattern].Public {
		handler = router.api.authScope("", handler)
	}
	router.ServeMux.HandleFunc(pattern, handler)
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
	rule := generated.HTTPRequestRules[r.Pattern]
	if len(raw) == 0 {
		if rule.BodyRequired {
			return service.Fail(400, "invalid_json", "请求必须包含 JSON 正文。")
		}
		return nil
	}
	if !rule.Object {
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return service.Fail(400, "invalid_json", "请求必须是有效的 JSON 对象。")
	}
	for _, name := range rule.Required {
		value, present := fields[name]
		if !present || bytes.Equal(bytes.TrimSpace(value), []byte("null")) && !slices.Contains(rule.Nullable, name) {
			return service.Fail(400, "invalid_json", "请求缺少必填字段或包含无效的空值。")
		}
	}
	if rule.Allowed != nil {
		for name := range fields {
			if !slices.Contains(rule.Allowed, name) {
				return service.Fail(400, "invalid_json", "请求包含未声明的字段。")
			}
		}
	}
	return nil
}
