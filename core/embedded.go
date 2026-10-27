package core

import (
	"mime/multipart"
	"reflect"
	"strings"
	"time"
)

var (
	timeType       = reflect.TypeOf(time.Time{})
	fileHeaderType = reflect.TypeOf(multipart.FileHeader{})
)

// EmbeddedStructType reports whether f is an embedded struct whose fields are promoted to the
// enclosing struct, mirroring encoding/json: the field must be anonymous, be a struct (or a
// pointer to one) and carry no json, form, param, query or header name. time.Time and
// multipart.FileHeader keep their special handling and are never flattened. A pointer to an
// unexported embedded type is ignored, as encoding/json does. The returned type has the
// pointer dereferenced.
func EmbeddedStructType(f reflect.StructField) (reflect.Type, bool) {
	if !f.Anonymous {
		return nil, false
	}
	if name, _, _ := strings.Cut(f.Tag.Get("json"), ","); name != "" {
		return nil, false
	}
	for _, tag := range [...]string{"form", "param", "query", "header"} {
		if f.Tag.Get(tag) != "" {
			return nil, false
		}
	}

	ft := f.Type
	isPtr := ft.Kind() == reflect.Pointer
	if isPtr {
		ft = ft.Elem()
	}
	if ft.Kind() != reflect.Struct || ft == timeType || ft == fileHeaderType {
		return nil, false
	}
	if isPtr && !f.IsExported() {
		return nil, false
	}
	return ft, true
}
