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
	"math"
	"reflect"
	"regexp"
	"sort"
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
	Request    string
	Cause      string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("cannot find value for %q in aspect %s/%s/%s: %s", e.Request, e.Account, e.BundleName, e.Aspect, e.Cause)
}

func (e *NotFoundError) Is(err error) bool {
	_, ok := err.(*NotFoundError)
	return ok
}

func notFoundErrorFrom(a *Aspect, request, errMsg string) *NotFoundError {
	return &NotFoundError{
		Account:    a.bundle.Account,
		BundleName: a.bundle.Name,
		Aspect:     a.Name,
		Request:    request,
		Cause:      errMsg,
	}
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
func NewAspectBundle(account string, bundleName string, aspects map[string]interface{}, schema Schema) (*Bundle, error) {
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

		aspect, err := newAspect(aspectBundle, name, accessPatterns)
		if err != nil {
			return nil, fmt.Errorf("cannot define aspect %q: %w", name, err)
		}

		aspectBundle.aspects[name] = aspect
	}

	return aspectBundle, nil
}

func newAspect(bundle *Bundle, name string, aspectPatterns []map[string]string) (*Aspect, error) {
	aspect := &Aspect{
		Name:           name,
		accessPatterns: make([]*accessPattern, 0, len(aspectPatterns)),
		bundle:         bundle,
	}

	readRequests := make(map[string]bool)
	for _, aspectPattern := range aspectPatterns {
		request, ok := aspectPattern["request"]
		if !ok || request == "" {
			return nil, errors.New(`access patterns must have a "request" field`)
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
		storage, ok := aspectPattern["storage"]
		if !ok || storage == "" {
			return nil, errors.New(`access patterns must have a "storage" field`)
		}

		if err := validateRequestStoragePair(request, storage); err != nil {
			return nil, err
		}

		switch aspectPattern["access"] {
		case "read", "read-write", "":
			if readRequests[request] {
				return nil, fmt.Errorf(`cannot have several reading rules with the same "request" field`)
			}
			readRequests[request] = true
		}

		accPattern, err := newAccessPattern(request, storage, aspectPattern["access"])
		if err != nil {
			return nil, err
		}

		aspect.accessPatterns = append(aspect.accessPatterns, accPattern)
	}

	return aspect, nil
}

// validateRequestStoragePair checks that:
//   - request and storage are composed of valid subkeys (see: validateAspectString)
//   - all placeholders in a request are in the storage and vice-versa
func validateRequestStoragePair(request, storage string) error {
	if err := validateAspectDottedPath(request); err != nil {
		return fmt.Errorf("invalid request %q: %w", request, err)
	}

	if err := validateAspectDottedPath(storage); err != nil {
		return fmt.Errorf("invalid storage %q: %w", storage, err)
	}

	reqPlaceholders, storagePlaceholders := getPlaceholders(request), getPlaceholders(storage)
	if len(reqPlaceholders) != len(storagePlaceholders) {
		return fmt.Errorf("request %q and storage %q have mismatched placeholders", request, storage)
	}

	for placeholder := range reqPlaceholders {
		if !storagePlaceholders[placeholder] {
			return fmt.Errorf("placeholder %q from request %q is absent from storage %q",
				placeholder, request, storage)
		}
	}

	return nil
}

var (
	subkeyRegex      = "(?:[a-z0-9]+-?)*[a-z](?:-?[a-z0-9])*"
	validSubkey      = regexp.MustCompile(fmt.Sprintf("^%s$", subkeyRegex))
	validPlaceholder = regexp.MustCompile(fmt.Sprintf("^{%s}$", subkeyRegex))
	// TODO: decide on what the format should be for user-defined types in schemas
	validUserType = validSubkey
)

// validateAspectDottedPath validates that request/storage strings in an aspect definition are:
//   - composed of non-empty, dot-separated subkeys with optional placeholders ("foo.{bar}")
//   - non-placeholder subkeys are made up of lowercase alphanumeric ASCII characters,
//     optionally with dashes between alphanumeric characters (e.g., "a-b-c")
//   - placeholder subkeys are composed of non-placeholder subkeys wrapped in curly brackets
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
func (a *Aspect) Set(databag DataBag, request string, value interface{}) error {
	requestSubkeys := strings.Split(request, ".")
	for _, accessPatt := range a.accessPatterns {
		placeholders, _, ok := accessPatt.match(requestSubkeys)
		if !ok {
			continue
		}

		storagePath, err := accessPatt.storagePath(placeholders)
		if err != nil {
			return err
		}

		if !accessPatt.isWriteable() {
			return &InvalidAccessError{RequestedAccess: write, FieldAccess: accessPatt.access, Field: request}
		}

		if err := databag.Set(storagePath, value); err != nil {
			return err
		}

		data, err := databag.Data()
		if err != nil {
			return err
		}

		return a.bundle.schema.Validate(data)
	}

	return notFoundErrorFrom(a, request, "no matching write rule")
}

// Get returns the aspect value identified by the request. If either the named
// aspect or the corresponding value can't be found, a NotFoundError is returned.
func (a *Aspect) Get(databag DataBag, request string, value *interface{}) error {
	matches, err := a.matchGetRequest(request)
	if err != nil {
		return err
	}

	var merged interface{}
	for _, match := range matches {
		var val interface{}
		if err := databag.Get(match.storagePath, &val); err != nil {
			if errors.Is(err, PathNotFoundError("")) {
				continue
			}
			return err
		}

		// build a namespace based on the unmatched suffix parts
		namespace := match.suffixParts
		for i := len(namespace) - 1; i >= 0; i-- {
			val = map[string]interface{}{namespace[i]: val}
		}

		// merge result with results from other matching rules
		merged, err = merge(merged, val)
		if err != nil {
			return err
		}
	}

	if merged == nil {
		return notFoundErrorFrom(a, request, "no value for matching rules")
	}

	// the top level maps the request to the remaining namespace
	*value = map[string]interface{}{request: merged}
	return nil
}

func merge(old, new interface{}) (interface{}, error) {
	if old == nil {
		return new, nil
	}

	oldType, newType := reflect.TypeOf(old).Kind(), reflect.TypeOf(new).Kind()
	if oldType != newType {
		return nil, fmt.Errorf("cannot merge results of different types %T, %T", old, new)
	}

	if oldType != reflect.Map {
		// if the values are both scalars/lists, the new replaces the old value
		return new, nil
	}

	// if the values are maps, merge them recursively
	oldMap, newMap := old.(map[string]interface{}), new.(map[string]interface{})
	for k, v := range newMap {
		if storeVal, ok := oldMap[k]; ok {
			merged, err := merge(storeVal, v)
			if err != nil {
				return nil, err
			}
			v = merged
		}

		oldMap[k] = v
	}

	return oldMap, nil
}

type requestMatch struct {
	// storagePath contains the storage path specified in the matching entry with
	// any placeholders provided by the request filled in.
	storagePath string

	// suffixParts contains the nested suffix of the entry's request that wasn't
	// matched by the request.
	suffixParts []string
}

// matchGetRequest either returns the first exact match for the request or, if
// no entry is an exact match, one or more entries that the request matches a
// prefix of. If no match is found, a NotFoundError is returned.
func (a *Aspect) matchGetRequest(request string) (matches []requestMatch, err error) {
	subkeys := strings.Split(request, ".")

	for _, accessPatt := range a.accessPatterns {
		placeholders, restSuffix, ok := accessPatt.match(subkeys)
		if !ok {
			continue
		}

		path, err := accessPatt.storagePath(placeholders)
		if err != nil {
			return nil, err
		}

		if !accessPatt.isReadable() {
			return nil, &InvalidAccessError{RequestedAccess: read, FieldAccess: accessPatt.access, Field: request}
		}

		m := requestMatch{storagePath: path, suffixParts: restSuffix}
		matches = append(matches, m)
	}

	// sort matches by namespace (unmatched suffix) to ensure that nested matches
	// are read after
	sort.Sort(byNamespace(matches))

	if len(matches) == 0 {
		return nil, notFoundErrorFrom(a, request, "no matching read rule")
	}

	return matches, nil
}

type byNamespace []requestMatch

func (b byNamespace) Len() int      { return len(b) }
func (b byNamespace) Swap(x, y int) { b[x], b[y] = b[y], b[x] }
func (b byNamespace) Less(x, y int) bool {
	xNamespace, yNamespace := b[x].suffixParts, b[y].suffixParts

	minLen := int(math.Min(float64(len(xNamespace)), float64(len(yNamespace))))
	for i := 0; i < minLen; i++ {
		if xNamespace[i] < yNamespace[i] {
			return true
		} else if xNamespace[i] > yNamespace[i] {
			return false
		}
	}

	return len(xNamespace) < len(yNamespace)
}

func newAccessPattern(request, storage, accesstype string) (*accessPattern, error) {
	accType, err := newAccessType(accesstype)
	if err != nil {
		return nil, fmt.Errorf("cannot create aspect pattern: %w", err)
	}

	requestSubkeys := strings.Split(request, ".")
	requestMatchers := make([]requestMatcher, 0, len(requestSubkeys))
	for _, subkey := range requestSubkeys {
		var patt requestMatcher
		if isPlaceholder(subkey) {
			patt = placeholder(subkey[1 : len(subkey)-1])
		} else {
			patt = literal(subkey)
		}

		requestMatchers = append(requestMatchers, patt)
	}

	pathSubkeys := strings.Split(storage, ".")
	pathWriters := make([]storageWriter, 0, len(pathSubkeys))
	for _, subkey := range pathSubkeys {
		var patt storageWriter
		if isPlaceholder(subkey) {
			patt = placeholder(subkey[1 : len(subkey)-1])
		} else {
			patt = literal(subkey)
		}

		pathWriters = append(pathWriters, patt)
	}

	return &accessPattern{
		originalRequest: request,
		request:         requestMatchers,
		storage:         pathWriters,
		access:          accType,
	}, nil
}

func isPlaceholder(part string) bool {
	return part[0] == '{' && part[len(part)-1] == '}'
}

// accessPattern represents an individual aspect access pattern. It can be used
// to match a request and map it into a corresponding storage, potentially with
// placeholders filled in.
type accessPattern struct {
	originalRequest string
	request         []requestMatcher
	storage         []storageWriter
	access          accessType
}

// match returns true if the subkeys match the pattern exactly or as a prefix.
// If placeholders are "filled in" when matching, those are returned in a map.
// If the subkeys match as a prefix, the remaining suffix is returned.
func (p *accessPattern) match(reqSubkeys []string) (placeholders map[string]string, restSuffix []string, match bool) {
	if len(p.request) < len(reqSubkeys) {
		return nil, nil, false
	}

	placeholders = make(map[string]string)
	for i, subkey := range reqSubkeys {
		if !p.request[i].match(subkey, placeholders) {
			return nil, nil, false
		}
	}

	for _, key := range p.request[len(reqSubkeys):] {
		restSuffix = append(restSuffix, key.String())
	}

	return placeholders, restSuffix, true
}

// storagePath takes a map of placeholders to their values in the aspect name and
// returns the path with its placeholder values filled in with the map's values.
func (p *accessPattern) storagePath(placeholders map[string]string) (string, error) {
	sb := &strings.Builder{}

	for _, subkey := range p.storage {
		if sb.Len() > 0 {
			if _, err := sb.WriteRune('.'); err != nil {
				return "", err
			}
		}

		// TODO: this doesn't support unmatched placeholders yet
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
type requestMatcher interface {
	match(subkey string, placeholders map[string]string) bool
	String() string
}

type storageWriter interface {
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

// String returns the placeholder as a string.
func (p placeholder) String() string {
	return "{" + string(p) + "}"
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

// String returns the literal as a string.
func (p literal) String() string {
	return string(p)
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
