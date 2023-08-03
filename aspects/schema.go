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
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

type validator interface {
	Validate(json.RawMessage) error
}

type stringValidator struct {
	// pattern is a regex pattern that the string must match.
	pattern *regexp.Regexp
	// choices holds the possible values the string can take, if non-empty.
	choices []string

	// TODO: JSON schema formats? which ones and how will they be defined?
}

func (*stringValidator) Validate(json.RawMessage) error {
	// TODO: implement validation
	return nil
}

func (v *stringValidator) parse(raw json.RawMessage) error {
	var constraints map[string]json.RawMessage
	if err := json.Unmarshal(raw, &constraints); err != nil {
		var typeErr *json.UnmarshalTypeError
		if !errors.As(err, &typeErr) {
			return err
		}

		var typ string
		if err := json.Unmarshal(raw, &typ); err != nil {
			return err
		}

		if typ != "string" {
			// NOTE: shouldn't happen save for a bug in parseValidator
			return fmt.Errorf(`cannot parse type %q as string`, typ)
		}

		// a simple "string" type with no constraints
		return nil
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

type mapValidator struct {
	schema *CustomSchema

	// entries map keys that can the map can contain to their expected types.
	// Alternatively, the schema can instead key and/or value types.
	entryTypes map[string]validator

	// valueType validates that the map's values match a certain type.
	valueType validator

	// keyType validates that the map's key match a certain type.
	keyType validator

	// required holds combinations of keys that an instance of the map is allowed
	// to have.
	required [][]string
}

func (v *mapValidator) Validate(json.RawMessage) error {
	// TODO: implement validation
	return nil
}

func (v *mapValidator) parse(level map[string]json.RawMessage) error {
	requiredRaw, ok := level["required"]
	if ok {
		if err := json.Unmarshal(requiredRaw, &v.required); err != nil {
			return fmt.Errorf(`cannot unmarshal map's "required" field: %v`, err)
		}
	}

	// a map can have a "schema" constrain with specific values
	if schema, ok := level["schema"]; ok {
		var nextLevel map[string]json.RawMessage
		if err := json.Unmarshal(schema, &nextLevel); err != nil {
			return fmt.Errorf(`cannot unmarshal map's "schema" field: %v`, err)
		}

		v.entryTypes = make(map[string]validator, len(nextLevel))
		for key, value := range nextLevel {
			validator, err := v.schema.parseValidator(value)
			if err != nil {
				return fmt.Errorf(`cannot parse constraint for key %q: %w`, key, err)
			}

			v.entryTypes[key] = validator
		}

		return nil
	}

	// alternatively, it can constrain the type of its keys and/or values
	keyDescription, ok := level["keys"]
	if ok {
		var err error
		v.keyType, err = v.schema.parseValidator(keyDescription)
		if err != nil {
			return fmt.Errorf(`cannot parse "keys" constraint in map schema: %w`, err)
		}
	}

	valuesDescriptor, ok := level["values"]
	if ok {
		var err error
		v.valueType, err = v.schema.parseValidator(valuesDescriptor)
		if err != nil {
			return fmt.Errorf(`cannot parse "values" constraint in map schema: %w`, err)
		}
	}

	return nil
}

type CustomSchema struct {
	userTypes map[string]validator
	validator validator
}

func (s *CustomSchema) Validate(raw json.RawMessage) error {
	return s.validator.Validate(raw)
}

func (s *CustomSchema) userTypeValidator(typeName string) validator {
	return s.userTypes[typeName]
}

func ParseSchema(raw []byte) (*CustomSchema, error) {
	var level map[string]json.RawMessage
	err := json.Unmarshal(raw, &level)
	if err != nil {
		return nil, fmt.Errorf("cannot parse top level schema: top level must be a map")
	}

	schema := &CustomSchema{}
	if val, ok := level["types"]; ok {
		var userTypes map[string]json.RawMessage
		if err := json.Unmarshal(val, &userTypes); err != nil {
			return nil, fmt.Errorf(`cannot parse user-defined types at top level (must be a map): %w`, err)
		}

		schema.userTypes = make(map[string]validator, len(userTypes))
		for userTypeName, typeDef := range userTypes {
			validator, err := schema.parseValidator(typeDef)
			if err != nil {
				return nil, fmt.Errorf(`cannot parse user-defined type %q: %w`, userTypeName, err)
			}

			schema.userTypes[userTypeName] = validator
		}
	}

	schema.validator, err = schema.parseValidator(raw)
	if err != nil {
		return nil, err
	}

	return schema, nil
}

func (s *CustomSchema) parseValidator(raw json.RawMessage) (validator, error) {
	var typ string
	var level map[string]json.RawMessage
	if err := json.Unmarshal(raw, &level); err != nil {
		var typeErr *json.UnmarshalTypeError
		if !errors.As(err, &typeErr) {
			return nil, err
		}

		if err := json.Unmarshal(raw, &typ); err != nil {
			return nil, err
		}
	} else {
		rawType, ok := level["type"]
		if !ok {
			typ = "map"
		} else {
			if err := json.Unmarshal(rawType, &typ); err != nil {
				return nil, err
			}
		}
	}

	var val validator
	switch typ {
	case "map":
		mapVal := &mapValidator{
			schema: s,
		}

		if err := mapVal.parse(level); err != nil {
			return nil, err
		}
		val = mapVal
	case "int":
		return nil, nil
	case "string":
		strVal := &stringValidator{}
		if err := strVal.parse(raw); err != nil {
			return nil, err
		}
		val = strVal
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
		val = userType
	}

	return val, nil
}
