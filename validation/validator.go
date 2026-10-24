package validation

import (
	"fmt"
	"net/mail"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/carlosxfelipe/gota/core"
)

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

// CheckTags verifies that every `validate` tag on the struct type t only uses
// known rules with valid parameters. Non-struct types are ignored.
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

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("validate")
		if tag == "" {
			continue
		}

		name := field.Name
		if jsonTag := field.Tag.Get("json"); jsonTag != "" && jsonTag != "-" {
			if n := strings.Split(jsonTag, ",")[0]; n != "" {
				name = n
			}
		}

		for _, rule := range strings.Split(tag, ",") {
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
			default:
				return fmt.Errorf("field %q: unknown validation rule %q", name, parts[0])
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

	// 1. Tag-based validation
	if val.Kind() == reflect.Struct {
		typ := val.Type()
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			fieldVal := val.Field(i)
			validateTag := field.Tag.Get("validate")
			if validateTag == "" {
				continue
			}

			fieldName := field.Name
			if jsonTag := field.Tag.Get("json"); jsonTag != "" && jsonTag != "-" {
				parts := strings.Split(jsonTag, ",")
				if parts[0] != "" {
					fieldName = parts[0]
				}
			}

			rules := strings.Split(validateTag, ",")
			for _, rule := range rules {
				rule = strings.TrimSpace(rule)
				if rule == "" {
					continue
				}

				if err := applyValidationRule(fieldName, fieldVal, rule); err != "" {
					errMap[fieldName] = append(errMap[fieldName], err)
				}
			}
		}
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

func applyValidationRule(field string, val reflect.Value, rule string) string {
	parts := strings.SplitN(rule, "=", 2)
	ruleName := parts[0]
	ruleParam := ""
	if len(parts) > 1 {
		ruleParam = parts[1]
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
				matched, _ := regexp.MatchString(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`, s)
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
