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

type NotFoundError struct {
	Account    string
	BundleName string
	Aspect     string
	Field      string
	Cause      string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("cannot find field %q of aspect %s/%s/%s: %s", e.Field, e.Account, e.BundleName, e.Aspect, e.Cause)
}

func (e *NotFoundError) Is(err error) bool {
	_, ok := err.(*NotFoundError)
	return ok
}

// InvalidAccessError represents a failure to perform a read or write due to the
// aspect's access control.
type InvalidAccessError struct {
	RequestedAccess accessType
	FieldAccess     accessType
	Field           string
}

func (e *InvalidAccessError) Error() string {
	return fmt.Sprintf("cannot %s field %q: only supports %s access",
		accessTypeStrings[e.RequestedAccess], e.Field, accessTypeStrings[e.FieldAccess])
}

func (e *InvalidAccessError) Is(err error) bool {
	_, ok := err.(*InvalidAccessError)
	return ok
}

// DataBag controls access to the aspect data storage.
type DataBag interface {
	Query(path string, params map[string]string, res interface{}) error
	Get(path string, value interface{}) error
	Set(path string, value interface{}) error
	Data() ([]byte, error)
}

// Schema takes in data from the DataBag and validates that it's valid and could
// be committed.
type Schema interface {
	Validate(data []byte) error
}

// Bundle holds a series of related aspects.
type Bundle struct {
	Account string
	Name    string
	schema  Schema
	aspects map[string]*Aspect
}

// NewAspectBundle returns a new aspect bundle for the specified aspects
// and access patterns.
func NewAspectBundle(account string, bundleName string, aspects map[string]interface{}, schema Schema, optionals map[string]bool) (*Bundle, error) {
	if len(aspects) == 0 {
		return nil, errors.New(`cannot define aspects bundle: no aspects`)
	}

	aspectBundle := &Bundle{
		Account: account,
		Name:    bundleName,
		schema:  schema,
		aspects: make(map[string]*Aspect, len(aspects)),
	}

	for name, v := range aspects {
		accessPatterns, ok := v.([]map[string]string)
		if !ok {
			return nil, fmt.Errorf("cannot define aspect %q: access patterns should be a list of maps", name)
		} else if len(accessPatterns) == 0 {
			return nil, fmt.Errorf("cannot define aspect %q: no access patterns found", name)
		}

		aspect, err := newAspect(aspectBundle, name, accessPatterns, optionals)
		if err != nil {
			return nil, fmt.Errorf("cannot define aspect %q: %w", name, err)
		}

		aspectBundle.aspects[name] = aspect
	}

	return aspectBundle, nil
}

func newAspect(bundle *Bundle, name string, aspectPatterns []map[string]string, optionals map[string]bool) (*Aspect, error) {
	aspect := &Aspect{
		Name:           name,
		accessPatterns: make([]*accessPattern, 0, len(aspectPatterns)),
		bundle:         bundle,
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

		if err := validateNamePathPair(name, path, optionals); err != nil {
			return nil, err
		}

		accPattern, err := newAccessPattern(name, path, aspectPattern["access"], optionals)
		if err != nil {
			return nil, err
		}

		aspect.accessPatterns = append(aspect.accessPatterns, accPattern)
	}

	return aspect, nil
}

// validateNamePathPair checks that:
//   - names and paths are composed of valid subkeys (see: validateAspectString)
//   - all placeholders in a name are in the path and vice-versa
func validateNamePathPair(name, path string, optionals map[string]bool) error {
	if err := validateAspectDottedPath(name); err != nil {
		return fmt.Errorf("invalid access name %q: %w", name, err)
	}

	if err := validateAspectDottedPath(path); err != nil {
		return fmt.Errorf("invalid path %q: %w", path, err)
	}

	namePlaceholders, pathPlaceholders := getPlaceholders(name), getPlaceholders(path)
	for placeholder := range pathPlaceholders {
		if _, ok := namePlaceholders[placeholder]; !ok && !optionals[placeholder] {
			return fmt.Errorf("non-optional placeholder %q from access name %q is absent from path %q",
				placeholder, name, path)
		}
	}

	for placeholder := range namePlaceholders {
		if _, ok := pathPlaceholders[placeholder]; !ok {
			return fmt.Errorf(`cannot find name placeholder %q in path`, placeholder)
		}
	}

	return nil
}

var (
	subkeyRegex              = "(?:[a-z0-9]+-?)*[a-z](?:-?[a-z0-9])*"
	validSubkey              = regexp.MustCompile(fmt.Sprintf("^%s$", subkeyRegex))
	validPlaceholder         = regexp.MustCompile(fmt.Sprintf("^{%s}$", subkeyRegex))
	validFilteredPlaceholder = regexp.MustCompile(fmt.Sprintf(`^{%s}(\[\.%s={%s}\])$`, subkeyRegex, subkeyRegex, subkeyRegex))
)

// validateAspectDottedPath validates that names/paths in an aspect definition are:
//   - composed of non-empty, dot-separated subkeys with optional placeholders ("foo.{bar}")
//   - non-placeholder subkeys are made up of lowercase alphanumeric ASCII characters,
//     optionally with dashes between alphanumeric characters (e.g., "a-b-c")
//   - placeholder subkeys are composed of non-placeholder subkeys wrapped in curly brackets
func validateAspectDottedPath(path string) (err error) {
	subkeys := splitPath(path)
	for _, subkey := range subkeys {
		if subkey == "" {
			return errors.New("cannot have empty subkeys")
		}

		if !(validSubkey.MatchString(subkey) || validPlaceholder.MatchString(subkey) || validFilteredPlaceholder.MatchString(subkey)) {
			return fmt.Errorf("invalid subkey %q", subkey)
		}
	}

	return nil
}

func splitPath(path string) []string {
	var parts []string

	lastDot := -1
	for i, char := range path {
		if char != '.' {
			continue
		}

		// dot inside a field placeholder; not a key separator
		if i > 0 && path[i-1] == '[' {
			continue
		}

		parts = append(parts, path[lastDot+1:i])
		lastDot = i
	}

	if lastDot != len(path)-1 {
		parts = append(parts, path[lastDot+1:])
	}

	return parts
}

// getPlaceholders returns the set of placeholders in the string or nil, if
// there is none.
func getPlaceholders(aspectStr string) map[string]bool {
	var placeholders map[string]bool

	subkeys := splitPath(aspectStr)
	for _, subkey := range subkeys {
		if subkey[0] == '{' {
			// may not be last char, if there's a field filter
			end := strings.Index(subkey, "}")
			if placeholders == nil {
				placeholders = make(map[string]bool)
			}
			placeholders[subkey[1:end]] = true
		}
	}

	return placeholders
}

// Aspect returns an aspect from the aspect bundle.
func (d *Bundle) Aspect(aspect string) *Aspect {
	return d.aspects[aspect]
}

// Aspect is a group of access patterns under a bundle.
type Aspect struct {
	Name           string
	accessPatterns []*accessPattern
	bundle         *Bundle
}

// Set sets the named aspect to a specified value.
func (a *Aspect) Set(databag DataBag, name string, value interface{}) error {
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
			return &InvalidAccessError{RequestedAccess: write, FieldAccess: accessPatt.access, Field: name}
		}

		if err := databag.Set(path, value); err != nil {
			return err
		}

		data, err := databag.Data()
		if err != nil {
			return err
		}

		return a.bundle.schema.Validate(data)
	}

	return &NotFoundError{
		Account:    a.bundle.Account,
		BundleName: a.bundle.Name,
		Aspect:     a.Name,
		Field:      name,
		Cause:      "field not found",
	}
}

// Get returns the aspect value identified by the name. If either the named aspect
// or the corresponding value can't be found, a NotFoundError is returned.
func (a *Aspect) Get(databag DataBag, name string, value interface{}) error {
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
			return &InvalidAccessError{RequestedAccess: read, FieldAccess: accessPatt.access, Field: name}
		}

		if err := databag.Get(path, value); err != nil {
			var pathErr PathNotFoundError
			if errors.As(err, &pathErr) {
				return &NotFoundError{
					Account:    a.bundle.Account,
					BundleName: a.bundle.Name,
					Aspect:     a.Name,
					Field:      name,
					Cause:      string(pathErr),
				}
			}
			return err
		}
		return nil
	}

	return &NotFoundError{
		Account:    a.bundle.Account,
		BundleName: a.bundle.Name,
		Aspect:     a.Name,
		Field:      name,
		Cause:      "field not found",
	}
}

func (a *Aspect) Query(databag DataBag, request, query string, res interface{}) error {
	var params map[string]string
	if query != "" {
		params = make(map[string]string)
		queryParts := strings.Split(query, ",")

		for _, query := range queryParts {
			parts := strings.Split(query, "=")
			if len(parts) != 2 {
				return fmt.Errorf(`cannot parse query: %s should be key=val`, query)
			}

			params[parts[0]] = parts[1]
		}
	}

	subkeys := splitPath(request)
	for _, accessPatt := range a.accessPatterns {
		placeholders, ok := accessPatt.match(subkeys)
		if !ok {
			continue
		}

		for ref, value := range params {
			placeholders[ref] = value
		}

		path, err := accessPatt.getPath(placeholders)
		if err != nil {
			return err
		}

		return databag.Query(path, params, res)
	}

	return fmt.Errorf(`cannot find path that matches request`)
}

func newAccessPattern(name, path, accesstype string, optionals map[string]bool) (*accessPattern, error) {
	accType, err := newAccessType(accesstype)
	if err != nil {
		return nil, fmt.Errorf("cannot create aspect pattern: %w", err)
	}

	nameSubkeys := splitPath(name)
	nameMatchers := make([]nameMatcher, 0, len(nameSubkeys))
	for _, subkey := range nameSubkeys {
		var patt nameMatcher
		switch {
		case isFilteredPlaceholder(subkey):
			patt = filteredPlaceholder(subkey)
		case isPlaceholder(subkey):
			patt = placeholder(subkey)
		default:
			patt = literal(subkey)
		}

		nameMatchers = append(nameMatchers, patt)
	}

	pathSubkeys := splitPath(path)
	pathWriters := make([]pathWriter, 0, len(pathSubkeys))
	for _, subkey := range pathSubkeys {
		var patt pathWriter
		switch {
		case isFilteredPlaceholder(subkey):
			patt = filteredPlaceholder(subkey)
		case isPlaceholder(subkey):
			patt = placeholder(subkey)
		default:
			patt = literal(subkey)
		}

		pathWriters = append(pathWriters, patt)
	}

	return &accessPattern{
		originalPath: path,
		name:         nameMatchers,
		path:         pathWriters,
		access:       accType,
		optionals:    optionals,
	}, nil
}

func isPlaceholder(part string) bool {
	return part[0] == '{' && part[len(part)-1] == '}'
}

func isFilteredPlaceholder(s string) bool {
	return validFilteredPlaceholder.MatchString(s)
}

// accessPattern represents an individual aspect access pattern. It can be used
// to match an input name and map it into a corresponding path, potentially with
// placeholders filled in.
type accessPattern struct {
	originalPath string
	name         []nameMatcher
	path         []pathWriter
	access       accessType
	optionals    map[string]bool
}

// match takes a list of subkeys and returns true if those subkeys match the pattern's
// name. If the name contains placeholders, those will be mapped to their values in
// the supplied subkeys and set in the map. Example: if pattern.name=["{foo}", "b", "{bar}"],
// and nameSubkeys=["a", "b", "c"], then it returns true and the map will contain
// {"foo": "a", "bar": "c"}.
func (p *accessPattern) match(nameSubkeys []string) (map[string]string, bool) {
	placeholders := make(map[string]string)
	for i, subkey := range nameSubkeys {
		if !p.optionals[subkey] && !p.name[i].match(subkey, placeholders) {
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

		if err := subkey.write(sb, placeholders, p.optionals); err != nil {
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
	write(sb *strings.Builder, placeholders map[string]string, optionals map[string]bool) error
}

// placeholder represents a subkey of a name/path (e.g., "{foo}") that can match
// with any value and map it from the input name to the path.
type placeholder string

// match adds a mapping to the placeholders map from this placeholder key to the
// supplied name subkey and returns true (a placeholder matches with any value).
func (p placeholder) match(subkey string, placeholders map[string]string) bool {
	// TODO: account for optionals
	placeholders[string(p)] = subkey
	return true
}

// write writes the value from the placeholders map corresponding to this placeholder
// key into the strings.Builder.
func (p placeholder) write(sb *strings.Builder, placeholders map[string]string, optionals map[string]bool) error {
	key := string(p)
	subkey, ok := placeholders[key]
	if !ok {
		if !optionals[key] {
			return fmt.Errorf("cannot find non-optional value for placeholder %q in the aspect name", key)
		}

		// placeholder is optional so use the placeholder instead of a concrete value
		subkey = key
	}

	_, err := sb.WriteString(subkey)
	return err
}

type filteredPlaceholder string

// match adds a mapping to the placeholders map from this placeholder key to the
// supplied name subkey and returns true (a placeholder matches with any value).
func (p filteredPlaceholder) match(subkey string, placeholders map[string]string) bool {
	// TODO: account for optionals
	end := strings.Index(string(p), "}")
	ref := string(p)[:end+1]
	placeholders[ref] = subkey
	return true
}

// write writes the value from the placeholders map corresponding to this placeholder
// key into the strings.Builder.
func (p filteredPlaceholder) write(sb *strings.Builder, placeholders map[string]string, optionals map[string]bool) error {
	end := strings.Index(string(p), "}")
	ref := string(p)[:end+1]
	subkey, ok := placeholders[ref]
	if !ok {
		if !optionals[ref] {
			return fmt.Errorf("cannot find non-optional value for placeholder %q in the aspect name", ref)
		}

		// placeholder is optional so use the placeholder instead of a concrete value
		subkey = ref
	}

	_, err := sb.WriteString(subkey)
	if err != nil {
		return err
	}

	_, err = sb.WriteString(string(p)[end+1:])
	return err
}

// literal is a non-placeholder name/path subkey.
type literal string

// match returns true if the subkey is equal to the literal.
func (p literal) match(subkey string, _ map[string]string) bool {
	return string(p) == subkey
}

// write writes the literal subkey into the strings.Builder.
func (p literal) write(sb *strings.Builder, _ map[string]string, _ map[string]bool) error {
	_, err := sb.WriteString(string(p))
	return err
}

type PathNotFoundError string

func (e PathNotFoundError) Error() string {
	return string(e)
}

func (e PathNotFoundError) Is(err error) bool {
	_, ok := err.(PathNotFoundError)
	return ok
}

// JSONDataBag is a simple DataBag implementation that keeps JSON in-memory.
type JSONDataBag map[string]json.RawMessage

// NewJSONDataBag returns a DataBag implementation that stores data in JSON.
// The top-level of the JSON structure is always a map.
func NewJSONDataBag() JSONDataBag {
	return JSONDataBag(make(map[string]json.RawMessage))
}

func (s JSONDataBag) Query(path string, params map[string]string, res interface{}) error {
	subKeys := splitPath(path)
	return query(subKeys, 0, s, params, res)
}

func query(subKeys []string, index int, node map[string]json.RawMessage, params map[string]string, res interface{}) error {
	key := subKeys[index]
	key, filt, fieldFilt := splitKeyAndFilter(key, params)

	if key != "*" {
		nextLevel, ok := node[key]
		if !ok {
			pathPrefix := strings.Join(subKeys[:index+1], ".")
			return PathNotFoundError(fmt.Sprintf("no value was found under path %q", pathPrefix))
		}

		node = nil
		if err := json.Unmarshal(nextLevel, &node); err != nil {
			var typeErr *json.UnmarshalTypeError
			if errors.As(err, &typeErr) {
				return fmt.Errorf("cannot filter by field: expected a map but got a %s", typeErr.Value)
			}
			return err
		}

		if fieldFilt != nil {
			pass, err := fieldFilt(node)

			if err != nil {
				return err
			}

			if !pass {
				// single result was filtered out
				node = map[string]json.RawMessage{}
			}
		}
	} else {
		// return all objects but filtered
		for field, val := range node {
			var nextLevel map[string]json.RawMessage
			if err := json.Unmarshal(val, &nextLevel); err != nil {
				var typeErr *json.UnmarshalTypeError
				if errors.As(err, &typeErr) {
					return fmt.Errorf("cannot filter by field: expected a map but got a %s", typeErr.Value)
				}
				return err
			}

			// filter element by it's key
			if !filt(field) {
				delete(node, field)
			}

			if fieldFilt == nil {
				continue
			}

			// filter element by it's field
			pass, err := fieldFilt(nextLevel)
			if err != nil {
				return err
			}

			if !pass {
				delete(node, field)
			}
		}
	}

	// read the final value
	if index == len(subKeys)-1 {
		res, ok := res.(*interface{})
		if !ok {
			return fmt.Errorf("expected return parameter to be a pointer")
		}
		*res = node
		return nil
	}

	return query(subKeys, index+1, node, params, res)
}

func splitKeyAndFilter(key string, params map[string]string) (string, keyFilter, objFilter) {
	if strings.IndexAny(key, "{[") == -1 {
		return key, nil, nil
	}

	// the subkey can have an object filter and/or a field filter, parse them
	var rawFieldFilter string
	fieldFiltStart := strings.Index(key, "[")
	if fieldFiltStart != -1 {
		// strip square brackets around filter filter
		rawFieldFilter = key[fieldFiltStart+1 : len(key)-1]
	}

	var rawObjFilter string
	if key[0] == '{' {
		endObjFilter := strings.Index(key, "}")
		// strip brackets around placeholder
		rawObjFilter = key[1:endObjFilter]

		// if there's an object filter, we want get all elements and them filter them
		key = "*"
	} else {
		key = key[:fieldFiltStart]
	}

	var filt keyFilter
	if rawObjFilter != "" {
		value := params[rawObjFilter]
		filt = func(val string) bool {
			return value == "" || val == value
		}
	}

	var fieldFilt objFilter
	if rawFieldFilter != "" {
		fieldFilt = getFieldFilter(rawFieldFilter, params)
	}

	return key, filt, fieldFilt
}

func getFieldFilter(key string, params map[string]string) objFilter {
	// split the field reference and the placeholder
	parts := strings.Split(key, "=")
	field, placeholder := parts[0], parts[1]

	// strip the dot before the field ref (we validated before)
	field = field[1:]
	// strip the {} around the placeholder (similarly, we already validated)
	placeholder = placeholder[1 : len(placeholder)-1]
	fieldValue, ok := params[placeholder]
	if !ok {
		// anything matches the filter
		return func(obj map[string]json.RawMessage) (bool, error) {
			return true, nil
		}
	}

	return func(obj map[string]json.RawMessage) (bool, error) {
		rawVal, ok := obj[field]
		if !ok {
			return false, nil
		}

		// to simplify, assume it's always a string
		var val string
		if err := json.Unmarshal(rawVal, &val); err != nil {
			return false, err
		}

		return val == fieldValue, nil
	}
}

type objFilter func(map[string]json.RawMessage) (bool, error)
type keyFilter func(string) bool

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
		return PathNotFoundError(fmt.Sprintf("no value was found under path %q", pathPrefix))
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
		// TODO: see TODO in newAspect()
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
// If the value is nil, the entry is deleted.
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
	// at this level (see the TODO in NewAspectBundle)
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

// Copy returns a copy of the databag.
func (s JSONDataBag) Copy() JSONDataBag {
	toplevel := map[string]json.RawMessage(s)
	copy := make(map[string]json.RawMessage, len(toplevel))

	for k, v := range toplevel {
		copy[k] = v
	}

	return JSONDataBag(copy)
}

// JSONSchema is the Schema implementation corresponding to JSONDataBag and it's
// able to validate its data.
type JSONSchema struct{}

// NewJSONSchema returns a Schema able to validate a JSONDataBag's data.
func NewJSONSchema() JSONSchema {
	return JSONSchema{}
}

// Validate validates that the specified data can be encoded into JSON.
func (s JSONSchema) Validate(jsonData []byte) error {
	// the top-level is always an object
	var data map[string]json.RawMessage
	return json.Unmarshal(jsonData, &data)
}
