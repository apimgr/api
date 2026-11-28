package server

import (
	"net/http"
	"reflect"

	"github.com/apimgr/api/src/common/sanitize"
	"github.com/apimgr/api/src/mode"
)

type taggedResponseObject = sanitize.TaggedObject

// writeTaggedJSON applies the PART 11 response field allow-list before
// encoding. The outer output middleware then performs its body-level stages.
func writeTaggedJSON(w http.ResponseWriter, value interface{}) error {
	return sanitize.WriteTaggedJSON(w, value, mode.IsAppModeProd())
}

func responseObject(fields ...sanitize.TaggedField) taggedResponseObject {
	return taggedResponseObject(fields)
}

func responseField(name string, value interface{}) sanitize.TaggedField {
	if value != nil {
		if _, ok := value.(sanitize.PublicResponseValue); !ok {
			if _, ok := value.(sanitize.TaggedObject); ok {
				return sanitize.TaggedField{Name: name, Value: value}
			}
			kind := reflect.TypeOf(value).Kind()
			if kind == reflect.Map || kind == reflect.Slice || kind == reflect.Array {
				if kind != reflect.Slice || reflect.TypeOf(value).Elem().Kind() != reflect.Uint8 {
					value = sanitize.OpaqueUserJSON{Value: value}
				}
			}
		}
	}
	return sanitize.TaggedField{Name: name, Value: value}
}

func responseFieldsFromMap(values map[string]interface{}, names ...string) taggedResponseObject {
	fields := make(taggedResponseObject, 0, len(names))
	for _, name := range names {
		if value, ok := values[name]; ok {
			fields = append(fields, responseField(name, value))
		}
	}
	return fields
}

func responseFieldsFromStringMap(values map[string]string, names ...string) taggedResponseObject {
	fields := make(taggedResponseObject, 0, len(names))
	for _, name := range names {
		if value, ok := values[name]; ok {
			fields = append(fields, responseField(name, value))
		}
	}
	return fields
}
