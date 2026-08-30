package render

import "time"

// FormMeta describes the form a submission was made against.
type FormMeta struct {
	ID          string
	DisplayName string
}

// RequestMeta carries per-request metadata available to every template.
type RequestMeta struct {
	RequestID     string
	Timestamp     time.Time
	SourceIP      string
	Origin        string
	SpamSuspected bool
	SpamReason    string
	SpamFilterErr string
}

// SubmissionData is the data made available to every template: delivery
// templates, spam-review templates, and the AI spam-filter prompt templates.
type SubmissionData struct {
	Form        FormMeta
	Fields      map[string]string
	FieldsMulti map[string][]string
	Meta        RequestMeta
}

// WithFieldsLimitedTo returns a copy of d whose Fields/FieldsMulti contain
// only the named fields (a present-but-unlisted field is dropped; a
// listed-but-absent name is simply skipped, not an error). Form and Meta
// are unchanged. An empty names list is a no-op, returning d unchanged,
// so callers can use this unconditionally without a separate branch for
// "no restriction configured".
//
// Used to build the AI spam classifier's view of a submission when
// spam_filter.include_fields is set, so PII fields never reach the
// classifier call, while the original d (used for delivery templates)
// keeps every field exactly as submitted.
func (d SubmissionData) WithFieldsLimitedTo(names []string) SubmissionData {
	if len(names) == 0 {
		return d
	}
	fields := make(map[string]string, len(names))
	fieldsMulti := make(map[string][]string, len(names))
	for _, name := range names {
		if v, ok := d.Fields[name]; ok {
			fields[name] = v
		}
		if v, ok := d.FieldsMulti[name]; ok {
			fieldsMulti[name] = v
		}
	}
	d.Fields = fields
	d.FieldsMulti = fieldsMulti
	return d
}
