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
