package server

import (
	"fmt"
	"net/http"
	"strings"

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
func validateStruct(w http.ResponseWriter, v interface{}) bool {
	if err := validate.Struct(v); err != nil {
		message, details := validationErrorDetails(err)
		writeEnvelopeError(w, http.StatusBadRequest, "VALIDATION_FAILED", message, details)
		return false
	}
	return true
}

// validationErrorDetails renders every failing field of a
// validator.ValidationErrors into a details map (field name -> message)
// and returns the first failure's message as the top-level message.
func validationErrorDetails(err error) (message string, details map[string]interface{}) {
	verrs, ok := err.(validator.ValidationErrors)
	if !ok || len(verrs) == 0 {
		return "request validation failed", nil
	}

	details = make(map[string]interface{}, len(verrs))
	for _, fe := range verrs {
		details[strings.ToLower(fe.Field())] = fieldErrorMessage(fe)
	}

	return fieldErrorMessage(verrs[0]), details
}

// fieldErrorMessage renders a single validator.FieldError as a concise,
// user-facing message naming the field, the failing rule, and (where
// relevant) the rule's parameter.
func fieldErrorMessage(fe validator.FieldError) string {
	field := strings.ToLower(fe.Field())
	switch fe.Tag() {
	case "required":
		return fmt.Sprintf("%s is required", field)
	case "min":
		return fmt.Sprintf("%s must be at least %s", field, fe.Param())
	case "max":
		return fmt.Sprintf("%s must be at most %s", field, fe.Param())
	case "gte":
		return fmt.Sprintf("%s must be >= %s", field, fe.Param())
	case "lte":
		return fmt.Sprintf("%s must be <= %s", field, fe.Param())
	case "gt":
		return fmt.Sprintf("%s must be > %s", field, fe.Param())
	case "lt":
		return fmt.Sprintf("%s must be < %s", field, fe.Param())
	case "oneof":
		return fmt.Sprintf("%s must be one of: %s", field, fe.Param())
	case "email":
		return fmt.Sprintf("%s must be a valid email address", field)
	case "url", "http_url", "uri":
		return fmt.Sprintf("%s must be a valid URL", field)
	case "latitude":
		return fmt.Sprintf("%s must be a valid latitude (-90 to 90)", field)
	case "longitude":
		return fmt.Sprintf("%s must be a valid longitude (-180 to 180)", field)
	case "numeric":
		return fmt.Sprintf("%s must be numeric", field)
	case "alpha":
		return fmt.Sprintf("%s must contain only letters", field)
	case "alphanum":
		return fmt.Sprintf("%s must be alphanumeric", field)
	case "hostname", "hostname_rfc1123", "fqdn":
		return fmt.Sprintf("%s must be a valid hostname", field)
	case "ip", "ip4_addr", "ip6_addr":
		return fmt.Sprintf("%s must be a valid IP address", field)
	case "len":
		return fmt.Sprintf("%s must be exactly %s characters", field, fe.Param())
	default:
		return fmt.Sprintf("%s failed validation (%s)", field, fe.Tag())
	}
}
