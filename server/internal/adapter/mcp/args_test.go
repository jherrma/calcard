package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// schemaProperties lists a tool's advertised argument names in schema order.
func schemaProperties(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var schema struct {
		Properties json.RawMessage `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(raw, &schema))
	if len(schema.Properties) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(schema.Properties))
	_, err := dec.Token() // {
	require.NoError(t, err)
	var names []string
	for dec.More() {
		tok, err := dec.Token()
		require.NoError(t, err)
		names = append(names, tok.(string))
		var skip json.RawMessage
		require.NoError(t, dec.Decode(&skip))
	}
	return names
}

// unknownArgumentAnswer is the refusal a tool with these accepted arguments
// gives for one unknown argument.
func unknownArgumentAnswer(name string, accepted []string) string {
	if len(accepted) == 0 {
		return fmt.Sprintf("Unknown argument %q; the tool did not run. This tool takes no arguments.", name)
	}
	return fmt.Sprintf("Unknown argument %q; the tool did not run. Accepted arguments: %s.", name, strings.Join(accepted, ", "))
}

// TestEveryToolRejectsUnknownArguments walks the REGISTRY, so a tool added
// later is covered without touching this test: every tool must refuse an
// argument its schema does not declare — before doing anything — and list
// exactly the arguments its schema advertises, in schema order. That last part
// also proves each tool's argument struct and its advertised schema agree.
func TestEveryToolRejectsUnknownArguments(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	e.newCalendar(alice.ID, "Work")
	e.newAddressBook(alice.ID, "Friends")
	require.Len(t, e.server.toolList, len(e.server.tools))

	for _, tool := range e.server.toolList {
		t.Run(tool.Name, func(t *testing.T) {
			accepted := schemaProperties(t, tool.InputSchema)
			before := e.dbSnapshot()

			result := e.callTool(alice.ID, tool.Name, map[string]interface{}{"not_an_argument": true})
			require.True(t, result.IsError, "%s ran with an argument its schema does not declare", tool.Name)
			assert.Equal(t, unknownArgumentAnswer("not_an_argument", accepted), result.Content[0].Text)

			// Names are case-sensitive, as in the schema: encoding/json would
			// quietly fill `calendar_id` from `CALENDAR_ID`.
			for _, name := range accepted {
				upper := strings.ToUpper(name)
				result := e.callTool(alice.ID, tool.Name, map[string]interface{}{upper: "x"})
				require.True(t, result.IsError, "%s accepted %s for %s", tool.Name, upper, name)
				assert.Equal(t, unknownArgumentAnswer(upper, accepted), result.Content[0].Text)
			}

			assert.Equal(t, before, e.dbSnapshot(), "a refused call must not write anything")
		})
	}
}

// typoCase is one realistic slip per tool: a valid call, and the same call with
// one argument misspelt (correct → misspelt) or an invented one added
// (correct == ""). Before strict decoding each of these ran with a default in
// place of the misspelt argument — the writes reported success.
type typoCase struct {
	args     func(f *storageFixture) map[string]interface{}
	correct  string
	misspelt string
	writes   bool // the valid call changes stored data
}

func typoCases() map[string]typoCase {
	none := func(*storageFixture) map[string]interface{} { return map[string]interface{}{} }
	return map[string]typoCase{
		"list_calendars": {args: none, misspelt: "include_shared"},
		"get_events": {args: func(f *storageFixture) map[string]interface{} {
			return map[string]interface{}{"calendar_id": f.calendar, "start": "2026-03-06T00:00:00Z", "end": "2026-03-07T00:00:00Z"}
		}, correct: "start", misspelt: "strat"},
		"create_event": {args: func(f *storageFixture) map[string]interface{} {
			return map[string]interface{}{"calendar_id": f.calendar, "title": "Review", "location": "Room 2",
				"start": "2026-03-07T09:00:00Z", "end": "2026-03-07T10:00:00Z"}
		}, correct: "location", misspelt: "locaton", writes: true},
		"update_event": {args: func(f *storageFixture) map[string]interface{} {
			return map[string]interface{}{"event_id": f.event, "title": "Retro"}
		}, correct: "title", misspelt: "titel", writes: true},
		"delete_event": {args: func(f *storageFixture) map[string]interface{} {
			return map[string]interface{}{"event_id": f.event, "scope": "all"}
		}, correct: "scope", misspelt: "scoep", writes: true},
		"search_events": {args: func(*storageFixture) map[string]interface{} {
			return map[string]interface{}{"query": "standup", "limit": 5}
		}, correct: "limit", misspelt: "max_results"},
		"list_address_books": {args: none, misspelt: "shared"},
		"get_contacts": {args: func(f *storageFixture) map[string]interface{} {
			return map[string]interface{}{"address_book_id": f.book, "limit": 10}
		}, correct: "address_book_id", misspelt: "addressbook_id"},
		"search_contacts": {args: func(*storageFixture) map[string]interface{} {
			return map[string]interface{}{"query": "smith", "limit": 5}
		}, correct: "limit", misspelt: "limt"},
		"create_contact": {args: func(f *storageFixture) map[string]interface{} {
			return map[string]interface{}{"address_book_id": f.book, "first_name": "Lynn", "email": "lynn@example.com"}
		}, correct: "email", misspelt: "e_mail", writes: true},
		"update_contact": {args: func(f *storageFixture) map[string]interface{} {
			return map[string]interface{}{"contact_id": f.contact, "last_name": "Doe"}
		}, correct: "last_name", misspelt: "lastname", writes: true},
		"delete_contact": {args: func(f *storageFixture) map[string]interface{} {
			return map[string]interface{}{"contact_id": f.contact}
		}, misspelt: "hard_delete", writes: true},
		"find_free_slots": {args: func(f *storageFixture) map[string]interface{} {
			return map[string]interface{}{"start": "2026-03-06T08:00:00Z", "end": "2026-03-06T18:00:00Z",
				"duration_minutes": 30, "calendar_ids": []string{f.calendar}}
		}, correct: "calendar_ids", misspelt: "calendar_id"},
		"create_calendar": {args: func(*storageFixture) map[string]interface{} {
			return map[string]interface{}{"name": "Climbing", "color": "#10b981"}
		}, correct: "color", misspelt: "colour", writes: true},
		"update_calendar": {args: func(f *storageFixture) map[string]interface{} {
			return map[string]interface{}{"calendar_id": f.calendar, "name": "Office"}
		}, correct: "name", misspelt: "title", writes: true},
		"delete_calendar": {args: func(f *storageFixture) map[string]interface{} {
			return map[string]interface{}{"calendar_id": f.calendar, "confirmation": "DELETE"}
		}, misspelt: "cascade", writes: true},
		"create_address_book": {args: func(*storageFixture) map[string]interface{} {
			return map[string]interface{}{"name": "Climbing partners", "description": "Belay buddies"}
		}, correct: "description", misspelt: "desc", writes: true},
		"update_address_book": {args: func(f *storageFixture) map[string]interface{} {
			return map[string]interface{}{"address_book_id": f.book, "name": "Mates"}
		}, correct: "name", misspelt: "nmae", writes: true},
		"delete_address_book": {args: func(f *storageFixture) map[string]interface{} {
			return map[string]interface{}{"address_book_id": f.otherBook, "confirmation": "DELETE"}
		}, misspelt: "force", writes: true},
	}
}

// TestTypoedArgumentsChangeNothing: a call with one misspelt argument is
// refused, names the slip and the argument meant, and leaves every table
// untouched; the same call spelt correctly then succeeds (so it was the typo,
// and nothing else, that stopped it).
func TestTypoedArgumentsChangeNothing(t *testing.T) {
	cases := typoCases()

	registered := newTestEnv(t).server.toolList
	var missing []string
	for _, tool := range registered {
		if _, ok := cases[tool.Name]; !ok {
			missing = append(missing, tool.Name)
		}
	}
	sort.Strings(missing)
	require.Empty(t, missing, "every tool needs a case in typoCases")

	for _, tool := range registered {
		tc := cases[tool.Name]
		t.Run(tool.Name, func(t *testing.T) {
			f := newStorageFixture(t)
			valid := tc.args(f)
			typo := map[string]interface{}{}
			for k, v := range valid {
				typo[k] = v
			}
			if tc.correct != "" {
				require.Contains(t, valid, tc.correct)
				typo[tc.misspelt] = typo[tc.correct]
				delete(typo, tc.correct)
			} else {
				typo[tc.misspelt] = true
			}

			before := f.e.dbSnapshot()
			refused := f.e.callTool(f.userID, tool.Name, typo)
			require.True(t, refused.IsError, "%s must refuse the misspelt %q", tool.Name, tc.misspelt)
			text := refused.Content[0].Text
			assert.True(t, strings.HasPrefix(text, fmt.Sprintf("Unknown argument %q; the tool did not run.", tc.misspelt)), text)
			if tc.correct != "" {
				assert.Contains(t, text, tc.correct, "the answer must list the argument the caller meant")
			}
			assert.Equal(t, before, f.e.dbSnapshot(), "a refused call must not write anything")

			done := f.e.callTool(f.userID, tool.Name, valid)
			require.False(t, done.IsError, "control: the correctly spelt call must succeed: %s", done.Content[0].Text)
			if tc.writes {
				assert.NotEqual(t, before, f.e.dbSnapshot(), "control: the correctly spelt call writes")
			}
		})
	}
}

// TestGetEventsTypoNoLongerReturnsEverything pins the read-side failure the
// strict decoder fixes: a misspelt window used to be dropped, so the call
// returned the whole calendar as if it were the asked-for day.
func TestGetEventsTypoNoLongerReturnsEverything(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	cal := e.newCalendar(alice.ID, "Work")
	mustCreate(t, e, alice.ID, cal.UUID, "In March", "2026-03-06T09:00:00Z", "2026-03-06T10:00:00Z")
	mustCreate(t, e, alice.ID, cal.UUID, "In June", "2026-06-06T09:00:00Z", "2026-06-06T10:00:00Z")

	result := e.callTool(alice.ID, "get_events", map[string]interface{}{
		"calendar_id": cal.UUID, "strat": "2026-03-01T00:00:00Z", "end": "2026-03-31T00:00:00Z",
	})
	require.True(t, result.IsError)
	assert.Equal(t, `Unknown argument "strat"; the tool did not run. Accepted arguments: calendar_id, start, end.`,
		result.Content[0].Text)
	assert.NotContains(t, result.Content[0].Text, "In June")
}

func TestDecodeArgs(t *testing.T) {
	type args struct {
		Name  string   `json:"name"`
		Limit int      `json:"limit"`
		Rate  float64  `json:"rate"`
		Flag  *bool    `json:"flag"`
		IDs   []string `json:"ids"`
		Note  *string  `json:"note"`
	}
	const accepted = "Accepted arguments: name, limit, rate, flag, ids, note."

	for _, tc := range []struct {
		name string
		raw  string
		want string // "" = decodes
	}{
		{"absent", ``, ""},
		{"null", `null`, ""},
		{"whitespace", "  \n", ""},
		{"empty object", `{}`, ""},
		{"null value of a known argument", `{"note": null, "limit": 3}`, ""},
		{"one unknown", `{"nmae": "x"}`, `Unknown argument "nmae"; the tool did not run. ` + accepted},
		{"case differs", `{"Name": "x"}`, `Unknown argument "Name"; the tool did not run. ` + accepted},
		{"several unknown, sorted", `{"zeta": 1, "name": "x", "alpha": 2}`,
			`Unknown arguments "alpha", "zeta"; the tool did not run. ` + accepted},
		{"many unknown, capped", `{"a1":1,"a2":1,"a3":1,"a4":1,"a5":1,"a6":1,"a7":1,"a8":1,"a9":1,"b1":1,"b2":1,"b3":1}`,
			`Unknown arguments "a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9", "b1" and 2 more; the tool did not run. ` + accepted},
		{"string for an integer", `{"limit": "lots"}`, `Argument "limit" must be an integer, got a string; the tool did not run.`},
		{"fraction for an integer", `{"limit": 1.5}`, `Argument "limit" must be an integer, got 1.5; the tool did not run.`},
		{"string for a number", `{"rate": "fast"}`, `Argument "rate" must be a number, got a string; the tool did not run.`},
		{"string for a boolean", `{"flag": "yes"}`, `Argument "flag" must be a boolean (true or false), got a string; the tool did not run.`},
		{"number in a string array", `{"ids": ["a", 2]}`, `Argument "ids" must be an array of strings, got a number; the tool did not run.`},
		{"string for an array", `{"ids": "a"}`, `Argument "ids" must be an array of strings, got a string; the tool did not run.`},
		{"object for a string", `{"note": {}}`, `Argument "note" must be a string, got an object; the tool did not run.`},
		{"not an object", `[1, 2]`, `The arguments must be a JSON object; the tool did not run.`},
		{"a bare string", `"name"`, `The arguments must be a JSON object; the tool did not run.`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var dst args
			bad := decodeArgs(json.RawMessage(tc.raw), &dst)
			if tc.want == "" {
				assert.Nil(t, bad, "unexpected refusal: %v", bad)
				return
			}
			require.NotNil(t, bad)
			assert.True(t, bad.IsError)
			text := bad.Content[0].Text
			assert.Equal(t, tc.want, text)
			for _, goName := range []string{"Go ", "struct", "[]", "*", "float64", "json:"} {
				assert.NotContains(t, text, goName, "the message must not leak Go type names")
			}
		})
	}

	t.Run("zero-argument tool", func(t *testing.T) {
		bad := decodeArgs(json.RawMessage(`{"verbose": true}`), &struct{}{})
		require.NotNil(t, bad)
		assert.Equal(t, `Unknown argument "verbose"; the tool did not run. This tool takes no arguments.`, bad.Content[0].Text)
		assert.Nil(t, decodeArgs(json.RawMessage(`{}`), &struct{}{}))
	})

	t.Run("decodes", func(t *testing.T) {
		var dst args
		require.Nil(t, decodeArgs(json.RawMessage(`{"name":"x","limit":3,"rate":0.5,"flag":false,"ids":["a"],"note":"n"}`), &dst))
		assert.Equal(t, "x", dst.Name)
		assert.Equal(t, 3, dst.Limit)
		assert.Equal(t, 0.5, dst.Rate)
		require.NotNil(t, dst.Flag)
		assert.False(t, *dst.Flag)
		assert.Equal(t, []string{"a"}, dst.IDs)
		require.NotNil(t, dst.Note)
		assert.Equal(t, "n", *dst.Note)
	})
}
