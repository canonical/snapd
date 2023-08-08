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

	"github.com/snapcore/snapd/strutil"
)

func ParseSchema(raw []byte) (*CustomSchema, error) {
	var schemaDef map[string]json.RawMessage
	err := json.Unmarshal(raw, &schemaDef)
	if err != nil {
		return nil, fmt.Errorf("cannot parse top level schema: top level must be a map")
	}

	schema := &CustomSchema{}
	if val, ok := schemaDef["types"]; ok {
		var userTypes map[string]json.RawMessage
		if err := json.Unmarshal(val, &userTypes); err != nil {
			return nil, fmt.Errorf(`cannot parse user-defined types at top level (must be a map): %w`, err)
		}

		schema.userTypes = make(map[string]Schema, len(userTypes))
		for userTypeName, typeDef := range userTypes {
			userTypeSchema, err := schema.Parse(typeDef)
			if err != nil {
				return nil, fmt.Errorf(`cannot parse user-defined type %q: %w`, userTypeName, err)
			}

			schema.userTypes[userTypeName] = userTypeSchema
		}
	}

	schema.topLevel, err = schema.Parse(raw)
	if err != nil {
		return nil, err
	}

	return schema, nil
}

type CustomSchema struct {
	userTypes map[string]Schema
	topLevel  Schema
}

func (s *CustomSchema) Validate(raw []byte) error {
	return s.topLevel.Validate(raw)
}

func (s *CustomSchema) Parse(raw json.RawMessage) (Schema, error) {
	var typ string
	var schemaDef map[string]json.RawMessage
	if err := json.Unmarshal(raw, &schemaDef); err != nil {
		var typeErr *json.UnmarshalTypeError
		if !errors.As(err, &typeErr) {
			return nil, err
		}

		if err := json.Unmarshal(raw, &typ); err != nil {
			return nil, err
		}
	} else {
		rawType, ok := schemaDef["type"]
		if !ok {
			typ = "map"
		} else {
			if err := json.Unmarshal(rawType, &typ); err != nil {
				return nil, err
			}
		}
	}

	var schema Schema
	switch typ {
	case "map":
		mapSchema := &mapSchema{
			baseSchema: s,
		}

		if err := mapSchema.Parse(raw); err != nil {
			return nil, err
		}
		schema = mapSchema
	case "int":
		intSchema := &intSchema{}

		if err := intSchema.Parse(raw); err != nil {
			return nil, err
		}
		schema = intSchema
	case "string":
		strSchema := &stringSchema{}
		if err := strSchema.Parse(raw); err != nil {
			return nil, err
		}
		schema = strSchema
	case "number":
		return nil, nil
	case "bool":
		return nil, nil
	case "array":
		return nil, nil
	default:
		if typ[0] != '$' {
			return nil, fmt.Errorf("cannot parse type %q: unknown", typ)
		}
		userType, ok := s.userTypes[typ[1:]]
		if !ok {
			return nil, fmt.Errorf(`cannot find referenced user-defined type %q`, typ)
		}
		schema = userType
	}

	return schema, nil
}

type stringSchema struct {
	// pattern is a regex pattern that the string must match.
	pattern *regexp.Regexp
	// choices holds the possible values the string can take, if non-empty.
	choices []string

	// TODO: JSON schema formats? which ones and how will they be defined?
}

func (v *stringSchema) Validate(raw []byte) error {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}

	if len(v.choices) != 0 && !strutil.ListContains(v.choices, value) {
		return fmt.Errorf(`string %q is not one of the allowed choices`, value)
	}

	if v.pattern != nil && !v.pattern.Match([]byte(value)) {
		return fmt.Errorf(`string %q doesn't match schema pattern %s`, value, v.pattern.String())
	}

	return nil
}

func (v *stringSchema) Parse(raw json.RawMessage) error {
	constraints, err := getTypeConstraints(raw, "string")
	if err != nil {
		return err
	}

	if rawPattern, ok := constraints["pattern"]; ok {
		var patt string
		err := json.Unmarshal(rawPattern, &patt)
		if err != nil {
			return err
		}

		if v.pattern, err = regexp.Compile(patt); err != nil {
			return err
		}
	}

	if rawChoices, ok := constraints["choices"]; ok {
		var choices []string
		err := json.Unmarshal(rawChoices, &choices)
		if err != nil {
			return err
		}

		if len(choices) == 0 {
			return fmt.Errorf(`cannot have "choices" constraint with empty list: field must be populated or not exist`)
		}

		v.choices = choices
	}

	return nil
}

type mapSchema struct {
	baseSchema *CustomSchema

	// entries map keys that can the map can contain to their expected types.
	// Alternatively, the schema can instead key and/or value types.
	entryTypes map[string]Schema

	// valueType validates that the map's values match a certain type.
	valueType Schema

	// keyType validates that the map's key match a certain type.
	keyType Schema

	// requiredCombs holds combinations of keys that an instance of the map is
	// allowed to have.
	requiredCombs [][]string
}

func (v *mapSchema) Validate(raw []byte) error {
	var schemaDef map[string]json.RawMessage
	if err := json.Unmarshal(raw, &schemaDef); err != nil {
		return err
	}

	var missing bool
	for _, required := range v.requiredCombs {
		missing = false
		for _, key := range required {
			if _, ok := schemaDef[key]; !ok {
				missing = true
				break
			}
		}

		// met one combination of required keys so we can stop
		if !missing {
			break
		}
	}

	if missing {
		return fmt.Errorf(`cannot find required combinations of keys`)
	}

	if v.entryTypes != nil {
		for key, val := range schemaDef {
			if validator, ok := v.entryTypes[key]; ok {
				if err := validator.Validate(val); err != nil {
					return err
				}
			} else {
				return fmt.Errorf(`unexpected field %q in map`, key)
			}
		}
	}

	if v.keyType != nil {
		for k := range schemaDef {
			rawKey, err := json.Marshal(k)
			if err != nil {
				return err
			}

			if err := v.keyType.Validate(rawKey); err != nil {
				return err
			}

		}
	}

	if v.valueType != nil {
		for _, val := range schemaDef {
			if err := v.valueType.Validate(val); err != nil {
				return err
			}
		}
	}

	return nil
}

func (v *mapSchema) Parse(raw json.RawMessage) error {
	var schemaDef map[string]json.RawMessage
	if err := json.Unmarshal(raw, &schemaDef); err != nil {
		return err
	}

	requiredRaw, ok := schemaDef["required"]
	if ok {
		if err := json.Unmarshal(requiredRaw, &v.requiredCombs); err != nil {
			return fmt.Errorf(`cannot unmarshal map's "required" field: %v`, err)
		}
	}

	// a map can have a "schema" constrain with specific values
	if schema, ok := schemaDef["schema"]; ok {
		var nextLevel map[string]json.RawMessage
		if err := json.Unmarshal(schema, &nextLevel); err != nil {
			return fmt.Errorf(`cannot unmarshal map's "schema" field: %v`, err)
		}

		v.entryTypes = make(map[string]Schema, len(nextLevel))
		for key, value := range nextLevel {
			validator, err := v.baseSchema.Parse(value)
			if err != nil {
				return fmt.Errorf(`cannot parse constraint for key %q: %w`, key, err)
			}

			v.entryTypes[key] = validator
		}

		return nil
	}

	// alternatively, it can constrain the type of its keys and/or values
	keyDescription, ok := schemaDef["keys"]
	if ok {
		var err error
		v.keyType, err = v.baseSchema.Parse(keyDescription)
		if err != nil {
			return fmt.Errorf(`cannot parse "keys" constraint in map schema: %w`, err)
		}
	}

	valuesDescriptor, ok := schemaDef["values"]
	if ok {
		var err error
		v.valueType, err = v.baseSchema.Parse(valuesDescriptor)
		if err != nil {
			return fmt.Errorf(`cannot parse "values" constraint in map schema: %w`, err)
		}
	}

	return nil
}

type intSchema struct {
	min     *int
	max     *int
	choices []int
}

func (v *intSchema) Validate(raw []byte) error {
	var num int
	if err := json.Unmarshal(raw, &num); err != nil {
		return err
	}

	if v.min != nil && num < *v.min {
		return fmt.Errorf(`integer %d is less than allowed minimum %d`, num, *v.min)
	}

	if v.max != nil && num > *v.max {
		return fmt.Errorf(`integer %d is greater than allowed maximum %d`, num, *v.max)
	}

	if len(v.choices) != 0 {
		var found bool
		for _, choice := range v.choices {
			if num == choice {
				found = true
				break
			}
		}

		if !found {
			return fmt.Errorf(`integer %d is not one of the allowed choices`, num)
		}
	}

	return nil
}

func (v *intSchema) Parse(raw json.RawMessage) error {
	constraints, err := getTypeConstraints(raw, "int")
	if err != nil {
		return err
	} else if constraints == nil {
		return nil
	}

	if rawChoices, ok := constraints["choices"]; ok {
		var choices []int
		err := json.Unmarshal(rawChoices, &choices)
		if err != nil {
			return err
		}

		if len(choices) == 0 {
			return fmt.Errorf(`cannot have "choices" constraint with empty list: field must be populated or not exist`)
		}

		v.choices = choices
	}

	if minRaw, ok := constraints["min"]; ok {
		var min int
		if err := json.Unmarshal(minRaw, &min); err != nil {
			return err
		}
		v.min = &min
	}

	if maxRaw, ok := constraints["max"]; ok {
		var max int
		if err := json.Unmarshal(maxRaw, &max); err != nil {
			return err
		}
		v.max = &max
	}

	return nil
}

func getTypeConstraints(raw json.RawMessage, expectedType string) (map[string]json.RawMessage, error) {
	var constraints map[string]json.RawMessage
	if err := json.Unmarshal(raw, &constraints); err != nil {
		var typeErr *json.UnmarshalTypeError
		if !errors.As(err, &typeErr) {
			return nil, err
		}

		var typ string
		if err := json.Unmarshal(raw, &typ); err != nil {
			return nil, fmt.Errorf("cannot parse type description as map or string: %w", err)
		}

		if typ != expectedType {
			return nil, fmt.Errorf(`cannot parse type %q as expected %s`, typ, expectedType)
		}

		// simple type declaration without any constraints
		return nil, nil
	}

	return constraints, nil
}
