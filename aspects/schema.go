// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/strutil"
)

type parser interface {
	Schema

	// expectsConstraints returns true if the parser must have a map definition
	// with constraints or false, if it may have a simple name definition.
	expectsConstraints() bool

	// parseConstraints parses constraints for a type defined as a JSON object.
	// Shouldn't be used with non-object/map type definitions.
	parseConstraints(map[string]json.RawMessage) error
}

// ParseSchema parses a JSON aspect schema and returns a Schema that can be
// used to validate aspects.
func ParseSchema(raw []byte) (*StorageSchema, error) {
	var schemaDef map[string]json.RawMessage
	mylog.Check(json.Unmarshal(raw, &schemaDef))

	if rawType, ok := schemaDef["type"]; ok {
		var typ string
		mylog.Check(json.Unmarshal(rawType, &typ))

		if typ != "map" {
			return nil, fmt.Errorf(`cannot parse top level schema: unexpected declared type %q, should be "map" or omitted`, typ)
		}
	}

	if _, ok := schemaDef["schema"]; !ok {
		return nil, fmt.Errorf(`cannot parse top level schema: must have a "schema" constraint`)
	}

	schema := new(StorageSchema)
	if aliasesRaw, ok := schemaDef["aliases"]; ok {
		var aliases map[string]json.RawMessage
		mylog.Check(json.Unmarshal(aliasesRaw, &aliases))

		// TODO: if we want to allow aliases to refer to others, this must be handled
		// explicitly since the "aliases" map doesn't have any implicit order
		schema.aliases = make(map[string]*aliasRefParser, len(aliases))
		for alias, typeDef := range aliases {
			if !validAliasName.Match([]byte(alias)) {
				return nil, fmt.Errorf(`cannot parse alias name %q: must match %s`, alias, validAliasName)
			}

			aliasSchema := mylog.Check2(schema.parse(typeDef))

			schema.aliases[alias] = newAliasRefParser(aliasSchema)
		}
	}

	schema.topLevel = mylog.Check2(schema.parse(raw))

	return schema, nil
}

// aliasRefParser parses references to aliases (e.g., $my-type).
type aliasRefParser struct {
	Schema

	stringBased bool
}

func newAliasRefParser(s Schema) *aliasRefParser {
	_, ok := s.(*stringSchema)
	return &aliasRefParser{
		Schema:      s,
		stringBased: ok,
	}
}

// expectsConstraints return false because a reference to an alias doesn't
// define constraints (these are defined under "aliases" at the top level).
func (*aliasRefParser) expectsConstraints() bool {
	return false
}

// parseConstraints is a no-op because type references can't define constraints.
func (v *aliasRefParser) parseConstraints(map[string]json.RawMessage) error {
	return nil
}

// isStringBased returns true if this reference's base type is a string.
func (u *aliasRefParser) isStringBased() bool {
	return u.stringBased
}

// StorageSchema represents an aspect schema and can be used to validate JSON
// aspects against it.
type StorageSchema struct {
	// topLevel is the schema for the top level map.
	topLevel Schema

	// aliases are schemas that can validate custom types defined by the user.
	aliases map[string]*aliasRefParser
}

// Validate validates the provided JSON object.
func (s *StorageSchema) Validate(raw []byte) error {
	return s.topLevel.Validate(raw)
}

// SchemaAt returns the types that may be stored at the specified path.
func (s *StorageSchema) SchemaAt(path []string) ([]Schema, error) {
	return s.topLevel.SchemaAt(path)
}

func (s *StorageSchema) Type() SchemaType {
	return s.topLevel.Type()
}

func (s *StorageSchema) parse(raw json.RawMessage) (Schema, error) {
	jsonType := mylog.Check2(parseTypeDefinition(raw))

	var typ string
	var schemaDef map[string]json.RawMessage
	switch typedVal := jsonType.(type) {
	case string:
		typ = typedVal

	case []json.RawMessage:
		alts := mylog.Check2(s.parseAlternatives(typedVal))

		return alts, nil

	case map[string]json.RawMessage:
		schemaDef = typedVal
		rawType, ok := schemaDef["type"]
		if !ok {
			typ = "map"
		} else {
			mylog.Check(json.Unmarshal(rawType, &typ))
		}

	default:
		// cannot happen save for programmer error
		return nil, fmt.Errorf(`cannot parse schema definition of JSON type %T`, jsonType)
	}

	schema := mylog.Check2(s.newTypeSchema(typ))

	// only parse the schema if it's a schema definition w/ constraints
	if schemaDef != nil {
		mylog.Check(schema.parseConstraints(schemaDef))
	} else if schema.expectsConstraints() {
		return nil, fmt.Errorf(`cannot parse %q: must be schema definition with constraints`, typ)
	}

	return schema, nil
}

// parseTypeDefinition tries to parse the raw JSON as a list, a map or a string
// (the accepted ways to express types).
func parseTypeDefinition(raw json.RawMessage) (interface{}, error) {
	var typeErr *json.UnmarshalTypeError

	var l []json.RawMessage
	if mylog.Check(json.Unmarshal(raw, &l)); err == nil {
		return l, nil
	} else if !errors.As(err, &typeErr) {
		return nil, err
	}

	var m map[string]json.RawMessage
	if mylog.Check(json.Unmarshal(raw, &m)); err == nil {
		return m, nil
	} else if !errors.As(err, &typeErr) {
		return nil, err
	}

	var s string
	if mylog.Check(json.Unmarshal(raw, &s)); err == nil {
		return s, nil
	} else {
		return nil, fmt.Errorf(`type must be expressed as map, string or list: %w`, err)
	}
}

// parseAlternatives takes a list of alternative types, parses them and creates
// a schema that accepts values matching any alternative.
func (s *StorageSchema) parseAlternatives(alternatives []json.RawMessage) (*alternativesSchema, error) {
	alt := &alternativesSchema{schemas: make([]Schema, 0, len(alternatives))}
	for _, altRaw := range alternatives {
		schema := mylog.Check2(s.parse(altRaw))

		alt.schemas = append(alt.schemas, schema)
	}

	if len(alt.schemas) == 0 {
		return nil, fmt.Errorf(`alternative type list cannot be empty`)
	}

	flatAlts := flattenAlternatives(alt)
	alt.schemas = flatAlts

	return alt, nil
}

// flattenAlternatives takes the schemas that comprise the alternative schema
// and flattens them into a single list.
func flattenAlternatives(alt *alternativesSchema) []Schema {
	var flat []Schema
	for _, schema := range alt.schemas {
		if altSchema, ok := schema.(*alternativesSchema); ok {
			nestedAlts := flattenAlternatives(altSchema)
			flat = append(flat, nestedAlts...)
		} else {
			flat = append(flat, schema)
		}
	}

	return flat
}

func (s *StorageSchema) newTypeSchema(typ string) (parser, error) {
	switch typ {
	case "map":
		return &mapSchema{topSchema: s}, nil
	case "string":
		return &stringSchema{}, nil
	case "int":
		return &intSchema{}, nil
	case "any":
		return &anySchema{}, nil
	case "number":
		return &numberSchema{}, nil
	case "bool":
		return &booleanSchema{}, nil
	case "array":
		return &arraySchema{topSchema: s}, nil
	default:
		if typ != "" && typ[0] == '$' {
			return s.getAlias(typ[1:])
		}

		return nil, fmt.Errorf("cannot parse unknown type %q", typ)
	}
}

func (s *StorageSchema) getAlias(ref string) (*aliasRefParser, error) {
	if alias, ok := s.aliases[ref]; ok {
		return alias, nil
	}

	return nil, fmt.Errorf("cannot find alias %q", ref)
}

type alternativesSchema struct {
	// schemas holds schemas for the types allowed for the corresponding value.
	schemas []Schema
}

// Validate that raw matches at least one of the schemas in the alternative list.
func (v *alternativesSchema) Validate(raw []byte) error {
	var errs []error
	for _, schema := range v.schemas {
		mylog.Check(schema.Validate(raw))
		if err == nil {
			return nil
		}

		errs = append(errs, err)
	}

	var sb strings.Builder
	sb.WriteString("no matching schema:")
	for i, err := range errs {
		sb.WriteString("\n\t")
		if i > 0 {
			sb.WriteString("or ")
		}

		if verr, ok := err.(*ValidationError); ok {
			err = verr.Err

			if len(verr.Path) != 0 {
				sb.WriteString("...\"")
				for i, part := range verr.Path {
					switch v := part.(type) {
					case string:
						if i > 0 {
							sb.WriteRune('.')
						}

						sb.WriteString(v)
					case int:
						sb.WriteString(fmt.Sprintf("[%d]", v))
					default:
						// can only happen due to bug
						sb.WriteString(".<n/a>")
					}
				}
				sb.WriteString("\": ")
			}
		}

		sb.WriteString(err.Error())
	}

	return validationErrorf(sb.String())
}

// SchemaAt returns the list of schemas at the end of the path or an error if
// the path cannot be followed.
func (v *alternativesSchema) SchemaAt(path []string) ([]Schema, error) {
	if len(path) == 0 {
		return v.schemas, nil
	}

	var types []Schema
	var lastErr error
	for _, alt := range v.schemas {
		altTypes := mylog.Check2(alt.SchemaAt(path))

		// some schemas may permit the path

		types = append(types, altTypes...)
	}

	// TODO: find better way to combine errors
	if len(types) == 0 {
		return nil, lastErr
	}

	return types, nil
}

func (v *alternativesSchema) Type() SchemaType {
	return Alt
}

type mapSchema struct {
	// topSchema is the schema for the top-level schema which contains the aliases.
	topSchema *StorageSchema

	// entrySchemas maps keys to their expected types. Alternatively, the schema
	// can constrain key and/or value types.
	entrySchemas map[string]Schema

	// valueSchema validates that the map's values match a certain type.
	valueSchema Schema

	// keySchema validates that the map's key match a certain type.
	keySchema Schema

	// requiredCombs holds combinations of keys that an instance of the map is
	// allowed to have.
	requiredCombs [][]string
}

// Validate that raw is a valid aspect map and meets the constraints set by the
// aspect schema.
func (v *mapSchema) Validate(raw []byte) error {
	var mapValue map[string]json.RawMessage
	mylog.Check(json.Unmarshal(raw, &mapValue))

	if mapValue == nil {
		return validationErrorf(`cannot accept null value for "map" type`)
	}
	mylog.Check(validMapKeys(mapValue))

	if v.entrySchemas != nil {
		for key := range mapValue {
			if _, ok := v.entrySchemas[key]; !ok {
				return validationErrorf(`map contains unexpected key %q`, key)
			}
		}
	}

	var missing bool
	for _, required := range v.requiredCombs {
		missing = false
		for _, key := range required {
			if _, ok := mapValue[key]; !ok {
				missing = true
				break
			}
		}

		if !missing {
			// matched possible combination of required keys so we can stop
			break
		}
	}

	if missing {
		return validationErrorf(`cannot find required combinations of keys`)
	}

	if v.entrySchemas != nil {
		for key, val := range mapValue {
			if validator, ok := v.entrySchemas[key]; ok {
				mylog.Check(validator.Validate(val))
			}
		}

		// all required entries are present and validated
		return nil
	}

	if v.keySchema != nil {
		for k := range mapValue {
			rawKey := mylog.Check2(json.Marshal(k))
			mylog.Check(v.keySchema.Validate(rawKey))

		}
	}

	if v.valueSchema != nil {
		for k, val := range mapValue {
			mylog.Check(v.valueSchema.Validate(val))
		}
	}

	return nil
}

func validMapKeys(v map[string]json.RawMessage) error {
	for k := range v {
		if !validSubkey.Match([]byte(k)) {
			return fmt.Errorf(`key %q doesn't conform to required format: %s`, k, validSubkey.String())
		}
	}

	return nil
}

// SchemaAt returns the Map schema if this is the last path element. If not, it
// calls SchemaAt for the next path element's schema if the path is valid.
func (v *mapSchema) SchemaAt(path []string) ([]Schema, error) {
	if len(path) == 0 {
		return []Schema{v}, nil
	}

	key := path[0]
	if v.entrySchemas != nil {
		valSchema, ok := v.entrySchemas[key]
		if !ok {
			return nil, schemaAtErrorf(path, `cannot use %q as key in map`, key)
		}

		return valSchema.SchemaAt(path[1:])
	}

	return v.valueSchema.SchemaAt(path[1:])
}

// Type returns the Map type.
func (v *mapSchema) Type() SchemaType {
	return Map
}

func (v *mapSchema) parseConstraints(constraints map[string]json.RawMessage) error {
	mylog.Check(checkExclusiveMapConstraints(constraints))

	// maps can be "schemas" with types for specific entries and optional "required" constraints
	if rawEntries, ok := constraints["schema"]; ok {
		var entries map[string]json.RawMessage
		mylog.Check(json.Unmarshal(rawEntries, &entries))
		mylog.Check(validMapKeys(entries))

		v.entrySchemas = make(map[string]Schema, len(entries))
		for key, value := range entries {
			entrySchema := mylog.Check2(v.topSchema.parse(value))

			v.entrySchemas[key] = entrySchema
		}

		// "required" can be a list of keys or many lists of alternative combinations
		if rawRequired, ok := constraints["required"]; ok {
			var requiredCombs [][]string
			mylog.Check(json.Unmarshal(rawRequired, &requiredCombs))

			for _, requiredComb := range v.requiredCombs {
				for _, required := range requiredComb {
					if _, ok := v.entrySchemas[required]; !ok {
						return fmt.Errorf(`cannot parse map's "required" constraint: required key %q must have schema entry`, required)
					}
				}
			}
		}

		return nil
	}

	// map can not specify "schemas" and constrain the type of keys and values instead
	rawKeyDef, ok := constraints["keys"]
	if ok {
		v.keySchema := mylog.Check2(v.parseMapKeyType(rawKeyDef))
	}

	rawValuesDef, ok := constraints["values"]
	if ok {
		v.valueSchema = mylog.Check2(v.topSchema.parse(rawValuesDef))
	}

	if v.entrySchemas == nil && v.keySchema == nil && v.valueSchema == nil {
		return fmt.Errorf(`cannot parse map: must have "schema" or "keys"/"values" constraint`)
	}

	return nil
}

// checkExclusiveMapConstraints checks if the map contains mutually exclusive constraints.
func checkExclusiveMapConstraints(obj map[string]json.RawMessage) error {
	has := func(k string) bool {
		_, ok := obj[k]
		return ok
	}

	if has("required") && !has("schema") {
		return fmt.Errorf(`cannot use "required" without "schema" constraint`)
	}
	if has("schema") && has("keys") {
		return fmt.Errorf(`cannot use "schema" and "keys" constraints simultaneously`)
	}
	if has("schema") && has("values") {
		return fmt.Errorf(`cannot use "schema" and "values" constraints simultaneously`)
	}

	return nil
}

func (v *mapSchema) parseMapKeyType(raw json.RawMessage) (Schema, error) {
	var typ string
	mylog.Check(json.Unmarshal(raw, &typ))

	if typ == "string" {
		return &stringSchema{}, nil
	}

	if typ != "" && typ[0] == '$' {
		alias := mylog.Check2(v.topSchema.getAlias(typ[1:]))

		if !alias.isStringBased() {
			return nil, fmt.Errorf(`key type %q must be based on string`, typ[1:])
		}

		return alias, nil
	}

	return nil, fmt.Errorf(`keys must be based on string but type was %s`, typ)
}

func (v *mapSchema) expectsConstraints() bool { return true }

type stringSchema struct {
	// pattern is a regex pattern that the string must match.
	pattern *regexp.Regexp

	// choices holds the possible values the string can take, if non-empty.
	choices []string
}

// Validate that raw is a valid aspect string and meets the schema's constraints.
func (v *stringSchema) Validate(raw []byte) (err error) {
	defer func() {
	}()

	var value *string
	mylog.Check(json.Unmarshal(raw, &value))

	if value == nil {
		return fmt.Errorf(`cannot accept null value for "string" type`)
	}

	if len(v.choices) != 0 && !strutil.ListContains(v.choices, *value) {
		return fmt.Errorf(`string %q is not one of the allowed choices`, *value)
	}

	if v.pattern != nil && !v.pattern.Match([]byte(*value)) {
		return fmt.Errorf(`expected string matching %s but value was %q`, v.pattern.String(), *value)
	}

	return nil
}

// SchemaAt returns the string schema if the path terminates at this schema and
// an error if not.
func (v *stringSchema) SchemaAt(path []string) ([]Schema, error) {
	if len(path) != 0 {
		return nil, schemaAtErrorf(path, `cannot follow path beyond "string" type`)
	}

	return []Schema{v}, nil
}

func (v *stringSchema) Type() SchemaType {
	return String
}

func (v *stringSchema) parseConstraints(constraints map[string]json.RawMessage) error {
	if rawChoices, ok := constraints["choices"]; ok {
		var choices []string
		mylog.Check(json.Unmarshal(rawChoices, &choices))

		if len(choices) == 0 {
			return fmt.Errorf(`cannot have a "choices" constraint with an empty list`)
		}

		v.choices = choices
	}

	if rawPattern, ok := constraints["pattern"]; ok {
		if v.choices != nil {
			return fmt.Errorf(`cannot use "choices" and "pattern" constraints in same schema`)
		}

		var patt string
		mylog.Check(json.Unmarshal(rawPattern, &patt))

		v.pattern := mylog.Check2(regexp.Compile(patt))

	}

	return nil
}

func (v *stringSchema) expectsConstraints() bool { return false }

type intSchema struct {
	min     *int64
	max     *int64
	choices []int64
}

// Validate that raw is a valid integer and meets the schema's constraints.
func (v *intSchema) Validate(raw []byte) (err error) {
	defer func() {
	}()

	var num *int64
	mylog.Check(json.Unmarshal(raw, &num))

	if num == nil {
		return fmt.Errorf(`cannot accept null value for "int" type`)
	}

	return validateNumber(*num, v.choices, v.min, v.max)
}

// SchemaAt returns the int schema if the path terminates here and an error if
// not.
func (v *intSchema) SchemaAt(path []string) ([]Schema, error) {
	if len(path) != 0 {
		return nil, schemaAtErrorf(path, `cannot follow path beyond "int" type`)
	}

	return []Schema{v}, nil
}

// Type returns the Int schema type.
func (v *intSchema) Type() SchemaType {
	return Int
}

func (v *intSchema) parseConstraints(constraints map[string]json.RawMessage) error {
	if rawChoices, ok := constraints["choices"]; ok {
		var choices []int64
		mylog.Check(json.Unmarshal(rawChoices, &choices))

		if len(choices) == 0 {
			return fmt.Errorf(`cannot have "choices" constraint with empty list`)
		}

		v.choices = choices
	}

	if rawMin, ok := constraints["min"]; ok {
		if v.choices != nil {
			return fmt.Errorf(`cannot have "choices" and "min" constraints`)
		}

		var min int64
		mylog.Check(json.Unmarshal(rawMin, &min))

		v.min = &min
	}

	if rawMax, ok := constraints["max"]; ok {
		if v.choices != nil {
			return fmt.Errorf(`cannot have "choices" and "max" constraints`)
		}

		var max int64
		mylog.Check(json.Unmarshal(rawMax, &max))

		v.max = &max
	}

	if v.min != nil && v.max != nil && *v.min > *v.max {
		return fmt.Errorf(`cannot have "min" constraint with value greater than "max"`)
	}

	return nil
}

func (v *intSchema) expectsConstraints() bool { return false }

type anySchema struct{}

func (v *anySchema) Validate(raw []byte) (err error) {
	defer func() {
	}()

	var val interface{}
	mylog.Check(json.Unmarshal(raw, &val))

	if val == nil {
		return fmt.Errorf(`cannot accept null value for "any" type`)
	}
	return nil
}

func (v *anySchema) parseConstraints(map[string]json.RawMessage) error {
	// no error because we're not explicitly rejecting unsupported keywords (for now)
	return nil
}

// SchemaAt returns the "any" schema.
func (v *anySchema) SchemaAt([]string) ([]Schema, error) {
	return []Schema{v}, nil
}

// Type returns the Any schema type.
func (v *anySchema) Type() SchemaType {
	return Any
}

func (v *anySchema) expectsConstraints() bool { return false }

type numberSchema struct {
	min     *float64
	max     *float64
	choices []float64
}

// Validate that raw is a valid number and meets the schema's constraints.
func (v *numberSchema) Validate(raw []byte) (err error) {
	defer func() {
	}()

	var num *float64
	mylog.Check(json.Unmarshal(raw, &num))

	if num == nil {
		return fmt.Errorf(`cannot accept null value for "number" type`)
	}

	return validateNumber(*num, v.choices, v.min, v.max)
}

// SchemaAt returns the number schema if the path terminates here and an error if
// not.
func (v *numberSchema) SchemaAt(path []string) ([]Schema, error) {
	if len(path) != 0 {
		return nil, schemaAtErrorf(path, `cannot follow path beyond "number" type`)
	}

	return []Schema{v}, nil
}

// Type returns the Number schema type.
func (v *numberSchema) Type() SchemaType {
	return Number
}

func validateNumber[Num ~int64 | ~float64](num Num, choices []Num, min, max *Num) error {
	if len(choices) != 0 {
		var found bool
		for _, choice := range choices {
			if num == choice {
				found = true
				break
			}
		}

		if !found {
			return fmt.Errorf(`%v is not one of the allowed choices`, num)
		}
	}

	// these comparisons are susceptible to floating-point errors but given that
	// this won't be used for general storage it should be precise enough
	if min != nil && num < *min {
		return fmt.Errorf(`%v is less than the allowed minimum %v`, num, *min)
	}

	if max != nil && num > *max {
		return fmt.Errorf(`%v is greater than the allowed maximum %v`, num, *max)
	}

	return nil
}

func (v *numberSchema) parseConstraints(constraints map[string]json.RawMessage) error {
	if rawChoices, ok := constraints["choices"]; ok {
		var choices []float64
		mylog.Check(json.Unmarshal(rawChoices, &choices))

		if len(choices) == 0 {
			return fmt.Errorf(`cannot have "choices" constraint with empty list`)
		}

		v.choices = choices
	}

	if rawMin, ok := constraints["min"]; ok {
		if v.choices != nil {
			return fmt.Errorf(`cannot have "choices" and "min" constraints`)
		}

		var min float64
		mylog.Check(json.Unmarshal(rawMin, &min))

		v.min = &min
	}

	if rawMax, ok := constraints["max"]; ok {
		if v.choices != nil {
			return fmt.Errorf(`cannot have "choices" and "max" constraints`)
		}

		var max float64
		mylog.Check(json.Unmarshal(rawMax, &max))

		v.max = &max
	}

	if v.min != nil && v.max != nil && *v.min > *v.max {
		return fmt.Errorf(`cannot have "min" constraint with value greater than "max"`)
	}

	return nil
}

func (v *numberSchema) expectsConstraints() bool { return false }

type booleanSchema struct{}

func (v *booleanSchema) Validate(raw []byte) (err error) {
	defer func() {
	}()

	var val *bool
	mylog.Check(json.Unmarshal(raw, &val))

	if val == nil {
		return fmt.Errorf(`cannot accept null value for "bool" type`)
	}

	return nil
}

// SchemaAt returns the boolean schema if the path terminates here and an error
// if not.
func (v *booleanSchema) SchemaAt(path []string) ([]Schema, error) {
	if len(path) != 0 {
		return nil, schemaAtErrorf(path, `cannot follow path beyond "bool" type`)
	}

	return []Schema{v}, nil
}

// Type return the Bool type.
func (v *booleanSchema) Type() SchemaType {
	return Bool
}

func (v *booleanSchema) parseConstraints(map[string]json.RawMessage) error {
	// no error because we're not explicitly rejecting unsupported keywords (for now)
	return nil
}

func (v *booleanSchema) expectsConstraints() bool { return false }

type arraySchema struct {
	// topSchema is the schema for the top-level schema which contains the aliases.
	topSchema *StorageSchema

	// elementType represents the type of the array's elements and can be used to
	// validate them.
	elementType Schema

	// unique is true if the array should not contain duplicates.
	unique bool
}

func (v *arraySchema) Validate(raw []byte) error {
	var array *[]json.RawMessage
	mylog.Check(json.Unmarshal(raw, &array))

	if array == nil {
		return validationErrorf(`cannot accept null value for "array" type`)
	}

	for e, val := range *array {
		mylog.Check(v.elementType.Validate([]byte(val)))
	}

	if v.unique {
		valSet := make(map[string]struct{}, len(*array))

		for _, val := range *array {
			encodedVal := string(val)
			if _, ok := valSet[encodedVal]; ok {
				return validationErrorf(`cannot accept duplicate values for array with "unique" constraint`)
			}
			valSet[encodedVal] = struct{}{}
		}
	}

	return nil
}

// SchemaAt returns the array schema the path is empty. Otherwise, it calls SchemaAt
// for the next path element's schema if the path is valid.
func (v *arraySchema) SchemaAt(path []string) ([]Schema, error) {
	if len(path) == 0 {
		return []Schema{v}, nil
	}

	key := path[0]
	_ := mylog.Check2(strconv.ParseUint(key, 10, 0))

	return v.elementType.SchemaAt(path[1:])
}

// Type returns the Array schema type.
func (v *arraySchema) Type() SchemaType {
	return Array
}

func (v *arraySchema) parseConstraints(constraints map[string]json.RawMessage) error {
	rawValues, ok := constraints["values"]
	if !ok {
		return fmt.Errorf(`cannot parse "array": must have "values" constraint`)
	}

	typ := mylog.Check2(v.topSchema.parse(rawValues))

	v.elementType = typ

	if rawUnique, ok := constraints["unique"]; ok {
		var unique bool
		mylog.Check(json.Unmarshal(rawUnique, &unique))

		v.unique = unique
	}

	return nil
}

func (v *arraySchema) expectsConstraints() bool { return true }

// TODO: keep a list of expected types (to support alternatives), an actual type/value
// and then optional unmet constraints for the expected types. Then this could be used
// to have more concise errors when there are many possible types
// https://github.com/snapcore/snapd/pull/13502#discussion_r1463658230
type ValidationError struct {
	Path []interface{}
	Err  error
}

func (v *ValidationError) Error() string {
	var msg string
	if len(v.Path) == 0 {
		msg = "cannot accept top level element"
	} else {
		var sb strings.Builder
		for i, part := range v.Path {
			switch v := part.(type) {
			case string:
				if i > 0 {
					sb.WriteRune('.')
				}

				sb.WriteString(v)
			case int:
				sb.WriteString(fmt.Sprintf("[%d]", v))
			default:
				// can only happen due to bug
				sb.WriteString(".<n/a>")
			}
		}

		msg = fmt.Sprintf("cannot accept element in %q", sb.String())
	}

	return fmt.Sprintf("%s: %v", msg, v.Err)
}

func validationErrorFrom(err error) error {
	return &ValidationError{Err: err}
}

func validationErrorf(format string, v ...interface{}) error {
	return &ValidationError{Err: fmt.Errorf(format, v...)}
}

type schemaAtError struct {
	left int
	err  error
}

func (e *schemaAtError) Error() string {
	return e.err.Error()
}

func schemaAtErrorf(path []string, format string, v ...interface{}) error {
	return &schemaAtError{
		left: len(path),
		err:  fmt.Errorf(format, v...),
	}
}
