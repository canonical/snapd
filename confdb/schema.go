// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023-2024 Canonical Ltd
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
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/strutil"
)

type parser interface {
	DatabagSchema

	// expectsConstraints returns true if the parser must have a map definition
	// with constraints or false, if it may have a simple name definition.
	expectsConstraints() bool

	// parseConstraints parses constraints for a type defined as a JSON object.
	// Shouldn't be used with non-object/map type definitions.
	parseConstraints(map[string]json.RawMessage) error
}

// ParseStorageSchema parses a JSON confdb schema and returns a Schema that can be
// used to validate storage.
func ParseStorageSchema(raw []byte) (*StorageSchema, error) {
	var schemaDef map[string]json.RawMessage
	err := json.Unmarshal(raw, &schemaDef)
	if err != nil {
		return nil, fmt.Errorf("cannot parse top level schema as map: %w", err)
	}

	if rawType, ok := schemaDef["type"]; ok {
		var typ string
		if err := json.Unmarshal(rawType, &typ); err != nil {
			return nil, fmt.Errorf(`cannot parse top level schema's "type" entry: %w`, err)
		}

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
		if err := json.Unmarshal(aliasesRaw, &aliases); err != nil {
			return nil, fmt.Errorf(`cannot parse aliases map: %w`, err)
		}

		// TODO: if we want to allow aliases to refer to others, this must be handled
		// explicitly since the "aliases" map doesn't have any implicit order
		schema.aliases = make(map[string]*userDefinedType, len(aliases))
		for alias, typeDef := range aliases {
			if !validAliasName.Match([]byte(alias)) {
				return nil, fmt.Errorf(`cannot parse alias name %q: must match %s`, alias, validAliasName)
			}

			aliasSchema, err := schema.parse(typeDef)
			if err != nil {
				return nil, fmt.Errorf(`cannot parse alias %q: %w`, alias, err)
			}

			if aliasSchema.NestedEphemeral() {
				return nil, fmt.Errorf(`cannot use "ephemeral" in user-defined type: %s`, alias)
			}

			if aliasSchema.NestedVisibility(SecretVisibility) {
				return nil, fmt.Errorf(`cannot use "visibility" in user-defined type: %s`, alias)
			}

			schema.aliases[alias] = newUserDefinedType(aliasSchema)
		}
	}

	schema.topLevel, err = schema.parse(raw)
	if err != nil {
		return nil, err
	}

	return schema, nil
}

func listContains[K comparable](list []K, element K) bool {
	for _, item := range list {
		if item == element {
			return true
		}
	}
	return false
}

// userDefinedType represents a user-defined type defined under "aliases".
type userDefinedType struct {
	DatabagSchema

	stringBased bool
	visibility  Visibility
}

func newUserDefinedType(s DatabagSchema) *userDefinedType {
	_, ok := s.(*stringSchema)
	return &userDefinedType{
		DatabagSchema: s,
		stringBased:   ok,
	}
}

func (v *userDefinedType) Ephemeral() bool {
	// TODO: this isn't allowed for now
	return false
}

func (v *userDefinedType) Visibility() Visibility {
	return v.visibility
}

func (v *userDefinedType) NestedVisibility(vis Visibility) bool {
	return v.visibility == vis
}

func (v *userDefinedType) PruneByVisibility(path []Accessor, index int, vis []Visibility, data []byte) ([]byte, error) {
	// Secrets are not allowed in user-defined types so this code should never be reached
	return data, nil
}

// aliasReference represents a reference to a user-defined type in the schema.
type aliasReference struct {
	alias *userDefinedType

	ephemeral  bool
	visibility Visibility
}

// expectsConstraints return false because a reference to an alias doesn't
// necessarily require constraints.
func (*aliasReference) expectsConstraints() bool {
	return false
}

// parseConstraints parses any constraints passed to the alias reference, if any
// exist.
func (v *aliasReference) parseConstraints(constraints map[string]json.RawMessage) (err error) {
	v.ephemeral, err = parseEphemeral(constraints)
	if err != nil {
		return err
	}
	visibility, err := parseVisibility(constraints)
	if err != nil {
		return err
	}
	v.visibility = visibility
	return nil
}

// isStringBased returns true if this reference's base type is a string.
func (v *aliasReference) isStringBased() bool {
	return v.alias.stringBased
}

// Validate validates the data according to the user-defined type referred to by
// this reference.
func (v *aliasReference) Validate(data []byte) error {
	return v.alias.Validate(data)
}

// SchemaAt returns the alias reference itself if the path terminates at it. If
// not, it uses the user-defined type to resolve the path.
func (v *aliasReference) SchemaAt(path []Accessor) ([]DatabagSchema, error) {
	if len(path) == 0 {
		return []DatabagSchema{v}, nil
	}

	return v.alias.SchemaAt(path)
}

// Type uses the user-defined alias to resolve the type.
func (v *aliasReference) Type() SchemaType {
	return v.alias.Type()
}

func (v *aliasReference) Ephemeral() bool {
	return v.ephemeral
}

func (s *aliasReference) NestedEphemeral() bool {
	// TODO: aliases can't be marked as ephemeral for now (only their references)
	// so there's no point in calling the alias' NestedEphemeral()
	return s.Ephemeral()
}

func (s *aliasReference) Visibility() Visibility {
	return s.visibility
}

func (s *aliasReference) NestedVisibility(vis Visibility) bool {
	return s.visibility == vis
}

func (s *aliasReference) PruneByVisibility(path []Accessor, index int, vis []Visibility, data []byte) ([]byte, error) {
	if data == nil {
		return nil, &NoDataError{}
	}
	if listContains(vis, s.Visibility()) {
		if index <= len(path) {
			return nil, &UnAuthorizedAccessError{}
		}
		return nil, nil
	}
	// user-defined types cannot contain secret data so if the alias is not secret,
	// then all the data it contains must not contain secrets.
	return data, nil
}

// scalarSchema holds the data and behaviours common to all types.
type scalarSchema struct {
	ephemeral bool

	// indicates whether the schema has secret visibility
	visibility Visibility
}

func (s scalarSchema) Ephemeral() bool {
	return s.ephemeral
}

func (s scalarSchema) NestedEphemeral() bool {
	return s.Ephemeral()
}

func (s scalarSchema) Visibility() Visibility {
	return s.visibility
}

func (s scalarSchema) NestedVisibility(vis Visibility) bool {
	return s.Visibility() == vis
}

func (s scalarSchema) PruneByVisibility(path []Accessor, index int, vis []Visibility, data []byte) ([]byte, error) {
	if index < len(path) {
		return nil, schemaAtErrorf(path, `cannot follow path beyond scalar type`)
	}
	if data == nil {
		return nil, &NoDataError{}
	}
	if listContains(vis, s.Visibility()) {
		if index == len(path) {
			return nil, &UnAuthorizedAccessError{}
		}
		return nil, nil
	}
	return data, nil
}

func (b *scalarSchema) parseConstraints(constraints map[string]json.RawMessage) (err error) {
	b.ephemeral, err = parseEphemeral(constraints)
	if err != nil {
		return err
	}
	visibility, err := parseVisibility(constraints)
	if err != nil {
		return err
	}
	b.visibility = visibility
	return nil
}

func parseEphemeral(constraints map[string]json.RawMessage) (bool, error) {
	if rawVal, ok := constraints["ephemeral"]; ok {
		var eph bool
		err := json.Unmarshal([]byte(rawVal), &eph)
		if err != nil {
			return false, err
		}
		return eph, nil
	}

	return false, nil
}

func parseVisibility(constraints map[string]json.RawMessage) (Visibility, error) {
	if rawPattern, ok := constraints["visibility"]; ok {
		var visibility string
		err := json.Unmarshal(rawPattern, &visibility)
		if err != nil {
			return DefaultVisibility, err
		}

		if visibility != "secret" {
			return DefaultVisibility, fmt.Errorf(`cannot have a visibility field set to a value other than secret`)
		}
		return SecretVisibility, nil
	}
	return DefaultVisibility, nil
}

// StorageSchema represents a confdb storage schema and can be used to validate
// the storage.
type StorageSchema struct {
	// topLevel is the schema for the top level map.
	topLevel DatabagSchema

	// aliases are schemas that can validate custom types defined by the user.
	aliases map[string]*userDefinedType
}

// Validate validates the provided JSON object.
func (s *StorageSchema) Validate(raw []byte) error {
	return s.topLevel.Validate(raw)
}

// SchemaAt returns the types that may be stored at the specified path.
func (s *StorageSchema) SchemaAt(path []Accessor) ([]DatabagSchema, error) {
	return s.topLevel.SchemaAt(path)
}

func (s *StorageSchema) Type() SchemaType {
	return s.topLevel.Type()
}

func (s *StorageSchema) Ephemeral() bool {
	return s.topLevel.Ephemeral()
}

func (s *StorageSchema) NestedEphemeral() bool {
	return s.topLevel.NestedEphemeral()
}

func (s *StorageSchema) Visibility() Visibility {
	return s.topLevel.Visibility()
}

func (s *StorageSchema) NestedVisibility(vis Visibility) bool {
	return s.topLevel.NestedVisibility(vis)
}

func (s *StorageSchema) PruneByVisibility(path []Accessor, _ int, vis []Visibility, data []byte) (prunedData []byte, err error) {
	if len(vis) == 0 {
		return data, nil
	}
	if data == nil {
		return nil, &NoDataError{}
	}
	return s.topLevel.PruneByVisibility(path, 0, vis, data)
}

func (s *StorageSchema) parse(raw json.RawMessage) (DatabagSchema, error) {
	jsonType, err := parseTypeDefinition(raw)
	if err != nil {
		return nil, fmt.Errorf(`cannot parse type definition: %w`, err)
	}

	var typ string
	var schemaDef map[string]json.RawMessage
	switch typedVal := jsonType.(type) {
	case string:
		typ = typedVal

	case []json.RawMessage:
		alts, err := s.parseAlternatives(typedVal)
		if err != nil {
			return nil, fmt.Errorf(`cannot parse alternative types: %w`, err)
		}
		return alts, nil

	case map[string]json.RawMessage:
		schemaDef = typedVal
		rawType, ok := schemaDef["type"]
		if !ok {
			typ = "map"
		} else {
			if err := json.Unmarshal(rawType, &typ); err != nil {
				return nil, fmt.Errorf(`cannot parse "type" constraint in type definition: %w`, err)
			}
		}

	default:
		// cannot happen save for programmer error
		return nil, fmt.Errorf(`cannot parse schema definition of JSON type %T`, jsonType)
	}

	schema, err := s.newTypeSchema(typ)
	if err != nil {
		return nil, err
	}

	// only parse the schema if it's a schema definition w/ constraints
	if schemaDef != nil {
		if err := schema.parseConstraints(schemaDef); err != nil {
			return nil, err
		}
	} else if schema.expectsConstraints() {
		return nil, fmt.Errorf(`cannot parse %q: must be schema definition with constraints`, typ)
	}

	return schema, nil
}

// parseTypeDefinition tries to parse the raw JSON as a list, a map or a string
// (the accepted ways to express types).
func parseTypeDefinition(raw json.RawMessage) (any, error) {
	var typeErr *json.UnmarshalTypeError

	var l []json.RawMessage
	if err := json.Unmarshal(raw, &l); err == nil {
		return l, nil
	} else if !errors.As(err, &typeErr) {
		return nil, err
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err == nil {
		return m, nil
	} else if !errors.As(err, &typeErr) {
		return nil, err
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	} else {
		return nil, fmt.Errorf(`type must be expressed as map, string or list: %w`, err)
	}
}

// parseAlternatives takes a list of alternative types, parses them and creates
// a schema that accepts values matching any alternative.
func (s *StorageSchema) parseAlternatives(alternatives []json.RawMessage) (*alternativesSchema, error) {
	alt := &alternativesSchema{schemas: make([]DatabagSchema, 0, len(alternatives))}

	for _, altRaw := range alternatives {
		schema, err := s.parse(altRaw)
		if err != nil {
			return nil, err
		}
		alt.schemas = append(alt.schemas, schema)
	}

	if len(alt.schemas) == 0 {
		return nil, fmt.Errorf(`alternative type list cannot be empty`)
	}

	flatAlts := flattenAlternatives(alt)
	first := flatAlts[0].Visibility()
	for _, alt := range flatAlts {
		if alt.Visibility() != first {
			return nil, fmt.Errorf(`cannot have alternatives with different levels of visibility`)
		}
	}
	alt.schemas = flatAlts

	return alt, nil
}

// flattenAlternatives takes the schemas that comprise the alternative schema
// and flattens them into a single list.
func flattenAlternatives(alt *alternativesSchema) []DatabagSchema {
	var flat []DatabagSchema
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
		if alias, ok := stripAlias(typ); ok {
			return s.getAlias(alias)
		}

		return nil, fmt.Errorf("cannot parse unknown type %q", typ)
	}
}

// stripAlias removes the ${...} used to refer to an alias and returns the alias
// name. If the string isn't wrapped in ${}, it returns an empty string and false.
func stripAlias(str string) (string, bool) {
	if len(str) < 4 || !strings.HasPrefix(str, "${") || !strings.HasSuffix(str, "}") {
		return "", false
	}
	return str[2 : len(str)-1], true
}

func (s *StorageSchema) getAlias(ref string) (*aliasReference, error) {
	if alias, ok := s.aliases[ref]; ok {
		return &aliasReference{alias: alias}, nil
	}

	return nil, fmt.Errorf("cannot find alias %q", ref)
}

type alternativesSchema struct {
	// schemas holds schemas for the types allowed for the corresponding value.
	schemas []DatabagSchema
}

// Validate that raw matches at least one of the schemas in the alternative list.
func (v *alternativesSchema) Validate(raw []byte) error {
	var errs []error
	for _, schema := range v.schemas {
		err := schema.Validate(raw)
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

	return validationErrorFrom(errors.New(sb.String()))
}

// SchemaAt returns the list of schemas at the end of the path or an error if
// the path cannot be followed.
func (v *alternativesSchema) SchemaAt(path []Accessor) ([]DatabagSchema, error) {
	if len(path) == 0 {
		return v.schemas, nil
	}

	var types []DatabagSchema
	var lastErr error
	for _, alt := range v.schemas {
		altTypes, err := alt.SchemaAt(path)
		if err != nil {
			// some schemas may permit the path
			lastErr = err
			continue
		}
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

func (v *alternativesSchema) Ephemeral() bool { return false }

func (v *alternativesSchema) NestedEphemeral() bool {
	for _, schema := range v.schemas {
		if schema.NestedEphemeral() {
			return true
		}
	}

	return false
}

func (v *alternativesSchema) Visibility() Visibility {
	// Thanks to parsing logic we have that:
	//  - the v.schemas list can never be empty
	//  - all schemas in v.schemas have the same visibility
	return v.schemas[0].Visibility()
}

func (v *alternativesSchema) NestedVisibility(vis Visibility) bool {
	for _, schema := range v.schemas {
		if schema.NestedVisibility(vis) {
			return true
		}
	}

	return false
}

func (v *alternativesSchema) PruneByVisibility(path []Accessor, index int, vis []Visibility, data []byte) ([]byte, error) {
	// To find the correct alternative, we need to validate the data, because the
	// path itself may not differentiate between which alternative is contained in
	// the data in the case of identical prefixes. Though potentially validating
	// each alternative may explode with n levels of nesting, we do not expect this
	// to be a problem since nesting much beyond a simple leaf-node make the schema
	// hard to understand.
	for _, schema := range v.schemas {
		if err := schema.Validate(data); err != nil {
			continue
		}
		return schema.PruneByVisibility(path, index, vis, data)
	}
	return nil, fmt.Errorf(`found no matching alternative`)
}

type mapSchema struct {
	// topSchema is the schema for the top-level schema which contains the aliases.
	topSchema *StorageSchema

	// entrySchemas maps keys to their expected types. Alternatively, the schema
	// can constrain key and/or value types.
	entrySchemas map[string]DatabagSchema

	// valueSchema validates that the map's values match a certain type.
	valueSchema DatabagSchema

	// keySchema validates that the map's key match a certain type.
	keySchema DatabagSchema

	// requiredCombs holds combinations of keys that an instance of the map is
	// allowed to have.
	requiredCombs [][]string

	ephemeral bool

	// indicates the schema's visibility
	visibility Visibility
}

// Validate that raw is a valid map and meets the constraints set by the
// confdb storage schema.
func (v *mapSchema) Validate(raw []byte) error {
	var mapValue map[string]json.RawMessage
	if err := json.Unmarshal(raw, &mapValue); err != nil {
		typeErr := &json.UnmarshalTypeError{}
		if errors.As(err, &typeErr) {
			return validationErrorf("expected map type but value was %s", typeErr.Value)
		}
		return validationErrorFrom(err)
	}

	if mapValue == nil {
		return validationErrorf(`cannot accept null value for "map" type`)
	}

	if err := validMapKeys(mapValue); err != nil {
		return validationErrorFrom(err)
	}

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
				if err := validator.Validate(val); err != nil {
					var valErr *ValidationError
					if errors.As(err, &valErr) {
						valErr.Path = append([]any{key}, valErr.Path...)
					}
					return err
				}
			}
		}

		// all required entries are present and validated
		return nil
	}

	if v.keySchema != nil {
		for k := range mapValue {
			rawKey, err := json.Marshal(k)
			if err != nil {
				return fmt.Errorf("internal error: %w", err)
			}

			if err := v.keySchema.Validate(rawKey); err != nil {
				var valErr *ValidationError
				if errors.As(err, &valErr) {
					valErr.Path = append([]any{k}, valErr.Path...)
				}
				return err
			}
		}
	}

	if v.valueSchema != nil {
		for k, val := range mapValue {
			if err := v.valueSchema.Validate(val); err != nil {
				var valErr *ValidationError
				if errors.As(err, &valErr) {
					valErr.Path = append([]any{k}, valErr.Path...)
				}
				return err
			}
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
func (v *mapSchema) SchemaAt(path []Accessor) ([]DatabagSchema, error) {
	if len(path) == 0 {
		return []DatabagSchema{v}, nil
	}

	key := path[0]
	if key.Type() != MapKeyType && key.Type() != KeyPlaceholderType {
		return nil, schemaAtErrorf(path, `cannot use %q as key in map`, key.Access())
	}

	if v.entrySchemas != nil {
		if key.Type() == MapKeyType {
			// the subkey is a literal map key so there has to be a corresponding entry
			valSchema, ok := v.entrySchemas[key.Name()]
			if !ok {
				return nil, schemaAtErrorf(path, `cannot use %q as key in map`, key.Access())
			}

			return valSchema.SchemaAt(path[1:])
		}

		// since the path has a placeholder, we don't require it to be accepted by
		// all sub-schemas but it should be by at least one
		var schemas []DatabagSchema
		var lastErr error
		for _, valSchema := range v.entrySchemas {
			schema, err := valSchema.SchemaAt(path[1:])
			if err != nil {
				lastErr = err
				continue
			}
			schemas = append(schemas, schema...)
		}

		if len(schemas) == 0 {
			return nil, lastErr
		}

		return schemas, nil
	}

	// using key/value constraints instead
	return v.valueSchema.SchemaAt(path[1:])
}

// Type returns the Map type.
func (v *mapSchema) Type() SchemaType {
	return Map
}

func (v *mapSchema) Ephemeral() bool {
	return v.ephemeral
}

func (v *mapSchema) NestedEphemeral() bool {
	if v.Ephemeral() {
		return true
	}

	for _, schema := range v.entrySchemas {
		if schema.NestedEphemeral() {
			return true
		}
	}

	if v.keySchema != nil {
		if v.keySchema.NestedEphemeral() {
			return true
		}
	}

	return v.valueSchema != nil && v.valueSchema.NestedEphemeral()
}

func (v *mapSchema) Visibility() Visibility {
	return v.visibility
}

func (v *mapSchema) NestedVisibility(vis Visibility) bool {
	if v.visibility == vis {
		return true
	}

	for _, schema := range v.entrySchemas {
		if schema.NestedVisibility(vis) {
			return true
		}
	}

	if v.keySchema != nil {
		if v.keySchema.NestedVisibility(vis) {
			return true
		}
	}

	if v.valueSchema != nil {
		if v.valueSchema.NestedVisibility(vis) {
			return true
		}
	}

	return false
}

func (v *mapSchema) PruneByVisibility(path []Accessor, index int, vis []Visibility, data []byte) ([]byte, error) {
	if index < len(path) && path[index].Type() != KeyPlaceholderType && path[index].Type() != MapKeyType {
		return nil, schemaAtErrorf(path, `cannot use %q as key in map`, path[index].Access())
	}
	if data == nil {
		return nil, &NoDataError{}
	}
	if listContains(vis, v.Visibility()) || (v.keySchema != nil && listContains(vis, v.keySchema.Visibility())) {
		if index <= len(path) {
			return nil, &UnAuthorizedAccessError{}
		}
		return nil, nil
	}
	decoded, err := unmarshalLevel(path, index, data)
	if err != nil {
		return nil, err
	}
	m, ok := decoded.(map[string]json.RawMessage)
	if !ok {
		return nil, err
	}

	if index < len(path) && path[index].Type() == MapKeyType {
		_, ok := m[path[index].Name()]
		if !ok {
			return nil, &NoDataError{}
		}
	}

	pruned := map[string]json.RawMessage{}
	for key, value := range m {
		if index < len(path) && path[index].Type() == MapKeyType {
			if path[index].Name() != key {
				// The data is not along the path. Do not prune; simply copy over
				pruned[key] = value
				continue
			}
		}
		if v.entrySchemas != nil {
			valSchema, ok := v.entrySchemas[key]
			if !ok {
				return nil, fmt.Errorf(`map contains unexpected key "%s"`, key)
			}
			res, err := valSchema.PruneByVisibility(path, index+1, vis, value)
			if err != nil {
				if errors.Is(err, &NoDataError{}) ||
					(errors.Is(err, &UnAuthorizedAccessError{}) &&
						!(index < len(path) && path[index].Type() == MapKeyType)) {
					// If the error is an unauthorized error, then if
					// - we're along the path but the accessor is a placeholder
					// - we're not along the path
					// then we want to collect multiple entries and simply exclude
					// those with private data rather than erroring here
					continue
				}
				return nil, err
			}
			if res != nil {
				pruned[key] = res
			}
		}
		if v.valueSchema != nil {
			res, err := v.valueSchema.PruneByVisibility(path, index+1, vis, value)
			if err != nil {
				if errors.Is(err, &NoDataError{}) ||
					errors.Is(err, &UnAuthorizedAccessError{}) &&
						!(index < len(path) && path[index].Type() == MapKeyType) {
					continue
				}
				return nil, err
			}
			if res != nil {
				pruned[key] = res
			}
		}
	}
	if index < len(path) && path[index].Type() == MapKeyType {
		if _, ok = pruned[path[index].Name()]; !ok {
			return nil, &UnAuthorizedAccessError{}
		}
	}
	if len(pruned) > 0 {
		marshelled, err := json.Marshal(pruned)
		if err != nil {
			return nil, err
		}
		return marshelled, nil
	} else if index <= len(path) && len(m) > 0 {
		// We are somewhere along the path and there was data in the map, yet it all got pruned.
		// The data must therefore be unauthorized since a map cannot contain nulls.
		return nil, &UnAuthorizedAccessError{}
	}
	return nil, nil
}

func (v *mapSchema) parseConstraints(constraints map[string]json.RawMessage) error {
	ephemeral, err := parseEphemeral(constraints)
	if err != nil {
		return err
	}
	v.ephemeral = ephemeral

	visibility, err := parseVisibility(constraints)
	if err != nil {
		return err
	}
	v.visibility = visibility

	err = checkExclusiveMapConstraints(constraints)
	if err != nil {
		return fmt.Errorf(`cannot parse map: %w`, err)
	}

	// maps can be "schemas" with types for specific entries and optional "required" constraints
	if rawEntries, ok := constraints["schema"]; ok {
		var entries map[string]json.RawMessage
		if err := json.Unmarshal(rawEntries, &entries); err != nil {
			return fmt.Errorf(`cannot parse map's "schema" constraint: %v`, err)
		}

		if err := validMapKeys(entries); err != nil {
			return fmt.Errorf(`cannot parse map: %w`, err)
		}

		v.entrySchemas = make(map[string]DatabagSchema, len(entries))
		for key, value := range entries {
			entrySchema, err := v.topSchema.parse(value)
			if err != nil {
				return err
			}

			v.entrySchemas[key] = entrySchema
		}

		// "required" can be a list of keys or many lists of alternative combinations
		if rawRequired, ok := constraints["required"]; ok {
			var requiredCombs [][]string
			if err := json.Unmarshal(rawRequired, &requiredCombs); err != nil {
				var typeErr *json.UnmarshalTypeError
				if !errors.As(err, &typeErr) {
					return fmt.Errorf(`cannot parse map's "required" constraint: %v`, err)
				}

				var required []string
				if err := json.Unmarshal(rawRequired, &required); err != nil {
					return fmt.Errorf(`cannot parse map's "required" constraint: %v`, err)
				}

				v.requiredCombs = [][]string{required}
			} else {
				v.requiredCombs = requiredCombs
			}

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
		if v.keySchema, err = v.parseMapKeyType(rawKeyDef); err != nil {
			return fmt.Errorf(`cannot parse "keys" constraint: %w`, err)
		}
	}

	rawValuesDef, ok := constraints["values"]
	if ok {
		v.valueSchema, err = v.topSchema.parse(rawValuesDef)
		if err != nil {
			return err
		}
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

func (v *mapSchema) parseMapKeyType(raw json.RawMessage) (DatabagSchema, error) {
	var typ string
	if err := json.Unmarshal(raw, &typ); err != nil {
		var typeErr *json.UnmarshalTypeError
		if !errors.As(err, &typeErr) {
			return nil, err
		}

		var schemaDef map[string]json.RawMessage
		if err := json.Unmarshal(raw, &schemaDef); err != nil {
			return nil, err
		}

		if rawType, ok := schemaDef["type"]; ok {
			if err := json.Unmarshal(rawType, &typ); err != nil {
				return nil, err
			}

			if typ != "string" {
				return nil, fmt.Errorf(`must be based on string but type was %s`, typ)
			}
		}

		schema := &stringSchema{}
		if err := schema.parseConstraints(schemaDef); err != nil {
			return nil, err
		}

		return schema, nil
	}

	if typ == "string" {
		return &stringSchema{}, nil
	}

	if aliasName, ok := stripAlias(typ); ok {
		alias, err := v.topSchema.getAlias(aliasName)
		if err != nil {
			return nil, err
		}

		if !alias.isStringBased() {
			return nil, fmt.Errorf(`key type %q must be based on string`, aliasName)
		}

		return alias, nil
	}

	return nil, fmt.Errorf(`keys must be based on string but type was %s`, typ)
}

func (v *mapSchema) expectsConstraints() bool { return true }

type stringSchema struct {
	scalarSchema

	// pattern is a regex pattern that the string must match.
	pattern *regexp.Regexp

	// choices holds the possible values the string can take, if non-empty.
	choices []string
}

// Validate that raw is a valid string and meets the schema's constraints.
func (v *stringSchema) Validate(raw []byte) (err error) {
	defer func() {
		if err != nil {
			err = validationErrorFrom(err)
		}
	}()

	var value *string
	if err := json.Unmarshal(raw, &value); err != nil {
		typeErr := &json.UnmarshalTypeError{}
		if errors.As(err, &typeErr) {
			return fmt.Errorf("expected string type but value was %s", typeErr.Value)
		}
		return err
	}

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
func (v *stringSchema) SchemaAt(path []Accessor) ([]DatabagSchema, error) {
	if len(path) != 0 {
		return nil, schemaAtErrorf(path, `cannot follow path beyond "string" type`)
	}

	return []DatabagSchema{v}, nil
}

func (v *stringSchema) Type() SchemaType {
	return String
}

func (v *stringSchema) parseConstraints(constraints map[string]json.RawMessage) error {
	if err := v.scalarSchema.parseConstraints(constraints); err != nil {
		return err
	}

	if rawChoices, ok := constraints["choices"]; ok {
		var choices []string
		if err := json.Unmarshal(rawChoices, &choices); err != nil {
			return fmt.Errorf(`cannot parse "choices" constraint: %w`, err)
		}

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
		err := json.Unmarshal(rawPattern, &patt)
		if err != nil {
			return fmt.Errorf(`cannot parse "pattern" constraint: %w`, err)
		}

		if v.pattern, err = regexp.Compile(patt); err != nil {
			return fmt.Errorf(`cannot parse "pattern" constraint: %w`, err)
		}
	}

	return nil
}

func (v *stringSchema) expectsConstraints() bool { return false }

type intSchema struct {
	scalarSchema

	min     *int64
	max     *int64
	choices []int64
}

// Validate that raw is a valid integer and meets the schema's constraints.
func (v *intSchema) Validate(raw []byte) (err error) {
	defer func() {
		if err != nil {
			err = validationErrorFrom(err)
		}
	}()

	var num *int64
	if err := json.Unmarshal(raw, &num); err != nil {
		typeErr := &json.UnmarshalTypeError{}
		if errors.As(err, &typeErr) {
			return fmt.Errorf("expected int type but value was %s", typeErr.Value)
		}
		return err
	}

	if num == nil {
		return fmt.Errorf(`cannot accept null value for "int" type`)
	}

	return validateNumber(*num, v.choices, v.min, v.max)
}

// SchemaAt returns the int schema if the path terminates here and an error if
// not.
func (v *intSchema) SchemaAt(path []Accessor) ([]DatabagSchema, error) {
	if len(path) != 0 {
		return nil, schemaAtErrorf(path, `cannot follow path beyond "int" type`)
	}

	return []DatabagSchema{v}, nil
}

// Type returns the Int schema type.
func (v *intSchema) Type() SchemaType {
	return Int
}

func (v *intSchema) parseConstraints(constraints map[string]json.RawMessage) error {
	if err := v.scalarSchema.parseConstraints(constraints); err != nil {
		return err
	}

	if rawChoices, ok := constraints["choices"]; ok {
		var choices []int64
		err := json.Unmarshal(rawChoices, &choices)
		if err != nil {
			return fmt.Errorf(`cannot parse "choices" constraint: %v`, err)
		}

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
		if err := json.Unmarshal(rawMin, &min); err != nil {
			return fmt.Errorf(`cannot parse "min" constraint: %v`, err)
		}
		v.min = &min
	}

	if rawMax, ok := constraints["max"]; ok {
		if v.choices != nil {
			return fmt.Errorf(`cannot have "choices" and "max" constraints`)
		}

		var max int64
		if err := json.Unmarshal(rawMax, &max); err != nil {
			return fmt.Errorf(`cannot parse "max" constraint: %v`, err)
		}
		v.max = &max
	}

	if v.min != nil && v.max != nil && *v.min > *v.max {
		return fmt.Errorf(`cannot have "min" constraint with value greater than "max"`)
	}

	return nil
}

func (v *intSchema) expectsConstraints() bool { return false }

type anySchema struct {
	scalarSchema
}

func (v *anySchema) Validate(raw []byte) (err error) {
	defer func() {
		if err != nil {
			err = validationErrorFrom(err)
		}
	}()

	var val any
	if err := json.Unmarshal(raw, &val); err != nil {
		return err
	}

	if val == nil {
		return fmt.Errorf(`cannot accept null value for "any" type`)
	}
	return nil
}

func (v *anySchema) parseConstraints(constraints map[string]json.RawMessage) error {
	return v.scalarSchema.parseConstraints(constraints)
}

// SchemaAt returns the "any" schema.
func (v *anySchema) SchemaAt([]Accessor) ([]DatabagSchema, error) {
	return []DatabagSchema{v}, nil
}

// Type returns the Any schema type.
func (v *anySchema) Type() SchemaType {
	return Any
}

func (v *anySchema) expectsConstraints() bool { return false }

type numberSchema struct {
	scalarSchema

	min     *float64
	max     *float64
	choices []float64
}

// Validate that raw is a valid number and meets the schema's constraints.
func (v *numberSchema) Validate(raw []byte) (err error) {
	defer func() {
		if err != nil {
			err = validationErrorFrom(err)
		}
	}()

	var num *float64
	if err := json.Unmarshal(raw, &num); err != nil {
		typeErr := &json.UnmarshalTypeError{}
		if errors.As(err, &typeErr) {
			return fmt.Errorf("expected number type but value was %s", typeErr.Value)
		}
		return err
	}

	if num == nil {
		return fmt.Errorf(`cannot accept null value for "number" type`)
	}

	return validateNumber(*num, v.choices, v.min, v.max)
}

// SchemaAt returns the number schema if the path terminates here and an error if
// not.
func (v *numberSchema) SchemaAt(path []Accessor) ([]DatabagSchema, error) {
	if len(path) != 0 {
		return nil, schemaAtErrorf(path, `cannot follow path beyond "number" type`)
	}

	return []DatabagSchema{v}, nil
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
	if err := v.scalarSchema.parseConstraints(constraints); err != nil {
		return err
	}

	if rawChoices, ok := constraints["choices"]; ok {
		var choices []float64
		err := json.Unmarshal(rawChoices, &choices)
		if err != nil {
			return fmt.Errorf(`cannot parse "choices" constraint: %v`, err)
		}

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
		if err := json.Unmarshal(rawMin, &min); err != nil {
			return fmt.Errorf(`cannot parse "min" constraint: %v`, err)
		}
		v.min = &min
	}

	if rawMax, ok := constraints["max"]; ok {
		if v.choices != nil {
			return fmt.Errorf(`cannot have "choices" and "max" constraints`)
		}

		var max float64
		if err := json.Unmarshal(rawMax, &max); err != nil {
			return fmt.Errorf(`cannot parse "max" constraint: %v`, err)
		}
		v.max = &max
	}

	if v.min != nil && v.max != nil && *v.min > *v.max {
		return fmt.Errorf(`cannot have "min" constraint with value greater than "max"`)
	}

	return nil
}

func (v *numberSchema) expectsConstraints() bool { return false }

type booleanSchema struct {
	scalarSchema
}

func (v *booleanSchema) Validate(raw []byte) (err error) {
	defer func() {
		if err != nil {
			err = validationErrorFrom(err)
		}
	}()

	var val *bool
	if err := json.Unmarshal(raw, &val); err != nil {
		typeErr := &json.UnmarshalTypeError{}
		if errors.As(err, &typeErr) {
			return fmt.Errorf("expected bool type but value was %s", typeErr.Value)
		}
		return err
	}

	if val == nil {
		return fmt.Errorf(`cannot accept null value for "bool" type`)
	}

	return nil
}

// SchemaAt returns the boolean schema if the path terminates here and an error
// if not.
func (v *booleanSchema) SchemaAt(path []Accessor) ([]DatabagSchema, error) {
	if len(path) != 0 {
		return nil, schemaAtErrorf(path, `cannot follow path beyond "bool" type`)
	}

	return []DatabagSchema{v}, nil
}

// Type return the Bool type.
func (v *booleanSchema) Type() SchemaType {
	return Bool
}

func (v *booleanSchema) parseConstraints(constraints map[string]json.RawMessage) error {
	return v.scalarSchema.parseConstraints(constraints)
}

func (v *booleanSchema) expectsConstraints() bool { return false }

type arraySchema struct {
	// topSchema is the schema for the top-level schema which contains the aliases.
	topSchema *StorageSchema

	// elementType represents the type of the array's elements and can be used to
	// validate them.
	elementType DatabagSchema

	// unique is true if the array should not contain duplicates.
	unique bool

	ephemeral bool

	// indicates the schema's visibility
	visibility Visibility
}

func (v *arraySchema) Validate(raw []byte) error {
	var array *[]json.RawMessage
	if err := json.Unmarshal(raw, &array); err != nil {
		typeErr := &json.UnmarshalTypeError{}
		if errors.As(err, &typeErr) {
			return validationErrorf("expected array type but value was %s", typeErr.Value)
		}
		return validationErrorFrom(err)
	}

	if array == nil {
		return validationErrorf(`cannot accept null value for "array" type`)
	}

	for e, val := range *array {
		if err := v.elementType.Validate([]byte(val)); err != nil {
			var vErr *ValidationError
			if errors.As(err, &vErr) {
				vErr.Path = append([]any{e}, vErr.Path...)
			}
			return err
		}
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
func (v *arraySchema) SchemaAt(path []Accessor) ([]DatabagSchema, error) {
	if len(path) == 0 {
		return []DatabagSchema{v}, nil
	}

	// key can be a number or a placeholder in square brackets ([1] or [{n}])
	key := path[0]
	if key.Type() != IndexPlaceholderType && key.Type() != ListIndexType {
		return nil, schemaAtErrorf(path, `key %q cannot be used to index array`, key.Access())
	}

	return v.elementType.SchemaAt(path[1:])
}

// Type returns the Array schema type.
func (v *arraySchema) Type() SchemaType {
	return Array
}

func (v *arraySchema) Ephemeral() bool {
	return v.ephemeral
}

func (v *arraySchema) NestedEphemeral() bool {
	return v.Ephemeral() || v.elementType.NestedEphemeral()
}

func (v *arraySchema) Visibility() Visibility {
	return v.visibility
}

func (v *arraySchema) NestedVisibility(vis Visibility) bool {
	if v.Visibility() == vis {
		return true
	}
	return v.elementType.NestedVisibility(vis)
}

func (v *arraySchema) PruneByVisibility(path []Accessor, index int, vis []Visibility, data []byte) ([]byte, error) {
	if index < len(path) && path[index].Type() != IndexPlaceholderType && path[index].Type() != ListIndexType {
		return nil, schemaAtErrorf(path, `key %q cannot be used to index array`, path[index].Access())
	}
	if data == nil {
		return nil, &NoDataError{}
	}
	if listContains(vis, v.Visibility()) {
		if index <= len(path) {
			return nil, &UnAuthorizedAccessError{}
		}
		return nil, nil
	}
	decoded, err := unmarshalLevel(path, index, data)
	if err != nil {
		return nil, err
	}
	array, ok := decoded.([]json.RawMessage)
	if !ok {
		return nil, fmt.Errorf(`data must be an array`)
	}
	arrayIndex := -1
	if index < len(path) && path[index].Type() == ListIndexType {
		arrayIndex, err = strconv.Atoi(path[index].Name())
		if err != nil {
			return nil, err
		}
		if index >= len(array) {
			return nil, &NoDataError{}
		}
	}
	pruned := []json.RawMessage{}
	for i, item := range array {
		if arrayIndex != -1 && arrayIndex != i {
			// This is not the data indicated in the path so do not prune; just copy over
			pruned = append(pruned, item)
			continue
		}
		res, err := v.elementType.PruneByVisibility(path, index+1, vis, item)

		if err != nil {
			if errors.Is(err, &NoDataError{}) ||
				(errors.Is(err, &UnAuthorizedAccessError{}) &&
					!(index < len(path) && path[index].Type() == ListIndexType)) {
				// If the error is an unauthorized error, then if
				// - we're along the path but the accessor is a placeholder
				// - we're not along the path
				// then we want to collect multiple entries and simply exclude
				// those with private data rather than erroring here
				continue
			}
			return nil, err
		}
		if res != nil {
			pruned = append(pruned, res)
		}
	}
	if len(pruned) > 0 {
		marshelled, err := json.Marshal(pruned)
		if err != nil {
			return nil, err
		}
		return marshelled, nil
	} else if index <= len(path) && len(array) > 0 {
		// If we are along the path and we pruned away all the data, since
		// we cannot return an empty container, consider this unauthorized.
		return nil, &UnAuthorizedAccessError{}
	}
	return nil, nil
}

func (v *arraySchema) parseConstraints(constraints map[string]json.RawMessage) error {
	eph, err := parseEphemeral(constraints)
	if err != nil {
		return err
	}
	v.ephemeral = eph

	visibility, err := parseVisibility(constraints)
	if err != nil {
		return err
	}
	v.visibility = visibility

	rawValues, ok := constraints["values"]
	if !ok {
		return fmt.Errorf(`cannot parse "array": must have "values" constraint`)
	}

	typ, err := v.topSchema.parse(rawValues)
	if err != nil {
		return fmt.Errorf(`cannot parse "array" values type: %v`, err)
	}

	v.elementType = typ

	if rawUnique, ok := constraints["unique"]; ok {
		var unique bool
		if err := json.Unmarshal(rawUnique, &unique); err != nil {
			return fmt.Errorf(`cannot parse array's "unique" constraint: %v`, err)
		}

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
	Path []any
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

func validationErrorf(format string, v ...any) error {
	return &ValidationError{Err: fmt.Errorf(format, v...)}
}

type schemaAtError struct {
	left int
	err  error
}

func (e *schemaAtError) Error() string {
	return e.err.Error()
}

func schemaAtErrorf(path []Accessor, format string, v ...any) error {
	return &schemaAtError{
		left: len(path),
		err:  fmt.Errorf(format, v...),
	}
}
