package binding

import (
	"encoding/json"
	"fmt"
	"mime/multipart"
	"reflect"
	"strconv"
	"strings"

	"github.com/carlosxfelipe/gota/core"
)

// BindRequest binds parameters, query strings, headers, form data, and JSON body from core.Context to a target pointer.
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

	contentType := ctx.Request.Header.Get("Content-Type")
	isMultipart := strings.HasPrefix(strings.ToLower(contentType), "multipart/form-data")
	isFormUrlencoded := strings.HasPrefix(strings.ToLower(contentType), "application/x-www-form-urlencoded")
	hasBody := ctx.Request.ContentLength != 0

	// 1. If multipart/form-data, parse multipart form
	if isMultipart {
		if err := ctx.Request.ParseMultipartForm(ctx.MaxMultipartMemory()); err != nil {
			return fmt.Errorf("failed to parse multipart form: %w", err)
		}
	} else if isFormUrlencoded {
		if err := ctx.Request.ParseForm(); err != nil {
			return fmt.Errorf("failed to parse form: %w", err)
		}
	} else if strings.Contains(contentType, "application/json") || (!isMultipart && !isFormUrlencoded && hasBody) {
		body, err := ctx.Body()
		if err != nil {
			return fmt.Errorf("failed to read body: %w", err)
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, target); err != nil {
				return fmt.Errorf("invalid json body: %w", err)
			}
			clearNonBodyFields(elem)
		}
	}

	// 2. Reflect on fields to bind `param`, `query`, `header`, and `form` tags
	return bindFields(ctx, elem)
}

// bindFields binds the `param`, `query`, `header` and `form` tagged fields of the struct value
// elem, descending into embedded structs whose fields encoding/json would promote.
func bindFields(ctx *core.Context, elem reflect.Value) error {
	return bindFieldsInternal(ctx, elem, nil)
}

func bindFieldsInternal(ctx *core.Context, elem reflect.Value, shadowed map[string]bool) error {
	typ := elem.Type()

	// Collect tags declared on this struct itself so they shadow promoted fields.
	localTags := make(map[string]bool)
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if _, ok := core.EmbeddedStructType(f); ok {
			continue
		}
		for _, tag := range [...]string{"param", "query", "header", "form"} {
			if v := f.Tag.Get(tag); v != "" {
				localTags[tag+":"+v] = true
			}
		}
	}

	mergedShadowed := make(map[string]bool, len(shadowed)+len(localTags))
	for k, v := range shadowed {
		mergedShadowed[k] = v
	}
	for k, v := range localTags {
		mergedShadowed[k] = v
	}

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		fieldVal := elem.Field(i)

		if ft, ok := core.EmbeddedStructType(field); ok {
			if err := bindEmbedded(ctx, fieldVal, ft, mergedShadowed); err != nil {
				return err
			}
			continue
		}

		if !fieldVal.CanSet() {
			continue
		}

		// Check param tag: param:"id"
		if paramTag := field.Tag.Get("param"); paramTag != "" && !shadowed["param:"+paramTag] {
			if strVal, ok := ctx.Params[paramTag]; ok && strVal != "" {
				if err := setFieldValue(fieldVal, strVal); err != nil {
					return fmt.Errorf("invalid path param %q: %w", paramTag, err)
				}
			}
		}

		// Check query tag: query:"search"
		if queryTag := field.Tag.Get("query"); queryTag != "" && !shadowed["query:"+queryTag] {
			if queryVals, ok := ctx.Request.URL.Query()[queryTag]; ok && len(queryVals) > 0 {
				if fieldVal.Kind() == reflect.Slice && fieldVal.Type().Elem().Kind() == reflect.String {
					if len(queryVals) > 1 {
						sliceVal := reflect.MakeSlice(fieldVal.Type(), len(queryVals), len(queryVals))
						for idx, qv := range queryVals {
							sliceVal.Index(idx).SetString(qv)
						}
						fieldVal.Set(sliceVal)
					} else {
						if err := setFieldValue(fieldVal, queryVals[0]); err != nil {
							return fmt.Errorf("invalid query param %q: %w", queryTag, err)
						}
					}
				} else {
					if err := setFieldValue(fieldVal, queryVals[0]); err != nil {
						return fmt.Errorf("invalid query param %q: %w", queryTag, err)
					}
				}
			}
		}

		// Check header tag: header:"X-Token"
		if headerTag := field.Tag.Get("header"); headerTag != "" && !shadowed["header:"+headerTag] {
			if strVal := ctx.Header(headerTag); strVal != "" {
				if err := setFieldValue(fieldVal, strVal); err != nil {
					return fmt.Errorf("invalid header %q: %w", headerTag, err)
				}
			}
		}

		// Check form tag: form:"avatar" or form:"name"
		if formTag := field.Tag.Get("form"); formTag != "" && !shadowed["form:"+formTag] {
			// Single file: *multipart.FileHeader
			if field.Type == reflect.TypeOf((*multipart.FileHeader)(nil)) {
				if ctx.Request.MultipartForm != nil && ctx.Request.MultipartForm.File != nil {
					if files := ctx.Request.MultipartForm.File[formTag]; len(files) > 0 {
						fieldVal.Set(reflect.ValueOf(files[0]))
					}
				}
				continue
			}

			// Multiple files: []*multipart.FileHeader
			if field.Type == reflect.TypeOf(([]*multipart.FileHeader)(nil)) {
				if ctx.Request.MultipartForm != nil && ctx.Request.MultipartForm.File != nil {
					if files := ctx.Request.MultipartForm.File[formTag]; len(files) > 0 {
						fieldVal.Set(reflect.ValueOf(files))
					}
				}
				continue
			}

			// Text / primitive form values
			var formVals []string
			if ctx.Request.MultipartForm != nil && ctx.Request.MultipartForm.Value != nil {
				formVals = ctx.Request.MultipartForm.Value[formTag]
			}
			if len(formVals) == 0 && ctx.Request.PostForm != nil {
				formVals = ctx.Request.PostForm[formTag]
			}
			if len(formVals) == 0 && ctx.Request.Form != nil {
				formVals = ctx.Request.Form[formTag]
			}

			if len(formVals) > 0 {
				if fieldVal.Kind() == reflect.Slice && fieldVal.Type().Elem().Kind() == reflect.String {
					sliceVal := reflect.MakeSlice(fieldVal.Type(), len(formVals), len(formVals))
					for idx, v := range formVals {
						sliceVal.Index(idx).SetString(v)
					}
					fieldVal.Set(sliceVal)
				} else {
					if err := setFieldValue(fieldVal, formVals[0]); err != nil {
						return fmt.Errorf("invalid form field %q: %w", formTag, err)
					}
				}
			}
		}
	}

	return nil
}

// bindEmbedded binds the fields of an embedded struct. A nil embedded pointer is only
// allocated when at least one of its fields was actually bound, so absent values keep it nil.
func bindEmbedded(ctx *core.Context, fieldVal reflect.Value, ft reflect.Type, shadowed map[string]bool) error {
	if fieldVal.Kind() != reflect.Pointer {
		return bindFieldsInternal(ctx, fieldVal, shadowed)
	}
	if !fieldVal.IsNil() {
		return bindFieldsInternal(ctx, fieldVal.Elem(), shadowed)
	}
	if !fieldVal.CanSet() {
		return nil
	}
	tmp := reflect.New(ft)
	if err := bindFieldsInternal(ctx, tmp.Elem(), shadowed); err != nil {
		return err
	}
	if !tmp.Elem().IsZero() {
		fieldVal.Set(tmp)
	}
	return nil
}

// clearNonBodyFields zeroes fields that are meant to come from the path, query, header or
// form, unless they declare an explicit json tag. Without this, a JSON body such as
// {"UserID": 1} could spoof a field tagged `header:"X-User-Id"` whenever the header is absent.
// Fields of embedded structs are promoted by encoding/json, so they are cleared as well.
func clearNonBodyFields(elem reflect.Value) {
	typ := elem.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if _, ok := core.EmbeddedStructType(field); ok {
			embedded := elem.Field(i)
			if embedded.Kind() == reflect.Pointer {
				if embedded.IsNil() {
					continue
				}
				clearNonBodyFields(embedded.Elem())
				if embedded.CanSet() && embedded.Elem().IsZero() {
					embedded.SetZero()
				}
				continue
			}
			clearNonBodyFields(embedded)
			continue
		}
		if !elem.Field(i).CanSet() {
			continue
		}
		if name, _, _ := strings.Cut(field.Tag.Get("json"), ","); name != "" && name != "-" {
			continue
		}
		if field.Tag.Get("param") != "" || field.Tag.Get("query") != "" ||
			field.Tag.Get("header") != "" || field.Tag.Get("form") != "" {
			elem.Field(i).SetZero()
		}
	}
}

// setFieldValue sets a reflect.Value from a string representation.
func setFieldValue(val reflect.Value, strVal string) error {
	switch val.Kind() {
	case reflect.Pointer:
		// Pointer fields (*int, *bool, ...) stay nil when the value is absent, so 0/false
		// can be told apart from "not provided".
		elem := reflect.New(val.Type().Elem())
		if err := setFieldValue(elem.Elem(), strVal); err != nil {
			return err
		}
		val.Set(elem)
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
