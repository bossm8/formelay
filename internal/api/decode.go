package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
)

// ErrFileUpload is returned when a multipart submission includes a file
// part — attachments are out of scope by design (see plan Security Model).
var ErrFileUpload = errors.New("api: file uploads are not accepted")

// decodeSubmission extracts submitted form fields, supporting
// application/x-www-form-urlencoded, multipart/form-data (text fields
// only), and application/json (flat string values only).
func decodeSubmission(w http.ResponseWriter, r *http.Request, maxBodyBytes int64) (map[string][]string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	contentType := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, fmt.Errorf("api: invalid Content-Type: %w", err)
	}

	switch mediaType {
	case "application/x-www-form-urlencoded":
		if err := r.ParseForm(); err != nil {
			return nil, fmt.Errorf("api: parse form: %w", err)
		}
		return map[string][]string(r.PostForm), nil

	case "multipart/form-data":
		if err := r.ParseMultipartForm(maxBodyBytes); err != nil {
			return nil, fmt.Errorf("api: parse multipart form: %w", err)
		}
		if r.MultipartForm != nil && len(r.MultipartForm.File) > 0 {
			return nil, ErrFileUpload
		}
		return map[string][]string(r.MultipartForm.Value), nil

	case "application/json":
		var raw map[string]string
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&raw); err != nil {
			return nil, fmt.Errorf("api: parse JSON body (flat string fields only): %w", err)
		}
		out := make(map[string][]string, len(raw))
		for k, v := range raw {
			out[k] = []string{v}
		}
		return out, nil

	default:
		return nil, fmt.Errorf("api: unsupported Content-Type %q", mediaType)
	}
}

// flatten takes the first value of each field for template/validation use.
func flatten(multi map[string][]string) map[string]string {
	out := make(map[string]string, len(multi))
	for k, v := range multi {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}
