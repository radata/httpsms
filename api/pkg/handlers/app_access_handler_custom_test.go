package handlers

import (
	"testing"
)

// CUSTOM FILE — not upstream. Tests for app_access_handler_custom.go.

func TestAppAccessValidateCustom(t *testing.T) {
	h := &AppAccessHandlerCustom{}
	valid := AppAccessRequestCustom{Name: "Jane", GooglePlay: "jane@gmail.com"}

	if errors := h.validate(valid); len(errors) != 0 {
		t.Fatalf("expected no errors, got %v", errors)
	}

	cases := map[string]struct {
		request AppAccessRequestCustom
		field   string
	}{
		"empty name":           {AppAccessRequestCustom{GooglePlay: "jane@gmail.com"}, "name"},
		"header injection":     {AppAccessRequestCustom{Name: "Jane\r\nBcc: x@y.z", GooglePlay: "jane@gmail.com"}, "name"},
		"bad email":            {AppAccessRequestCustom{Name: "Jane", GooglePlay: "not-an-email"}, "google_play_email"},
		"display name in mail": {AppAccessRequestCustom{Name: "Jane", GooglePlay: "Jane <jane@gmail.com>"}, "google_play_email"},
		"long note":            {AppAccessRequestCustom{Name: "Jane", GooglePlay: "jane@gmail.com", Note: string(make([]byte, 1001))}, "note"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if errors := h.validate(tc.request); errors.Get(tc.field) == "" {
				t.Fatalf("expected an error on %q, got %v", tc.field, errors)
			}
		})
	}
}
