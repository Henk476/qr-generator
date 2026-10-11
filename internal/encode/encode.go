// Package encode turns user-facing content (a URL, a Wi-Fi network, a
// contact card, ...) into the exact string that gets stored in a QR code.
//
// Every content type is described by a Type: its name, its input fields and
// a builder. Inputs are plain string maps so the CLI and web forms can share
// one code path.
package encode

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Field describes one input of a content type.
type Field struct {
	Name     string
	Usage    string
	Required bool
}

// Type is a QR content type such as "url" or "wifi".
type Type struct {
	Name  string
	Title string
	// Primary is the field that a bare "data" value maps to (for the CLI's
	// --data flag). Empty when the type has no single obvious field.
	Primary string
	Fields  []Field
	build   func(f fields) (string, error)
}

// Build validates the inputs and returns the payload to encode.
func (t Type) Build(values map[string]string) (string, error) {
	for name := range values {
		if !t.hasField(name) {
			return "", fmt.Errorf("%s: unknown field %q (valid: %s)", t.Name, name, strings.Join(t.fieldNames(), ", "))
		}
	}
	f := fields{}
	for k, v := range values {
		f[k] = strings.TrimSpace(v)
	}
	for _, fd := range t.Fields {
		if fd.Required && f[fd.Name] == "" {
			return "", fmt.Errorf("%s: field %q is required", t.Name, fd.Name)
		}
	}
	p, err := t.build(f)
	if err != nil {
		return "", fmt.Errorf("%s: %w", t.Name, err)
	}
	return p, nil
}

func (t Type) hasField(name string) bool {
	return slices.ContainsFunc(t.Fields, func(f Field) bool { return f.Name == name })
}

func (t Type) fieldNames() []string {
	names := make([]string, len(t.Fields))
	for i, f := range t.Fields {
		names[i] = f.Name
	}
	return names
}

// fields is the trimmed input map handed to builders.
type fields map[string]string

var registry = map[string]Type{}

func register(t Type) {
	if _, dup := registry[t.Name]; dup {
		panic("encode: duplicate type " + t.Name)
	}
	registry[t.Name] = t
}

// Lookup returns the content type with the given name.
func Lookup(name string) (Type, bool) {
	t, ok := registry[strings.ToLower(name)]
	return t, ok
}

// Types returns all content types sorted by name.
func Types() []Type {
	ts := make([]Type, 0, len(registry))
	for _, t := range registry {
		ts = append(ts, t)
	}
	sort.Slice(ts, func(i, j int) bool { return ts[i].Name < ts[j].Name })
	return ts
}

// Build is shorthand for looking up a type and building its payload.
func Build(typeName string, values map[string]string) (string, error) {
	t, ok := Lookup(typeName)
	if !ok {
		names := make([]string, 0, len(registry))
		for _, t := range Types() {
			names = append(names, t.Name)
		}
		return "", fmt.Errorf("unknown content type %q (valid: %s)", typeName, strings.Join(names, ", "))
	}
	return t.Build(values)
}
