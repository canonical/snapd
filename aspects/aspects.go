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
	Operation  string
	Request    string
	Cause      string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("cannot %s %q in aspect %s/%s/%s: %s", e.Operation, e.Request, e.Account, e.BundleName, e.Aspect, e.Cause)
}

func (e *NotFoundError) Is(err error) bool {
	_, ok := err.(*NotFoundError)
	return ok
}

func notFoundErrorFrom(a *Aspect, op, request, errMsg string) *NotFoundError {
	return &NotFoundError{
		Account:    a.bundle.Account,
		BundleName: a.bundle.Name,
		Aspect:     a.Name,
		Operation:  op,
		Request:    request,
		Cause:      errMsg,
	}
}

type BadRequestError struct {
	Account    string
	BundleName string
	Aspect     string
	Operation  string
	Request    string
	Cause      string
}

func (e *BadRequestError) Error() string {
	return fmt.Sprintf("cannot %s %q in aspect %s/%s/%s: %s", e.Operation, e.Request, e.Account, e.BundleName, e.Aspect, e.Cause)
}

func (e *BadRequestError) Is(err error) bool {
	_, ok := err.(*BadRequestError)
	return ok
}

func badRequestErrorFrom(a *Aspect, operation, request, errMsg string, v ...interface{}) *BadRequestError {
	return &BadRequestError{
		Account:    a.bundle.Account,
		BundleName: a.bundle.Name,
		Aspect:     a.Name,
		Operation:  operation,
		Request:    request,
		Cause:      fmt.Sprintf(errMsg, v...),
	}
}

// DataBag controls access to the aspect data storage.
type DataBag interface {
	Get(path string) (interface{}, error)
	Set(path string, value interface{}) error
	Data() ([]byte, error)
}

// Schema takes in data from the DataBag and validates that it's valid and could
// be committed.
type Schema interface {
	Validate(data []byte) error

	// SchemaAt returns the schemas (e.g., string, int, etc) that may be at the
	// provided path. If the path cannot be followed, an error is returned.
	SchemaAt(path []string) ([]Schema, error)

	// Type returns the SchemaType corresponding to the Schema.
	Type() SchemaType
}

type SchemaType uint

func (v SchemaType) String() string {
	if int(v) >= len(typeStrings) {
		panic("unknown schema type")
	}

	return typeStrings[v]
}

const (
	Int SchemaType = iota
	Number
	String
	Bool
	Map
	Array
	Any
	Alt
)

var typeStrings = [...]string{"int", "number", "string", "bool", "map", "array", "any", "alt"}

// Bundle holds a series of related aspects.
type Bundle struct {
	Account string
	Name    string
	schema  Schema
	aspects map[string]*Aspect
}

// NewBundle returns a new aspect bundle for the specified aspects
// and access patterns.
func NewBundle(account string, bundleName string, aspects map[string]interface{}, schema Schema) (*Bundle, error) {
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
		aspectMap, ok := v.(map[string]interface{})
		if !ok || len(aspectMap) == 0 {
			return nil, fmt.Errorf("cannot define aspect %q: aspect must be non-empty map", name)
		}

		rules, ok := aspectMap["rules"].([]interface{})
		if !ok || len(rules) == 0 {
			return nil, fmt.Errorf("cannot define aspect %q: aspect rules must be non-empty list", name)
		}

		aspect, err := newAspect(aspectBundle, name, rules)
		if err != nil {
			return nil, fmt.Errorf("cannot define aspect %q: %w", name, err)
		}

		aspectBundle.aspects[name] = aspect
	}

	return aspectBundle, nil
}

func newAspect(bundle *Bundle, name string, aspectPatterns []interface{}) (*Aspect, error) {
	aspect := &Aspect{
		Name:           name,
		accessPatterns: make([]*accessPattern, 0, len(aspectPatterns)),
		bundle:         bundle,
	}

	readRequests := make(map[string]bool)
	for _, aspectPatternRaw := range aspectPatterns {
		aspectPattern, ok := aspectPatternRaw.(map[string]interface{})
		if !ok {
			return nil, errors.New("each access pattern should be a map")
		}

		requestRaw, ok := aspectPattern["request"]
		if !ok || requestRaw == "" {
			return nil, errors.New(`access patterns must have a "request" field`)
		}

		request, ok := requestRaw.(string)
		if !ok {
			return nil, errors.New(`"request" must be a string`)
		}

		storageRaw, ok := aspectPattern["storage"]
		if !ok || storageRaw == "" {
			return nil, errors.New(`access patterns must have a "storage" field`)
		}

		storage, ok := storageRaw.(string)
		if !ok {
			return nil, errors.New(`"storage" must be a string`)
		}

		if err := validateRequestStoragePair(request, storage); err != nil {
			return nil, err
		}

		accessRaw, ok := aspectPattern["access"]
		var access string
		if ok {
			access, ok = accessRaw.(string)
			if !ok {
				return nil, errors.New(`"access" must be a string`)
			}
		}

		switch access {
		case "read", "read-write", "":
			if readRequests[request] {
				return nil, fmt.Errorf(`cannot have several reading rules with the same "request" field`)
			}
			readRequests[request] = true
		}

		accPattern, err := newAccessPattern(request, storage, access)
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
	opts := &validationOptions{allowPlaceholder: true}
	if err := validateAspectDottedPath(request, opts); err != nil {
		return fmt.Errorf("invalid request %q: %w", request, err)
	}

	if err := validateAspectDottedPath(storage, opts); err != nil {
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

type validationOptions struct {
	// allowPlaceholder means that placeholders are accepted when validating.
	allowPlaceholder bool
}

// validateAspectDottedPath validates that request/storage strings in an aspect definition are:
//   - composed of non-empty, dot-separated subkeys with optional placeholders ("foo.{bar}"),
//     if allowed by the validationOptions
//   - non-placeholder subkeys are made up of lowercase alphanumeric ASCII characters,
//     optionally with dashes between alphanumeric characters (e.g., "a-b-c")
//   - placeholder subkeys are composed of non-placeholder subkeys wrapped in curly brackets
func validateAspectDottedPath(path string, opts *validationOptions) (err error) {
	if opts == nil {
		opts = &validationOptions{}
	}

	subkeys := strings.Split(path, ".")
	for _, subkey := range subkeys {
		if subkey == "" {
			return errors.New("cannot have empty subkeys")
		}

		if !validSubkey.MatchString(subkey) && (!opts.allowPlaceholder || !validPlaceholder.MatchString(subkey)) {
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
		if isPlaceholder(subkey) {
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
	if err := validateAspectDottedPath(request, nil); err != nil {
		return badRequestErrorFrom(a, "set", request, err.Error())
	}

	var matches []requestMatch
	subkeys := strings.Split(request, ".")
	for _, accessPatt := range a.accessPatterns {
		placeholders, suffixParts, ok := accessPatt.match(subkeys)
		if !ok {
			continue
		}

		for _, part := range suffixParts {
			// TODO: add support for setting with unmatched placeholders
			if isPlaceholder(part) {
				return badRequestErrorFrom(a, "set", request, "cannot set with unmatched placeholders")
			}
		}

		if !accessPatt.isWriteable() {
			continue
		}

		path, err := accessPatt.storagePath(placeholders)
		if err != nil {
			return err
		}

		matches = append(matches, requestMatch{
			storagePath: path,
			suffixParts: suffixParts,
			request:     accessPatt.originalRequest,
		})
	}

	if len(matches) == 0 {
		return notFoundErrorFrom(a, "set", request, "no matching write rule")
	}

	// sort less nested paths before more nested ones so that writes aren't overwritten
	sort.Slice(matches, func(x, y int) bool {
		return matches[x].storagePath < matches[y].storagePath
	})

	if value != nil {
		if err := checkSchemaMismatch(a.bundle.schema, matches); err != nil {
			return err
		}
	}

	for _, match := range matches {
		nestedValue := value

		for _, part := range match.suffixParts {
			mapVal, ok := nestedValue.(map[string]interface{})
			if !ok {
				return badRequestErrorFrom(a, "set", request, `expected map for unmatched request parts but got %T`, nestedValue)
			}

			nestedValue, ok = mapVal[part]
			if !ok {
				return badRequestErrorFrom(a, "set", request, `cannot find nested value with unmatched key %q`, part)
			}
		}

		if err := databag.Set(match.storagePath, nestedValue); err != nil {
			return err
		}

		data, err := databag.Data()
		if err != nil {
			return err
		}

		if err := a.bundle.schema.Validate(data); err != nil {
			return fmt.Errorf(`cannot write data: %w`, err)
		}
	}

	return nil
}

func checkSchemaMismatch(schema Schema, matches []requestMatch) error {
	pathTypes := make(map[string][]SchemaType)
out:
	for _, match := range matches {
		path := match.storagePath
		pathParts := strings.Split(path, ".")
		schemas, err := schema.SchemaAt(pathParts)
		if err != nil {
			var serr *schemaAtError
			if errors.As(err, &serr) {
				parts := strings.Split(path, ".")
				subParts := parts[:len(parts)-serr.left]
				subPath := strings.Join(subParts, ".")

				return fmt.Errorf(`path %q for request %q is invalid after %q: %w`,
					path, match.request, subPath, serr.err)
			}

			return fmt.Errorf(`internal error: unexpected error finding schema at %q: %w`, path, err)
		}

		var newTypes []SchemaType
		for _, schema := range schemas {
			switch t := schema.Type(); t {
			case Any:
				// schema accepts "any" so it's never incompatible w/ other paths
				continue out
			case Alt:
				// shouldn't happen except for programmer error because alternatives'
				// SchemaAt should return the composing schemas, not itself
				return fmt.Errorf(`internal error: unexpected Alt schema type along path`)
			default:
				newTypes = append(newTypes, t)
			}

		}

		for oldPath, oldTypes := range pathTypes {
			var pathMatch bool
		pathMatching:
			for _, newType := range newTypes {
				// find a pair of types in the two paths that can accept the same data
				for _, oldType := range oldTypes {
					if newType == oldType || (newType == Number && oldType == Int) || (newType == Int && oldType == Number) {
						// accept two different types of number since an int could apply to both
						pathMatch = true
						break pathMatching
					}
				}
			}

			if !pathMatch {
				oldSetStr, newSetStr := schemaTypesStr(oldTypes), schemaTypesStr(newTypes)
				return fmt.Errorf(`storage paths %q and %q for request %q require incompatible types: %s != %s`,
					oldPath, path, match.request, oldSetStr, newSetStr)
			}
		}

		pathTypes[path] = newTypes
	}

	return nil
}

func schemaTypesStr(types []SchemaType) string {
	if len(types) == 1 {
		return types[0].String()
	}

	var sb strings.Builder
	sb.WriteRune('[')
	for i, typ := range types {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(typ.String())
	}
	sb.WriteRune(']')

	return sb.String()
}

// namespaceResult creates a nested namespace around the result that corresponds
// to the unmatched entry parts. Unmatched placeholders are filled in using maps
// of all the matching values in the databag.
func namespaceResult(res interface{}, suffixParts []string) (interface{}, error) {
	if len(suffixParts) == 0 {
		return res, nil
	}

	// check if the part is an unmatched placeholder which should have been filled
	// by the databag with all possible values
	part := suffixParts[0]
	if isPlaceholder(part) {
		values, ok := res.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("internal error: expected databag to return map for unmatched placeholder")
		}

		level := make(map[string]interface{}, len(values))
		for k, v := range values {
			nested, err := namespaceResult(v, suffixParts[1:])
			if err != nil {
				return nil, err
			}

			level[k] = nested
		}

		return level, nil
	}

	nested, err := namespaceResult(res, suffixParts[1:])
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{part: nested}, nil
}

// Get returns the aspect value identified by the request. If either the named
// aspect or the corresponding value can't be found, a NotFoundError is returned.
func (a *Aspect) Get(databag DataBag, request string) (interface{}, error) {
	if err := validateAspectDottedPath(request, nil); err != nil {
		return nil, badRequestErrorFrom(a, "get", request, err.Error())
	}

	matches, err := a.matchGetRequest(request)
	if err != nil {
		return nil, err
	}

	var merged interface{}
	for _, match := range matches {
		val, err := databag.Get(match.storagePath)
		if err != nil {
			if errors.Is(err, PathError("")) {
				continue
			}
			return nil, err
		}

		// build a namespace around the result based on the unmatched suffix parts
		val, err = namespaceResult(val, match.suffixParts)
		if err != nil {
			return nil, err
		}

		// merge result with results from other matching rules
		merged, err = mergeNamespaces(merged, val)
		if err != nil {
			return nil, err
		}
	}

	if merged == nil {
		return nil, notFoundErrorFrom(a, "get", request, "matching rules don't map to any values")
	}

	return merged, nil
}

func mergeNamespaces(old, new interface{}) (interface{}, error) {
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
			merged, err := mergeNamespaces(storeVal, v)
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

	// request is the full request as it appears in the assertion's access rule.
	request string
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
			continue
		}

		m := requestMatch{
			storagePath: path,
			suffixParts: restSuffix,
			request:     accessPatt.originalRequest,
		}
		matches = append(matches, m)
	}

	if len(matches) == 0 {
		return nil, notFoundErrorFrom(a, "get", request, "no matching read rule")
	}

	// sort matches by namespace (unmatched suffix) to ensure that nested matches
	// are read after
	sort.Slice(matches, func(x, y int) bool {
		xNamespace, yNamespace := matches[x].suffixParts, matches[y].suffixParts

		minLen := int(math.Min(float64(len(xNamespace)), float64(len(yNamespace))))
		for i := 0; i < minLen; i++ {
			if xNamespace[i] == yNamespace[i] {
				continue
			}
			return xNamespace[i] < yNamespace[i]
		}

		return len(xNamespace) < len(yNamespace)
	})

	return matches, nil
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
		// placeholder wasn't matched, return the original key in brackets
		subkey = fmt.Sprintf("{%s}", string(p))
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

type PathError string

func (e PathError) Error() string {
	return string(e)
}

func (e PathError) Is(err error) bool {
	_, ok := err.(PathError)
	return ok
}

func pathErrorf(str string, v ...interface{}) PathError {
	return PathError(fmt.Sprintf(str, v...))
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
func (s JSONDataBag) Get(path string) (interface{}, error) {
	// TODO: create this in the return below as well?
	var value interface{}
	subKeys := strings.Split(path, ".")
	if err := get(subKeys, 0, s, &value); err != nil {
		return nil, err
	}

	return value, nil
}

// get takes a dotted path split into sub-keys and uses it to traverse a JSON object.
// The path's sub-keys can be literals, in which case that value is used to
// traverse the tree, or a bracketed placeholder (e.g., "{foo}"). For placeholders,
// we take all sub-paths and try to match the remaining path. The results for
// any sub-path that matched the request path are then merged in a map and returned.
func get(subKeys []string, index int, node map[string]json.RawMessage, result *interface{}) error {
	key := subKeys[index]
	matchAll := isPlaceholder(key)

	rawLevel, ok := node[key]
	if !matchAll && !ok {
		pathPrefix := strings.Join(subKeys[:index+1], ".")
		return pathErrorf("no value was found under path %q", pathPrefix)
	}

	// read the final value
	if index == len(subKeys)-1 {
		if matchAll {
			// request ends in placeholder so return map to all values (but unmarshal the rest first)
			level := make(map[string]interface{}, len(node))
			for k, v := range node {
				var deser interface{}
				if err := json.Unmarshal(v, &deser); err != nil {
					return fmt.Errorf(`internal error: %w`, err)
				}
				level[k] = deser
			}

			*result = level
			return nil
		}

		if err := json.Unmarshal(rawLevel, result); err != nil {
			return fmt.Errorf(`internal error: %w`, err)
		}

		return nil
	}

	if matchAll {
		results := make(map[string]interface{})

		for k, v := range node {
			var level map[string]json.RawMessage
			if err := jsonutil.DecodeWithNumber(bytes.NewReader(v), &level); err != nil {
				if _, ok := err.(*json.UnmarshalTypeError); ok {
					// we consider only the values for which the rest of the nested sub-keys
					// can be fulfilled
					continue
				}
				return err
			}

			// walk the path under all possible values, only return an error if no value
			// is found under any path
			var res interface{}
			if err := get(subKeys, index+1, level, &res); err != nil {
				if errors.Is(err, PathError("")) {
					continue
				}
			}

			if res != nil {
				results[k] = res
			}
		}

		if len(results) == 0 {
			pathPrefix := strings.Join(subKeys[:index+1], ".")
			return pathErrorf("no value was found under path %q", pathPrefix)
		}

		*result = results
		return nil
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

	var level map[string]json.RawMessage
	err := jsonutil.DecodeWithNumber(bytes.NewReader(rawLevel), &level)
	if err != nil {
		var uerr *json.UnmarshalTypeError
		if !errors.As(err, &uerr) {
			return nil, err
		}
	}

	// stored valued wasn't map but new write expects one so overwrite value
	if level == nil {
		level = make(map[string]json.RawMessage)
	}

	rawLevel, err = set(subKeys, index+1, level, value)
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

// SchemaAt always returns the JSONSchema.
func (v JSONSchema) SchemaAt(path []string) ([]Schema, error) {
	return []Schema{v}, nil
}

func (v JSONSchema) Type() SchemaType {
	return Any
}
