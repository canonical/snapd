// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package aspects

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/snapcore/snapd/jsonutil"
	"github.com/snapcore/snapd/strutil"
)

type accessType int

const (
	readWrite accessType = iota
	read
	write
)

var accessTypeStrings = []string{"read-write", "read", "write"}

func newAccessType(access string) (accessType, error) {
	// default to read-write access
	if access == "" {
		access = "read-write"
	}

	for i, accessStr := range accessTypeStrings {
		if accessStr == access {
			return accessType(i), nil
		}
	}

	return readWrite, fmt.Errorf("expected 'access' to be one of %s but was %q", strutil.Quoted(accessTypeStrings), access)
}

type NotFoundError struct {
	Message string
}

func (e *NotFoundError) Error() string {
	return e.Message
}

func (e *NotFoundError) Is(err error) bool {
	_, ok := err.(*NotFoundError)
	return ok
}

// DataBag controls access to the aspect data storage.
type DataBag interface {
	Get(path string, value interface{}) error
	Set(path string, value interface{}) error
	Data() ([]byte, error)
}

// Schema takes in data from the DataBag and validates that it's valid and could
// be committed.
type Schema interface {
	Validate(data []byte) error
}

// Directory holds a series of related aspects.
type Directory struct {
	Name    string
	dataBag DataBag
	schema  Schema
	aspects map[string]*Aspect
}

// NewAspectDirectory returns a new aspect directory for the following aspects
// and access patterns.
func NewAspectDirectory(name string, aspects map[string]interface{}, dataBag DataBag, schema Schema) (*Directory, error) {
	if len(aspects) == 0 {
		return nil, errors.New(`cannot create aspects directory: no aspects`)
	}

	aspectDir := Directory{
		Name:    name,
		dataBag: dataBag,
		schema:  schema,
		aspects: make(map[string]*Aspect, len(aspects)),
	}

	for name, v := range aspects {
		aspectPatterns, ok := v.([]map[string]string)
		if !ok {
			return nil, errors.New("cannot create aspect: access patterns should be a list of maps")
		} else if len(aspectPatterns) == 0 {
			return nil, errors.New("cannot create aspect without access patterns")
		}

		aspect := &Aspect{
			Name:           name,
			accessPatterns: make([]*accessPattern, 0, len(aspectPatterns)),
			directory:      aspectDir,
		}

		for _, aspectPattern := range aspectPatterns {
			name, ok := aspectPattern["name"]
			if !ok || name == "" {
				return nil, errors.New(`cannot create aspect pattern without a "name" field`)
			}

			// TODO: either
			// * Validate that a path isn't a subset of another
			//   (possibly somewhere else).  Otherwise, we can
			//   write a user value in a subkey of a path (that
			//   should be map).
			// * Our schema should be able to provide
			//   allowed/expected types given a path; these should
			//   guide and take precedence resolving conflicts
			//   between data in the data bags or written E.g
			//   possibly return null or empty object if at a path
			//   were the schema expects an object there is scalar?
			path, ok := aspectPattern["path"]
			if !ok || path == "" {
				return nil, errors.New(`cannot create aspect pattern without a "path" field`)
			}

			accPattern, err := newAccessPattern(name, path, aspectPattern["access"])
			if err != nil {
				return nil, err
			}

			aspect.accessPatterns = append(aspect.accessPatterns, accPattern)
		}

		aspectDir.aspects[name] = aspect
	}

	return &aspectDir, nil
}

// Aspect returns an aspect from the aspect directory.
func (d *Directory) Aspect(aspect string) *Aspect {
	return d.aspects[aspect]
}

// Aspect is a group of access patterns under a directory.
type Aspect struct {
	Name           string
	accessPatterns []*accessPattern
	directory      Directory
}

// Set sets the named aspect to a specified value.
func (a *Aspect) Set(name string, value interface{}) error {
	nameParts := strings.Split(name, ".")
	for _, accessPatt := range a.accessPatterns {
		placeholders, ok := accessPatt.match(nameParts)
		if !ok {
			continue
		}

		path, err := accessPatt.getPath(placeholders)
		if err != nil {
			return err
		}

		if !accessPatt.isWriteable() {
			return fmt.Errorf("cannot set %q: path is not writeable", name)
		}

		if err := a.directory.dataBag.Set(path, value); err != nil {
			return err
		}

		data, err := a.directory.dataBag.Data()
		if err != nil {
			return err
		}

		return a.directory.schema.Validate(data)
	}

	return &NotFoundError{fmt.Sprintf("cannot set %q: name not found", name)}
}

// Get returns the aspect value identified by the name. If either the named aspect
// or the corresponding value can't be found, a NotFoundError is returned.
func (a *Aspect) Get(name string, value interface{}) error {
	nameParts := strings.Split(name, ".")
	for _, accessPatt := range a.accessPatterns {
		placeholders, ok := accessPatt.match(nameParts)
		if !ok {
			continue
		}

		path, err := accessPatt.getPath(placeholders)
		if err != nil {
			return err
		}

		if !accessPatt.isReadable() {
			return fmt.Errorf("cannot get %q: path is not readable", name)
		}

		if err := a.directory.dataBag.Get(path, value); err != nil {
			if errors.Is(err, &NotFoundError{}) {
				return &NotFoundError{fmt.Sprintf("cannot get %q: %v", name, err)}
			}

			return err
		}
		return nil
	}

	return &NotFoundError{fmt.Sprintf("cannot get %q: name not found", name)}
}

func newAccessPattern(name, path, accesstype string) (*accessPattern, error) {
	accType, err := newAccessType(accesstype)
	if err != nil {
		return nil, fmt.Errorf("cannot create aspect pattern: %w", err)
	}

	nameParts := strings.Split(name, ".")
	nameMatchers := make([]nameMatcher, 0, len(nameParts))
	for _, part := range nameParts {
		var patt nameMatcher
		if part[0] == '{' && part[len(part)-1] == '}' {
			patt = placeholder(part[1 : len(part)-1])
		} else {
			patt = literal(part)
		}

		nameMatchers = append(nameMatchers, patt)
	}

	pathParts := strings.Split(path, ".")
	pathWriters := make([]pathWriter, 0, len(pathParts))
	for _, part := range pathParts {
		var patt pathWriter
		if part[0] == '{' && part[len(part)-1] == '}' {
			patt = placeholder(part[1 : len(part)-1])
		} else {
			patt = literal(part)
		}

		pathWriters = append(pathWriters, patt)
	}

	return &accessPattern{
		name:   nameMatchers,
		path:   pathWriters,
		access: accType,
	}, nil
}

// accessPattern represents an individual aspect access pattern. It can be used
// to match an input name and map it into a corresponding path, potentially with
// placeholders filled in.
type accessPattern struct {
	name   []nameMatcher
	path   []pathWriter
	access accessType
}

// match takes a list of parts and returns true if those parts match the pattern's
// name. If the name contains placeholders, those will be mapped to their values in
// the supplied parts and set in the map. Example: if pattern.name=["{foo}", "b", "{bar}"],
// and nameParts=["a", "b", "c"], then it returns true and the map will contain
// {"foo": "a", "bar": "c"}.
func (p *accessPattern) match(nameParts []string) (map[string]string, bool) {
	if len(p.name) != len(nameParts) {
		return nil, false
	}

	placeholders := make(map[string]string)
	for i, part := range nameParts {
		if !p.name[i].match(part, placeholders) {
			return nil, false
		}
	}

	return placeholders, true
}

// getPath takes a map of placeholders to their values in the aspect name and
// returns the path with its placeholder values filled in with the map's values.
func (p *accessPattern) getPath(placeholders map[string]string) (string, error) {
	sb := &strings.Builder{}

	for _, part := range p.path {
		if sb.Len() > 0 {
			if _, err := sb.WriteRune('.'); err != nil {
				return "", err
			}
		}

		if err := part.write(sb, placeholders); err != nil {
			return "", err
		}

	}

	return sb.String(), nil
}

func (p accessPattern) isReadable() bool {
	return p.access == readWrite || p.access == read
}

func (p accessPattern) isWriteable() bool {
	return p.access == readWrite || p.access == write
}

// pattern is an individual part of a dot-separated name or path pattern. It
// can be a literal value of a placeholder delineated by curly brackets.
type nameMatcher interface {
	match(part string, placeholders map[string]string) bool
}

type pathWriter interface {
	write(sb *strings.Builder, placeholders map[string]string) error
}

// placeholder represents a part of a name/path (e.g., "{foo}") that can match
// with any value and map it from the input name to the path.
type placeholder string

// match adds a mapping to the placeholders map from this placeholder key to the
// supplied name part and returns true (a placeholder matches with any value).
func (p placeholder) match(part string, placeholders map[string]string) bool {
	placeholders[string(p)] = part
	return true
}

// write writes the value from the placeholders map corresponding to this placeholder
// key into the strings.Builder.
func (p placeholder) write(sb *strings.Builder, placeholders map[string]string) error {
	part, ok := placeholders[string(p)]
	if !ok {
		return fmt.Errorf("cannot find path placeholder %q in the aspect name", p)
	}

	_, err := sb.WriteString(part)
	return err
}

// literal is a non-placeholder name/path part.
type literal string

// match returns true if the part is equal to the literal.
func (p literal) match(part string, _ map[string]string) bool {
	return string(p) == part
}

// write writes the literal part into the strings.Builder.
func (p literal) write(sb *strings.Builder, _ map[string]string) error {
	_, err := sb.WriteString(string(p))
	return err
}

// JSONDataBag is a simple DataBag implementation that keeps JSON in-memory.
type JSONDataBag map[string]json.RawMessage

// NewJSONDataBag returns a DataBag implementation that stores data in JSON.
// The top-level of the JSON structure is always a map.
func NewJSONDataBag() JSONDataBag {
	storage := make(map[string]json.RawMessage)
	return storage
}

// Get takes a path and a pointer to a variable into which the value referenced
// by the path is written. The path can be dotted. For each dot a JSON object
// is expected to exist (e.g., "a.b" is mapped to {"a": {"b": <value>}}).
func (s JSONDataBag) Get(path string, value interface{}) error {
	subKeys := strings.Split(path, ".")
	return get(subKeys, 0, s, value)
}

func get(subKeys []string, index int, node map[string]json.RawMessage, result interface{}) error {
	key := subKeys[index]
	rawLevel, ok := node[key]
	if !ok {
		pathPrefix := strings.Join(subKeys[:index+1], ".")
		return &NotFoundError{fmt.Sprintf("value of key path %q not found", pathPrefix)}
	}

	// read the final value
	if index == len(subKeys)-1 {
		err := json.Unmarshal(rawLevel, result)
		if uErr, ok := err.(*json.UnmarshalTypeError); ok {
			path := strings.Join(subKeys, ".")
			return fmt.Errorf("cannot read value of %q into %T: maps to %s", path, result, uErr.Value)
		}

		return err
	}

	// decode the next map level
	var level map[string]json.RawMessage
	if err := jsonutil.DecodeWithNumber(bytes.NewReader(rawLevel), &level); err != nil {
		// TODO see TODO in NewAspectDirectory
		if uErr, ok := err.(*json.UnmarshalTypeError); ok {
			pathPrefix := strings.Join(subKeys[:index+1], ".")
			return fmt.Errorf("cannot read path prefix %q: prefix maps to %s", pathPrefix, uErr.Value)
		}
		return err
	}

	return get(subKeys, index+1, level, result)
}

// Set takes a path to which the value will be written. The path can be dotted,
// in which case, a nested JSON object is created for each sub-key found after a dot.
func (s JSONDataBag) Set(path string, value interface{}) error {
	subKeys := strings.Split(path, ".")
	_, err := set(subKeys, 0, s, value)
	return err
}

func set(subKeys []string, index int, node map[string]json.RawMessage, value interface{}) (json.RawMessage, error) {
	key := subKeys[index]
	if index == len(subKeys)-1 {
		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}

		node[key] = data
		newData, err := json.Marshal(node)
		if err != nil {
			return nil, err
		}

		return newData, nil
	}

	rawLevel, ok := node[key]
	if !ok {
		rawLevel = []byte("{}")
	}

	// TODO this will error ungraciously if there is not an object
	// at this level, see the TODO in NewAspectDirectory
	var level map[string]json.RawMessage
	if err := jsonutil.DecodeWithNumber(bytes.NewReader(rawLevel), &level); err != nil {
		return nil, err
	}

	rawLevel, err := set(subKeys, index+1, level, value)
	if err != nil {
		return nil, err
	}

	node[key] = rawLevel
	return json.Marshal(node)
}

// Data returns all of the bag's data encoded in JSON.
func (s JSONDataBag) Data() ([]byte, error) {
	return json.Marshal(s)
}

// JSONSchema is the Schema implementation corresponding to JSONDataBag and it's
// able to validate its data.
type JSONSchema struct{}

// NewJSONSchema returns a Schema able to validate a JSONDataBag's data.
func NewJSONSchema() *JSONSchema {
	return &JSONSchema{}
}

// Validate validates that the specified data can be encoded into JSON.
func (s *JSONSchema) Validate(jsonData []byte) error {
	// the top-level is always an object
	var data map[string]json.RawMessage
	return json.Unmarshal(jsonData, &data)
}
