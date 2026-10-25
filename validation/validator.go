package validation

import (
	"fmt"
	"net/mail"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/carlosxfelipe/gota/core"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Validator is an interface that structs can implement for custom validation logic.
type Validator interface {
	Validate() error
}

// ContextValidator is an interface that structs can implement for validation with Context access.
type ContextValidator interface {
	Validate(ctx *core.Context) error
}

// ValidationError holds structured field validation errors.
type ValidationError struct {
	Message string              `json:"message"`
	Errors  map[string][]string `json:"errors"`
	Status  int                 `json:"status"`
}

func (v *ValidationError) Error() string {
	var parts []string
	for field, msgs := range v.Errors {
		parts = append(parts, fmt.Sprintf("%s: %s", field, strings.Join(msgs, ", ")))
	}
	return fmt.Sprintf("validation failed: %s", strings.Join(parts, "; "))
}

// HTTPStatus returns the HTTP status used when this error is rendered (422 by default).
func (v *ValidationError) HTTPStatus() int {
	if v.Status == 0 {
		return 422
	}
	return v.Status
}

// isStdLibStruct reports whether a type is a standard library struct that should not be recursed into.
func isStdLibStruct(t reflect.Type) bool {
	if t == reflect.TypeOf(time.Time{}) {
		return true
	}
	pkg := t.PkgPath()
	return strings.HasPrefix(pkg, "mime/") || strings.HasPrefix(pkg, "net/") || strings.HasPrefix(pkg, "io")
}

// isKnownRule reports whether a string represents a known validation rule name.
func isKnownRule(s string) bool {
	if s == "required" || s == "email" || s == "url" || s == "uuid" {
		return true
	}
	parts := strings.SplitN(s, "=", 2)
	switch parts[0] {
	case "min", "max", "len", "oneof":
		return true
	default:
		return false
	}
}

// splitRules splits a validation tag into individual rules, preserving commas inside oneof parameters.
func splitRules(tag string) []string {
	var rules []string
	for _, part := range strings.Split(tag, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if len(rules) > 0 && strings.HasPrefix(rules[len(rules)-1], "oneof=") && !isKnownRule(part) {
			rules[len(rules)-1] += "," + part
		} else {
			rules = append(rules, part)
		}
	}
	return rules
}

// CheckTags verifies that every `validate` tag on the struct type t only uses
// known rules with valid parameters, recursing into nested structs and slices.
// It is meant to run once at route registration so typos fail fast
// instead of silently disabling a rule.
func CheckTags(t reflect.Type) error {
	if t == nil {
		return nil
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	if isStdLibStruct(t) {
		return nil
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("validate")

		name := field.Name
		if formTag := field.Tag.Get("form"); formTag != "" && formTag != "-" {
			if n := strings.Split(formTag, ",")[0]; n != "" {
				name = n
			}
		} else if jsonTag := field.Tag.Get("json"); jsonTag != "" && jsonTag != "-" {
			if n := strings.Split(jsonTag, ",")[0]; n != "" {
				name = n
			}
		}

		if tag != "" {
			for _, rule := range splitRules(tag) {
				rule = strings.TrimSpace(rule)
				if rule == "" {
					continue
				}
				parts := strings.SplitN(rule, "=", 2)
				switch parts[0] {
				case "required", "email", "url", "uuid":
				case "min", "max":
					if len(parts) < 2 || parts[1] == "" {
						return fmt.Errorf("field %q: rule %q requires a numeric parameter (e.g. %s=3)", name, parts[0], parts[0])
					}
					if _, err := strconv.ParseFloat(parts[1], 64); err != nil {
						return fmt.Errorf("field %q: rule %q has an invalid numeric parameter %q", name, parts[0], parts[1])
					}
				case "len":
					if len(parts) < 2 || parts[1] == "" {
						return fmt.Errorf("field %q: rule %q requires an integer parameter (e.g. len=5)", name, parts[0])
					}
					if _, err := strconv.Atoi(parts[1]); err != nil {
						return fmt.Errorf("field %q: rule %q has an invalid integer parameter %q", name, parts[0], parts[1])
					}
				case "oneof":
					if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
						return fmt.Errorf("field %q: rule %q requires at least one choice (e.g. oneof=a,b,c)", name, parts[0])
					}
				default:
					return fmt.Errorf("field %q: unknown validation rule %q", name, parts[0])
				}
			}
		}

		// Recurse into nested struct or slice of structs
		ft := field.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Slice {
			ft = ft.Elem()
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
		}
		if ft.Kind() == reflect.Struct && !isStdLibStruct(ft) {
			if err := CheckTags(ft); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidateStruct validates a struct instance using both its validate tags and Validator interface.
func ValidateStruct(ctx *core.Context, target any) *ValidationError {
	val := reflect.ValueOf(target)
	if val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return nil
		}
		val = val.Elem()
	}

	errMap := make(map[string][]string)

	if val.Kind() == reflect.Struct {
		validateStructRecursive(ctx, val, "", errMap)
	}

	// 2. Custom Validator interface
	if v, ok := target.(Validator); ok {
		if err := v.Validate(); err != nil {
			errMap["_general"] = append(errMap["_general"], err.Error())
		}
	}

	// 3. Custom ContextValidator interface
	if cv, ok := target.(ContextValidator); ok {
		if err := cv.Validate(ctx); err != nil {
			errMap["_general"] = append(errMap["_general"], err.Error())
		}
	}

	if len(errMap) > 0 {
		return &ValidationError{
			Message: "validation failed",
			Errors:  errMap,
			Status:  422,
		}
	}

	return nil
}

func validateStructRecursive(ctx *core.Context, val reflect.Value, prefix string, errMap map[string][]string) {
	if val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return
		}
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct || isStdLibStruct(val.Type()) {
		return
	}

	typ := val.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		// Unexported fields cannot be read via reflection (val.Interface() would panic).
		if !field.IsExported() && !field.Anonymous {
			continue
		}
		fieldVal := val.Field(i)

		// Determine field name
		fieldName := field.Name
		if formTag := field.Tag.Get("form"); formTag != "" && formTag != "-" {
			parts := strings.Split(formTag, ",")
			if parts[0] != "" {
				fieldName = parts[0]
			}
		} else if jsonTag := field.Tag.Get("json"); jsonTag != "" && jsonTag != "-" {
			parts := strings.Split(jsonTag, ",")
			if parts[0] != "" {
				fieldName = parts[0]
			}
		} else if paramTag := field.Tag.Get("param"); paramTag != "" {
			fieldName = paramTag
		} else if queryTag := field.Tag.Get("query"); queryTag != "" {
			fieldName = queryTag
		}

		fullName := fieldName
		if prefix != "" {
			fullName = prefix + "." + fieldName
		}

		validateTag := field.Tag.Get("validate")
		if validateTag != "" {
			rules := splitRules(validateTag)
			for _, rule := range rules {
				rule = strings.TrimSpace(rule)
				if rule == "" {
					continue
				}

				if err := applyValidationRule(fullName, fieldVal, rule); err != "" {
					errMap[fullName] = append(errMap[fullName], err)
				}
			}
		}

		// Recurse into nested structs or slice of structs
		ft := field.Type
		if ft.Kind() == reflect.Pointer {
			if !fieldVal.IsNil() {
				validateStructRecursive(ctx, fieldVal.Elem(), fullName, errMap)
			}
		} else if ft.Kind() == reflect.Struct && !isStdLibStruct(ft) {
			validateStructRecursive(ctx, fieldVal, fullName, errMap)
		} else if ft.Kind() == reflect.Slice {
			elemType := ft.Elem()
			if elemType.Kind() == reflect.Pointer {
				elemType = elemType.Elem()
			}
			if elemType.Kind() == reflect.Struct && !isStdLibStruct(elemType) {
				for idx := 0; idx < fieldVal.Len(); idx++ {
					item := fieldVal.Index(idx)
					itemPrefix := fmt.Sprintf("%s[%d]", fullName, idx)
					validateStructRecursive(ctx, item, itemPrefix, errMap)
				}
			}
		}
	}

	// If the struct itself implements Validator or ContextValidator
	if val.CanAddr() {
		if v, ok := val.Addr().Interface().(Validator); ok && prefix != "" {
			if err := v.Validate(); err != nil {
				errMap[prefix] = append(errMap[prefix], err.Error())
			}
		}
		if cv, ok := val.Addr().Interface().(ContextValidator); ok && prefix != "" {
			if err := cv.Validate(ctx); err != nil {
				errMap[prefix] = append(errMap[prefix], err.Error())
			}
		}
	}
}

func applyValidationRule(field string, val reflect.Value, rule string) string {
	parts := strings.SplitN(rule, "=", 2)
	ruleName := parts[0]
	ruleParam := ""
	if len(parts) > 1 {
		ruleParam = parts[1]
	}

	// Every rule except `required` validates the pointed-to value; a nil pointer means
	// "not provided" and is left to `required`. Without this, pointer fields such as
	// *string with min=3 or *int with max=10 would silently skip validation.
	if ruleName != "required" {
		for val.Kind() == reflect.Pointer || val.Kind() == reflect.Interface {
			if val.IsNil() {
				return ""
			}
			val = val.Elem()
		}
	}

	switch ruleName {
	case "required":
		if isZero(val) {
			return fmt.Sprintf("field %s is required", field)
		}

	case "min":
		if ruleParam == "" {
			return ""
		}
		if (val.Kind() == reflect.Pointer || val.Kind() == reflect.Interface) && val.IsNil() {
			return ""
		}
		minVal, err := strconv.ParseFloat(ruleParam, 64)
		if err != nil {
			return ""
		}
		switch val.Kind() {
		case reflect.String:
			if float64(utf8.RuneCountInString(val.String())) < minVal {
				return fmt.Sprintf("field %s must be at least %v characters", field, minVal)
			}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if float64(val.Uint()) < minVal {
				return fmt.Sprintf("field %s must be at least %v", field, minVal)
			}
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if float64(val.Int()) < minVal {
				return fmt.Sprintf("field %s must be at least %v", field, minVal)
			}
		case reflect.Float32, reflect.Float64:
			if val.Float() < minVal {
				return fmt.Sprintf("field %s must be at least %v", field, minVal)
			}
		case reflect.Slice:
			if float64(val.Len()) < minVal {
				return fmt.Sprintf("field %s must contain at least %v items", field, minVal)
			}
		}

	case "max":
		if ruleParam == "" {
			return ""
		}
		if (val.Kind() == reflect.Pointer || val.Kind() == reflect.Interface) && val.IsNil() {
			return ""
		}
		maxVal, err := strconv.ParseFloat(ruleParam, 64)
		if err != nil {
			return ""
		}
		switch val.Kind() {
		case reflect.String:
			if float64(utf8.RuneCountInString(val.String())) > maxVal {
				return fmt.Sprintf("field %s must not exceed %v characters", field, maxVal)
			}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if float64(val.Uint()) > maxVal {
				return fmt.Sprintf("field %s must not exceed %v", field, maxVal)
			}
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if float64(val.Int()) > maxVal {
				return fmt.Sprintf("field %s must not exceed %v", field, maxVal)
			}
		case reflect.Float32, reflect.Float64:
			if val.Float() > maxVal {
				return fmt.Sprintf("field %s must not exceed %v", field, maxVal)
			}
		case reflect.Slice:
			if float64(val.Len()) > maxVal {
				return fmt.Sprintf("field %s must not contain more than %v items", field, maxVal)
			}
		}

	case "len":
		if ruleParam == "" {
			return ""
		}
		if (val.Kind() == reflect.Pointer || val.Kind() == reflect.Interface) && val.IsNil() {
			return ""
		}
		targetLen, err := strconv.Atoi(ruleParam)
		if err != nil {
			return ""
		}
		switch val.Kind() {
		case reflect.String:
			if utf8.RuneCountInString(val.String()) != targetLen {
				return fmt.Sprintf("field %s must be exactly %d characters", field, targetLen)
			}
		case reflect.Slice, reflect.Array, reflect.Map:
			if val.Len() != targetLen {
				return fmt.Sprintf("field %s must contain exactly %d items", field, targetLen)
			}
		}

	case "oneof":
		if ruleParam == "" {
			return ""
		}
		if (val.Kind() == reflect.Pointer || val.Kind() == reflect.Interface) && val.IsNil() {
			return ""
		}
		if val.Kind() == reflect.String && val.String() == "" {
			return ""
		}
		var validOptions []string
		if strings.Contains(ruleParam, ",") {
			validOptions = strings.Split(ruleParam, ",")
		} else {
			validOptions = strings.Fields(ruleParam)
		}
		for i := range validOptions {
			validOptions[i] = strings.TrimSpace(validOptions[i])
		}

		actualVal := fmt.Sprintf("%v", val.Interface())
		found := false
		for _, opt := range validOptions {
			if opt == actualVal {
				found = true
				break
			}
		}
		if !found {
			return fmt.Sprintf("field %s must be one of [%s]", field, strings.Join(validOptions, ", "))
		}

	case "email":
		if val.Kind() == reflect.String {
			s := val.String()
			if s != "" {
				_, err := mail.ParseAddress(s)
				if err != nil || !strings.Contains(s, "@") || !strings.Contains(s, ".") {
					return fmt.Sprintf("field %s must be a valid email address", field)
				}
			}
		}

	case "url":
		if val.Kind() == reflect.String {
			s := val.String()
			if s != "" {
				u, err := url.ParseRequestURI(s)
				if err != nil || u.Scheme == "" || u.Host == "" {
					return fmt.Sprintf("field %s must be a valid URL", field)
				}
			}
		}

	case "uuid":
		if val.Kind() == reflect.String {
			s := val.String()
			if s != "" {
				matched := uuidPattern.MatchString(s)
				if !matched {
					return fmt.Sprintf("field %s must be a valid UUID", field)
				}
			}
		}
	}

	return ""
}

func isZero(val reflect.Value) bool {
	if !val.IsValid() {
		return true
	}
	switch val.Kind() {
	case reflect.String:
		return strings.TrimSpace(val.String()) == ""
	case reflect.Slice, reflect.Map:
		return val.IsNil() || val.Len() == 0
	case reflect.Pointer, reflect.Interface:
		return val.IsNil()
	default:
		return val.IsZero()
	}
}
