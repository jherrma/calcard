package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCalendarHandler_Kind pins `kind` on the calendar routes (#170):
// POST /calendars takes an optional kind and stores the matching
// supported_components, an unknown kind is a 400 that stores nothing, and the
// create, get, update and list responses all carry a read-only kind next to
// the unchanged supported_components.
func TestCalendarHandler_Kind(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	ctx := context.Background()
	userRepo := repository.NewUserRepository(db.DB())
	calRepo := repository.NewCalendarRepository(db.DB())
	shareRepo := repository.NewCalendarShareRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	mkUser := func(name string) (*user.User, string) {
		u := &user.User{
			Email: name + "@example.com", Username: name, PasswordHash: "hash",
			IsActive: true, EmailVerified: true, UUID: name + "-uuid", DisplayName: name,
		}
		require.NoError(t, userRepo.Create(ctx, u))
		token, _, err := jwtManager.GenerateAccessToken(u.UUID, u.Email)
		require.NoError(t, err)
		return u, token
	}
	owner, token := mkUser("kindowner")
	other, _ := mkUser("kindother")

	call := func(method, path, body string) (int, map[string]any, string) {
		var reader io.Reader
		if body != "" {
			reader = bytes.NewReader([]byte(body))
		}
		req := httptest.NewRequest(method, "/api/v1/calendars"+path, reader)
		req.Header.Set("Authorization", "Bearer "+token)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := app.Test(req)
		require.NoError(t, err)
		raw, _ := io.ReadAll(resp.Body)
		var fields map[string]any
		_ = json.Unmarshal(raw, &fields)
		return resp.StatusCode, fields, string(raw)
	}
	countOwned := func() int64 {
		n, err := calRepo.CountByUserID(ctx, owner.ID)
		require.NoError(t, err)
		return n
	}

	created := map[string]string{} // kind -> calendar UUID
	for _, tc := range []struct {
		name, body, stored, kind string
	}{
		{"tasks", `{"name":"Errands","kind":"tasks"}`, "VTODO", "tasks"},
		{"events", `{"name":"Work","kind":"events"}`, "VEVENT", "events"},
		{"both", `{"name":"Mixed","kind":"both"}`, "VEVENT,VTODO", "both"},
		{"absent", `{"name":"Plain"}`, "VEVENT", "events"},
		{"empty", `{"name":"Empty","kind":""}`, "VEVENT", "events"},
	} {
		t.Run("create "+tc.name, func(t *testing.T) {
			status, fields, raw := call(http.MethodPost, "", tc.body)
			require.Equal(t, fiber.StatusCreated, status, raw)
			assert.Equal(t, tc.kind, fields["kind"], raw)
			assert.Equal(t, tc.stored, fields["supported_components"], raw)

			uuid, _ := fields["uuid"].(string)
			require.NotEmpty(t, uuid, raw)
			stored, err := calRepo.GetByUUID(ctx, uuid)
			require.NoError(t, err)
			assert.Equal(t, tc.stored, stored.SupportedComponents, "stored set")
			created[tc.name] = uuid
		})
	}

	t.Run("create with an unknown kind is a 400 and stores nothing", func(t *testing.T) {
		before := countOwned()
		for _, body := range []string{
			`{"name":"X","kind":"todo"}`,
			`{"name":"X","kind":"Tasks"}`,
			`{"name":"X","kind":"VTODO"}`,
			`{"name":"X","kind":"journal"}`,
		} {
			status, fields, raw := call(http.MethodPost, "", body)
			assert.Equal(t, fiber.StatusBadRequest, status, body)
			assert.Equal(t, map[string]any{
				"status":  "error",
				"message": `kind must be "events", "tasks" or "both"`,
			}, fields, raw)
		}
		assert.Equal(t, before, countOwned(), "no calendar may have been created")

		// A kind of the wrong JSON type never reaches the use case.
		status, _, raw := call(http.MethodPost, "", `{"name":"X","kind":1}`)
		assert.Equal(t, fiber.StatusBadRequest, status, raw)
		assert.Equal(t, before, countOwned())
	})

	t.Run("get carries kind", func(t *testing.T) {
		for name, kind := range map[string]string{"tasks": "tasks", "events": "events", "both": "both", "absent": "events"} {
			status, fields, raw := call(http.MethodGet, "/"+created[name], "")
			require.Equal(t, fiber.StatusOK, status, raw)
			assert.Equal(t, kind, fields["kind"], name)
			assert.Equal(t, created[name], fields["uuid"], name)
			assert.Contains(t, fields, "supported_components", "kept for compatibility")
		}
	})

	t.Run("update carries kind and does not change it", func(t *testing.T) {
		status, fields, raw := call(http.MethodPatch, "/"+created["tasks"], `{"name":"Chores"}`)
		require.Equal(t, fiber.StatusOK, status, raw)
		assert.Equal(t, "Chores", fields["name"])
		assert.Equal(t, "tasks", fields["kind"])
		assert.Equal(t, "VTODO", fields["supported_components"])
	})

	t.Run("list carries kind for owned, legacy and shared calendars", func(t *testing.T) {
		// A pre-#170 row with an empty set, and another user's task list shared
		// with the caller.
		require.NoError(t, calRepo.Create(ctx, &calendar.Calendar{
			UUID: "legacy-kind-cal", UserID: owner.ID, Name: "Legacy", Path: "legacy.ics",
			Timezone: "UTC", Color: "#112233", SupportedComponents: "",
		}))
		sharedList := &calendar.Calendar{
			UUID: "shared-kind-list", UserID: other.ID, Name: "Family chores", Path: "chores.ics",
			Timezone: "UTC", Color: "#445566", SupportedComponents: "VTODO",
		}
		require.NoError(t, calRepo.Create(ctx, sharedList))
		require.NoError(t, shareRepo.Create(ctx, &sharing.CalendarShare{
			UUID: "kind-share", CalendarID: sharedList.ID, SharedWithID: owner.ID, Permission: "read-write",
		}))

		status, _, raw := call(http.MethodGet, "", "")
		require.Equal(t, fiber.StatusOK, status, raw)
		var body struct {
			Calendars []map[string]any `json:"calendars"`
		}
		require.NoError(t, json.Unmarshal([]byte(raw), &body))

		got := map[string]string{}
		for _, cal := range body.Calendars {
			uuid, _ := cal["uuid"].(string)
			kind, _ := cal["kind"].(string)
			got[uuid] = kind
			assert.Contains(t, cal, "supported_components", uuid)
			assert.Contains(t, cal, "event_count", uuid)
			assert.NotContains(t, cal, "task_count", uuid)
		}
		assert.Equal(t, map[string]string{
			created["tasks"]:   "tasks",
			created["events"]:  "events",
			created["both"]:    "both",
			created["absent"]:  "events",
			created["empty"]:   "events",
			"legacy-kind-cal":  "both",
			"shared-kind-list": "tasks",
		}, got)
	})
}

// TestCalendarHandler_WidenOnlyKind pins `kind` on PATCH /calendars/:id (#170):
// "both" widens an event calendar or a task list (200, stored "VEVENT,VTODO",
// ctag, sync token and change log advance); the stored kind is a 200 that
// writes nothing; every narrowing is a 409 with the one message, whether or not
// the calendar holds objects of the dropped component, and writes nothing; an
// unknown kind is the create 400; a calendar the caller does not own stays the
// usual 404 whatever the kind.
func TestCalendarHandler_WidenOnlyKind(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	ctx := context.Background()
	gdb := db.DB()
	userRepo := repository.NewUserRepository(gdb)
	calRepo := repository.NewCalendarRepository(gdb)
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	mkUser := func(name string) (*user.User, string) {
		u := &user.User{
			Email: name + "@example.com", Username: name, PasswordHash: "hash",
			IsActive: true, EmailVerified: true, UUID: name + "-uuid", DisplayName: name,
		}
		require.NoError(t, userRepo.Create(ctx, u))
		token, _, err := jwtManager.GenerateAccessToken(u.UUID, u.Email)
		require.NoError(t, err)
		return u, token
	}
	owner, ownerToken := mkUser("widenowner")
	_, strangerToken := mkUser("widenstranger")

	call := func(token, method, path, body string) (int, map[string]any, string) {
		var reader io.Reader
		if body != "" {
			reader = bytes.NewReader([]byte(body))
		}
		req := httptest.NewRequest(method, "/api/v1/calendars/"+path, reader)
		req.Header.Set("Authorization", "Bearer "+token)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := app.Test(req)
		require.NoError(t, err)
		raw, _ := io.ReadAll(resp.Body)
		var fields map[string]any
		_ = json.Unmarshal(raw, &fields)
		return resp.StatusCode, fields, string(raw)
	}

	seq := 0
	mkCalendar := func(set string) *calendar.Calendar {
		seq++
		uid := fmt.Sprintf("widen-cal-%d", seq)
		cal := &calendar.Calendar{
			UUID: uid, UserID: owner.ID, Name: uid, Path: uid + ".ics",
			Timezone: "UTC", Color: "#112233", SupportedComponents: set,
		}
		require.NoError(t, calRepo.Create(ctx, cal))
		return cal
	}
	store := func(cal *calendar.Calendar, comp string) {
		seq++
		uid := fmt.Sprintf("widen-obj-%d", seq)
		body := "BEGIN:" + comp + "\r\nUID:" + uid + "\r\nDTSTAMP:20300101T000000Z\r\n" +
			"DTSTART:20300310T100000Z\r\nSUMMARY:x\r\nEND:" + comp + "\r\n"
		obj := &calendar.CalendarObject{
			UUID: uid + "-uuid", CalendarID: cal.ID, UID: uid, Path: uid + ".ics", ETag: calendar.NewETag(),
			ICalData: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Test//EN\r\n" + body + "END:VCALENDAR\r\n",
		}
		require.NoError(t, obj.PopulateDenormFieldsFromICal())
		require.NoError(t, calRepo.CreateCalendarObject(ctx, obj))
	}

	// snapshot is everything a refused or no-op PATCH must leave alone.
	type snapshot struct {
		cal      calendar.Calendar
		logCount int64
	}
	snap := func(cal *calendar.Calendar) snapshot {
		var s snapshot
		require.NoError(t, gdb.First(&s.cal, cal.ID).Error)
		require.NoError(t, gdb.Model(&calendar.SyncChangeLog{}).Where("calendar_id = ?", cal.ID).Count(&s.logCount).Error)
		return s
	}
	assertUnchanged := func(t *testing.T, before snapshot, cal *calendar.Calendar, msg string) {
		t.Helper()
		after := snap(cal)
		assert.Equal(t, before.cal.SupportedComponents, after.cal.SupportedComponents, msg+": component set")
		assert.Equal(t, before.cal.CTag, after.cal.CTag, msg+": ctag")
		assert.Equal(t, before.cal.SyncToken, after.cal.SyncToken, msg+": sync token")
		assert.Equal(t, before.cal.Name, after.cal.Name, msg+": name")
		assert.Equal(t, before.logCount, after.logCount, msg+": change log")
	}

	t.Run("both widens an event calendar and a task list", func(t *testing.T) {
		for _, stored := range []string{"VEVENT", "VTODO"} {
			cal := mkCalendar(stored)
			store(cal, stored) // a calendar in use, not an empty one
			before := snap(cal)

			status, fields, raw := call(ownerToken, http.MethodPatch, cal.UUID, `{"kind":"both"}`)
			require.Equal(t, fiber.StatusOK, status, raw)
			assert.Equal(t, "both", fields["kind"], raw)
			assert.Equal(t, "VEVENT,VTODO", fields["supported_components"], raw)
			assert.Equal(t, cal.Name, fields["name"], "nothing else changes")

			after := snap(cal)
			assert.Equal(t, "VEVENT,VTODO", after.cal.SupportedComponents, "stored")
			assert.NotEqual(t, before.cal.CTag, after.cal.CTag, "the ctag advances")
			assert.NotEqual(t, before.cal.SyncToken, after.cal.SyncToken, "the sync token advances")
			assert.Equal(t, before.logCount+1, after.logCount, "one change-log anchor")

			status, fields, raw = call(ownerToken, http.MethodGet, cal.UUID, "")
			require.Equal(t, fiber.StatusOK, status, raw)
			assert.Equal(t, "both", fields["kind"], "GET reports the widened kind")
		}
	})

	t.Run("a rename and a widening in one request", func(t *testing.T) {
		cal := mkCalendar("VTODO")
		status, fields, raw := call(ownerToken, http.MethodPatch, cal.UUID, `{"name":"Home","kind":"both"}`)
		require.Equal(t, fiber.StatusOK, status, raw)
		assert.Equal(t, "Home", fields["name"])
		assert.Equal(t, "both", fields["kind"])
		after := snap(cal)
		assert.Equal(t, "Home", after.cal.Name)
		assert.Equal(t, "VEVENT,VTODO", after.cal.SupportedComponents)
	})

	t.Run("the stored kind is a no-op", func(t *testing.T) {
		for _, tc := range []struct{ stored, kind string }{
			{"VEVENT", "events"},
			{"VTODO", "tasks"},
			{"VEVENT,VTODO", "both"},
			{"", "both"}, // a legacy row stores both and is not rewritten
		} {
			cal := mkCalendar(tc.stored)
			before := snap(cal)
			status, fields, raw := call(ownerToken, http.MethodPatch, cal.UUID, `{"kind":"`+tc.kind+`"}`)
			require.Equal(t, fiber.StatusOK, status, raw)
			assert.Equal(t, tc.kind, fields["kind"], raw)
			assert.Equal(t, tc.stored, fields["supported_components"], raw)
			assertUnchanged(t, before, cal, tc.stored+" PATCH "+tc.kind)
		}
	})

	t.Run("every narrowing is a 409 and writes nothing", func(t *testing.T) {
		want := map[string]any{"status": "error", "message": "Cannot narrow a calendar's component set"}
		for _, tc := range []struct {
			stored, kind string
			holds        []string // objects in the calendar
		}{
			{"VEVENT,VTODO", "events", nil},
			{"VEVENT,VTODO", "events", []string{"VEVENT", "VTODO"}},
			{"VEVENT,VTODO", "tasks", nil},
			{"VEVENT,VTODO", "tasks", []string{"VEVENT"}},
			{"VEVENT", "tasks", nil},
			{"VEVENT", "tasks", []string{"VEVENT"}},
			{"VTODO", "events", nil},
			{"VTODO", "events", []string{"VTODO"}},
			{"", "events", []string{"VTODO"}},
			{"", "tasks", nil},
		} {
			cal := mkCalendar(tc.stored)
			for _, comp := range tc.holds {
				store(cal, comp)
			}
			before := snap(cal)
			msg := fmt.Sprintf("%q PATCH %s holding %v", tc.stored, tc.kind, tc.holds)

			for _, body := range []string{
				`{"kind":"` + tc.kind + `"}`,
				`{"name":"Renamed","color":"#abcdef","kind":"` + tc.kind + `"}`,
			} {
				status, fields, raw := call(ownerToken, http.MethodPatch, cal.UUID, body)
				assert.Equal(t, fiber.StatusConflict, status, "%s: %s", msg, raw)
				assert.Equal(t, want, fields, "%s: %s", msg, raw)
				assertUnchanged(t, before, cal, msg+" "+body)
			}
		}
	})

	t.Run("an unknown kind is the create 400 and writes nothing", func(t *testing.T) {
		cal := mkCalendar("VEVENT")
		before := snap(cal)
		for _, body := range []string{`{"kind":"todo"}`, `{"kind":"Both"}`, `{"name":"Renamed","kind":"VEVENT,VTODO"}`} {
			status, fields, raw := call(ownerToken, http.MethodPatch, cal.UUID, body)
			assert.Equal(t, fiber.StatusBadRequest, status, raw)
			assert.Equal(t, map[string]any{
				"status":  "error",
				"message": `kind must be "events", "tasks" or "both"`,
			}, fields, raw)
		}
		status, _, raw := call(ownerToken, http.MethodPatch, cal.UUID, `{"kind":1}`)
		assert.Equal(t, fiber.StatusBadRequest, status, raw)
		assertUnchanged(t, before, cal, "unknown kind")
	})

	t.Run("a foreign or unknown calendar is the same 404 whatever the kind", func(t *testing.T) {
		cal := mkCalendar("VEVENT,VTODO")
		before := snap(cal)
		for _, kind := range []string{"both", "events", "tasks", "bogus"} {
			body := `{"kind":"` + kind + `"}`
			foreignStatus, _, foreignBody := call(strangerToken, http.MethodPatch, cal.UUID, body)
			unknownStatus, _, unknownBody := call(ownerToken, http.MethodPatch, "00000000-0000-0000-0000-000000000000", body)
			assert.Equal(t, fiber.StatusNotFound, foreignStatus, foreignBody)
			assert.Equal(t, fiber.StatusNotFound, unknownStatus, unknownBody)
			assert.Equal(t, unknownBody, foreignBody, kind)
		}
		assertUnchanged(t, before, cal, "foreign PATCH")
	})
}
