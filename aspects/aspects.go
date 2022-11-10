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

			access, err := newAccessType(aspectPattern["access"])
			if err != nil {
				return nil, fmt.Errorf("cannot create aspect pattern: %w", err)
			}

			aspect.accessPatterns = append(aspect.accessPatterns, &accessPattern{
				name:   name,
				path:   path,
				access: access,
			})
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
	for _, accessPatt := range a.accessPatterns {
		path, err := accessPatt.getPath(name)
		if err != nil {
			return err
		}

		if path == "" {
			continue
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
	for _, accessPatt := range a.accessPatterns {
		path, err := accessPatt.getPath(name)
		if err != nil {
			return err
		}

		if path == "" {
			continue
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

// accessPattern holds information on how to access an aspect.
type accessPattern struct {
	name   string
	path   string
	access accessType
}

// getPath returns a path if the specified name matches the access pattern's name
// (with optional placeholders) and an empty string, if not.
func (p *accessPattern) getPath(name string) (string, error) {
	if strings.ContainsAny(p.name, "{}") {
		matches := matchedPlaceholders(p.name, name)
		if matches == nil {
			// aspect name w/ placeholders doesn't match the specified name
			return "", nil
		}

		return fillInPlaceholders(p.path, matches)
	} else if p.name == name {
		return p.path, nil
	}

	return "", nil
}

// matchedPlaceholders takes a dot-separated pattern with optional placeholders
// and a name. To match the pattern, the name's dot-separated parts must be equal
// to the pattern's non-placeholder parts (a pattern "foo.{bar}" is matched by a
// name with a part "foo" followed by any part). It returns a map from placeholders
// to their matches in the name (can be empty, if the path has no placeholders).
// If there's no match, it returns nil.
func matchedPlaceholders(pattern, name string) map[string]string {
	patternParts, nameParts := strings.Split(pattern, "."), strings.Split(name, ".")

	if len(patternParts) != len(nameParts) {
		return nil
	}

	placeholderMatches := make(map[string]string)
	for i, patt := range patternParts {
		// TODO: check assertions for:
		//	* empty name parts (e.g., "a..b")
		//	* names with non-ASCII characters (or we can't index the name directly)
		//	* mismatched or out-of-place curly brackets (assuming they can only be used for placeholders)
		//	* placeholders in a key but not in the path and vice-versa
		if patt[0] == '{' && patt[len(patt)-1] == '}' {
			trimmedPatt := strings.Trim(patt, "{}")
			placeholderMatches[trimmedPatt] = nameParts[i]
		} else if patt != nameParts[i] {
			return nil
		}
	}

	return placeholderMatches
}

// fillInPlaceholders takes a dot-separated path with optional placeholders and
// fills in the values using the matching values in the map.
func fillInPlaceholders(path string, placeholderValues map[string]string) (string, error) {
	var sb strings.Builder

	writePart := func(part string) error {
		if sb.Len() > 0 {
			if _, err := sb.WriteRune('.'); err != nil {
				return err
			}
		}

		_, err := sb.WriteString(part)
		return err
	}

	pathParts := strings.Split(path, ".")
	for _, part := range pathParts {
		if part[0] == '{' && part[len(part)-1] == '}' {
			trimmedPart := strings.Trim(part, "{}")

			var ok bool
			part, ok = placeholderValues[trimmedPart]
			if !ok {
				return "", fmt.Errorf("path placeholder %q is absent from the name", trimmedPart)
			}
		}

		if err := writePart(part); err != nil {
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
