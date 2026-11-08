// Package validate collects field-level input errors for API responses.
package validate

import (
	"net/mail"
	"strings"
	"unicode/utf8"
)

// Errors maps a field name to a human-readable message.
type Errors map[string]string

func (e Errors) Add(field, msg string) {
	if _, exists := e[field]; !exists {
		e[field] = msg
	}
}

func (e Errors) OK() bool { return len(e) == 0 }

func (e Errors) Required(field, value string) {
	if strings.TrimSpace(value) == "" {
		e.Add(field, "is required")
	}
}

func (e Errors) MaxLen(field, value string, n int) {
	if utf8.RuneCountInString(value) > n {
		e.Add(field, "is too long")
	}
}

func (e Errors) Len(field, value string, minLen, maxLen int) {
	n := utf8.RuneCountInString(value)
	if n < minLen {
		e.Add(field, "is too short")
	} else if n > maxLen {
		e.Add(field, "is too long")
	}
}

func (e Errors) Email(field, value string) {
	addr, err := mail.ParseAddress(value)
	if err != nil || addr.Address != value || len(value) > 254 {
		e.Add(field, "must be a valid email address")
	}
}

func (e Errors) OneOf(field, value string, allowed ...string) {
	for _, a := range allowed {
		if value == a {
			return
		}
	}
	e.Add(field, "must be one of: "+strings.Join(allowed, ", "))
}
