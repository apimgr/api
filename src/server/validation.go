package server

import (
	"net/http"
	"strings"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/go-playground/validator/v10"
)

// validate is the shared go-playground/validator/v10 instance used to
// enforce struct-tag validation on every handler's input, per AI.md
// PART 3's "Required Go Libraries" table ("Validation":
// github.com/go-playground/validator/v10).
var validate = validator.New()

// validateStruct runs the shared validator against v and, on failure,
// writes a canonical VALIDATION_FAILED envelope response (PART 9 error
// code table: VALIDATION_FAILED -> 400) and returns false. Callers must
// return immediately when validateStruct returns false.
func validateStruct(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	if err := validate.Struct(v); err != nil {
		message, details := validationErrorDetails(err, i18n.LangFromRequest(r))
		writeEnvelopeError(w, http.StatusBadRequest, "VALIDATION_FAILED", message, details)
		return false
	}
	return true
}

// validationErrorDetails renders every failing field of a
// validator.ValidationErrors into a details map (field name -> message)
// and returns the first failure's message as the top-level message.
func validationErrorDetails(err error, lang string) (message string, details map[string]interface{}) {
	verrs, ok := err.(validator.ValidationErrors)
	if !ok || len(verrs) == 0 {
		return i18n.Translate(lang, "errors.bad_request"), nil
	}

	details = make(map[string]interface{}, len(verrs))
	for _, fe := range verrs {
		details[strings.ToLower(fe.Field())] = fieldErrorMessage(fe, lang)
	}

	return fieldErrorMessage(verrs[0], lang), details
}

// fieldErrorMessage renders a single validator.FieldError as a concise,
// user-facing message naming the field, the failing rule, and (where
// relevant) the rule's parameter.
func fieldErrorMessage(fe validator.FieldError, lang string) string {
	field := strings.ToLower(fe.Field())
	key := "errors.validation.invalid"
	args := map[string]string{"field": field, "param": fe.Param()}
	switch fe.Tag() {
	case "required":
		key = "errors.validation.required"
	case "min":
		key = "errors.validation.min"
	case "max":
		key = "errors.validation.max"
	case "gte":
		key = "errors.validation.gte"
	case "lte":
		key = "errors.validation.lte"
	case "gt":
		key = "errors.validation.gt"
	case "lt":
		key = "errors.validation.lt"
	case "oneof":
		key = "errors.validation.oneof"
	case "email":
		key = "errors.validation.email"
	case "url", "http_url", "uri":
		key = "errors.validation.url"
	case "latitude":
		key = "errors.validation.latitude"
	case "longitude":
		key = "errors.validation.longitude"
	case "numeric":
		key = "errors.validation.numeric"
	case "alpha":
		key = "errors.validation.alpha"
	case "alphanum":
		key = "errors.validation.alphanum"
	case "hostname", "hostname_rfc1123", "fqdn":
		key = "errors.validation.hostname"
	case "ip", "ip4_addr", "ip6_addr":
		key = "errors.validation.ip"
	case "len":
		key = "errors.validation.len"
	default:
		args["rule"] = fe.Tag()
	}
	return i18n.TranslateFormat(lang, key, args)
}
