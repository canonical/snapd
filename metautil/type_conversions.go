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

package metautil

import (
	"fmt"
	"reflect"

	"github.com/ddkwork/golibrary/mylog"
)

func convertValue(value reflect.Value, outputType reflect.Type) (reflect.Value, error) {
	inputType := value.Type()
	if inputType == outputType {
		return value, nil
	}

	var nullValue reflect.Value
	switch value.Kind() {
	case reflect.Slice, reflect.Array:
		if outputType.Kind() != reflect.Array && outputType.Kind() != reflect.Slice {
			break
		}
		outputValue := reflect.MakeSlice(outputType, 0, value.Len())
		for i := 0; i < value.Len(); i++ {
			convertedElem := mylog.Check2(convertValue(value.Index(i), outputType.Elem()))

			outputValue = reflect.Append(outputValue, convertedElem)
		}
		return outputValue, nil
	case reflect.Interface:
		return convertValue(value.Elem(), outputType)
	case reflect.Map:
		if outputType.Kind() != reflect.Map {
			break
		}
		outputValue := reflect.MakeMapWithSize(outputType, value.Len())
		iter := value.MapRange()
		for iter.Next() {
			convertedKey := mylog.Check2(convertValue(iter.Key(), outputType.Key()))

			convertedValue := mylog.Check2(convertValue(iter.Value(), outputType.Elem()))

			outputValue.SetMapIndex(convertedKey, convertedValue)
		}
		return outputValue, nil
	}
	return nullValue, fmt.Errorf(`cannot convert value "%v" into a %v`, value, outputType)
}

// AttributeNotCompatibleError represents a type mismatch error between an interface
// attribute and an expected type.
type AttributeNotCompatibleError struct {
	SnapName      string
	InterfaceName string
	AttributeName string
	AttributeType reflect.Type
	ExpectedType  reflect.Type
}

func (e AttributeNotCompatibleError) Error() string {
	return fmt.Sprintf("snap %q has interface %q with invalid value type %s for %q attribute: %s", e.SnapName, e.InterfaceName, e.AttributeType, e.AttributeName, e.ExpectedType)
}

func (e AttributeNotCompatibleError) Is(target error) bool {
	_, ok := target.(AttributeNotCompatibleError)
	return ok
}

// SetValueFromAttribute attempts to convert the attribute value read from the
// given snap/interface into the desired type.
//
// The snapName, ifaceName and attrName are only used to produce contextual
// error messages, but are not otherwise significant. This function only
// operates converting the attrVal parameter into a value which can fit into
// the val parameter, which therefore must be a pointer.
func SetValueFromAttribute(snapName string, ifaceName string, attrName string, attrVal interface{}, val interface{}) error {
	rt := reflect.TypeOf(val)
	if rt.Kind() != reflect.Ptr || val == nil {
		return fmt.Errorf("internal error: cannot get %q attribute of interface %q with non-pointer value", attrName, ifaceName)
	}

	converted := mylog.Check2(convertValue(reflect.ValueOf(attrVal), rt.Elem()))

	rv := reflect.ValueOf(val)
	rv.Elem().Set(converted)
	return nil
}
