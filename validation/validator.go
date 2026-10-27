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
	if s == "required" || s == "email" || s == "url" || s == "uuid" || s == "nowarn" {
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

// parseMessageTag parses custom error messages from `msg` or `message` struct tags.
// Supports both a fallback message ("name is required") and rule-specific messages
// separated by "|" ("required:name is required|min:name is too short").
func parseMessageTag(tag string) map[string]string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return nil
	}
	msgs := make(map[string]string)
	for _, part := range strings.Split(tag, "|") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if idx := strings.Index(part, ":"); idx != -1 {
			rule := strings.TrimSpace(part[:idx])
			msg := strings.TrimSpace(part[idx+1:])
			if rule != "" && msg != "" {
				msgs[rule] = msg
			}
		} else {
			msgs["_"] = part
		}
	}
	return msgs
}

// resolveCustomMessage returns a customized error message if defined, falling back to defaultMsg.
func resolveCustomMessage(customMsgs map[string]string, ruleName, fullName, defaultMsg string) string {
	if len(customMsgs) == 0 {
		return defaultMsg
	}
	msg, ok := customMsgs[ruleName]
	if !ok {
		msg, ok = customMsgs["_"]
	}
	if !ok {
		return defaultMsg
	}
	if strings.Contains(msg, "{field}") {
		return strings.ReplaceAll(msg, "{field}", fullName)
	}
	return msg
}

// SilenceTagWarnings controls whether startup warning logs are printed for suspicious validation tags
// (such as validate:"required" on primitive bool or numeric fields).
var SilenceTagWarnings bool

// TagWarningLogger, if set, redirects tag warning messages. When nil, log.Printf is used by endpoint wrappers.
var TagWarningLogger func(warning string)

// CheckTags verifies that every `validate` tag on the struct type t only uses
// known rules with valid parameters, recursing into nested structs and slices.
// It is meant to run once at route registration so typos fail fast
// instead of silently disabling a rule.
func CheckTags(t reflect.Type) error {
	return checkTagsInternal(t, make(map[reflect.Type]bool))
}

func checkTagsInternal(t reflect.Type, seen map[reflect.Type]bool) error {
	if t == nil {
		return nil
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || isStdLibStruct(t) || seen[t] {
		return nil
	}
	seen[t] = true
	defer delete(seen, t)

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("validate")
		name := fieldIdentifier(field)

		// Anti-pattern check: using path:"id" instead of param:"id"
		if pathTag := field.Tag.Get("path"); pathTag != "" {
			return fmt.Errorf("field %q: tag `path:%q` is not supported; Gota uses `param:%q` for route path parameters", name, pathTag, pathTag)
		}

		msgTag := field.Tag.Get("msg")
		if msgTag == "" {
			msgTag = field.Tag.Get("message")
		}
		if msgTag != "" {
			if tag == "" {
				return fmt.Errorf("field %q: tag `msg` defined without a `validate` tag", name)
			}
			declaredRules := make(map[string]bool)
			for _, r := range splitRules(tag) {
				parts := strings.SplitN(strings.TrimSpace(r), "=", 2)
				declaredRules[parts[0]] = true
			}
			customMsgs := parseMessageTag(msgTag)
			for ruleKey := range customMsgs {
				if ruleKey == "_" {
					continue
				}
				if !isKnownRule(ruleKey) {
					return fmt.Errorf("field %q: unknown rule %q in `msg` tag", name, ruleKey)
				}
				if !declaredRules[ruleKey] {
					return fmt.Errorf("field %q: `msg` specifies message for rule %q, but rule is not present in `validate:%q`", name, ruleKey, tag)
				}
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
				case "required", "email", "url", "uuid", "nowarn", "omitempty":
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
					switch parts[0] {
					case "gte":
						param := ""
						if len(parts) > 1 {
							param = "=" + parts[1]
						}
						return fmt.Errorf("field %q: unknown validation rule 'gte'; did you mean 'min%s'?", name, param)
					case "gt":
						param := ""
						if len(parts) > 1 {
							param = "=" + parts[1]
						}
						return fmt.Errorf("field %q: unknown validation rule 'gt'; did you mean 'min%s'?", name, param)
					case "lte":
						param := ""
						if len(parts) > 1 {
							param = "=" + parts[1]
						}
						return fmt.Errorf("field %q: unknown validation rule 'lte'; did you mean 'max%s'?", name, param)
					case "lt":
						param := ""
						if len(parts) > 1 {
							param = "=" + parts[1]
						}
						return fmt.Errorf("field %q: unknown validation rule 'lt'; did you mean 'max%s'?", name, param)
					case "length":
						param := ""
						if len(parts) > 1 {
							param = "=" + parts[1]
						}
						return fmt.Errorf("field %q: unknown validation rule 'length'; did you mean 'len%s'?", name, param)
					case "dive":
						return fmt.Errorf("field %q: rule 'dive' is not needed; Gota automatically validates nested structs and slices", name)
					case "isdefault":
						return fmt.Errorf("field %q: rule 'isdefault' is not needed; in Gota, fields are optional by default", name)
					default:
						return fmt.Errorf("field %q: unknown validation rule %q", name, parts[0])
					}
				}
			}
		}

		// Recurse into nested struct or slice of structs
		ft := unwrapStructType(field.Type)
		if ft.Kind() == reflect.Struct && !isStdLibStruct(ft) {
			if err := checkTagsInternal(ft, seen); err != nil {
				return err
			}
		}
	}
	return nil
}

// CheckTagWarnings inspects struct type t for suspicious validation tags, returning
// non-fatal warning messages. In particular, it warns when validate:"required" is placed
// on non-pointer bool or numeric fields where Go's zero values (false, 0) fail required validation.
// A warning on a specific field can be silenced with validate:"...,nowarn" or gota:"nowarn",
// or globally via gota.SilenceTagWarnings(true).
func CheckTagWarnings(t reflect.Type) []string {
	var warnings []string
	checkTagWarningsInternal(t, make(map[reflect.Type]bool), nil, &warnings)
	return warnings
}

func checkTagWarningsInternal(t reflect.Type, seen map[reflect.Type]bool, shadowed map[string]bool, warnings *[]string) {
	if t == nil {
		return
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || isStdLibStruct(t) || seen[t] {
		return
	}
	seen[t] = true
	defer delete(seen, t)

	mergedShadowed := collectShadowedFields(t, shadowed)

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if _, ok := core.EmbeddedStructType(field); ok {
			continue
		}
		if !field.IsExported() {
			continue
		}
		name := fieldIdentifier(field)
		if shadowed[name] {
			continue
		}

		suppressed := field.Tag.Get("gota") == "nowarn"
		tag := field.Tag.Get("validate")
		hasRequired := false
		hasOmitempty := false
		if tag != "" {
			for _, rule := range splitRules(tag) {
				rule = strings.TrimSpace(rule)
				if rule == "nowarn" {
					suppressed = true
				}
				if rule == "required" {
					hasRequired = true
				}
				if rule == "omitempty" {
					hasOmitempty = true
				}
			}
		}

		typeName := t.Name()
		if typeName == "" {
			typeName = "struct"
		}

		if hasOmitempty && !suppressed {
			msg := fmt.Sprintf(
				"field %q in %s uses validate:\"omitempty\"; in Gota, all fields are optional by default unless marked with \"required\". You can safely remove it.",
				name, typeName,
			)
			*warnings = append(*warnings, msg)
		}

		if hasRequired && !suppressed && isPrimitiveRejectingZero(field.Type) {
			kindStr := field.Type.String()
			zeroVal := zeroValueString(field.Type)
			msg := fmt.Sprintf(
				"field %q in %s is a non-pointer %s with validate:\"required\"; in Go, zero value (%s) will fail validation. Use *%s if zero values are allowed, or silence with gota.SilenceTagWarnings(true).",
				name, typeName, kindStr, zeroVal, kindStr,
			)
			*warnings = append(*warnings, msg)
		}

		ft := unwrapStructType(field.Type)
		if ft.Kind() == reflect.Struct && !isStdLibStruct(ft) {
			checkTagWarningsInternal(ft, seen, nil, warnings)
		}
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		ft, ok := core.EmbeddedStructType(field)
		if !ok {
			continue
		}
		checkTagWarningsInternal(ft, seen, mergedShadowed, warnings)
	}
}

func isPrimitiveRejectingZero(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

func zeroValueString(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Bool:
		return "false"
	default:
		return "0"
	}
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
		validateStructRecursive(ctx, val, "", errMap, nil, make(map[reflect.Type]bool))
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

func fieldIdentifier(field reflect.StructField) string {
	if formTag := field.Tag.Get("form"); formTag != "" && formTag != "-" {
		parts := strings.Split(formTag, ",")
		if parts[0] != "" {
			return parts[0]
		}
	} else if jsonTag := field.Tag.Get("json"); jsonTag != "" && jsonTag != "-" {
		parts := strings.Split(jsonTag, ",")
		if parts[0] != "" {
			return parts[0]
		}
	} else if paramTag := field.Tag.Get("param"); paramTag != "" {
		return paramTag
	} else if queryTag := field.Tag.Get("query"); queryTag != "" {
		return queryTag
	} else if headerTag := field.Tag.Get("header"); headerTag != "" {
		return headerTag
	}
	return field.Name
}

func validateStructRecursive(ctx *core.Context, val reflect.Value, prefix string, errMap map[string][]string, shadowed map[string]bool, embedding map[reflect.Type]bool) {
	if val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return
		}
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct || isStdLibStruct(val.Type()) {
		return
	}

	t := val.Type()
	if embedding[t] {
		return
	}
	embedding[t] = true
	defer delete(embedding, t)

	mergedShadowed := collectShadowedFields(t, shadowed)

	// 1. Validate own/direct fields
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if _, ok := core.EmbeddedStructType(field); ok {
			continue
		}
		// Unexported fields cannot be read via reflection (val.Interface() would panic).
		if !field.IsExported() {
			continue
		}
		fieldName := fieldIdentifier(field)
		if shadowed[fieldName] {
			continue
		}
		fullName := joinPrefix(prefix, fieldName)
		fieldVal := val.Field(i)

		validateTag := field.Tag.Get("validate")
		msgTag := field.Tag.Get("msg")
		if msgTag == "" {
			msgTag = field.Tag.Get("message")
		}
		customMsgs := parseMessageTag(msgTag)

		if validateTag != "" {
			for _, rule := range splitRules(validateTag) {
				rule = strings.TrimSpace(rule)
				if rule == "" {
					continue
				}

				if err := applyValidationRule(fullName, fieldVal, rule); err != "" {
					ruleName := strings.SplitN(rule, "=", 2)[0]
					err = resolveCustomMessage(customMsgs, ruleName, fullName, err)
					errMap[fullName] = append(errMap[fullName], err)
				}
			}
		}

		// Recurse into nested struct (not embedded) or slice of structs
		ft := field.Type
		if ft.Kind() == reflect.Pointer {
			if !fieldVal.IsNil() {
				validateStructRecursive(ctx, fieldVal.Elem(), fullName, errMap, nil, embedding)
			}
		} else if ft.Kind() == reflect.Struct && !isStdLibStruct(ft) {
			validateStructRecursive(ctx, fieldVal, fullName, errMap, nil, embedding)
		} else if ft.Kind() == reflect.Slice {
			elemType := ft.Elem()
			if elemType.Kind() == reflect.Pointer {
				elemType = elemType.Elem()
			}
			if elemType.Kind() == reflect.Struct && !isStdLibStruct(elemType) {
				for idx := 0; idx < fieldVal.Len(); idx++ {
					item := fieldVal.Index(idx)
					itemPrefix := fmt.Sprintf("%s[%d]", fullName, idx)
					validateStructRecursive(ctx, item, itemPrefix, errMap, nil, embedding)
				}
			}
		}
	}

	// 2. Validate promoted fields from embedded structs with the same prefix
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		ft, ok := core.EmbeddedStructType(field)
		if !ok {
			continue
		}
		fieldVal := val.Field(i)
		if fieldVal.Kind() == reflect.Pointer {
			if fieldVal.IsNil() {
				validateNilEmbedded(ft, prefix, errMap, mergedShadowed)
				continue
			}
			validateStructRecursive(ctx, fieldVal.Elem(), prefix, errMap, mergedShadowed, embedding)
			continue
		}
		validateStructRecursive(ctx, fieldVal, prefix, errMap, mergedShadowed, embedding)
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

// validateNilEmbedded checks whether any unshadowed fields of an absent embedded struct
// are marked required, reporting "field <name> is required" with the correct promoted name.
func validateNilEmbedded(ft reflect.Type, prefix string, errMap map[string][]string, shadowed map[string]bool) {
	for i := 0; i < ft.NumField(); i++ {
		f := ft.Field(i)
		if subType, ok := core.EmbeddedStructType(f); ok {
			validateNilEmbedded(subType, prefix, errMap, shadowed)
			continue
		}
		if !f.IsExported() {
			continue
		}
		name := fieldIdentifier(f)
		if shadowed[name] {
			continue
		}
		fullName := joinPrefix(prefix, name)
		tag := f.Tag.Get("validate")
		msgTag := f.Tag.Get("msg")
		if msgTag == "" {
			msgTag = f.Tag.Get("message")
		}
		customMsgs := parseMessageTag(msgTag)
		if tag != "" {
			for _, rule := range splitRules(tag) {
				if strings.TrimSpace(rule) == "required" {
					msg := fmt.Sprintf("field %s is required", fullName)
					msg = resolveCustomMessage(customMsgs, "required", fullName, msg)
					errMap[fullName] = append(errMap[fullName], msg)
				}
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
	case "nowarn", "omitempty":
		return ""
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
		if num, unit, ok := toComparableValue(val); ok && num < minVal {
			if unit == " items" {
				return fmt.Sprintf("field %s must contain at least %v items", field, minVal)
			}
			return fmt.Sprintf("field %s must be at least %v%s", field, minVal, unit)
		}

	case "max":
		if ruleParam == "" {
			return ""
		}
		maxVal, err := strconv.ParseFloat(ruleParam, 64)
		if err != nil {
			return ""
		}
		if num, unit, ok := toComparableValue(val); ok && num > maxVal {
			if unit == " items" {
				return fmt.Sprintf("field %s must not contain more than %v items", field, maxVal)
			}
			return fmt.Sprintf("field %s must not exceed %v%s", field, maxVal, unit)
		}

	case "len":
		if ruleParam == "" {
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

// unwrapStructType unwraps pointers and slice elements down to the underlying type.
func unwrapStructType(t reflect.Type) reflect.Type {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() == reflect.Slice {
		t = t.Elem()
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
	}
	return t
}

// collectShadowedFields records direct field identifiers (skipping embedded structs)
// and merges them with shadowed fields to prevent outer fields from being overridden.
func collectShadowedFields(t reflect.Type, shadowed map[string]bool) map[string]bool {
	merged := make(map[string]bool, len(shadowed)+t.NumField())
	for k, v := range shadowed {
		merged[k] = v
	}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if _, ok := core.EmbeddedStructType(field); ok {
			continue
		}
		merged[fieldIdentifier(field)] = true
	}
	return merged
}

// joinPrefix concatenates a prefix and a field name using dot notation.
func joinPrefix(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

// toComparableValue extracts a float64 representation and message unit from comparable kinds.
func toComparableValue(val reflect.Value) (float64, string, bool) {
	switch val.Kind() {
	case reflect.String:
		return float64(utf8.RuneCountInString(val.String())), " characters", true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(val.Uint()), "", true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(val.Int()), "", true
	case reflect.Float32, reflect.Float64:
		return val.Float(), "", true
	case reflect.Slice:
		return float64(val.Len()), " items", true
	default:
		return 0, "", false
	}
}
