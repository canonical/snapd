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
	"regexp"
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

// NotFoundError represents an error caused by a missing entity.
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
		return nil, errors.New(`cannot define aspects directory: no aspects`)
	}

	aspectDir := &Directory{
		Name:    name,
		dataBag: dataBag,
		schema:  schema,
		aspects: make(map[string]*Aspect, len(aspects)),
	}

	for name, v := range aspects {
		aspectPatterns, ok := v.([]map[string]string)
		if !ok {
			return nil, fmt.Errorf("cannot define aspect %q: access patterns should be a list of maps", name)
		} else if len(aspectPatterns) == 0 {
			return nil, fmt.Errorf("cannot define aspect %q: no access patterns found", name)
		}

		aspect, err := newAspect(aspectDir, name, aspectPatterns)
		if err != nil {
			return nil, fmt.Errorf("cannot define aspect %q: %w", name, err)
		}

		aspectDir.aspects[name] = aspect
	}

	return aspectDir, nil
}

func newAspect(dir *Directory, name string, aspectPatterns []map[string]string) (*Aspect, error) {
	aspect := &Aspect{
		Name:           name,
		accessPatterns: make([]*accessPattern, 0, len(aspectPatterns)),
		directory:      dir,
	}

	for _, aspectPattern := range aspectPatterns {
		name, ok := aspectPattern["name"]
		if !ok || name == "" {
			return nil, errors.New(`access patterns must have a "name" field`)
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
			return nil, errors.New(`access patterns must have a "path" field`)
		}

		if err := validateNamePathPair(name, path); err != nil {
			return nil, err
		}

		accPattern, err := newAccessPattern(name, path, aspectPattern["access"])
		if err != nil {
			return nil, err
		}

		aspect.accessPatterns = append(aspect.accessPatterns, accPattern)
	}

	return aspect, nil
}

// validateNamePathPair checks that:
//     * names and paths are composed of valid subkeys (see: validateAspectString)
//     * all placeholders in a name are in the path and vice-versa
func validateNamePathPair(name, path string) error {
	if err := validateAspectDottedPath(name); err != nil {
		return fmt.Errorf("invalid access name %q: %w", name, err)
	}

	if err := validateAspectDottedPath(path); err != nil {
		return fmt.Errorf("invalid path %q: %w", path, err)
	}

	namePlaceholders, pathPlaceholders := getPlaceholders(name), getPlaceholders(path)
	if len(namePlaceholders) != len(pathPlaceholders) {
		return fmt.Errorf("access name %q and path %q have mismatched placeholders", name, path)
	}

	for placeholder := range namePlaceholders {
		if !pathPlaceholders[placeholder] {
			return fmt.Errorf("placeholder %q from access name %q is absent from path %q",
				placeholder, name, path)
		}
	}

	return nil
}

var (
	subkeyRegex      = "(?:[a-z0-9]+-?)*[a-z](?:-?[a-z0-9])*"
	validSubkey      = regexp.MustCompile(fmt.Sprintf("^%s$", subkeyRegex))
	validPlaceholder = regexp.MustCompile(fmt.Sprintf("^{%s}$", subkeyRegex))
)

// validateAspectDottedPath validates that names/paths in an aspect definition are:
//     * composed of non-empty, dot-separated subkeys with optional placeholders ("foo.{bar}")
//     * non-placeholder subkeys are made up of lowercase alphanumeric ASCII characters,
//			optionally with dashes between alphanumeric characters (e.g., "a-b-c")
//     * placeholder subkeys are composed of non-placeholder subkeys wrapped in curly brackets
func validateAspectDottedPath(path string) (err error) {
	subkeys := strings.Split(path, ".")

	for _, subkey := range subkeys {
		if subkey == "" {
			return errors.New("cannot have empty subkeys")
		}

		if !(validSubkey.MatchString(subkey) || validPlaceholder.MatchString(subkey)) {
			return fmt.Errorf("invalid subkey %q", subkey)
		}
	}

	return nil
}

// getPlaceholders returns the set of placeholders in the string or nil, if
// there is none.
func getPlaceholders(aspectStr string) map[string]bool {
	var placeholders map[string]bool

	subkeys := strings.Split(aspectStr, ".")
	for _, subkey := range subkeys {
		if subkey[0] == '{' && subkey[len(subkey)-1] == '}' {
			if placeholders == nil {
				placeholders = make(map[string]bool)
			}

			placeholders[subkey] = true
		}
	}

	return placeholders
}

// Aspect returns an aspect from the aspect directory.
func (d *Directory) Aspect(aspect string) *Aspect {
	return d.aspects[aspect]
}

// Aspect is a group of access patterns under a directory.
type Aspect struct {
	Name           string
	accessPatterns []*accessPattern
	directory      *Directory
}

// Set sets the named aspect to a specified value.
func (a *Aspect) Set(name string, value interface{}) error {
	nameSubkeys := strings.Split(name, ".")
	for _, accessPatt := range a.accessPatterns {
		placeholders, ok := accessPatt.match(nameSubkeys)
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
	subkeys := strings.Split(name, ".")
	for _, accessPatt := range a.accessPatterns {
		placeholders, ok := accessPatt.match(subkeys)
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
		return nil, fmt.Errorf("cannot  aspect pattern: %w", err)
	}

	nameSubkeys := strings.Split(name, ".")
	nameMatchers := make([]nameMatcher, 0, len(nameSubkeys))
	for _, subkey := range nameSubkeys {
		var patt nameMatcher
		if subkey[0] == '{' && subkey[len(subkey)-1] == '}' {
			patt = placeholder(subkey[1 : len(subkey)-1])
		} else {
			patt = literal(subkey)
		}

		nameMatchers = append(nameMatchers, patt)
	}

	pathSubkeys := strings.Split(path, ".")
	pathWriters := make([]pathWriter, 0, len(pathSubkeys))
	for _, subkey := range pathSubkeys {
		var patt pathWriter
		if subkey[0] == '{' && subkey[len(subkey)-1] == '}' {
			patt = placeholder(subkey[1 : len(subkey)-1])
		} else {
			patt = literal(subkey)
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

// match takes a list of subkeys and returns true if those subkeys match the pattern's
// name. If the name contains placeholders, those will be mapped to their values in
// the supplied subkeys and set in the map. Example: if pattern.name=["{foo}", "b", "{bar}"],
// and nameSubkeys=["a", "b", "c"], then it returns true and the map will contain
// {"foo": "a", "bar": "c"}.
func (p *accessPattern) match(nameSubkeys []string) (map[string]string, bool) {
	if len(p.name) != len(nameSubkeys) {
		return nil, false
	}

	placeholders := make(map[string]string)
	for i, subkey := range nameSubkeys {
		if !p.name[i].match(subkey, placeholders) {
			return nil, false
		}
	}

	return placeholders, true
}

// getPath takes a map of placeholders to their values in the aspect name and
// returns the path with its placeholder values filled in with the map's values.
func (p *accessPattern) getPath(placeholders map[string]string) (string, error) {
	sb := &strings.Builder{}

	for _, subkey := range p.path {
		if sb.Len() > 0 {
			if _, err := sb.WriteRune('.'); err != nil {
				return "", err
			}
		}

		if err := subkey.write(sb, placeholders); err != nil {
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

// pattern is an individual subkey of a dot-separated name or path pattern. It
// can be a literal value of a placeholder delineated by curly brackets.
type nameMatcher interface {
	match(subkey string, placeholders map[string]string) bool
}

type pathWriter interface {
	write(sb *strings.Builder, placeholders map[string]string) error
}

// placeholder represents a subkey of a name/path (e.g., "{foo}") that can match
// with any value and map it from the input name to the path.
type placeholder string

// match adds a mapping to the placeholders map from this placeholder key to the
// supplied name subkey and returns true (a placeholder matches with any value).
func (p placeholder) match(subkey string, placeholders map[string]string) bool {
	placeholders[string(p)] = subkey
	return true
}

// write writes the value from the placeholders map corresponding to this placeholder
// key into the strings.Builder.
func (p placeholder) write(sb *strings.Builder, placeholders map[string]string) error {
	subkey, ok := placeholders[string(p)]
	if !ok {
		// the validation at create-time checks for mismatched placeholders so this
		// shouldn't be possible save for programmer error
		return fmt.Errorf("cannot find path placeholder %q in the aspect name", p)
	}

	_, err := sb.WriteString(subkey)
	return err
}

// literal is a non-placeholder name/path subkey.
type literal string

// match returns true if the subkey is equal to the literal.
func (p literal) match(subkey string, _ map[string]string) bool {
	return string(p) == subkey
}

// write writes the literal subkey into the strings.Builder.
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

	var err error
	if value == nil {
		_, err = unset(subKeys, 0, s)
	} else {
		_, err = set(subKeys, 0, s, value)
	}

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

	// TODO: this will error ungraciously if there isn't an object
	// at this level (see the TODO in NewAspectDirectory)
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

func unset(subKeys []string, index int, node map[string]json.RawMessage) (json.RawMessage, error) {
	key := subKeys[index]
	if index == len(subKeys)-1 {
		delete(node, key)
		// if the parent node has no other entries, it can also be deleted
		if len(node) == 0 {
			return nil, nil
		}

		return json.Marshal(node)
	}

	rawLevel, ok := node[key]
	if !ok {
		// no such entry, nothing to unset
		return json.Marshal(node)
	}

	var level map[string]json.RawMessage
	if err := jsonutil.DecodeWithNumber(bytes.NewReader(rawLevel), &level); err != nil {
		return nil, err
	}

	rawLevel, err := unset(subKeys, index+1, level)
	if err != nil {
		return nil, err
	}

	if rawLevel == nil {
		delete(node, key)

		if len(node) == 0 {
			return nil, nil
		}
	} else {
		node[key] = rawLevel
	}

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
