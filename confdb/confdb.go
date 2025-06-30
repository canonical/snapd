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
	return fmt.Sprintf("cannot %s %s through confdb view %s: %s", e.operation, reqStr, e.viewID, e.cause)
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
	opts := &validationOptions{allowPlaceholder: true}
	reqAccessors, err = validateViewDottedPath(request, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid request %q: %w", request, err)
	}

	storageAccessors, err = validateViewDottedPath(storage, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid storage %q: %w", storage, err)
	}
	reqKeyVars, err := getKeyPlaceholders(reqAccessors)
	if err != nil {
		return nil, nil, err
	}

	storageKeyVars, err := getKeyPlaceholders(storageAccessors)
	if err != nil {
		return nil, nil, err
	}

	// check that the request and storage key placeholders match
	err = checkForMatchingPlaceholders(request, storage, reqKeyVars, storageKeyVars)
	if err != nil {
		return nil, nil, err
	}

	// check that the request and storage list index placeholders match
	reqIndexVars, err := getIndexPlaceholders(reqAccessors)
	if err != nil {
		return nil, nil, err
	}

	storageIndexVars, err := getIndexPlaceholders(storageAccessors)
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

type validationOptions struct {
	// allowPlaceholder means that placeholders are accepted when validating.
	allowPlaceholder bool
}

// validateViewDottedPath validates that request/storage strings in a view definition are:
//   - composed of non-empty, dot or bracket separated subkeys with optional
//     placeholders (e.g., foo.{bar}, a[{n}].bar), if allowed by the validationOptions
//   - non-placeholder subkeys are made up of lowercase alphanumeric ASCII characters,
//     optionally with dashes between alphanumeric characters (e.g., "a-b-c")
//   - placeholder subkeys are composed of non-placeholder subkeys wrapped in curly brackets
//   - bracketed subkeys that aren't placeholders can only contain integers
//
// If the validation succeeds, it returns an []accessor which contains typed
// representations of each type of subkey (e.g., key placeholder, index, etc).
func validateViewDottedPath(path string, opts *validationOptions) ([]accessor, error) {
	if opts == nil {
		opts = &validationOptions{}
	}

	subkeys, err := splitViewPath(path)
	if err != nil {
		return nil, err
	}

	for _, subkey := range subkeys {
		// straight literal accesses, without placeholders (e.g., foo.bar, foo[1])
		isLiteral := validSubkey.MatchString(subkey) || validIndexSubkey.MatchString(subkey)
		// placeholder subkeys (e.g., foo.{bar}, foo[{n}])
		isPlaceholder := validPlaceholder.MatchString(subkey) || validIndexPlaceholder.MatchString(subkey)

		if !isLiteral && (!opts.allowPlaceholder || !isPlaceholder) {
			return nil, fmt.Errorf("invalid subkey %q", subkey)
		}
	}

	return pathIntoAccessors(subkeys), nil
}

type accessor interface {
	fmt.Stringer
}

func pathIntoAccessors(path []string) []accessor {
	accessors := make([]accessor, 0, len(path))

	for _, subkey := range path {
		var next accessor
		switch subkey[0] {
		case '{':
			next = keyPlaceholder(subkey[1 : len(subkey)-1])

		case '[':
			if subkey[1] == '{' {
				next = indexPlaceholder(subkey[2 : len(subkey)-2])
			} else {
				next = index(subkey)
			}

		default:
			next = key(subkey)
		}

		accessors = append(accessors, next)
	}

	return accessors
}

func splitViewPath(path string) ([]string, error) {
	var subkeys []string
	sb := &strings.Builder{}

	finishSubkey := func() error {
		if sb.Len() == 0 {
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
			if _, err := sb.WriteRune(c); err != nil {
				return nil, err
			}
		}
	}

	// there should be a subkey to be finished (paths like "a." are invalid)
	if err := finishSubkey(); err != nil {
		return nil, err
	}

	return subkeys, nil
}

// getPlaceholders returns the number of occurrences of placeholder names, for
// a given type of placeholder.
func getPlaceholders[T key | keyPlaceholder | index | indexPlaceholder](accessors []accessor) (map[string]int, error) {
	var placeholders map[string]int
	count := func(key T) {
		if placeholders == nil {
			placeholders = make(map[string]int)
		}
		placeholders[string(key)]++
	}

	for _, acc := range accessors {
		subAcc, ok := acc.(T)
		if !ok {
			continue
		}

		count(subAcc)
	}

	return placeholders, nil
}

func getKeyPlaceholders(accs []accessor) (map[string]int, error) {
	return getPlaceholders[keyPlaceholder](accs)
}

func getIndexPlaceholders(accs []accessor) (map[string]int, error) {
	return getPlaceholders[indexPlaceholder](accs)
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
	// TODO: ignore accessors for now (will address in its own PR)
	if _, err := validateViewDottedPath(request, nil); err != nil {
		return badRequestErrorFrom(v, "set", request, err.Error())
	}

	depth := 1
	if err := validateSetValue(value, depth); err != nil {
		return badRequestErrorFrom(v, "set", request, err.Error())
	}

	if value == nil {
		return fmt.Errorf("internal error: Set value cannot be nil")
	}

	matches, err := v.matchWriteRequest(request)
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
		pathsToValues, err := getValuesThroughPaths(match.storagePath, match.suffixParts, value)
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
		suffixes[strings.Join(match.suffixParts, ".")] = struct{}{}
	}

	// check if value is entirely used. If not, we fail so this is consistent
	// with doing the same write individually (one branch at a time)
	if err := checkForUnusedBranches(value, suffixes); err != nil {
		return badRequestErrorFrom(v, "set", request, err.Error())
	}

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
	// TODO: ignore accessors for now (will address in its own PR)
	if _, err := validateViewDottedPath(request, nil); err != nil {
		return badRequestErrorFrom(v, "unset", request, err.Error())
	}

	matches, err := v.matchWriteRequest(request)
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

func (v *View) matchWriteRequest(request string) ([]requestMatch, error) {
	var matches []requestMatch
	subkeys, err := splitViewPath(request)
	if err != nil {
		return nil, err
	}

	for _, rule := range v.rules {
		placeholders, suffixParts, ok := rule.match(subkeys)
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
			storagePath: path,
			suffixParts: suffixParts,
			request:     rule.originalRequest,
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
		pathParts, err := splitViewPath(path)
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

func getValuesThroughPathsImpl(storagePath string, reqSuffixParts []string, val any) (map[string]any, error) {
	// use the non-placeholder parts of the suffix to find the value to write
	var placeIndex int
	for _, part := range reqSuffixParts {
		if isPlaceholder(part) {
			// there is a placeholder, we have to consider potentially many candidates
			break
		}

		mapVal, ok := val.(map[string]any)
		if !ok {
			return nil, fmt.Errorf(`expected map for unmatched request parts but got %T`, val)
		}

		val, ok = mapVal[part]
		if !ok {
			return nil, fmt.Errorf(`cannot use unmatched part %q as key in %v`, part, mapVal)
		}

		placeIndex++
	}

	// we reached the end of the suffix (there are no unmatched placeholders) so
	// we have the full storage path and final value
	if placeIndex == len(reqSuffixParts) {
		return map[string]any{storagePath: val}, nil
	}

	mapVal, ok := val.(map[string]any)
	if !ok {
		return nil, fmt.Errorf(`expected map for unmatched request parts but got %T`, val)
	}

	storagePathsToValues := make(map[string]any)
	// suffix has an unmatched placeholder, try all possible values to fill it and
	// find the corresponding nested value.
	for cand, candVal := range mapVal {
		newStoragePath := replaceIn(storagePath, reqSuffixParts[placeIndex], cand)
		pathsToValues, err := getValuesThroughPathsImpl(newStoragePath, reqSuffixParts[placeIndex+1:], candVal)
		if err != nil {
			return nil, err
		}

		for path, val := range pathsToValues {
			storagePathsToValues[path] = val
		}
	}

	return storagePathsToValues, nil
}

func replaceIn(path, key, value string) string {
	parts := strings.Split(path, ".")
	for i, part := range parts {
		if part == key {
			parts[i] = value
		}
	}

	// TODO: what to do about this. will have to do this manually
	return strings.Join(parts, ".")
}

// checkForUnusedBranches checks that the value is entirely covered by the paths.
func checkForUnusedBranches(value any, paths map[string]struct{}) error {
	// prune each path from the value. If anything is left at the end, the paths
	// don't collectively cover the entire value
	copyValue := deepCopy(value)
	for path := range paths {
		var err error
		var pathParts []string
		if path != "" {
			pathParts, err = splitViewPath(path)
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

	var parts []string
	for copyValue != nil {
		mapVal, ok := copyValue.(map[string]any)
		if !ok {
			break
		}

		for k, v := range mapVal {
			parts = append(parts, k)
			copyValue = v
			break
		}
	}

	return fmt.Errorf("value contains unused data under %q", strings.Join(parts, "."))
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

func prunePathInValue(parts []string, val any) (any, error) {
	if len(parts) == 0 {
		return nil, nil
	} else if val == nil {
		return nil, nil
	}

	mapVal, ok := val.(map[string]any)
	if !ok {
		// shouldn't happen since we already checked this
		return nil, fmt.Errorf(`expected map but got %T`, val)
	}

	if isPlaceholder(parts[0]) {
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
	}

	nested, ok := mapVal[parts[0]]
	if !ok {
		// shouldn't happen since we already checked this
		return nil, fmt.Errorf(`cannot use unmatched part %q as key in %v`, parts[0], nested)
	}

	newValue, err := prunePathInValue(parts[1:], nested)
	if err != nil {
		return nil, err
	}

	if newValue == nil {
		delete(mapVal, parts[0])
	} else {
		mapVal[parts[0]] = newValue
	}

	if len(mapVal) == 0 {
		return nil, nil
	}

	return mapVal, nil
}

// namespaceResult creates a nested namespace around the result that corresponds
// to the unmatched entry parts. Unmatched placeholders are filled in using maps
// of all the matching values in the databag.
func namespaceResult(res any, suffixParts []string) (any, error) {
	if len(suffixParts) == 0 {
		return res, nil
	}

	// check if the part is an unmatched placeholder which should have been filled
	// by the databag with all possible values
	part := suffixParts[0]
	if isPlaceholder(part) {
		values, ok := res.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("internal error: expected storage to return map for unmatched placeholder")
		}

		level := make(map[string]any, len(values))
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

	return map[string]any{part: nested}, nil
}

// Get returns the view value identified by the request. Returns a NoMatchError
// if the view can't be found. Returns a NoDataError if there's no data for
// the request.
func (v *View) Get(databag Databag, request string) (any, error) {
	if request != "" {
		// TODO: ignore accessors for now (will address in its own PR)
		if _, err := validateViewDottedPath(request, nil); err != nil {
			return nil, badRequestErrorFrom(v, "get", request, err.Error())
		}
	}

	matches, err := v.matchGetRequest(request)
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
		var requests []string
		if request != "" {
			requests = []string{request}
		}
		return nil, NewNoDataError(v, requests)
	}

	return merged, nil
}

func mergeNamespaces(old, new any) (any, error) {
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
	oldMap, newMap := old.(map[string]any), new.(map[string]any)
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

// ReadAffectsEphemeral returns true if any of the requests might be used to
// set ephemeral data. The requests are mapped to storage paths as in GetViaView.
func (v *View) ReadAffectsEphemeral(requests []string) (bool, error) {
	if len(requests) == 0 {
		// try to match all like we'd to read
		requests = []string{""}
	}

	var matches []requestMatch
	for _, request := range requests {
		reqMatches, err := v.matchGetRequest(request)
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

	// suffixParts contains the nested suffix of the entry's request that wasn't
	// matched by the request.
	suffixParts []string

	// request is the full request as it appears in the assertion's access rule.
	request string
}

// matchGetRequest either returns the first exact match for the request or, if
// no entry is an exact match, one or more entries that the request matches a
// prefix of. If no match is found, a NoMatchError is returned.
func (v *View) matchGetRequest(request string) (matches []requestMatch, err error) {
	var subkeys []string
	if request != "" {
		subkeys, err = splitViewPath(request)
		if err != nil {
			return nil, err
		}
	}

	for _, rule := range v.rules {
		placeholders, restSuffix, ok := rule.match(subkeys)
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
			storagePath: path,
			suffixParts: restSuffix,
			request:     rule.originalRequest,
		}
		matches = append(matches, m)
	}

	if len(matches) == 0 {
		return nil, NewNoMatchError(v, "get", []string{request})
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

func (v *View) ID() string { return v.schema.Account + "/" + v.schema.Name + "/" + v.Name }

func newViewRule(request, storage []accessor, accesstype string) (*viewRule, error) {
	accType, err := newAccessType(accesstype)
	if err != nil {
		return nil, fmt.Errorf("cannot create view rule: %w", err)
	}

	requestMatchers := make([]requestMatcher, 0, len(request))
	for _, acc := range request {
		requestMatchers = append(requestMatchers, acc.(requestMatcher))
	}

	pathWriters := make([]storageWriter, 0, len(storage))
	for _, acc := range storage {
		pathWriters = append(pathWriters, acc.(storageWriter))
	}

	return &viewRule{
		originalRequest: joinPathParts(request),
		originalStorage: joinPathParts(storage),
		request:         requestMatchers,
		storage:         pathWriters,
		access:          accType,
	}, nil
}

func joinPathParts(parts []accessor) string {
	var sb strings.Builder
	for i, part := range parts {
		_, isIndexPlaceholder := part.(indexPlaceholder)
		_, isIndex := part.(index)
		if !(isIndexPlaceholder || isIndex || i == 0) {
			sb.WriteRune('.')
		}
		sb.WriteString(part.String())
	}

	return sb.String()
}

func isPlaceholder(part string) bool {
	return len(part) > 2 && part[0] == '{' && part[len(part)-1] == '}'
}

func stripIndex(part string) (string, bool) {
	if len(part) < 3 || part[0] != '[' || part[len(part)-1] != ']' {
		return "", false
	}
	return part[1 : len(part)-1], true
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
func (p *viewRule) match(reqSubkeys []string) (matched *matchedPlaceholders, restSuffix []string, match bool) {
	if len(p.request) < len(reqSubkeys) {
		return nil, nil, false
	}

	matched = &matchedPlaceholders{}
	for i, subkey := range reqSubkeys {
		// empty request matches everything
		if len(reqSubkeys) != 0 && !p.request[i].match(subkey, matched) {
			return nil, nil, false
		}
	}

	for _, key := range p.request[len(reqSubkeys):] {
		restSuffix = append(restSuffix, key.String())
	}

	return matched, restSuffix, true
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

	match(subkey string, matched *matchedPlaceholders) bool
}

type writeOpts struct {
	topLevel bool
}

type storageWriter interface {
	accessor

	write(sb *strings.Builder, matched *matchedPlaceholders, opts writeOpts)
}

// placeholder represents a subkey of a name/path (e.g., "{foo}") that can match
// with any value and map it from the input name to the path.
type keyPlaceholder string

// match adds an entry to matchedPlaceholders mapping this placeholder key to the
// supplied name subkey and returns true (a placeholder matches with any value).
func (p keyPlaceholder) match(subkey string, matched *matchedPlaceholders) bool {
	if matched.key == nil {
		matched.key = make(map[string]string)
	}
	matched.key[string(p)] = subkey
	return true
}

// write writes the value from the matchedPlaceholders entry corresponding to
// this placeholder key into the strings.Builder.
func (p keyPlaceholder) write(sb *strings.Builder, matched *matchedPlaceholders, opts writeOpts) {
	subkey, ok := matched.key[string(p)]
	if !ok {
		// placeholder wasn't matched, return the original key in brackets
		subkey = p.String()
	}

	if !opts.topLevel {
		sb.WriteRune('.')
	}

	sb.WriteString(subkey)
}

// String returns the placeholder as a string.
func (p keyPlaceholder) String() string {
	return "{" + string(p) + "}"
}

type matchedPlaceholders struct {
	index map[string]string
	key   map[string]string
}

// indexPlaceholder represents a subkey of a name/path (e.g., "[{n}]") that can
// match an index value and map it from the input name to the path.
type indexPlaceholder string

// match checks if the subkey can be used to index a list. If so, it adds an
// entry to matchedPlaceholders mapping this placeholder key to the supplied
// name subkey and returns true.
func (p indexPlaceholder) match(subkey string, matched *matchedPlaceholders) bool {
	subkey, ok := stripIndex(subkey)
	if !ok {
		return false
	}

	if _, err := strconv.Atoi(subkey); err != nil {
		// subkey can't be used as a index placeholder value
		return false
	}

	if matched.index == nil {
		matched.index = make(map[string]string)
	}
	matched.index[string(p)] = subkey
	return true
}

// write writes the value from the matchedPlaceholders entry corresponding to
// this placeholder key into the strings.Builder.
func (p indexPlaceholder) write(sb *strings.Builder, matched *matchedPlaceholders, _ writeOpts) {
	subkey, ok := matched.index[string(p)]
	if !ok {
		// placeholder wasn't matched, return the original key in brackets
		subkey = p.String()
	}

	sb.WriteString("[" + subkey + "]")
}

// String returns the placeholder as a string.
func (p indexPlaceholder) String() string {
	return "[{" + string(p) + "}]"
}

// key is a non-placeholder object key.
type key string

// match returns true if the subkey is equal to the literal.
func (k key) match(subkey string, _ *matchedPlaceholders) bool {
	return string(k) == subkey
}

// write writes the key into the strings.Builder with a prefixing '.', if it's
// not the top level accessor.
func (k key) write(sb *strings.Builder, _ *matchedPlaceholders, opts writeOpts) {
	if !opts.topLevel {
		sb.WriteRune('.')
	}

	sb.WriteString(string(k))
}

// String returns the key as a string.
func (k key) String() string {
	return string(k)
}

type index string

// match returns true if the subkey is equal to the literal.
func (i index) match(subkey string, _ *matchedPlaceholders) bool {
	return string(i) == subkey
}

// write writes the literal subkey into the strings.Builder.
func (p index) write(sb *strings.Builder, _ *matchedPlaceholders, opts writeOpts) {
	sb.WriteString(string(p))
}

// String returns the index wrapped in brackets.
func (i index) String() string {
	return string(i)
}

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
	// TODO: create this in the return below as well?
	var value any
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
func get(subKeys []string, index int, node map[string]json.RawMessage, result *any) error {
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
			pathPrefix := strings.Join(subKeys[:index+1], ".")
			return pathErrorf("no value was found under path %q", pathPrefix)
		}

		*result = results
		return nil
	}

	// decode the next map level
	var level map[string]json.RawMessage
	if err := jsonutil.DecodeWithNumber(bytes.NewReader(rawLevel), &level); err != nil {
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
func (s JSONDatabag) Set(path string, value any) error {
	subKeys := strings.Split(path, ".")

	var err error
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

func set(subKeys []string, index int, node map[string]json.RawMessage, value any) (json.RawMessage, error) {
	key := subKeys[index]
	if index == len(subKeys)-1 {
		// remove nil values that may be nested in the value
		value = removeNilValues(value)

		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}

		node[key] = data
		return json.Marshal(node)
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

func (s JSONDatabag) Unset(path string) error {
	subKeys := strings.Split(path, ".")
	_, err := unset(subKeys, 0, s)
	return err
}

func unset(subKeys []string, index int, node map[string]json.RawMessage) (json.RawMessage, error) {
	key := subKeys[index]
	matchAll := isPlaceholder(key)

	if index == len(subKeys)-1 {
		if matchAll {
			// remove entire level
			return nil, nil
		}

		delete(node, key)
		return json.Marshal(node)
	}

	unsetKey := func(level map[string]json.RawMessage, key string) error {
		nextLevelRaw, ok := level[key]
		if !ok {
			return nil
		}

		var nextLevel map[string]json.RawMessage
		if err := jsonutil.DecodeWithNumber(bytes.NewReader(nextLevelRaw), &nextLevel); err != nil {
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

	if matchAll {
		for k := range node {
			if err := unsetKey(node, k); err != nil {
				return nil, err
			}
		}
	} else {
		if err := unsetKey(node, key); err != nil {
			return nil, err
		}
	}

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
