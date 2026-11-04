package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
)

// maxReportedUnknownArgs caps how many unknown argument names one error lists.
// The names come from the request itself; a client sending hundreds of keys
// needs to learn that it did, not to have every one echoed back.
const maxReportedUnknownArgs = 10

// decodeArgs decodes a tool's arguments into dst, a pointer to the tool's flat
// argument struct, and refuses every argument that struct does not declare.
// It returns nil on success; otherwise a tool error to return as is, before
// the tool has done anything.
//
// Every tool advertises `additionalProperties: false`, but a plain
// json.Unmarshal drops an unknown key without a word. A misspelt argument —
// `calender_id`, `strat`, `titel` — therefore used to run the tool with that
// argument's default: an update that changed nothing still reported success,
// and a get_events with a typo in its window returned the whole calendar. All
// tools decode through here, so the schema's promise is kept in one place.
//
// Names are matched EXACTLY. encoding/json matches keys case-insensitively, and
// Decoder.DisallowUnknownFields keeps that leniency (`{"LIMIT": 3}` fills
// `limit`), while a JSON Schema property name is case-sensitive — so the key
// check is done here rather than left to the decoder.
//
// A bad argument is a tool error (isError), not a JSON-RPC error: it is input
// validation, which the model can correct from the message, and MCP 2025-11-25
// says such errors belong in the tool result for exactly that reason. The
// messages name the argument and, for an unknown one, every accepted name, and
// never mention Go types or struct names.
func decodeArgs(args json.RawMessage, dst interface{}) *toolCallResult {
	trimmed := bytes.TrimSpace(args)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		trimmed = []byte("{}")
	}

	var keys map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &keys); err != nil {
		// Unreachable over the wire — handleToolCall refuses a non-object as a
		// malformed request — but a tool must not trust its caller for that.
		return errorResult("The arguments must be a JSON object; the tool did not run.")
	}

	fields := argumentFields(reflect.TypeOf(dst))
	var unknown []string
	for key := range keys {
		if _, ok := fields[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		return errorResult(unknownArgumentsText(unknown, argumentNames(reflect.TypeOf(dst))))
	}

	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if bad := wronglyTypedArgument(keys, reflect.TypeOf(dst)); bad != nil {
			return bad
		}
		return errorResult("The arguments do not match the tool's input schema; the tool did not run.")
	}
	return nil
}

// wronglyTypedArgument finds the argument the decoder refused by decoding each
// one alone into its field's type, in declaration order, and describes the
// first that fails; nil when none does on its own.
//
// It does not read the field name off the decoder's UnmarshalTypeError: an
// argument with its own UnmarshalJSON (dto.OptionalField, the nullable
// arguments of update_task) fails inside a nested json.Unmarshal, and what
// encoding/json then puts into Field is an implementation detail.
func wronglyTypedArgument(keys map[string]json.RawMessage, t reflect.Type) *toolCallResult {
	fields := argumentFields(t)
	for _, name := range argumentNames(t) {
		raw, ok := keys[name]
		if !ok {
			continue
		}
		f := fields[name]
		err := json.Unmarshal(raw, reflect.New(f.Type).Interface())
		if err == nil {
			continue
		}
		got := describeJSONRaw(raw)
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			// The decoder's own word for the offending value: the element of
			// an array ("a number" in ["a", 2]) or a fraction's literal.
			got = describeJSONValue(typeErr.Value)
		}
		return errorResult(fmt.Sprintf("Argument %q must be %s, got %s; the tool did not run.",
			name, jsonTypeOf(f.Type), got))
	}
	return nil
}

// unknownArgumentsText is the refusal for arguments the tool does not take.
func unknownArgumentsText(unknown, accepted []string) string {
	sort.Strings(unknown)
	more := 0
	if len(unknown) > maxReportedUnknownArgs {
		more = len(unknown) - maxReportedUnknownArgs
		unknown = unknown[:maxReportedUnknownArgs]
	}
	quoted := make([]string, len(unknown))
	for i, name := range unknown {
		quoted[i] = fmt.Sprintf("%q", name)
	}

	var b strings.Builder
	if len(quoted) == 1 && more == 0 {
		b.WriteString("Unknown argument ")
	} else {
		b.WriteString("Unknown arguments ")
	}
	b.WriteString(strings.Join(quoted, ", "))
	if more > 0 {
		fmt.Fprintf(&b, " and %d more", more)
	}
	b.WriteString("; the tool did not run. ")
	if len(accepted) == 0 {
		b.WriteString("This tool takes no arguments.")
	} else {
		b.WriteString("Accepted arguments: " + strings.Join(accepted, ", ") + ".")
	}
	return b.String()
}

// argumentFields maps each JSON argument name of an argument struct to its
// field. Argument structs are flat: no embedding, every field tagged.
func argumentFields(t reflect.Type) map[string]reflect.StructField {
	fields := map[string]reflect.StructField{}
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return fields
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		fields[name] = f
	}
	return fields
}

// argumentNames lists the accepted argument names in declaration order, which
// the tools keep in the order of their advertised schema.
func argumentNames(t reflect.Type) []string {
	fields := argumentFields(t)
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return fields[names[i]].Index[0] < fields[names[j]].Index[0] })
	return names
}

// jsonTypeOf names, in JSON terms, what an argument of Go type t must be.
func jsonTypeOf(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Pointer:
		return jsonTypeOf(t.Elem())
	case reflect.String:
		return "a string"
	case reflect.Bool:
		return "a boolean (true or false)"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "an integer"
	case reflect.Float32, reflect.Float64:
		return "a number"
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.String {
			return "an array of strings"
		}
		return "an array"
	case reflect.Struct:
		if inner, ok := optionalFieldValue(t); ok {
			return jsonTypeOf(inner) + " or null"
		}
		return "an object"
	case reflect.Map:
		return "an object"
	default:
		return "of another type"
	}
}

// optionalFieldValue reports whether t is a dto.OptionalField[T] — an argument
// that takes a value or null (null removes, absent keeps) — and returns T. The
// type is generic, so it is recognised by its package and name rather than
// compared with one instantiation.
func optionalFieldValue(t reflect.Type) (reflect.Type, bool) {
	ref := reflect.TypeOf(dto.OptionalField[int]{})
	if t.Kind() != reflect.Struct || t.PkgPath() != ref.PkgPath() || !strings.HasPrefix(t.Name(), "OptionalField[") {
		return nil, false
	}
	value, ok := t.FieldByName("Value")
	if !ok || value.Type.Kind() != reflect.Pointer {
		return nil, false
	}
	return value.Type.Elem(), true
}

// describeJSONRaw names the JSON type of a raw value, for a refusal that has
// no UnmarshalTypeError to take the wording from.
func describeJSONRaw(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "a value of another type"
	}
	switch trimmed[0] {
	case '"':
		return "a string"
	case '{':
		return "an object"
	case '[':
		return "an array"
	case 't', 'f':
		return "a boolean"
	case 'n':
		return "null"
	default:
		return string(trimmed)
	}
}

// describeJSONValue renders json.UnmarshalTypeError.Value ("string",
// "number 1.5", "bool", …) for a message.
func describeJSONValue(v string) string {
	if literal, ok := strings.CutPrefix(v, "number "); ok {
		return literal
	}
	switch v {
	case "string":
		return "a string"
	case "number":
		return "a number"
	case "bool":
		return "a boolean"
	case "array":
		return "an array"
	case "object":
		return "an object"
	default:
		return "a value of another type"
	}
}
