package binding

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/carlosxfelipe/gota/core"
)

// BindRequest binds parameters, query strings, headers, and JSON body from core.Context to a target pointer.
func BindRequest(ctx *core.Context, target any) error {
	val := reflect.ValueOf(target)
	if val.Kind() != reflect.Pointer || val.IsNil() {
		return fmt.Errorf("gota: bind target must be a non-nil pointer, got %T", target)
	}

	elem := val.Elem()
	if elem.Kind() != reflect.Struct {
		body, err := ctx.Body()
		if err != nil {
			return fmt.Errorf("failed to read body: %w", err)
		}
		if len(body) > 0 {
			return json.Unmarshal(body, target)
		}
		return nil
	}

	// 1. If request has JSON body, attempt to unmarshal into target struct
	contentType := ctx.Request.Header.Get("Content-Type")
	hasBody := ctx.Request.ContentLength != 0
	if strings.Contains(contentType, "application/json") || hasBody {
		body, err := ctx.Body()
		if err != nil {
			return fmt.Errorf("failed to read body: %w", err)
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, target); err != nil {
				return fmt.Errorf("invalid json body: %w", err)
			}
		}
	}

	// 2. Reflect on fields to bind `param`, `query`, and `header` tags
	typ := elem.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		fieldVal := elem.Field(i)

		if !fieldVal.CanSet() {
			continue
		}

		// Check param tag: param:"id"
		if paramTag := field.Tag.Get("param"); paramTag != "" {
			if strVal, ok := ctx.Params[paramTag]; ok && strVal != "" {
				if err := setFieldValue(fieldVal, strVal); err != nil {
					return fmt.Errorf("invalid path param %q: %w", paramTag, err)
				}
			}
		}

		// Check query tag: query:"search"
		if queryTag := field.Tag.Get("query"); queryTag != "" {
			if strVal := ctx.Query(queryTag); strVal != "" {
				if err := setFieldValue(fieldVal, strVal); err != nil {
					return fmt.Errorf("invalid query param %q: %w", queryTag, err)
				}
			}
		}

		// Check header tag: header:"X-Token"
		if headerTag := field.Tag.Get("header"); headerTag != "" {
			if strVal := ctx.Header(headerTag); strVal != "" {
				if err := setFieldValue(fieldVal, strVal); err != nil {
					return fmt.Errorf("invalid header %q: %w", headerTag, err)
				}
			}
		}
	}

	return nil
}

// setFieldValue sets a reflect.Value from a string representation.
func setFieldValue(val reflect.Value, strVal string) error {
	switch val.Kind() {
	case reflect.String:
		val.SetString(strVal)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		intVal, err := strconv.ParseInt(strVal, 10, 64)
		if err != nil {
			return err
		}
		val.SetInt(intVal)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		uintVal, err := strconv.ParseUint(strVal, 10, 64)
		if err != nil {
			return err
		}
		val.SetUint(uintVal)
	case reflect.Float32, reflect.Float64:
		floatVal, err := strconv.ParseFloat(strVal, 64)
		if err != nil {
			return err
		}
		val.SetFloat(floatVal)
	case reflect.Bool:
		switch strings.ToLower(strVal) {
		case "true", "1", "t", "yes", "y":
			val.SetBool(true)
		case "false", "0", "f", "no", "n":
			val.SetBool(false)
		default:
			return fmt.Errorf("invalid boolean value %q", strVal)
		}
	case reflect.Slice:
		if val.Type().Elem().Kind() == reflect.String {
			parts := strings.Split(strVal, ",")
			sliceVal := reflect.MakeSlice(val.Type(), len(parts), len(parts))
			for i, p := range parts {
				sliceVal.Index(i).SetString(strings.TrimSpace(p))
			}
			val.Set(sliceVal)
		}
	default:
		return fmt.Errorf("unsupported field type: %s", val.Kind())
	}
	return nil
}
