package binding

import (
	"encoding"
	"fmt"
	"mime/multipart"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/carlosxfelipe/gota/core"
)

var (
	fileHeaderPtrType    = reflect.TypeOf((*multipart.FileHeader)(nil))
	fileHeaderSliceType  = reflect.TypeOf(([]*multipart.FileHeader)(nil))
	timeType             = reflect.TypeOf(time.Time{})
	compiledBindingCache sync.Map // map[reflect.Type]*BindingPlan
)

// BindingPlan holds the pre-compiled schema and plan for binding a struct type.
type BindingPlan struct {
	items    []planItem
	clear    *clearFieldPlan
	hasQuery bool
}

type planItem struct {
	index int

	// Embedded struct support
	isEmbedded   bool
	isPointer    bool
	embeddedType reflect.Type
	embeddedPlan *BindingPlan

	// Direct field binding
	paramTag    string
	queryTag    string
	headerTag   string
	formTag     string
	isFormFile  bool
	isFormFiles bool
	isSlice     bool
}

type clearFieldPlan struct {
	fieldIndices []int
	embedded     []embeddedClearPlan
}

type embeddedClearPlan struct {
	index     int
	isPointer bool
	plan      *clearFieldPlan
}

func (p *clearFieldPlan) isEmpty() bool {
	return p == nil || (len(p.fieldIndices) == 0 && len(p.embedded) == 0)
}

func (p *clearFieldPlan) execute(elem reflect.Value) {
	if p == nil {
		return
	}
	for _, idx := range p.fieldIndices {
		f := elem.Field(idx)
		if f.CanSet() {
			f.SetZero()
		}
	}
	for i := range p.embedded {
		emb := &p.embedded[i]
		f := elem.Field(emb.index)
		if emb.isPointer {
			if f.IsNil() {
				continue
			}
			emb.plan.execute(f.Elem())
			if f.CanSet() && f.Elem().IsZero() {
				f.SetZero()
			}
		} else {
			emb.plan.execute(f)
		}
	}
}

func buildClearPlan(t reflect.Type, seen map[reflect.Type]bool) *clearFieldPlan {
	if t == nil || seen[t] {
		return nil
	}
	seen[t] = true
	defer delete(seen, t)

	p := &clearFieldPlan{}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if ft, ok := core.EmbeddedStructType(field); ok {
			subPlan := buildClearPlan(ft, seen)
			if subPlan != nil && !subPlan.isEmpty() {
				p.embedded = append(p.embedded, embeddedClearPlan{
					index:     i,
					isPointer: field.Type.Kind() == reflect.Pointer,
					plan:      subPlan,
				})
			}
			continue
		}
		if !field.IsExported() {
			continue
		}
		if name, _, _ := strings.Cut(field.Tag.Get("json"), ","); name != "" && name != "-" {
			continue
		}
		if field.Tag.Get("param") != "" || field.Tag.Get("query") != "" ||
			field.Tag.Get("header") != "" || (field.Tag.Get("form") != "" && field.Tag.Get("form") != "-") {
			p.fieldIndices = append(p.fieldIndices, i)
		}
	}
	return p
}

// Compile compiles and caches the binding plan for struct type t.
// If t is a pointer, it compiles for the element type.
func Compile(t reflect.Type) *BindingPlan {
	if t == nil {
		return nil
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	if cached, ok := compiledBindingCache.Load(t); ok {
		return cached.(*BindingPlan)
	}
	plan := compilePlan(t, nil, make(map[reflect.Type]bool))
	compiledBindingCache.Store(t, plan)
	return plan
}

func compilePlan(typ reflect.Type, shadowed map[string]bool, seen map[reflect.Type]bool) *BindingPlan {
	if seen[typ] {
		return nil
	}
	seen[typ] = true
	defer delete(seen, typ)

	// Collect tags declared on this struct itself so they shadow promoted fields.
	mergedShadowed := make(map[string]bool, len(shadowed)+typ.NumField()*4)
	for k, v := range shadowed {
		mergedShadowed[k] = v
	}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if _, ok := core.EmbeddedStructType(f); ok {
			continue
		}
		for _, tag := range [...]string{"param", "query", "header", "form"} {
			if v := f.Tag.Get(tag); v != "" && v != "-" {
				mergedShadowed[tag+":"+v] = true
			}
		}
	}

	clearPlan := buildClearPlan(typ, make(map[reflect.Type]bool))
	if clearPlan != nil && clearPlan.isEmpty() {
		clearPlan = nil
	}

	plan := &BindingPlan{
		clear: clearPlan,
	}

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)

		if ft, ok := core.EmbeddedStructType(field); ok {
			embPlan := compilePlan(ft, mergedShadowed, seen)
			if embPlan != nil && len(embPlan.items) > 0 {
				if embPlan.hasQuery {
					plan.hasQuery = true
				}
				plan.items = append(plan.items, planItem{
					isEmbedded:   true,
					index:        i,
					isPointer:    field.Type.Kind() == reflect.Pointer,
					embeddedType: ft,
					embeddedPlan: embPlan,
				})
			}
			continue
		}

		if !field.IsExported() {
			continue
		}

		var item planItem
		item.index = i
		item.isSlice = field.Type.Kind() == reflect.Slice
		hasAction := false

		// Check param tag: param:"id"
		if paramTag := field.Tag.Get("param"); paramTag != "" && !shadowed["param:"+paramTag] {
			item.paramTag = paramTag
			hasAction = true
		}

		// Check query tag: query:"search"
		if queryTag := field.Tag.Get("query"); queryTag != "" && !shadowed["query:"+queryTag] {
			item.queryTag = queryTag
			hasAction = true
			plan.hasQuery = true
		}

		// Check header tag: header:"X-Token"
		if headerTag := field.Tag.Get("header"); headerTag != "" && !shadowed["header:"+headerTag] {
			item.headerTag = headerTag
			hasAction = true
		}

		// Check form tag: form:"avatar" or form:"name"
		if formTag := field.Tag.Get("form"); formTag != "" && formTag != "-" && !shadowed["form:"+formTag] {
			item.formTag = formTag
			hasAction = true
			if field.Type == fileHeaderPtrType {
				item.isFormFile = true
			} else if field.Type == fileHeaderSliceType {
				item.isFormFiles = true
			}
		}

		if hasAction {
			plan.items = append(plan.items, item)
		}
	}

	return plan
}

func (p *BindingPlan) execute(ctx *core.Context, elem reflect.Value, queryVals *url.Values) error {
	for i := range p.items {
		item := &p.items[i]
		fieldVal := elem.Field(item.index)

		if item.isEmbedded {
			if !item.isPointer {
				if err := item.embeddedPlan.execute(ctx, fieldVal, queryVals); err != nil {
					return err
				}
				continue
			}
			if !fieldVal.IsNil() {
				if err := item.embeddedPlan.execute(ctx, fieldVal.Elem(), queryVals); err != nil {
					return err
				}
				continue
			}
			if !fieldVal.CanSet() {
				continue
			}
			tmp := reflect.New(item.embeddedType)
			if err := item.embeddedPlan.execute(ctx, tmp.Elem(), queryVals); err != nil {
				return err
			}
			if !tmp.Elem().IsZero() {
				fieldVal.Set(tmp)
			}
			continue
		}

		if !fieldVal.CanSet() {
			continue
		}

		// Param tag
		if item.paramTag != "" {
			if strVal, ok := ctx.Params[item.paramTag]; ok && strVal != "" {
				if err := setFieldValue(fieldVal, strVal); err != nil {
					return fmt.Errorf("invalid path param %q: %w", item.paramTag, err)
				}
			}
		}

		// Query tag
		if item.queryTag != "" {
			if *queryVals == nil {
				*queryVals = ctx.Request.URL.Query()
			}
			if qVals, ok := (*queryVals)[item.queryTag]; ok && len(qVals) > 0 {
				if len(qVals) > 1 && item.isSlice {
					if err := setSliceValues(fieldVal, qVals, false); err != nil {
						return fmt.Errorf("invalid query param %q: %w", item.queryTag, err)
					}
				} else if err := setFieldValue(fieldVal, qVals[0]); err != nil {
					return fmt.Errorf("invalid query param %q: %w", item.queryTag, err)
				}
			}
		}

		// Header tag
		if item.headerTag != "" {
			if strVal := ctx.Header(item.headerTag); strVal != "" {
				if err := setFieldValue(fieldVal, strVal); err != nil {
					return fmt.Errorf("invalid header %q: %w", item.headerTag, err)
				}
			}
		}

		// Form tag
		if item.formTag != "" {
			if item.isFormFile {
				if files := getFormFiles(ctx, item.formTag); len(files) > 0 {
					fieldVal.Set(reflect.ValueOf(files[0]))
				}
				continue
			}
			if item.isFormFiles {
				if files := getFormFiles(ctx, item.formTag); len(files) > 0 {
					fieldVal.Set(reflect.ValueOf(files))
				}
				continue
			}

			// Text / primitive form values
			formVals := getFormValues(ctx, item.formTag)
			if len(formVals) > 0 {
				if item.isSlice {
					if err := setSliceValues(fieldVal, formVals, false); err != nil {
						return fmt.Errorf("invalid form field %q: %w", item.formTag, err)
					}
				} else if err := setFieldValue(fieldVal, formVals[0]); err != nil {
					return fmt.Errorf("invalid form field %q: %w", item.formTag, err)
				}
			}
		}
	}
	return nil
}

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
			return ctx.DecodeJSON(body, target)
		}
		return nil
	}

	plan := Compile(elem.Type())

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
			if err := ctx.DecodeJSON(body, target); err != nil {
				return fmt.Errorf("invalid json body: %w", err)
			}
			if plan != nil && plan.clear != nil {
				plan.clear.execute(elem)
			}
		}
	}

	// 2. Execute precompiled binding plan
	if plan != nil {
		var queryVals url.Values
		return plan.execute(ctx, elem, &queryVals)
	}
	return nil
}

// setFieldValue sets a reflect.Value from a string representation.
func setFieldValue(val reflect.Value, strVal string) error {
	if val.Kind() == reflect.Pointer {
		// Pointer fields (*int, *bool, *time.Time, ...) stay nil when the value is absent,
		// so 0/false can be told apart from "not provided".
		elem := reflect.New(val.Type().Elem())
		if err := setFieldValue(elem.Elem(), strVal); err != nil {
			return err
		}
		val.Set(elem)
		return nil
	}

	if val.Type() == timeType {
		for _, layout := range [...]string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
			if parsed, err := time.Parse(layout, strVal); err == nil {
				val.Set(reflect.ValueOf(parsed))
				return nil
			}
		}
		return fmt.Errorf("invalid time value %q (expected RFC3339 or YYYY-MM-DD)", strVal)
	}

	if val.CanAddr() {
		if unmarshaler, ok := val.Addr().Interface().(encoding.TextUnmarshaler); ok {
			return unmarshaler.UnmarshalText([]byte(strVal))
		}
	}

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
		parts := strings.Split(strVal, ",")
		return setSliceValues(val, parts, true)
	default:
		return fmt.Errorf("unsupported field type: %s", val.Kind())
	}
	return nil
}

func setSliceValues(val reflect.Value, items []string, trim bool) error {
	sliceVal := reflect.MakeSlice(val.Type(), len(items), len(items))
	for i, item := range items {
		if trim {
			item = strings.TrimSpace(item)
		}
		if err := setFieldValue(sliceVal.Index(i), item); err != nil {
			return err
		}
	}
	val.Set(sliceVal)
	return nil
}

func getFormFiles(ctx *core.Context, tag string) []*multipart.FileHeader {
	if ctx.Request.MultipartForm != nil && ctx.Request.MultipartForm.File != nil {
		return ctx.Request.MultipartForm.File[tag]
	}
	return nil
}

func getFormValues(ctx *core.Context, key string) []string {
	if ctx.Request.MultipartForm != nil && ctx.Request.MultipartForm.Value != nil {
		if vals := ctx.Request.MultipartForm.Value[key]; len(vals) > 0 {
			return vals
		}
	}
	if ctx.Request.PostForm != nil {
		if vals := ctx.Request.PostForm[key]; len(vals) > 0 {
			return vals
		}
	}
	if ctx.Request.Form != nil {
		return ctx.Request.Form[key]
	}
	return nil
}
