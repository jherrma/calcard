package calendar

import (
	"encoding/json"
	"strings"
	"testing"
)

// SCOPE: P03. A calendar response must not carry its owner's numeric user id —
// a sharee would learn the owner's primary key with every shared calendar. The
// calendar's own id and uuid stay: they are the handles the web UI uses.
func TestCalendarDoesNotSerialiseOwnerUserID(t *testing.T) {
	b, err := json.Marshal(Calendar{ID: 7, UUID: "cal-uuid", UserID: 4242, Name: "Work"})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["user_id"]; ok {
		t.Errorf("Calendar JSON exposes user_id: %s", b)
	}
	if strings.Contains(string(b), "4242") {
		t.Errorf("Calendar JSON contains the owner's numeric id: %s", b)
	}
	for _, kept := range []string{"id", "uuid", "name"} {
		if _, ok := fields[kept]; !ok {
			t.Errorf("Calendar JSON lost %q, which the web UI relies on: %s", kept, b)
		}
	}
}
