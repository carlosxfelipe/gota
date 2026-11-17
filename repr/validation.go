package repr

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"
)

// Validator can be implemented by request structs for custom cross-field validation.
type Validator interface {
	Validate() error
}

// ContextValidator can be implemented by request structs needing Context during validation.
type ContextValidator interface {
	Validate(ctx *Context) error
}

var (
	validateEngine = newValidatorEngine()
	msgCache       sync.Map // map[reflect.Type]map[string]map[string]string
)

func newValidatorEngine() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	_ = v.RegisterValidation("nowarn", func(validator.FieldLevel) bool { return true })
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		for _, tag := range [...]string{"json", "form", "query", "param", "header"} {
			if raw := fld.Tag.Get(tag); raw != "" {
				name, _, _ := strings.Cut(raw, ",")
				if name != "" && name != "-" {
					return name
				}
			}
		}
		return fld.Name
	})
	return v
}

// RegisterValidation registers a custom validator rule on the underlying go-playground/validator engine.
func RegisterValidation(tag string, fn validator.Func) error {
	return validateEngine.RegisterValidation(tag, fn)
}

// ValidateStruct validates target using go-playground/validator/v10 and any Validator / ContextValidator hooks.
func ValidateStruct(ctx *Context, target any) *ValidationError {
	if target == nil {
		return nil
	}

	errMap := make(map[string][]string)

	if err := validateEngine.Struct(target); err != nil {
		if valErrs, ok := errors.AsType[validator.ValidationErrors](err); ok {
			fieldMsgs := customMessagesForType(reflect.TypeOf(target))
			for _, fe := range valErrs {
				fieldKey := fe.Field()
				if ns := fe.Namespace(); ns != "" {
					if _, after, ok := strings.Cut(ns, "."); ok && after != "" {
						fieldKey = after
					}
				}
				msg := resolveValidationMessage(fieldMsgs[fe.StructField()], fe)
				errMap[fieldKey] = append(errMap[fieldKey], msg)
			}
		} else {
			errMap["_"] = append(errMap["_"], err.Error())
		}
	}

	if v, ok := target.(Validator); ok {
		if err := v.Validate(); err != nil {
			mergeCustomValidatorError(errMap, err)
		}
	}
	if ctx != nil {
		if cv, ok := target.(ContextValidator); ok {
			if err := cv.Validate(ctx); err != nil {
				mergeCustomValidatorError(errMap, err)
			}
		}
	}

	if len(errMap) == 0 {
		return nil
	}
	return &ValidationError{
		Message: "Validation failed",
		Errors:  errMap,
		Status:  http.StatusUnprocessableEntity,
	}
}

func mergeCustomValidatorError(errMap map[string][]string, err error) {
	if ve, ok := errors.AsType[*ValidationError](err); ok {
		for k, msgs := range ve.Errors {
			errMap[k] = append(errMap[k], msgs...)
		}
		return
	}
	errMap["_"] = append(errMap["_"], err.Error())
}

func resolveValidationMessage(custom map[string]string, fe validator.FieldError) string {
	if len(custom) > 0 {
		if msg, ok := custom[fe.Tag()]; ok {
			return msg
		}
		if msg, ok := custom["_"]; ok {
			return msg
		}
	}
	switch fe.Tag() {
	case "required":
		return fmt.Sprintf("%s is required", fe.Field())
	case "email":
		return fmt.Sprintf("%s must be a valid email address", fe.Field())
	case "url":
		return fmt.Sprintf("%s must be a valid URL", fe.Field())
	case "uuid":
		return fmt.Sprintf("%s must be a valid UUID", fe.Field())
	case "min":
		return fmt.Sprintf("%s must be at least %s", fe.Field(), fe.Param())
	case "max":
		return fmt.Sprintf("%s must be at most %s", fe.Field(), fe.Param())
	case "len":
		return fmt.Sprintf("%s must have length %s", fe.Field(), fe.Param())
	case "oneof":
		return fmt.Sprintf("%s must be one of [%s]", fe.Field(), fe.Param())
	default:
		if fe.Param() != "" {
			return fmt.Sprintf("%s failed validation %s=%s", fe.Field(), fe.Tag(), fe.Param())
		}
		return fmt.Sprintf("%s failed validation %s", fe.Field(), fe.Tag())
	}
}

func customMessagesForType(t reflect.Type) map[string]map[string]string {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	if cached, ok := msgCache.Load(t); ok {
		return cached.(map[string]map[string]string)
	}
	out := make(map[string]map[string]string)
	collectCustomMessages(t, out, map[reflect.Type]bool{})
	msgCache.Store(t, out)
	return out
}

func collectCustomMessages(t reflect.Type, out map[string]map[string]string, seen map[reflect.Type]bool) {
	if seen[t] {
		return
	}
	seen[t] = true
	defer delete(seen, t)

	for i := range t.NumField() {
		f := t.Field(i)
		if ft, ok := embeddedStructType(f); ok {
			collectCustomMessages(ft, out, seen)
			continue
		}
		if !f.IsExported() {
			continue
		}
		if msgTag := f.Tag.Get("msg"); msgTag != "" {
			out[f.Name] = parseMessageTag(msgTag)
		}
		ft := f.Type
		for ft.Kind() == reflect.Pointer || ft.Kind() == reflect.Slice {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct && ft != timeType && ft != fileHeaderType {
			collectCustomMessages(ft, out, seen)
		}
	}
}

func parseMessageTag(tag string) map[string]string {
	msgs := make(map[string]string)
	for part := range strings.SplitSeq(tag, "|") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if rule, msg, ok := strings.Cut(part, ":"); ok {
			rule = strings.TrimSpace(rule)
			msg = strings.TrimSpace(msg)
			if rule != "" && msg != "" {
				msgs[rule] = msg
			}
		} else {
			msgs["_"] = part
		}
	}
	return msgs
}

// checkStructTags performs fail-fast checks on common struct tag mistakes at route registration time.
func checkStructTags(t reflect.Type, seen map[reflect.Type]bool) error {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct || t == timeType || t == fileHeaderType || seen[t] {
		return nil
	}
	seen[t] = true
	defer delete(seen, t)

	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() && !f.Anonymous {
			continue
		}
		if p := f.Tag.Get("path"); p != "" {
			return fmt.Errorf("field %q: tag `path:%q` is not supported; use `param:%q`", f.Name, p, p)
		}
		if m := f.Tag.Get("message"); m != "" {
			return fmt.Errorf("field %q: tag `message:%q` is not supported; use `msg:%q`", f.Name, m, m)
		}
		if f.Tag.Get("msg") != "" && f.Tag.Get("validate") == "" {
			return fmt.Errorf("field %q: tag `msg` defined without a `validate` tag", f.Name)
		}
		for _, bindTag := range [...]string{"param", "query", "header", "form"} {
			if raw := f.Tag.Get(bindTag); strings.Contains(raw, ",") {
				base, opt, _ := strings.Cut(raw, ",")
				return fmt.Errorf("field %q: tag `%s:%q` contains unsupported option %q; use `%s:%q`", f.Name, bindTag, raw, opt, bindTag, base)
			}
		}
		ft := f.Type
		for ft.Kind() == reflect.Pointer || ft.Kind() == reflect.Slice {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct {
			if err := checkStructTags(ft, seen); err != nil {
				return err
			}
		}
	}
	return dryRunValidateStruct(t)
}

func dryRunValidateStruct(t reflect.Type) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("invalid validate tag in %s: %v", t.Name(), r)
		}
	}()
	_ = validateEngine.Struct(reflect.New(t).Elem().Interface())
	return nil
}
