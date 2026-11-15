package repr

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
)

var (
	timeType             = reflect.TypeOf(time.Time{})
	fileHeaderType       = reflect.TypeOf(multipart.FileHeader{})
	fileHeaderPtrType    = reflect.TypeOf((*multipart.FileHeader)(nil))
	fileHeaderSliceType  = reflect.TypeOf(([]*multipart.FileHeader)(nil))
	compiledBindingCache sync.Map // map[reflect.Type]*bindingPlan
)

// embeddedStructType reports whether f is an anonymous embedded struct whose fields promote
// into the enclosing struct (mirroring encoding/json promotion rules).
func embeddedStructType(f reflect.StructField) (reflect.Type, bool) {
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

type bindingPlan struct {
	items []planItem
	clear *clearFieldPlan
}

type planItem struct {
	index        int
	isEmbedded   bool
	isPointer    bool
	embeddedType reflect.Type
	embeddedPlan *bindingPlan

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
		if f := elem.Field(idx); f.CanSet() {
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
		if ft, ok := embeddedStructType(field); ok {
			sub := buildClearPlan(ft, seen)
			if sub != nil && !sub.isEmpty() {
				p.embedded = append(p.embedded, embeddedClearPlan{
					index:     i,
					isPointer: field.Type.Kind() == reflect.Pointer,
					plan:      sub,
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

func compileBindingPlan(t reflect.Type) *bindingPlan {
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
		return cached.(*bindingPlan)
	}
	plan := buildBindingPlan(t, nil, make(map[reflect.Type]bool))
	compiledBindingCache.Store(t, plan)
	return plan
}

func buildBindingPlan(typ reflect.Type, shadowed map[string]bool, seen map[reflect.Type]bool) *bindingPlan {
	if seen[typ] {
		return nil
	}
	seen[typ] = true
	defer delete(seen, typ)

	mergedShadowed := make(map[string]bool, len(shadowed)+typ.NumField()*4)
	for k, v := range shadowed {
		mergedShadowed[k] = v
	}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if _, ok := embeddedStructType(f); ok {
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

	plan := &bindingPlan{clear: clearPlan}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if ft, ok := embeddedStructType(field); ok {
			embPlan := buildBindingPlan(ft, mergedShadowed, seen)
			if embPlan != nil && len(embPlan.items) > 0 {
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

		item := planItem{
			index:   i,
			isSlice: field.Type.Kind() == reflect.Slice,
		}
		hasAction := false

		if paramTag := field.Tag.Get("param"); paramTag != "" && !shadowed["param:"+paramTag] {
			item.paramTag = paramTag
			hasAction = true
		}
		if queryTag := field.Tag.Get("query"); queryTag != "" && !shadowed["query:"+queryTag] {
			item.queryTag = queryTag
			hasAction = true
		}
		if headerTag := field.Tag.Get("header"); headerTag != "" && !shadowed["header:"+headerTag] {
			item.headerTag = headerTag
			hasAction = true
		}
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

func (p *bindingPlan) execute(ctx *Context, elem reflect.Value, queryVals *url.Values) error {
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

		if item.paramTag != "" {
			if strVal := ctx.Param(item.paramTag); strVal != "" {
				if err := setFieldValue(fieldVal, strVal); err != nil {
					return fmt.Errorf("invalid path param %q: %w", item.paramTag, err)
				}
			}
		}

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

		if item.headerTag != "" {
			if strVal := ctx.Header(item.headerTag); strVal != "" {
				if err := setFieldValue(fieldVal, strVal); err != nil {
					return fmt.Errorf("invalid header %q: %w", item.headerTag, err)
				}
			}
		}

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
			if formVals := getFormValues(ctx, item.formTag); len(formVals) > 0 {
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

// bindRequest binds path params, query params, headers, form data, and JSON body into target.
func bindRequest(ctx *Context, target any) error {
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

	plan := compileBindingPlan(elem.Type())

	lowerCT := strings.ToLower(ctx.Request.Header.Get("Content-Type"))
	isMultipart := strings.HasPrefix(lowerCT, "multipart/form-data")
	isFormURLEncoded := strings.HasPrefix(lowerCT, "application/x-www-form-urlencoded")
	hasBody := ctx.Request.ContentLength != 0

	if isMultipart {
		if err := ctx.Request.ParseMultipartForm(ctx.maxMultipartMemory); err != nil {
			return fmt.Errorf("failed to parse multipart form: %w", err)
		}
	} else if isFormURLEncoded {
		if err := ctx.Request.ParseForm(); err != nil {
			return fmt.Errorf("failed to parse form: %w", err)
		}
	} else if strings.Contains(lowerCT, "application/json") || hasBody {
		body, err := ctx.Body()
		if err != nil {
			return fmt.Errorf("failed to read body: %w", err)
		}
		if len(body) > 0 {
			if err := ctx.DecodeJSON(body, target); err != nil {
				return fmt.Errorf("invalid json body: %w", err)
			}
			// Clear fields that are bound exclusively from param/query/header/form so
			// JSON payloads cannot spoof path/query/header fields.
			if plan != nil && plan.clear != nil {
				plan.clear.execute(elem)
			}
		}
	}

	if plan != nil {
		var queryVals url.Values
		return plan.execute(ctx, elem, &queryVals)
	}
	return nil
}

func setFieldValue(val reflect.Value, strVal string) error {
	if val.Kind() == reflect.Pointer {
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
		if u, ok := val.Addr().Interface().(encoding.TextUnmarshaler); ok {
			return u.UnmarshalText([]byte(strVal))
		}
	}

	switch val.Kind() {
	case reflect.String:
		val.SetString(strVal)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(strVal, 10, val.Type().Bits())
		if err != nil {
			return err
		}
		val.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(strVal, 10, val.Type().Bits())
		if err != nil {
			return err
		}
		val.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(strVal, val.Type().Bits())
		if err != nil {
			return err
		}
		val.SetFloat(f)
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
		return setSliceValues(val, strings.Split(strVal, ","), true)
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

func getFormFiles(ctx *Context, key string) []*multipart.FileHeader {
	if ctx.Request.MultipartForm != nil && ctx.Request.MultipartForm.File != nil {
		return ctx.Request.MultipartForm.File[key]
	}
	return nil
}

func getFormValues(ctx *Context, key string) []string {
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
