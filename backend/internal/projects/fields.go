package projects

import (
	"bytes"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/validate"
)

// Fields is the full editable state of a project. nil means "empty".
type Fields struct {
	Name          *string
	ProfileID     *uuid.UUID
	ClientName    *string
	OrderID       *string
	ClientContact *string
	ProjectTypeID *uuid.UUID
	Scope         *string
	AgreedPrice   *string
	Currency      string
	StartDate     *string // YYYY-MM-DD
	Deadline      *string
	Priority      string
	SpecialNotes  *string
}

var Priorities = []string{"low", "medium", "high", "urgent"}

func defaultFields() Fields { return Fields{Currency: "USD", Priority: "medium"} }

func fieldsFromDetail(p db.ProjectDetail) Fields {
	return Fields{
		Name: p.Name, ProfileID: p.ProfileID, ClientName: p.ClientName, OrderID: p.OrderID,
		ClientContact: p.ClientContact, ProjectTypeID: p.ProjectTypeID, Scope: p.Scope,
		AgreedPrice: p.AgreedPrice, Currency: p.Currency,
		StartDate: p.StartDate, Deadline: p.Deadline, Priority: p.Priority, SpecialNotes: p.SpecialNotes,
	}
}

// fieldNames are the JSON keys accepted on create/update.
var fieldNames = []string{
	"name", "profile_id", "client_name", "order_id", "client_contact", "project_type_id", "scope",
	"agreed_price", "currency", "start_date", "deadline", "priority", "special_notes",
}

// Important changes need an acknowledgement from the assigned member (Phase 3).
var importantFields = map[string]bool{"scope": true, "deadline": true}

// applyPatch overlays a JSON object onto f. Present keys overwrite; null or "" clears.
// Unknown keys are rejected.
func applyPatch(f *Fields, raw map[string]json.RawMessage) error {
	v := validate.Errors{}
	str := func(key string, dst **string) {
		msg, ok := raw[key]
		if !ok {
			return
		}
		if bytes.Equal(bytes.TrimSpace(msg), []byte("null")) {
			*dst = nil
			return
		}
		var s string
		if json.Unmarshal(msg, &s) != nil {
			v.Add(key, "must be a string")
			return
		}
		if s = strings.TrimSpace(s); s == "" {
			*dst = nil
		} else {
			*dst = &s
		}
	}
	id := func(key string, dst **uuid.UUID) {
		var s *string
		str(key, &s)
		if _, ok := raw[key]; !ok {
			return
		}
		if s == nil {
			*dst = nil
			return
		}
		u, err := uuid.Parse(*s)
		if err != nil {
			v.Add(key, "is not a valid id")
			return
		}
		*dst = &u
	}
	// Money may arrive as a JSON number or string; keep it as text.
	money := func(key string, dst **string) {
		msg, ok := raw[key]
		if !ok {
			return
		}
		var n json.Number
		if err := json.Unmarshal(msg, &n); err == nil {
			s := n.String()
			*dst = &s
			return
		}
		str(key, dst)
	}

	for k := range raw {
		if !slices.Contains(fieldNames, k) && k != "lock_version" {
			v.Add(k, "is not a recognised field")
		}
	}
	str("name", &f.Name)
	id("profile_id", &f.ProfileID)
	str("client_name", &f.ClientName)
	str("order_id", &f.OrderID)
	str("client_contact", &f.ClientContact)
	id("project_type_id", &f.ProjectTypeID)
	str("scope", &f.Scope)
	money("agreed_price", &f.AgreedPrice)
	str("start_date", &f.StartDate)
	str("deadline", &f.Deadline)
	str("special_notes", &f.SpecialNotes)
	var cur, pri *string
	str("currency", &cur)
	str("priority", &pri)
	if _, ok := raw["currency"]; ok {
		f.Currency = strings.ToUpper(deref(cur))
	}
	if _, ok := raw["priority"]; ok {
		f.Priority = deref(pri)
	}
	if !v.OK() {
		return apperr.Validation(v)
	}
	return nil
}

var (
	moneyRe    = regexp.MustCompile(`^\d{1,10}(\.\d{1,2})?$`)
	currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)
)

// validateShape checks formats and lengths (not lookups or required-ness).
func validateShape(f Fields) validate.Errors {
	v := validate.Errors{}
	maxLen := func(key string, s *string, n int) {
		if s != nil {
			v.MaxLen(key, *s, n)
		}
	}
	maxLen("name", f.Name, 200)
	maxLen("client_name", f.ClientName, 200)
	maxLen("order_id", f.OrderID, 100)
	maxLen("client_contact", f.ClientContact, 500)
	maxLen("scope", f.Scope, 50000)
	maxLen("special_notes", f.SpecialNotes, 20000)
	for key, m := range map[string]*string{"agreed_price": f.AgreedPrice} {
		if m != nil && !moneyRe.MatchString(*m) {
			v.Add(key, "must be an amount like 1500 or 1500.50")
		}
	}
	if !currencyRe.MatchString(f.Currency) {
		v.Add("currency", "must be a 3-letter code like USD")
	}
	v.OneOf("priority", f.Priority, Priorities...)
	start, startOK := parseDate(v, "start_date", f.StartDate)
	end, endOK := parseDeadline(v, f.Deadline)
	if startOK && endOK && start.After(end) {
		v.Add("deadline", "must be on or after the start date")
	}
	return v
}

// requiredForSubmit lists what every project needs: it goes straight to its team when created.
func requiredForSubmit(f Fields) validate.Errors {
	v := validate.Errors{}
	req := func(key string, filled bool) {
		if !filled {
			v.Add(key, "is required")
		}
	}
	req("name", f.Name != nil)
	req("profile_id", f.ProfileID != nil)
	req("client_name", f.ClientName != nil)
	req("order_id", f.OrderID != nil)
	req("project_type_id", f.ProjectTypeID != nil)
	req("scope", f.Scope != nil)
	req("agreed_price", f.AgreedPrice != nil)
	req("deadline", f.Deadline != nil)
	return v
}

// The delivery deadline carries a time. A plain date means the end of that day, and the
// database renders it back with an offset, so all three forms are accepted.
const deadlineFormatHint = "must be a date and time (YYYY-MM-DDTHH:MM)"

var deadlineLayouts = []string{"2006-01-02T15:04", "2006-01-02T15:04:05", time.RFC3339, timestampLayout}

func parseDeadlineInput(s string, loc *time.Location) (time.Time, error) {
	if d, err := time.ParseInLocation(time.DateOnly, s, loc); err == nil {
		return d.Add(23*time.Hour + 59*time.Minute), nil // "by the end of that day"
	}
	var err error
	for _, layout := range deadlineLayouts {
		var t time.Time
		if t, err = time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, err
}

// parseDeadline validates the deadline field without needing the timezone (shape only).
func parseDeadline(v validate.Errors, s *string) (time.Time, bool) {
	if s == nil {
		return time.Time{}, false
	}
	t, err := parseDeadlineInput(*s, time.UTC)
	if err != nil {
		v.Add("deadline", deadlineFormatHint)
		return t, false
	}
	return t, true
}

func parseDate(v validate.Errors, key string, s *string) (time.Time, bool) {
	if s == nil {
		return time.Time{}, false
	}
	t, err := time.Parse(time.DateOnly, *s)
	if err != nil {
		v.Add(key, "must be a date (YYYY-MM-DD)")
		return t, false
	}
	return t, true
}

// change is one field-level difference, with display values.
type change struct {
	Field    string
	Old, New *string
}

func diff(a, b Fields) []change {
	var out []change
	add := func(field string, x, y *string) {
		if deref(x) != deref(y) {
			out = append(out, change{field, x, y})
		}
	}
	idStr := func(u *uuid.UUID) *string {
		if u == nil {
			return nil
		}
		s := u.String()
		return &s
	}
	add("name", a.Name, b.Name)
	add("profile_id", idStr(a.ProfileID), idStr(b.ProfileID))
	add("client_name", a.ClientName, b.ClientName)
	add("order_id", a.OrderID, b.OrderID)
	add("client_contact", a.ClientContact, b.ClientContact)
	add("project_type_id", idStr(a.ProjectTypeID), idStr(b.ProjectTypeID))
	add("scope", a.Scope, b.Scope)
	add("agreed_price", normMoney(a.AgreedPrice), normMoney(b.AgreedPrice))
	add("currency", &a.Currency, &b.Currency)
	add("start_date", a.StartDate, b.StartDate)
	add("deadline", a.Deadline, b.Deadline)
	add("priority", &a.Priority, &b.Priority)
	add("special_notes", a.SpecialNotes, b.SpecialNotes)
	return out
}

// normMoney makes "1500", "1500.0" and "1500.00" compare equal.
func normMoney(s *string) *string {
	if s == nil {
		return nil
	}
	whole, frac, _ := strings.Cut(*s, ".")
	frac = (frac + "00")[:2]
	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	out := whole + "." + frac
	return &out
}

// deref is the pointed-at value, or the zero value when there is nothing there.
func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}
