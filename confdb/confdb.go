// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022-2025 Canonical Ltd
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

package confdb

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/i18n"
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

	return readWrite, fmt.Errorf("expected 'access' to be either %s or empty but was %q", strutil.Quoted(accessTypeStrings), access)
}

type NoDataError struct {
	requests []string
	view     string
}

func (e *NoDataError) Is(err error) bool {
	_, ok := err.(*NoDataError)
	return ok
}

func (e *NoDataError) Error() string {
	var reqStr string
	switch len(e.requests) {
	case 0:
		// leave empty, so the message reflects the whole view
	case 1:
		reqStr = fmt.Sprintf(i18n.G(" %q through"), e.requests[0])
	default:
		reqStr = fmt.Sprintf(i18n.G(" %s through"), strutil.Quoted(e.requests))
	}

	return fmt.Sprintf(i18n.G("cannot get%s %s: no data"), reqStr, e.view)
}

func NewNoDataError(view *View, requests []string) *NoDataError {
	return &NoDataError{
		requests: requests,
		view:     view.ID(),
	}
}

type NoMatchError struct {
	operation string
	requests  []string
	view      string
}

func (e *NoMatchError) Is(err error) bool {
	_, ok := err.(*NoMatchError)
	return ok
}

func (e *NoMatchError) Error() string {
	var reqStr string
	switch len(e.requests) {
	case 1:
		reqStr = "\"" + e.requests[0] + "\""
	default:
		reqStr = strutil.Quoted(e.requests)
	}

	return fmt.Sprintf(i18n.G("cannot %s %s through %s: no matching rule"), e.operation, reqStr, e.view)
}

func NewNoMatchError(view *View, operation string, requests []string) *NoMatchError {
	return &NoMatchError{
		operation: operation,
		requests:  requests,
		view:      view.ID(),
	}
}

type BadRequestError struct {
	viewID    string
	operation string
	request   string
	cause     string
}

func (e *BadRequestError) Error() string {
	var reqStr string
	if e.request != "" {
		reqStr = "\"" + e.request + "\""
	} else {
		reqStr = "empty path"
	}

	var causeSuffix string
	if e.cause != "" {
		causeSuffix = ": " + e.cause
	}
	return fmt.Sprintf("cannot %s %s through confdb view %s%s", e.operation, reqStr, e.viewID, causeSuffix)
}

func (e *BadRequestError) Is(err error) bool {
	_, ok := err.(*BadRequestError)
	return ok
}

func badRequestErrorFrom(v *View, operation, request, msg string) *BadRequestError {
	return &BadRequestError{
		viewID:    v.ID(),
		operation: operation,
		request:   request,
		cause:     msg,
	}
}

// Databag controls access to the confdb data storage.
type Databag interface {
	Get(path string) (any, error)
	Set(path string, value any) error
	Unset(path string) error
	Data() ([]byte, error)
}

// DatabagSchema takes in data from the Databag and validates that it's valid
// and could be committed.
type DatabagSchema interface {
	// Validate checks that the data conforms to the schema.
	Validate(data []byte) error

	// SchemaAt returns the schemas (e.g., string, int, etc) that may be at the
	// provided path. If the path cannot be followed, an error is returned.
	SchemaAt(path []string) ([]DatabagSchema, error)

	// Type returns the SchemaType corresponding to the Schema.
	Type() SchemaType

	// Ephemeral returns true if the data corresponding to this type should not be
	// saved by snapd.
	Ephemeral() bool

	// NestedEphemeral returns true if the type or any of its nested types are
	// ephemeral.
	NestedEphemeral() bool
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

var (
	typeStrings = [...]string{"int", "number", "string", "bool", "map", "array", "any", "alt"}

	ValidConfdbName = validSubkey
	ValidViewName   = validSubkey

	validSubkey           = regexp.MustCompile(fmt.Sprintf("^%s$", subkeyRegex))
	validIndexSubkey      = regexp.MustCompile(`^\[[0-9]+\]$`)
	validPlaceholder      = regexp.MustCompile(fmt.Sprintf("^{%s}$", subkeyRegex))
	validIndexPlaceholder = regexp.MustCompile(fmt.Sprintf("^\\[{%s}\\]$", subkeyRegex))
	// TODO: decide on what the format should be for aliases in schemas
	validAliasName = validSubkey
	subkeyRegex    = "[a-z](?:-?[a-z0-9])*"
)

// Schema holds a set of views that describe how the confdb can be accessed as
// well as a schema for the storage.
type Schema struct {
	Account       string
	Name          string
	DatabagSchema DatabagSchema
	views         map[string]*View
}

// GetViewsAffectedByPath returns all the views in the confdb schema that have
// visibility into a storage path.
func (s *Schema) GetViewsAffectedByPath(path string) []*View {
	var views []*View
	for _, view := range s.views {
		for _, rule := range view.rules {
			if pathChangeAffects(path, rule.originalStorage) {
				views = append(views, view)
				break
			}
		}
	}

	return views
}

func pathChangeAffects(modified, affected string) bool {
	moddedPathKeys, affectedPathKeys := strings.Split(modified, "."), strings.Split(affected, ".")

	for i, affectedKey := range affectedPathKeys {
		if isPlaceholder(affectedKey) {
			continue
		}

		if len(moddedPathKeys) <= i {
			// 'affected' is a sub-path of 'modified' so changes to the latter may
			// affect the former (they also may not but we need to play it safe)
			return true
		}

		if moddedPathKeys[i] != affectedKey {
			return false
		}
	}

	// 'modified' is a sub-path of 'affected' so changes to the former are visible
	// to the latter
	return true
}

// NewSchema returns a new confdb schema with the specified views (and their
// rules) and storage schema.
func NewSchema(account string, dbSchemaName string, views map[string]any, schema DatabagSchema) (*Schema, error) {
	if len(views) == 0 {
		return nil, errors.New(`cannot define confdb schema: no views`)
	}

	dbSchema := &Schema{
		Account:       account,
		Name:          dbSchemaName,
		DatabagSchema: schema,
		views:         make(map[string]*View, len(views)),
	}

	for name, v := range views {
		if !ValidViewName.Match([]byte(name)) {
			return nil, fmt.Errorf("cannot define view %q: name must conform to %s", name, subkeyRegex)
		}

		viewMap, ok := v.(map[string]any)
		if !ok || len(viewMap) == 0 {
			return nil, fmt.Errorf("cannot define view %q: view must be non-empty map", name)
		}

		if summary, ok := viewMap["summary"]; ok {
			if _, ok = summary.(string); !ok {
				return nil, fmt.Errorf("cannot define view %q: view summary must be a string but got %T", name, summary)
			}
		}

		rules, ok := viewMap["rules"].([]any)
		if !ok || len(rules) == 0 {
			return nil, fmt.Errorf("cannot define view %q: view rules must be non-empty list", name)
		}

		view, err := newView(dbSchema, name, rules)
		if err != nil {
			return nil, fmt.Errorf("cannot define view %q: %w", name, err)
		}

		dbSchema.views[name] = view
	}

	return dbSchema, nil
}

func newView(dbSchema *Schema, name string, viewRules []any) (*View, error) {
	view := &View{
		Name:   name,
		rules:  make([]*viewRule, 0, len(viewRules)),
		schema: dbSchema,
	}

	for _, ruleRaw := range viewRules {
		rules, err := parseRule(nil, ruleRaw)
		if err != nil {
			return nil, err
		}

		view.rules = append(view.rules, rules...)
	}

	readRequests := make(map[string]bool)
	for _, rule := range view.rules {
		switch rule.access {
		case read, readWrite:
			if readRequests[rule.originalRequest] {
				return nil, fmt.Errorf(`cannot have several reading rules with the same "request" field`)
			}

			readRequests[rule.originalRequest] = true
		}
	}

	// check that the rules matching a given request can be satisfied with some
	// data type (otherwise, no data can ever be written there)
	pathToRules := make(map[string][]*viewRule)
	for _, rule := range view.rules {
		// TODO: once the paths support list index placeholders, also add mapping
		// for the prefixes of each path and their implied types (Map or Array)
		path := rule.originalRequest
		pathToRules[path] = append(pathToRules[path], rule)
	}

	for _, rules := range pathToRules {
		if err := checkSchemaMismatch(dbSchema.DatabagSchema, rules); err != nil {
			return nil, err
		}
	}

	return view, nil
}

func parseRule(parent *viewRule, ruleRaw any) ([]*viewRule, error) {
	ruleMap, ok := ruleRaw.(map[string]any)
	if !ok {
		return nil, errors.New("each view rule should be a map")
	}

	storageRaw, ok := ruleMap["storage"]
	if !ok || storageRaw == "" {
		return nil, errors.New(`view rules must have a "storage" field`)
	}

	storage, ok := storageRaw.(string)
	if !ok {
		return nil, errors.New(`"storage" must be a string`)
	}

	requestRaw, ok := ruleMap["request"]
	if !ok {
		// if omitted the "request" field defaults to the same as the "storage"
		requestRaw = storage
	} else if requestRaw == "" {
		return nil, errors.New(`view rules' "request" field must be non-empty, if it exists`)
	}

	request, ok := requestRaw.(string)
	if !ok {
		return nil, errors.New(`"request" must be a string`)
	}

	// content sub-rules are shorthands for paths that include the parent's path
	if parent != nil {
		if request[0] != '[' {
			request = "." + request
		}
		request = parent.originalRequest + request

		if storage[0] != '[' {
			storage = "." + storage
		}
		storage = parent.originalStorage + storage
	}

	reqAccessors, storageAccessors, err := validateRequestStoragePair(request, storage)
	if err != nil {
		return nil, err
	}

	accessRaw, ok := ruleMap["access"]
	var access string
	if ok {
		access, ok = accessRaw.(string)
		if !ok {
			return nil, errors.New(`"access" must be a string`)
		}
	}

	rule, err := newViewRule(reqAccessors, storageAccessors, access)
	if err != nil {
		return nil, err
	}

	rules := []*viewRule{rule}
	if contentRaw, ok := ruleMap["content"]; ok {
		contentRulesRaw, ok := contentRaw.([]any)
		if !ok || len(contentRulesRaw) == 0 {
			return nil, fmt.Errorf(`"content" must be a non-empty list`)
		}

		for _, contentRule := range contentRulesRaw {
			nestedRules, err := parseRule(rule, contentRule)
			if err != nil {
				return nil, err
			}

			rules = append(rules, nestedRules...)
		}
	}

	return rules, nil
}

// validateRequestStoragePair checks that:
//   - request and storage are composed of valid subkeys (see: validateViewDottedPath)
//   - all placeholders in a request are in the storage and vice-versa
//   - names used for index placeholders are not used for key placeholders and vice-versa
//
// If the validation succeeds, it returns lists of typed representations of each
// path.
func validateRequestStoragePair(request, storage string) (reqAccessors []accessor, storageAccessors []accessor, err error) {
	opts := parseOpts{pathType: viewPath, forbidIndexes: true}
	reqAccessors, err = parsePathIntoAccessors(request, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid request %q: %w", request, err)
	}

	opts.forbidIndexes = false
	storageAccessors, err = parsePathIntoAccessors(storage, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid storage %q: %w", storage, err)
	}

	reqKeyVars, err := countAccessorsOfType(reqAccessors, keyPlaceholderType)
	if err != nil {
		return nil, nil, err
	}

	storageKeyVars, err := countAccessorsOfType(storageAccessors, keyPlaceholderType)
	if err != nil {
		return nil, nil, err
	}

	// check that the request and storage key placeholders match
	err = checkForMatchingPlaceholders(request, storage, reqKeyVars, storageKeyVars)
	if err != nil {
		return nil, nil, err
	}

	// check that the request and storage list index placeholders match
	reqIndexVars, err := countAccessorsOfType(reqAccessors, indexPlaceholderType)
	if err != nil {
		return nil, nil, err
	}

	storageIndexVars, err := countAccessorsOfType(storageAccessors, indexPlaceholderType)
	if err != nil {
		return nil, nil, err
	}

	err = checkForMatchingPlaceholders(request, storage, reqIndexVars, storageIndexVars)
	if err != nil {
		return nil, nil, err
	}

	// check that there are no key and index placeholders with the same name.
	// technically, this would work (there's no ambiguity because no value matches
	// both a key and an index) but it could make view paths very confusing
	for name := range reqKeyVars {
		if _, ok := reqIndexVars[name]; ok {
			return nil, nil, fmt.Errorf("cannot use same name %q for key and index placeholder: %s", name, request)
		}
	}

	return reqAccessors, storageAccessors, nil
}

// check that placeholders used in a request path are accounted in the storage
// path (and vice-versa) and that we don't use them to mean more than one thing
func checkForMatchingPlaceholders(request, storage string, reqPlaceholders, storagePlaceholders map[string]int) error {
	if len(reqPlaceholders) != len(storagePlaceholders) {
		return fmt.Errorf("request %q and storage %q have mismatched placeholders", request, storage)
	}

	for name, count := range reqPlaceholders {
		if count != 1 {
			return fmt.Errorf("request cannot have more than one placeholder with the same name %q: %s",
				name, request)
		}

		if storagePlaceholders[name] == 0 {
			return fmt.Errorf("placeholder %q from request %q is absent from storage %q",
				name, request, storage)
		}
	}
	return nil
}

// pathType determines which type of path is being validated, a view path
// defined in a confdb-schema's rule or a user supplied path. User paths can
// only contain literal parts while assertion paths can contain placeholders.
type pathType uint8

const (
	// viewPath represents a path coming for a view's rule definition which can
	// have any type of sub-key (including placeholders).
	viewPath pathType = iota
	// userPath represents a path supplied by a user which can only include literals
	// but not placeholders.
	userPath
)

type parseOpts struct {
	pathType         pathType
	forbidIndexes    bool
	allowPartialPath bool
}

// parsePathIntoAccessors validates that the path is composed of (some of these
// may be disabled depending on the options passed):
//   - composed of non-empty, dot or bracket separated subkeys with optional
//     placeholders (e.g., foo.{bar}, a[{n}].bar), if allowed by the validationOptions
//   - non-placeholder subkeys are made up of lowercase alphanumeric ASCII characters,
//     optionally with dashes between alphanumeric characters (e.g., "a-b-c")
//   - placeholder subkeys are composed of non-placeholder subkeys wrapped in curly brackets
//   - bracketed subkeys that aren't placeholders can only contain integers
//
// If the validation succeeds, it returns an []accessor which contains typed
// representations of each type of subkey (e.g., key placeholder, index, etc).
func parsePathIntoAccessors(path string, opts parseOpts) ([]accessor, error) {
	if path == "" {
		return nil, nil
	}

	subkeys, err := splitViewPath(path, opts)
	if err != nil {
		return nil, err
	}

	accessors := make([]accessor, 0, len(subkeys))
	for _, subkey := range subkeys {
		isKey := validSubkey.MatchString(subkey)
		isIndex := validIndexSubkey.MatchString(subkey)
		isKeyPlaceholder := validPlaceholder.MatchString(subkey)
		isIndexPlaceholder := validIndexPlaceholder.MatchString(subkey)

		switch {
		case isKey:
			accessors = append(accessors, key(subkey))
		case isIndex:
			if opts.forbidIndexes {
				return nil, fmt.Errorf("invalid subkey %q: view paths cannot have literal indexes (only index placeholders)", subkey)
			}
			accessors = append(accessors, index(subkey[1:len(subkey)-1]))

		case opts.pathType == userPath:
			// user supplied paths cannot contain placeholders
			var errSuffix string
			if isKeyPlaceholder || isIndexPlaceholder {
				errSuffix = ": path only supports literal keys and indexes"
			}
			return nil, fmt.Errorf("invalid subkey %q%s", subkey, errSuffix)

		case isKeyPlaceholder:
			accessors = append(accessors, keyPlaceholder(subkey[1:len(subkey)-1]))
		case isIndexPlaceholder:
			accessors = append(accessors, indexPlaceholder(subkey[2:len(subkey)-2]))
		default:
			return nil, fmt.Errorf("invalid subkey %q", subkey)
		}
	}

	return accessors, nil
}

type keyType uint8

const (
	mapKeyType keyType = iota
	listIndexType
	keyPlaceholderType
	indexPlaceholderType
)

type accessor interface {
	// name returns the value of the path sub-key excluding any separators (dots
	// or brackets), both for literal and placeholders.
	name() string

	// access returns the value of the sub-key wrapped in any separators or brackets
	// the type may require to be composed into a path.
	access() string

	// keyType returns a type that represents the kind of path sub-key the accessor is.
	keyType() keyType
}

func splitViewPath(path string, opts parseOpts) ([]string, error) {
	var subkeys []string
	sb := &strings.Builder{}

	finishSubkey := func() error {
		if sb.Len() == 0 {
			if len(subkeys) == 0 && opts.allowPartialPath {
				// we may be parsing a suffix of a path 'foo[2].bar' so allow a path to
				// start with a separator '[2].bar'
				return nil
			}
			return errors.New("cannot have empty subkeys")
		}
		subkeys = append(subkeys, sb.String())
		sb.Reset()
		return nil
	}

	for _, c := range path {
		switch c {
		case '.':
			if err := finishSubkey(); err != nil {
				return nil, err
			}

		case '[':
			if err := finishSubkey(); err != nil {
				return nil, err
			}

			// include the square brackets as they imply a different type of placeholder
			fallthrough

		default:
			sb.WriteRune(c)
		}
	}

	// there should be a subkey to be finished (paths like "a." are invalid)
	if err := finishSubkey(); err != nil {
		return nil, err
	}

	return subkeys, nil
}

// countAccessorsOfType returns the number of occurrences of path sub-keys of
// a given type of accessor (e.g., key placeholder, etc).
func countAccessorsOfType(accessors []accessor, keyType keyType) (map[string]int, error) {
	var freqs map[string]int
	count := func(key accessor) {
		if freqs == nil {
			freqs = make(map[string]int)
		}
		freqs[key.name()]++
	}

	for _, acc := range accessors {
		if acc.keyType() != keyType {
			continue
		}

		count(acc)
	}

	return freqs, nil
}

// View returns a view from the confdb schema.
func (s *Schema) View(view string) *View {
	return s.views[view]
}

// View carries access rules for a particular view in a confdb schema.
type View struct {
	Name   string
	rules  []*viewRule
	schema *Schema
}

func (v *View) Schema() *Schema {
	return v.schema
}

type expandedMatch struct {
	// storagePath is dot-separated storage path without unfilled placeholders.
	storagePath string

	// request is the original request field that the request was matched with.
	request string

	// value is the nested value obtained after removing the original values' outer
	// layers that correspond to the unmatched suffix.
	value any
}

// maxValueDepth is the limit on a value's nestedness. Creating a highly nested
// JSON value only requires a few bytes per level, but when recursively traversing
// such a value, each level requires about 2Kb stack. Prevent excessive stack
// usage by limiting the recursion depth.
var maxValueDepth = 64

// validateSetValue checks that map keys conform to the same format as path sub-keys.
func validateSetValue(v any, depth int) error {
	if depth > maxValueDepth {
		return fmt.Errorf("value cannot have more than %d nested levels", maxValueDepth)
	}

	var nestedVals []any
	switch typedVal := v.(type) {
	case map[string]any:
		for k, v := range typedVal {
			if !validSubkey.Match([]byte(k)) {
				return fmt.Errorf(`key %q doesn't conform to required format: %s`, k, validSubkey.String())
			}

			nestedVals = append(nestedVals, v)
		}

	case []any:
		nestedVals = typedVal
	}

	for _, v := range nestedVals {
		if v == nil {
			// the value can be nil (used to unset values for compatibility w/ options)
			continue
		}

		if err := validateSetValue(v, depth+1); err != nil {
			return err
		}
	}

	return nil
}

// Set sets the named view to a specified non-nil value.
func (v *View) Set(databag Databag, request string, value any) error {
	if request == "" {
		return badRequestErrorFrom(v, "set", request, "")
	}

	opts := parseOpts{pathType: userPath}
	accessors, err := parsePathIntoAccessors(request, opts)
	if err != nil {
		return badRequestErrorFrom(v, "set", request, err.Error())
	}

	depth := 1
	if err := validateSetValue(value, depth); err != nil {
		return badRequestErrorFrom(v, "set", request, err.Error())
	}

	if value == nil {
		return fmt.Errorf("internal error: Set value cannot be nil")
	}

	matches, err := v.matchWriteRequest(accessors)
	if err != nil {
		return err
	}

	if len(matches) == 0 {
		return NewNoMatchError(v, "set", []string{request})
	}

	// sort less nested paths before more nested ones so that writes aren't overwritten
	sort.Slice(matches, func(x, y int) bool {
		return matches[x].storagePath < matches[y].storagePath
	})

	var expandedMatches []expandedMatch
	suffixes := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		pathsToValues, err := getValuesThroughPaths(match.storagePath, match.unmatchedSuffix, value)
		if err != nil {
			return badRequestErrorFrom(v, "set", request, err.Error())
		}

		for path, val := range pathsToValues {
			expandedMatches = append(expandedMatches, expandedMatch{
				storagePath: path,
				request:     match.request,
				value:       val,
			})
		}

		// store the suffix in a map so we deduplicate them before checking if the
		// value is used in its entirety
		suffixPath := joinAccessors(match.unmatchedSuffix)
		suffixes[suffixPath] = struct{}{}
	}

	// check if value is entirely used. If not, we fail so this is consistent
	// with doing the same write individually (one branch at a time)
	if err := checkForUnusedBranches(value, suffixes); err != nil {
		return badRequestErrorFrom(v, "set", request, err.Error())
	}

	// sort again since we may have unpacked a list into many expanded matches.
	// Since list Set()s depend on the length of the existing list, the order matters
	sort.Slice(expandedMatches, func(x, y int) bool {
		return expandedMatches[x].storagePath < expandedMatches[y].storagePath
	})

	for _, match := range expandedMatches {
		if err := databag.Set(match.storagePath, match.value); err != nil {
			return err
		}
	}

	data, err := databag.Data()
	if err != nil {
		return err
	}

	// TODO: when using a transaction, the data only changes on commit so
	// this is a bit of a waste. Maybe cache the result so we only do the first
	// validation and then in viewstate on Commit
	if err := v.schema.DatabagSchema.Validate(data); err != nil {
		return fmt.Errorf(`cannot write data: %w`, err)
	}

	return nil
}

func (v *View) Unset(databag Databag, request string) error {
	opts := parseOpts{pathType: userPath}
	accessors, err := parsePathIntoAccessors(request, opts)
	if err != nil {
		return badRequestErrorFrom(v, "unset", request, err.Error())
	}

	matches, err := v.matchWriteRequest(accessors)
	if err != nil {
		return err
	}

	if len(matches) == 0 {
		return NewNoMatchError(v, "unset", []string{request})
	}

	for _, match := range matches {
		if err := databag.Unset(match.storagePath); err != nil {
			return err
		}

		data, err := databag.Data()
		if err != nil {
			return err
		}

		// TODO: when using a transaction, the data only changes on commit so
		// this is a bit of a waste. Maybe cache the result so we only do the first
		// validation and then in viewstate on Commit
		if err := v.schema.DatabagSchema.Validate(data); err != nil {
			return fmt.Errorf(`cannot unset data: %w`, err)
		}
	}

	return nil
}

func (v *View) matchWriteRequest(request []accessor) ([]requestMatch, error) {
	var matches []requestMatch
	for _, rule := range v.rules {
		placeholders, unmatchedSuffix, ok := rule.match(request)
		if !ok {
			continue
		}

		if !rule.isWriteable() {
			continue
		}

		path, err := rule.storagePath(placeholders)
		if err != nil {
			return nil, err
		}

		matches = append(matches, requestMatch{
			storagePath:     path,
			unmatchedSuffix: unmatchedSuffix,
			request:         rule.originalRequest,
		})
	}

	return matches, nil
}

// checkSchemaMismatch checks whether the rules accept compatible schema types.
// If not, then no data can satisfy these rules and the view should be rejected.
func checkSchemaMismatch(schema DatabagSchema, rules []*viewRule) error {
	pathTypes := make(map[string][]SchemaType)
out:
	for _, rule := range rules {
		path := rule.originalStorage
		pathParts, err := splitViewPath(path, parseOpts{})
		if err != nil {
			return err
		}

		schemas, err := schema.SchemaAt(pathParts)
		if err != nil {
			var serr *schemaAtError
			if errors.As(err, &serr) {
				subParts := pathParts[:len(pathParts)-serr.left]
				subPath := strings.Join(subParts, ".")

				return fmt.Errorf(`storage path %q for request %q is invalid after %q: %w`,
					path, rule.originalRequest, subPath, serr.err)
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
					oldPath, path, rule.originalRequest, oldSetStr, newSetStr)
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

// getValuesThroughPaths takes a match's storage path and unmatched request
// suffix and strips the outer layers of the value to be set so it can be used
// at the storage path. Parts of the suffix that are placeholders will be
// expanded based on what keys exist in the value at that point and the mapping
// will be used to complete the storage path.
var getValuesThroughPaths = getValuesThroughPathsImpl

func getValuesThroughPathsImpl(storagePath string, unmatchedSuffix []accessor, val any) (map[string]any, error) {
	for unmatchedIndex, unmatchedPart := range unmatchedSuffix {
		switch unmatchedPart.keyType() {
		case keyPlaceholderType:
			mapVal, ok := val.(map[string]any)
			if !ok {
				return nil, fmt.Errorf(`expected map for unmatched request parts but got %T`, val)
			}

			storagePathsToValues := make(map[string]any)
			// suffix has an unmatched placeholder, try all possible values to fill it and
			// find the corresponding nested value.
			for cand, candVal := range mapVal {
				newStoragePath, err := replaceIn(storagePath, unmatchedPart.access(), cand)
				if err != nil {
					return nil, err
				}

				pathsToValues, err := getValuesThroughPathsImpl(newStoragePath, unmatchedSuffix[unmatchedIndex+1:], candVal)
				if err != nil {
					return nil, err
				}

				for path, val := range pathsToValues {
					storagePathsToValues[path] = val
				}
			}
			return storagePathsToValues, nil

		case mapKeyType:
			// use the non-placeholder parts of the suffix to find the value to write
			mapVal, ok := val.(map[string]any)
			if !ok {
				return nil, fmt.Errorf(`expected map for unmatched request parts but got %T`, val)
			}

			val, ok = mapVal[unmatchedPart.name()]
			if !ok {
				return nil, fmt.Errorf(`cannot use unmatched part %q as key in %v`, unmatchedPart, mapVal)
			}

		case indexPlaceholderType:
			list, ok := val.([]any)
			if !ok {
				return nil, fmt.Errorf(`expected list for unmatched request parts but got %T`, val)
			}

			// TODO: can this be optimised? Maybe by changing the databag logic to be more
			// match-aware instead of using these values to expand the matches?
			storagePathsToValues := make(map[string]any)
			for i, el := range list {
				newStoragePath, err := replaceIn(storagePath, unmatchedPart.access(), "["+strconv.Itoa(i)+"]")
				if err != nil {
					return nil, err
				}

				pathsToValues, err := getValuesThroughPathsImpl(newStoragePath, unmatchedSuffix[unmatchedIndex+1:], el)
				if err != nil {
					return nil, err
				}

				for path, val := range pathsToValues {
					storagePathsToValues[path] = val
				}
			}

			return storagePathsToValues, nil

		case listIndexType:
			// we don't allow literal indexes in request paths and check this early
			// so shouldn't be possible to hit this
			return nil, fmt.Errorf("internal error: unexpected index %q in unmatched suffix", unmatchedPart)
		}
	}

	// we reached the end of the suffix (there are no unmatched placeholders) so
	// we have the full storage path and final value
	return map[string]any{storagePath: val}, nil
}

func replaceIn(path, key, value string) (string, error) {
	opts := parseOpts{
		pathType:         viewPath,
		allowPartialPath: true,
	}
	parts, err := splitViewPath(path, opts)
	if err != nil {
		return "", err
	}

	for i, part := range parts {
		if part == key {
			parts[i] = value
		}
	}

	return joinPathParts(parts), nil
}

// checkForUnusedBranches checks that the value is entirely covered by the paths.
func checkForUnusedBranches(value any, paths map[string]struct{}) error {
	// prune each path from the value. If anything is left at the end, the paths
	// don't collectively cover the entire value
	copyValue := deepCopy(value)
	for path := range paths {
		var err error
		var pathParts []accessor

		if path != "" {
			opts := parseOpts{
				pathType:         viewPath,
				allowPartialPath: true,
			}

			pathParts, err = parsePathIntoAccessors(path, opts)
			if err != nil {
				return err
			}
		}

		copyValue, err = prunePathInValue(pathParts, copyValue)
		if err != nil {
			return err
		}
	}

	// after pruning each path the value is nil, so all of it is used
	if copyValue == nil {
		return nil
	}

	return fmt.Errorf("value contains unused data: %v", copyValue)
}

// deepCopy returns a deep copy of the value. Only supports the types that the
// API can take (so maps, slices and primitive types).
func deepCopy(value any) any {
	switch typeVal := value.(type) {
	case map[string]any:
		mapCopy := make(map[string]any, len(typeVal))
		for k, v := range typeVal {
			mapCopy[k] = deepCopy(v)
		}
		return mapCopy

	case []any:
		sliceCopy := make([]any, 0, len(typeVal))
		for _, v := range typeVal {
			sliceCopy = append(sliceCopy, deepCopy(v))
		}
		return sliceCopy

	default:
		return value
	}
}

func prunePathInValue(parts []accessor, val any) (any, error) {
	if len(parts) == 0 {
		return nil, nil
	} else if val == nil {
		return nil, nil
	}

	switch parts[0].keyType() {
	case keyPlaceholderType:
		mapVal, ok := val.(map[string]any)
		if !ok {
			// shouldn't happen since we already checked this
			return nil, fmt.Errorf(`internal error: expected map but got %T`, val)
		}

		nested := make(map[string]any)
		for k, v := range mapVal {
			newVal, err := prunePathInValue(parts[1:], v)
			if err != nil {
				return nil, err
			}

			if newVal != nil {
				nested[k] = newVal
			}
		}

		if len(nested) == 0 {
			return nil, nil
		}

		return nested, nil

	case indexPlaceholderType:
		list, ok := val.([]any)
		if !ok {
			// shouldn't happen since we already checked this
			return nil, fmt.Errorf(`internal error: expected list but got %T`, val)
		}

		nested := make([]any, 0, len(list))
		for _, v := range list {
			newVal, err := prunePathInValue(parts[1:], v)
			if err != nil {
				return nil, err
			}

			if newVal != nil {
				nested = append(nested, newVal)
			}
		}

		if len(nested) == 0 {
			return nil, nil
		}

		return nested, nil

	case mapKeyType:
		mapVal, ok := val.(map[string]any)
		if !ok {
			// shouldn't happen since we already checked this
			return nil, fmt.Errorf(`internal error: expected map but got %T`, val)
		}

		nested, ok := mapVal[parts[0].name()]
		if !ok {
			// shouldn't happen since we already checked this
			return nil, fmt.Errorf(`internal error: cannot use unmatched part %q as key in %v`, parts[0], mapVal)
		}

		newValue, err := prunePathInValue(parts[1:], nested)
		if err != nil {
			return nil, err
		}

		if newValue == nil {
			delete(mapVal, parts[0].name())
		} else {
			mapVal[parts[0].name()] = newValue
		}

		if len(mapVal) == 0 {
			return nil, nil
		}
		return mapVal, nil

	case listIndexType:
		// we don't allow literal indexes in request paths and check this early
		// so shouldn't be possible to hit this
		return nil, fmt.Errorf("internal error: unexpected index %q in request path", parts[0])

	default:
		return nil, fmt.Errorf("internal error: unknown key type %d", parts[0].keyType())
	}
}

// namespaceResult creates a nested namespace around the result that corresponds
// to the unmatched entry parts. Unmatched placeholders are filled in using maps
// of all the matching values in the databag.
func namespaceResult(res any, unmatchedSuffix []accessor) (any, error) {
	if len(unmatchedSuffix) == 0 {
		return res, nil
	}

	// check if the part is an unmatched placeholder which should have been filled
	// by the databag with all possible values
	switch part := unmatchedSuffix[0]; part.keyType() {
	case keyPlaceholderType:
		values, ok := res.(map[string]any)
		if !ok {
			return nil, errors.New("internal error: expected storage to return map for unmatched key placeholder")
		}

		level := make(map[string]any, len(values))
		for k, v := range values {
			nested, err := namespaceResult(v, unmatchedSuffix[1:])
			if err != nil {
				return nil, err
			}

			level[k] = nested
		}

		return level, nil

	case indexPlaceholderType:
		values, ok := res.([]any)
		if !ok {
			return nil, errors.New("internal error: expected storage to return list for unmatched index placeholder")
		}

		list := make([]any, 0, len(values))
		for _, v := range values {
			nested, err := namespaceResult(v, unmatchedSuffix[1:])
			if err != nil {
				return nil, err
			}

			list = append(list, nested)
		}

		return list, nil

	case mapKeyType:
		nested, err := namespaceResult(res, unmatchedSuffix[1:])
		if err != nil {
			return nil, err
		}

		return map[string]any{part.name(): nested}, nil

	case listIndexType:
		// we don't allow literal indexes in request paths and check this early
		// so shouldn't be possible to hit this
		return nil, fmt.Errorf("internal error: unexpected index %q in unmatched suffix", part)

	default:
		return nil, fmt.Errorf("internal error: unknown key type %d", part.keyType())
	}
}

// Get returns the view value identified by the request. Returns a NoMatchError
// if the view can't be found. Returns a NoDataError if there's no data for
// the request.
func (v *View) Get(databag Databag, request string) (any, error) {
	var accessors []accessor
	if request != "" {
		var err error
		opts := parseOpts{pathType: userPath}
		accessors, err = parsePathIntoAccessors(request, opts)
		if err != nil {
			return nil, badRequestErrorFrom(v, "get", request, err.Error())
		}
	}

	matches, err := v.matchGetRequest(accessors)
	if err != nil {
		return nil, err
	}

	var merged any
	for _, match := range matches {
		val, err := databag.Get(match.storagePath)
		if err != nil {
			if errors.Is(err, PathError("")) {
				continue
			}
			return nil, err
		}

		// build a namespace around the result based on the unmatched suffix parts
		val, err = namespaceResult(val, match.unmatchedSuffix)
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
		var requests []string
		if request != "" {
			requests = []string{request}
		}
		return nil, NewNoDataError(v, requests)
	}

	return merged, nil
}

// mergeNamespaces takes two results of reading confdb (the same request can match
// many view paths) and merges them recursively. The results should be possible to
// merge as long as the types are consistent. This isn't guaranteed to be true,
// if the schema rules allow for strange mappings.
func mergeNamespaces(old, new any) (any, error) {
	if old == nil {
		return new, nil
	} else if new == nil {
		return old, nil
	}

	oldType, newType := reflect.TypeOf(old).Kind(), reflect.TypeOf(new).Kind()
	if oldType != newType {
		return nil, fmt.Errorf("cannot merge results of different types %T, %T", old, new)
	}

	if oldType != reflect.Map && oldType != reflect.Slice {
		// if the values are both scalars, the new value replaces the old one
		return new, nil
	}

	if oldType == reflect.Map {
		oldMap, newMap := old.(map[string]any), new.(map[string]any)
		return mergeMaps(oldMap, newMap)
	}

	oldList, newList := old.([]any), new.([]any)
	return mergeLists(oldList, newList)
}

// mergeMaps merges two maps recursively, combining the merged values into a
// single map.
func mergeMaps(old, new map[string]any) (map[string]any, error) {
	for k, v := range new {
		if storeVal, ok := old[k]; ok {
			merged, err := mergeNamespaces(storeVal, v)
			if err != nil {
				return nil, err
			}
			v = merged
		}

		old[k] = v
	}

	return old, nil
}

// mergeLists merges two lists of results recursively. The lists are merged
// by merging the element from both until one list runs out of elements to merge,
// at that point the other list's remaining are appended.
func mergeLists(old, new []any) ([]any, error) {
	for i, oldEl := range old {
		if i >= len(new) {
			break
		}

		merged, err := mergeNamespaces(oldEl, new[i])
		if err != nil {
			return nil, err
		}
		old[i] = merged
	}

	if len(old) < len(new) {
		old = append(old, new[len(old):]...)
	}
	return old, nil
}

// ReadAffectsEphemeral returns true if any of the requests might be used to
// set ephemeral data. The requests are mapped to storage paths as in GetViaView.
func (v *View) ReadAffectsEphemeral(requests []string) (bool, error) {
	if len(requests) == 0 {
		// try to match all like we'd to read
		requests = []string{""}
	}

	opts := parseOpts{pathType: userPath}
	var matches []requestMatch
	for _, request := range requests {
		accessors, err := parsePathIntoAccessors(request, opts)
		if err != nil {
			return false, err
		}

		reqMatches, err := v.matchGetRequest(accessors)
		if err != nil {
			if errors.Is(err, &NoMatchError{}) {
				// we serve partial reads so check other paths
				continue
			}
			// no match
			return false, err
		}

		if len(reqMatches) != 0 {
			matches = append(matches, reqMatches...)
		}
	}

	if len(matches) == 0 {
		return false, NewNoMatchError(v, "get", requests)
	}

	schema := []DatabagSchema{v.schema.DatabagSchema}
	for _, match := range matches {
		pathParts := strings.Split(match.storagePath, ".")
		ephemeral, err := anyEphemeralSchema(schema, pathParts)
		if err != nil {
			// shouldn't be possible unless there's a view/schema mismatch
			return false, fmt.Errorf("cannot check if read affects ephemeral data: %v", err)
		}

		if ephemeral {
			return true, nil
		}
	}

	return false, nil
}

// WriteAffectsEphemeral returns true if the storage paths can affect ephemeral
// data.
func (v *View) WriteAffectsEphemeral(paths []string) (bool, error) {
	schema := []DatabagSchema{v.schema.DatabagSchema}
	for _, path := range paths {
		pathParts := strings.Split(path, ".")
		ephemeral, err := anyEphemeralSchema(schema, pathParts)
		if err != nil {
			// shouldn't be possible unless the paths don't match the schema somehow
			return false, fmt.Errorf("cannot check if write affects ephemeral data: %v", err)
		}

		if ephemeral {
			return true, nil
		}
	}

	return false, nil
}

func anyEphemeralSchema(schemas []DatabagSchema, pathParts []string) (bool, error) {
	for _, schema := range schemas {
		if schema.Ephemeral() {
			return true, nil
		}

		if len(pathParts) == 0 {
			if schema.NestedEphemeral() {
				return true, nil
			}
			continue
		}

		nestedSchemas, err := schema.SchemaAt([]string{pathParts[0]})
		if err != nil {
			return false, err
		}

		eph, err := anyEphemeralSchema(nestedSchemas, pathParts[1:])
		if err != nil {
			return false, err
		}

		if eph {
			return true, nil
		}
	}

	return false, nil
}

type requestMatch struct {
	// storagePath contains the storage path specified in the matching entry with
	// any placeholders provided by the request filled in.
	storagePath string

	// unmatchedSuffix contains the nested suffix of the entry's request that
	// wasn't matched by the request.
	unmatchedSuffix []accessor

	// request is the full request as it appears in the assertion's access rule.
	request string
}

// matchGetRequest either returns the first exact match for the request or, if
// no entry is an exact match, one or more entries that the request matches a
// prefix of. If no match is found, a NoMatchError is returned.
func (v *View) matchGetRequest(accessors []accessor) (matches []requestMatch, err error) {
	for _, rule := range v.rules {
		placeholders, unmatchedSuffix, ok := rule.match(accessors)
		if !ok {
			continue
		}

		path, err := rule.storagePath(placeholders)
		if err != nil {
			return nil, err
		}

		if !rule.isReadable() {
			continue
		}

		m := requestMatch{
			storagePath:     path,
			unmatchedSuffix: unmatchedSuffix,
			request:         rule.originalRequest,
		}
		matches = append(matches, m)
	}

	if len(matches) == 0 {
		request := joinAccessors(accessors)
		return nil, NewNoMatchError(v, "get", []string{request})
	}

	// sort matches by namespace (unmatched suffix) to ensure that nested matches
	// are read after
	sort.Slice(matches, func(x, y int) bool {
		xNamespace, yNamespace := matches[x].unmatchedSuffix, matches[y].unmatchedSuffix

		minLen := int(math.Min(float64(len(xNamespace)), float64(len(yNamespace))))
		for i := 0; i < minLen; i++ {
			if xNamespace[i].access() == yNamespace[i].access() {
				continue
			}
			return xNamespace[i].access() < yNamespace[i].access()
		}

		return len(xNamespace) < len(yNamespace)
	})

	return matches, nil
}

func (v *View) ID() string { return v.schema.Account + "/" + v.schema.Name + "/" + v.Name }

func newViewRule(request, storage []accessor, accesstype string) (*viewRule, error) {
	accType, err := newAccessType(accesstype)
	if err != nil {
		return nil, fmt.Errorf("cannot create view rule: %w", err)
	}

	requestMatchers := make([]requestMatcher, 0, len(request))
	for _, acc := range request {
		matcher, ok := acc.(requestMatcher)
		if !ok {
			return nil, fmt.Errorf("internal error: cannot convert accessor into requestMatcher")
		}
		requestMatchers = append(requestMatchers, matcher)
	}

	pathWriters := make([]storageWriter, 0, len(storage))
	for _, acc := range storage {
		writer, ok := acc.(storageWriter)
		if !ok {
			return nil, fmt.Errorf("internal error: cannot convert accessor into storageWriter")
		}
		pathWriters = append(pathWriters, writer)
	}

	return &viewRule{
		originalRequest: joinAccessors(request),
		originalStorage: joinAccessors(storage),
		request:         requestMatchers,
		storage:         pathWriters,
		access:          accType,
	}, nil
}

func joinAccessors(parts []accessor) string {
	var sb strings.Builder
	for i, part := range parts {
		if !(part.keyType() == indexPlaceholderType || part.keyType() == listIndexType || i == 0) {
			sb.WriteRune('.')
		}

		sb.WriteString(part.access())
	}

	return sb.String()
}

func joinPathParts(parts []string) string {
	var sb strings.Builder
	for i, part := range parts {
		if !(strings.HasPrefix(part, "[") || strings.HasSuffix(part, "]") || i == 0) {
			sb.WriteRune('.')
		}

		sb.WriteString(part)
	}

	return sb.String()
}

func isPlaceholder(part string) bool {
	return len(part) > 2 && strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}")
}

// viewRule represents an individual view rule. It can be used to match a
// request and map it into a corresponding storage path, potentially with
// placeholders filled in.
type viewRule struct {
	originalRequest string
	originalStorage string

	request []requestMatcher
	storage []storageWriter
	access  accessType
}

// match returns true if the subkeys match the pattern exactly or as a prefix.
// If placeholders are "filled in" when matching, those are returned in "matched"
// according to which kind of placeholder they are. If the subkeys match as a
// prefix, the remaining suffix is returned.
func (p *viewRule) match(reqSubkeys []accessor) (matched *matchedPlaceholders, unmatched []accessor, match bool) {
	if len(p.request) < len(reqSubkeys) {
		return nil, nil, false
	}

	matched = &matchedPlaceholders{}
	for i, subkey := range reqSubkeys {
		if !p.request[i].match(subkey, matched) {
			return nil, nil, false
		}
	}

	// we match requests on a prefix of rule paths, save the unmatched suffix
	for _, key := range p.request[len(reqSubkeys):] {
		unmatched = append(unmatched, key)
	}

	return matched, unmatched, true
}

// storagePath takes a matchedPlaceholders struct mapping key and index
// placeholder names to their values in the view name and returns the path with
// its placeholder values filled in with the map's values.
func (p *viewRule) storagePath(matched *matchedPlaceholders) (string, error) {
	sb := &strings.Builder{}

	opts := writeOpts{topLevel: true}
	for _, subkey := range p.storage {
		subkey.write(sb, matched, opts)
		opts.topLevel = false
	}

	return sb.String(), nil
}

func (p viewRule) isReadable() bool {
	return p.access == readWrite || p.access == read
}

func (p viewRule) isWriteable() bool {
	return p.access == readWrite || p.access == write
}

// pattern is an individual subkey of a dot-separated name or path pattern. It
// can be a literal value of a placeholder delineated by curly brackets.
type requestMatcher interface {
	accessor

	match(subkey accessor, matched *matchedPlaceholders) bool
}

type writeOpts struct {
	topLevel bool
}

type storageWriter interface {
	write(sb *strings.Builder, matched *matchedPlaceholders, opts writeOpts)
}

// placeholder represents a subkey of a name/path (e.g., "{foo}") that can match
// with any value and map it from the input name to the path.
type keyPlaceholder string

// match adds an entry to matchedPlaceholders mapping this placeholder key to the
// supplied name subkey and returns true (a placeholder matches with any value).
func (p keyPlaceholder) match(subkey accessor, matched *matchedPlaceholders) bool {
	if subkey.keyType() != mapKeyType {
		return false
	}

	matched.setKey(string(p), subkey.name())
	return true
}

// write writes the value from the matchedPlaceholders entry corresponding to
// this placeholder key into the strings.Builder.
func (p keyPlaceholder) write(sb *strings.Builder, matched *matchedPlaceholders, opts writeOpts) {
	subkey, ok := matched.key[string(p)]
	if !ok {
		// placeholder wasn't matched, return the original key in brackets
		subkey = p.access()
	}

	if !opts.topLevel {
		sb.WriteRune('.')
	}

	sb.WriteString(subkey)
}

func (p keyPlaceholder) access() string {
	return "{" + string(p) + "}"
}

func (p keyPlaceholder) name() string     { return string(p) }
func (p keyPlaceholder) keyType() keyType { return keyPlaceholderType }

type matchedPlaceholders struct {
	index map[string]string
	key   map[string]string
}

func (m *matchedPlaceholders) setKey(placeholderName, keyValue string) {
	if m.key == nil {
		m.key = make(map[string]string)
	}
	m.key[placeholderName] = keyValue
}

func (m *matchedPlaceholders) setIndex(placeholderName, indexValue string) {
	if m.index == nil {
		m.index = make(map[string]string)
	}
	m.index[placeholderName] = indexValue
}

// indexPlaceholder represents a subkey of a name/path (e.g., "[{n}]") that can
// match an index value and map it from the input name to the path.
type indexPlaceholder string

// match checks if the subkey can be used to index a list. If so, it adds an
// entry to matchedPlaceholders mapping this placeholder key to the supplied
// name subkey and returns true.
func (p indexPlaceholder) match(subkey accessor, matched *matchedPlaceholders) bool {
	if subkey.keyType() != listIndexType {
		return false
	}

	matched.setIndex(string(p), subkey.name())
	return true
}

// write writes the value from the matchedPlaceholders entry corresponding to
// this placeholder key into the strings.Builder.
func (p indexPlaceholder) write(sb *strings.Builder, matched *matchedPlaceholders, _ writeOpts) {
	subkey, ok := matched.index[string(p)]
	if !ok {
		// placeholder wasn't matched, return the original key in brackets
		subkey = p.access()
	} else {
		subkey = "[" + subkey + "]"
	}

	sb.WriteString(subkey)
}

func (p indexPlaceholder) access() string   { return "[{" + string(p) + "}]" }
func (p indexPlaceholder) name() string     { return string(p) }
func (p indexPlaceholder) keyType() keyType { return indexPlaceholderType }

// key is a non-placeholder object key.
type key string

// match returns true if the subkey is equal to the literal key.
func (k key) match(subkey accessor, _ *matchedPlaceholders) bool {
	return subkey.keyType() == mapKeyType && string(k) == subkey.name()
}

// write writes the key into the strings.Builder with a prefixing '.', if it's
// not the top level accessor.
func (k key) write(sb *strings.Builder, _ *matchedPlaceholders, opts writeOpts) {
	if !opts.topLevel {
		sb.WriteRune('.')
	}
	sb.WriteString(k.access())
}

func (k key) access() string   { return k.name() }
func (k key) name() string     { return string(k) }
func (k key) keyType() keyType { return mapKeyType }

type index string

// write writes the literal subkey into the strings.Builder.
func (i index) write(sb *strings.Builder, _ *matchedPlaceholders, _ writeOpts) {
	sb.WriteString(i.access())
}

func (i index) access() string   { return "[" + i.name() + "]" }
func (i index) name() string     { return string(i) }
func (i index) keyType() keyType { return listIndexType }

type PathError string

func (e PathError) Error() string {
	return string(e)
}

func (e PathError) Is(err error) bool {
	_, ok := err.(PathError)
	return ok
}

func pathErrorf(str string, v ...any) PathError {
	return PathError(fmt.Sprintf(str, v...))
}

// JSONDatabag is a simple Databag implementation that keeps JSON in-memory.
type JSONDatabag map[string]json.RawMessage

// NewJSONDatabag returns a Databag implementation that stores data in JSON.
// The top-level of the JSON structure is always a map.
func NewJSONDatabag() JSONDatabag {
	return JSONDatabag(make(map[string]json.RawMessage))
}

// Get takes a path and a pointer to a variable into which the value referenced
// by the path is written. The path can be dotted. For each dot a JSON object
// is expected to exist (e.g., "a.b" is mapped to {"a": {"b": <value>}}).
func (s JSONDatabag) Get(path string) (any, error) {
	opts := parseOpts{pathType: viewPath}
	subKeys, err := parsePathIntoAccessors(path, opts)
	if err != nil {
		return nil, err
	}

	// TODO: create this in the return below as well?
	var value any
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
func get(subKeys []accessor, index int, node any, result *any) error {
	// the first level will be typed as JSONDatabag so we have to convert it
	if bag, ok := node.(JSONDatabag); ok {
		node = map[string]json.RawMessage(bag)
	}

	switch node := node.(type) {
	case map[string]json.RawMessage:
		return getMap(subKeys, index, node, result)
	case []json.RawMessage:
		return getList(subKeys, index, node, result)
	default:
		// should be impossible since we handle terminal cases in the type specific functions
		path := joinAccessors(subKeys[:index+1])
		return pathErrorf("internal error: expected level %q to be map or list but got %T", path, node)
	}
}

// getMap traverses node (a decoded JSON object) and, depending on the path being
// followed, does one of the following:
//   - decodes a value from it into the result parameter
//   - decodes all map entries, if the path ends in an unmatched placeholder
//   - goes into one specific sub-path and recurses into get()
//   - goes into potentially many sub-paths and merges the results, if the current
//     path sub-key is an unmatched placeholder
func getMap(subKeys []accessor, index int, node map[string]json.RawMessage, result *any) error {
	curPath := joinAccessors(subKeys[:index+1])
	key := subKeys[index]

	var matchAll bool
	var rawLevel json.RawMessage
	if key.keyType() == mapKeyType {
		var ok bool
		rawLevel, ok = node[key.name()]
		if !ok {
			return pathErrorf("no value was found under path %q", curPath)
		}
	} else if key.keyType() == keyPlaceholderType {
		matchAll = true
	} else {
		pathPrefix := joinAccessors(subKeys[:index])
		return fmt.Errorf("cannot use %q to access map at path %q", key.access(), pathPrefix)
	}

	// read the final value
	if index == len(subKeys)-1 {
		if matchAll {
			// request ends in placeholder so return map to all values (but unmarshal the rest first)
			level := make(map[string]any, len(node))
			for k, v := range node {
				var deser any
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
		results := make(map[string]any)

		for k, v := range node {
			level, err := unmarshalLevel(subKeys, index, v)
			if err != nil {
				if errors.As(err, new(*noContainerError)) {
					// ignore entries that don't map to containers since the path expects
					// more nested levels (this isn't the last path sub-key)
					continue
				}
				return err
			}

			// walk the path under all possible values, only return an error if no value
			// is found under any path
			var res any
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
			return pathErrorf("no value was found under path %q", curPath)
		}

		*result = results
		return nil
	}

	level, err := unmarshalLevel(subKeys, index, rawLevel)
	if err != nil {
		return err
	}

	return get(subKeys, index+1, level, result)
}

// getList traverses node (a decoded JSON list) and, depending on the path being
// followed, does one of the following:
//   - decodes a value from it into the result parameter
//   - decodes all list elements, if the path ends in an unmatched placeholder
//   - goes into one specific sub-path and recurses into get()
//   - goes into potentially many sub-paths and accumulates the results, if the
//     current path sub-key is an unmatched placeholder
func getList(subKeys []accessor, keyIndex int, list []json.RawMessage, result *any) error {
	key := subKeys[keyIndex]

	var matchAll bool
	listIndex := -1
	if key.keyType() == listIndexType {
		listIndex, _ = strconv.Atoi(key.name())
	} else if key.keyType() == indexPlaceholderType {
		matchAll = true
	} else {
		pathPrefix := joinAccessors(subKeys[:keyIndex])
		return fmt.Errorf("cannot use %q to index list at path %q", key.access(), pathPrefix)
	}

	curPath := joinAccessors(subKeys[:keyIndex+1])
	if listIndex >= len(list) {
		return pathErrorf("no value was found under path %q", curPath)
	}

	// read the final value
	if keyIndex == len(subKeys)-1 {
		if matchAll {
			// request ends in placeholder so return map to all values (but unmarshal the rest first)
			level := make([]any, len(list))
			for i, v := range list {
				var deser any
				if err := json.Unmarshal(v, &deser); err != nil {
					return fmt.Errorf(`internal error: %w`, err)
				}
				level[i] = deser
			}

			*result = level
			return nil
		}

		if err := json.Unmarshal(list[listIndex], result); err != nil {
			return fmt.Errorf(`internal error: %w`, err)
		}

		return nil
	}

	if matchAll {
		results := make([]any, 0, len(list))

		for _, el := range list {
			level, err := unmarshalLevel(subKeys, keyIndex+1, el)
			if err != nil {
				if errors.As(err, new(*noContainerError)) {
					// ignore entries that don't map to containers since the path expects
					// more nested levels, since we're not at the last sub-key
					continue
				}
				return err
			}

			// walk the path under all possible values, only return an error if no value
			// is found under any path
			var res any
			if err := get(subKeys, keyIndex+1, level, &res); err != nil {
				if errors.Is(err, PathError("")) {
					continue
				}
			}

			if res != nil {
				results = append(results, res)
			}
		}

		if len(results) == 0 {
			return pathErrorf("no value was found under path %q", curPath)
		}

		*result = results
		return nil
	}

	// decode the next level
	level, err := unmarshalLevel(subKeys, keyIndex, list[listIndex])
	if err != nil {
		return err
	}

	return get(subKeys, keyIndex+1, level, result)
}

// noContainerError is used when the traversal logic expected some JSON to
// be decodable into a container type (based on the path its following) but it
// it couldn't unmarshal it into a map or list.
type noContainerError struct {
	path       string
	actualType string
}

func (e *noContainerError) Error() string {
	return fmt.Sprintf("cannot decode databag at path %q: expected container type but got %v", e.path, e.actualType)
}

func newNoContainerError(path, actualType string) *noContainerError {
	return &noContainerError{
		path:       path,
		actualType: actualType,
	}
}

// unmarshalLevel decodes rawLevel into whatever container type it represents
// (list or map). It returns a noContainerError if the raw JSON can't be
// unmarshalled to either container type.
func unmarshalLevel(subKeys []accessor, index int, rawLevel json.RawMessage) (any, error) {
	var mapLevel map[string]json.RawMessage
	if err := jsonutil.DecodeWithNumber(bytes.NewReader(rawLevel), &mapLevel); err != nil {
		_, ok := err.(*json.UnmarshalTypeError)
		if !ok {
			return nil, err
		}

		// next level isn't an object, try list
		var listLevel []json.RawMessage
		if err := jsonutil.DecodeWithNumber(bytes.NewReader(rawLevel), &listLevel); err != nil {
			// also isn't list so we can't traverse it as expected -> error
			uErr, ok := err.(*json.UnmarshalTypeError)
			if ok {
				pathPrefix := joinAccessors(subKeys[:index+1])
				return nil, newNoContainerError(pathPrefix, uErr.Value)
			}
			return nil, err
		}

		return listLevel, nil
	}

	return mapLevel, nil
}

// Set takes a path to which the value will be written. The path can be dotted,
// in which case, a nested JSON object is created for each sub-key found after a dot.
// If the value is nil, the entry is deleted.
func (s JSONDatabag) Set(path string, value any) error {
	opts := parseOpts{pathType: viewPath}
	subKeys, err := parsePathIntoAccessors(path, opts)
	if err != nil {
		return err
	}

	if value != nil {
		_, err = set(subKeys, 0, s, value)
	} else {
		_, err = unset(subKeys, 0, s)
	}

	return err
}

func removeNilValues(value any) any {
	level, ok := value.(map[string]any)
	if !ok {
		return value
	}

	for k, v := range level {
		if v == nil {
			delete(level, k)
			continue
		}

		level[k] = removeNilValues(v)
	}

	return level
}

func set(subKeys []accessor, index int, node any, value any) (json.RawMessage, error) {
	// the first level will be typed as JSONDatabag so we have to convert it
	if bag, ok := node.(JSONDatabag); ok {
		node = map[string]json.RawMessage(bag)
	}

	if obj, ok := node.(map[string]json.RawMessage); ok {
		return setMap(subKeys, index, obj, value)
	} else if list, ok := node.([]json.RawMessage); ok {
		return setList(subKeys, index, list, value)
	}

	// should be impossible since we handle terminal cases in the type specific functions
	path := joinAccessors(subKeys[:index+1])
	return nil, pathErrorf("internal error: expected level %q to be map or list but got %T", path, node)
}

func setMap(subKeys []accessor, index int, node map[string]json.RawMessage, value any) (json.RawMessage, error) {
	key := subKeys[index]
	if key.keyType() != mapKeyType {
		pathPrefix := joinAccessors(subKeys[:index])
		return nil, fmt.Errorf("cannot use %q to access map at path %q", key.access(), pathPrefix)
	}

	if index == len(subKeys)-1 {
		// remove nil values that may be nested in the value
		value = removeNilValues(value)

		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}

		node[key.name()] = data
		return json.Marshal(node)
	}

	var level any
	rawLevel, ok := node[key.name()]
	if ok {
		var err error
		level, err = unmarshalLevel(subKeys, index+1, rawLevel)
		if err != nil {
			if !errors.As(err, new(*noContainerError)) {
				return nil, err
			}
			// stored value wasn't map but new write expects one so overwrite value.
			// Shouldn't be possible if schema stays the same but let's be robust in
			// case schema is evolved in a way that overwrites previous paths
		}
	}

	// next level doesn't exist yet or isn't right type so overwrite
	if level == nil {
		nextKey := subKeys[index+1]
		level = emptyContainerForType(nextKey)
	}

	rawLevel, err := set(subKeys, index+1, level, value)
	if err != nil {
		return nil, err
	}

	node[key.name()] = rawLevel
	return json.Marshal(node)
}

func setList(subKeys []accessor, keyIndex int, list []json.RawMessage, value any) (json.RawMessage, error) {
	key := subKeys[keyIndex]

	if key.keyType() != listIndexType {
		pathPrefix := joinAccessors(subKeys[:keyIndex])
		return nil, fmt.Errorf("cannot use %q to index list at path %q", key.access(), pathPrefix)
	}

	listIndex, _ := strconv.Atoi(key.name())
	// note that the index can exceed the list length by 1 (in which case we
	// append the entry, extending the list)
	if listIndex > len(list) {
		pathPrefix := joinAccessors(subKeys[:keyIndex+1])
		return nil, pathErrorf("cannot access %q: list has length %d", pathPrefix, len(list))
	}

	if keyIndex == len(subKeys)-1 {
		// remove nil values that may be nested in the value
		value = removeNilValues(value)
		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}

		if listIndex == len(list) {
			list = append(list, data)
		} else {
			list[listIndex] = data
		}
		return json.Marshal(list)
	}

	var level any
	// if we're setting new element to list there's no value to unmarshal
	if listIndex < len(list) {
		var err error
		level, err = unmarshalLevel(subKeys, keyIndex+1, list[listIndex])
		if err != nil {
			if !errors.As(err, new(*noContainerError)) {
				return nil, err
			}
			// stored value isn't container but path expects one so overwrite value.
			// Shouldn't be possible if schema stays the same but let's be robust in
			// case schema is evolved in way that overwrites previous paths
		}
	}

	// if we're adding a new nested level or overriding a previous one, create it
	// according to whether the path expects a map or list
	if level == nil {
		nextKey := subKeys[keyIndex+1]
		level = emptyContainerForType(nextKey)
	}

	rawLevel, err := set(subKeys, keyIndex+1, level, value)
	if err != nil {
		return nil, err
	}

	if listIndex == len(list) {
		list = append(list, rawLevel)
	} else {
		list[listIndex] = rawLevel
	}
	return json.Marshal(list)
}

func emptyContainerForType(acc accessor) any {
	if acc.keyType() == keyPlaceholderType || acc.keyType() == mapKeyType {
		return map[string]json.RawMessage{}
	}
	return []json.RawMessage{}
}

func (s JSONDatabag) Unset(path string) error {
	opts := parseOpts{pathType: viewPath}
	subKeys, err := parsePathIntoAccessors(path, opts)
	if err != nil {
		return err
	}

	_, err = unset(subKeys, 0, s)
	return err
}

func unset(subKeys []accessor, index int, node any) (json.RawMessage, error) {
	// the first level will be typed as JSONDatabag so we have to convert it
	if bag, ok := node.(JSONDatabag); ok {
		node = map[string]json.RawMessage(bag)
	}

	if obj, ok := node.(map[string]json.RawMessage); ok {
		return unsetMap(subKeys, index, obj)
	} else if list, ok := node.([]json.RawMessage); ok {
		return unsetList(subKeys, index, list)
	}

	// should be impossible since we handle terminal cases in the type specific functions
	path := joinAccessors(subKeys[:index+1])
	return nil, pathErrorf("internal error: expected level %q to be map or list but got %T", path, node)
}

func unsetMap(subKeys []accessor, index int, node map[string]json.RawMessage) (json.RawMessage, error) {
	key := subKeys[index]

	pathPrefix := joinAccessors(subKeys[:index])
	if key.keyType() != mapKeyType && key.keyType() != keyPlaceholderType {
		return nil, fmt.Errorf("cannot use %q to access map at path %q", key.access(), pathPrefix)
	}

	if index == len(subKeys)-1 {
		if key.keyType() == keyPlaceholderType {
			// remove entire level
			return nil, nil
		}

		// NOTE: don't remove entire level even if all entries are unset to keep it
		// consistent with options
		delete(node, key.name())
		return json.Marshal(node)
	}

	unsetKey := func(level map[string]json.RawMessage, key string) error {
		nextLevelRaw, ok := level[key]
		if !ok {
			return nil
		}

		nextLevel, err := unmarshalLevel(subKeys, index+1, nextLevelRaw)
		if err != nil {
			return err
		}

		updated, err := unset(subKeys, index+1, nextLevel)
		if err != nil {
			return err
		}

		// update the map with the sublevel which may have changed or been removed
		if updated == nil {
			delete(level, key)
		} else {
			level[key] = updated
		}

		return nil
	}

	if key.keyType() == keyPlaceholderType {
		for k := range node {
			if err := unsetKey(node, k); err != nil {
				return nil, err
			}
		}
	} else {
		if err := unsetKey(node, key.name()); err != nil {
			return nil, err
		}
	}

	return json.Marshal(node)
}

func unsetList(subKeys []accessor, keyIndex int, node []json.RawMessage) (json.RawMessage, error) {
	key := subKeys[keyIndex]

	pathPrefix := joinAccessors(subKeys[:keyIndex])
	if key.keyType() != listIndexType && key.keyType() != indexPlaceholderType {
		return nil, fmt.Errorf("cannot use %q to index list at path %q", key.access(), pathPrefix)
	}

	if keyIndex == len(subKeys)-1 {
		if key.keyType() == indexPlaceholderType {
			// remove entire level
			return nil, nil
		}

		i, _ := strconv.Atoi(key.name())
		if i < len(node) {
			node = append(node[:i], node[i+1:]...)
		}

		// NOTE: we don't remove the list if all entries were unset to keep this
		// consistent with maps (which in turn are consistent w/ how options work)
		return json.Marshal(node)
	}

	unsetIndex := func(list []json.RawMessage, index int) (json.RawMessage, error) {
		nextLevel, err := unmarshalLevel(subKeys, keyIndex+1, list[index])
		if err != nil {
			return nil, err
		}

		return unset(subKeys, keyIndex+1, nextLevel)
	}

	if key.keyType() == indexPlaceholderType {
		var wi int
		for i := range node {
			updated, err := unsetIndex(node, i)
			if err != nil {
				return nil, err
			}

			if updated == nil {
				continue
			}

			node[wi] = updated
			wi++
		}

		node = node[:wi]
	} else {
		i, _ := strconv.Atoi(key.name())
		if i >= len(node) {
			// nothing to remove
			return json.Marshal(node)
		}

		updated, err := unsetIndex(node, i)
		if err != nil {
			return nil, err
		}

		if updated == nil {
			node = append(node[:i], node[i+1:]...)
		} else {
			node[i] = updated
		}
	}

	// NOTE: we don't remove the list if all entries were unset to keep this
	// consistent with maps (which in turn are consistent w/ how options work)
	return json.Marshal(node)
}

// Data returns all of the bag's data encoded in JSON.
func (s JSONDatabag) Data() ([]byte, error) {
	return json.Marshal(s)
}

// Copy returns a copy of the databag.
func (s JSONDatabag) Copy() JSONDatabag {
	toplevel := map[string]json.RawMessage(s)
	copy := make(map[string]json.RawMessage, len(toplevel))

	for k, v := range toplevel {
		copy[k] = v
	}

	return JSONDatabag(copy)
}

// Overwrite replaces the entire databag with the provided data.
func (s *JSONDatabag) Overwrite(data []byte) error {
	var unmarshalledBag map[string]json.RawMessage
	if err := json.Unmarshal(data, &unmarshalledBag); err != nil {
		return err
	}

	*s = JSONDatabag(unmarshalledBag)
	return nil
}

// JSONSchema is the Schema implementation corresponding to JSONDatabag and it's
// able to validate its data.
type JSONSchema struct{}

// NewJSONSchema returns a Schema able to validate a JSONDatabag's data.
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
func (v JSONSchema) SchemaAt(path []string) ([]DatabagSchema, error) {
	return []DatabagSchema{v}, nil
}

func (v JSONSchema) Type() SchemaType      { return Any }
func (v JSONSchema) Ephemeral() bool       { return false }
func (v JSONSchema) NestedEphemeral() bool { return false }
