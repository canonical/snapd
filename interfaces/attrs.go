// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package interfaces

import (
	"fmt"
)

type Attrs struct {
	staticAttrs  map[string]interface{}
	dynamicAttrs map[string]interface{}
}

func newSlotAttrs(slot *Slot, dynamicAttrs map[string]interface{}) (*Attrs, error) {
	attrs, err := copyAttributes(slot.Attrs)
	if err != nil {
		return nil, err
	}
	return &Attrs{
		staticAttrs:  attrs,
		dynamicAttrs: dynamicAttrs,
	}, nil
}

func newPlugAttrs(plug *Plug, dynamicAttrs map[string]interface{}) (*Attrs, error) {
	attrs, err := copyAttributes(plug.Attrs)
	if err != nil {
		return nil, err
	}
	return &Attrs{
		staticAttrs:  attrs,
		dynamicAttrs: dynamicAttrs,
	}, nil
}

func (attrs *Attrs) StaticAttr(key string) (interface{}, error) {
	if val, ok := attrs.staticAttrs[key]; ok {
		return val, nil
	}
	return nil, fmt.Errorf("attribute %q not found", key)
}

func (attrs *Attrs) StaticAttrs() map[string]interface{} {
	return attrs.staticAttrs
}

func (attrs *Attrs) Attr(key string) (interface{}, error) {
	if attrs.dynamicAttrs == nil {
		return nil, fmt.Errorf("dynamic attributes not initialized")
	}

	if val, ok := attrs.dynamicAttrs[key]; ok {
		return val, nil
	}
	return nil, fmt.Errorf("attribute %q not found", key)
}

func (attrs *Attrs) Attrs() (map[string]interface{}, error) {
	if attrs.dynamicAttrs == nil {
		return nil, fmt.Errorf("dynamic attributes not initialized")
	}
	return attrs.dynamicAttrs, nil
}

func (attrs *Attrs) SetAttr(key string, value interface{}) error {
	if attrs.dynamicAttrs == nil {
		return fmt.Errorf("dynamic attributes not initialized")
	}
	attrs.dynamicAttrs[key] = value
	return nil
}

func copyAttributes(value map[string]interface{}) (map[string]interface{}, error) {
	cpy, err := copyRecursive(value)
	if err != nil {
		return nil, err
	}
	return cpy.(map[string]interface{}), err
}

func copyRecursive(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case bool:
		return v, nil
	case int:
		return int64(v), nil
	case int64:
		return v, nil
	case []interface{}:
		arr := make([]interface{}, len(v))
		for i, el := range v {
			tmp, err := copyRecursive(el)
			if err != nil {
				return nil, err
			}
			arr[i] = tmp
		}
		return arr, nil
	case map[string]interface{}:
		mp := make(map[string]interface{}, len(v))
		for key, item := range v {
			tmp, err := copyRecursive(item)
			if err != nil {
				return nil, err
			}
			mp[key] = tmp
		}
		return mp, nil
	default:
		return nil, fmt.Errorf("unsupported attribute type '%T', value '%v'", value, value)
	}
}
