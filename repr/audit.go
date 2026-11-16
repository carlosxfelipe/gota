package repr

import (
	"fmt"
	"log"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
)

var loggedSensitiveWarnings sync.Map

// IsSensitiveFieldName reports whether a struct field name strongly resembles a sensitive secret
// that should generally not be exposed in JSON or OpenAPI contracts.
func IsSensitiveFieldName(name string) (bool, string) {
	lower := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(name, "_", ""), "-", ""))
	switch {
	case lower == "password" || lower == "passwd":
		return true, "plaintext password"
	case strings.Contains(lower, "passwordhash") || strings.Contains(lower, "passwdhash") || strings.Contains(lower, "pwdhash") || strings.Contains(lower, "passhash"):
		return true, "password hash"
	case strings.Contains(lower, "passwordsalt") || lower == "salt":
		return true, "cryptographic salt"
	case strings.Contains(lower, "clientsecret") || strings.Contains(lower, "apisecret") || strings.Contains(lower, "jwtsecret") || lower == "secret":
		return true, "secret key"
	case strings.Contains(lower, "privatekey") || strings.Contains(lower, "signingkey") || strings.Contains(lower, "encryptionkey") || strings.Contains(lower, "masterkey"):
		return true, "private/cryptographic key"
	case strings.Contains(lower, "resettoken") || strings.Contains(lower, "refreshtoken") || strings.Contains(lower, "authtoken") || strings.Contains(lower, "sessiontoken") || strings.Contains(lower, "bearertoken"):
		return true, "authentication/session token"
	case lower == "cvv" || lower == "cvc" || lower == "cardcvv" || lower == "cardpin" || lower == "securitycode" || lower == "cardsecuritycode":
		return true, "payment security code"
	default:
		return false, ""
	}
}

// AuditModel inspects a struct model (recursing into embedded structs) and returns warning messages
// for any exported fields that appear to expose sensitive secrets without `json:"-"` or `form:"-"`.
func AuditModel(model any) []string {
	if model == nil {
		return nil
	}
	t := reflect.TypeOf(model)
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	var warnings []string
	auditModelRecursive(t, make(map[reflect.Type]bool), &warnings)
	return warnings
}

func auditModelRecursive(t reflect.Type, seen map[reflect.Type]bool, warnings *[]string) {
	if seen[t] {
		return
	}
	seen[t] = true
	defer delete(seen, t)

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if ft, ok := embeddedStructType(f); ok {
			if f.Tag.Get("json") != "-" && f.Tag.Get("form") != "-" {
				auditModelRecursive(ft, seen, warnings)
			}
			continue
		}
		if !f.IsExported() {
			continue
		}
		if f.Tag.Get("json") == "-" || f.Tag.Get("form") == "-" {
			continue
		}
		if !hasValidateRule(f.Tag.Get("validate"), "nowarn") {
			if isSens, reason := IsSensitiveFieldName(f.Name); isSens {
				*warnings = append(*warnings, fmt.Sprintf(
					"field %q in struct %s appears to contain a %s and lacks `json:\"-\"`",
					f.Name, t.Name(), reason,
				))
			}
		}
		ft := f.Type
		for ft.Kind() == reflect.Pointer || ft.Kind() == reflect.Slice {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct && ft != timeType && ft != fileHeaderType {
			auditModelRecursive(ft, seen, warnings)
		}
	}
}

func warnIfSensitiveField(structType reflect.Type, field reflect.StructField, silence bool) {
	if silence || hasValidateRule(field.Tag.Get("validate"), "nowarn") {
		return
	}
	if isSens, reason := IsSensitiveFieldName(field.Name); isSens {
		warnKey := fmt.Sprintf("%s.%s", structType.String(), field.Name)
		if _, loaded := loggedSensitiveWarnings.LoadOrStore(warnKey, true); !loaded {
			log.Printf("gota [SECURITY]: field %q in struct %s appears to contain a %s without `json:\"-\"`", field.Name, structType.String(), reason)
		}
	}
}

// AuditRoutes scans all documented responses of registered routes for sensitive fields
// inadvertently exposed in API contracts without `json:"-"` or `form:"-"`.
func (a *App) AuditRoutes() []string {
	var warnings []string
	seenModels := make(map[reflect.Type]bool)
	for _, rt := range a.routes {
		if rt.doc == nil || rt.doc.Hidden {
			continue
		}
		for status, resp := range rt.doc.Responses {
			if resp == nil || resp.Model == nil {
				continue
			}
			t := reflect.TypeOf(resp.Model)
			for t != nil && (t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice) {
				t = t.Elem()
			}
			if t == nil || t.Kind() != reflect.Struct || seenModels[t] {
				continue
			}
			seenModels[t] = true
			for _, w := range AuditModel(resp.Model) {
				warnings = append(warnings, fmt.Sprintf("%s %s (status %d): %s", strings.ToUpper(rt.method), rt.path, status, w))
			}
		}
	}
	return warnings
}

// SanitizeFilename strips directory paths, control characters, and null bytes from a user-supplied
// filename, returning an empty string if the name is unsafe or hidden.
func SanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, ".") {
		return ""
	}
	return name
}
