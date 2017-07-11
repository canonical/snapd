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
	return &Attrs{
		staticAttrs:  slot.Attrs,
		dynamicAttrs: dynamicAttrs,
	}, nil
}

func newPlugAttrs(plug *Plug, dynamicAttrs map[string]interface{}) (*Attrs, error) {
	return &Attrs{
		staticAttrs:  plug.Attrs,
		dynamicAttrs: dynamicAttrs,
	}, nil
}

func (attrs *Attrs) StaticAttr(key string) (interface{}, error) {
	if val, ok := attrs.staticAttrs[key]; ok {
		return val, nil
	}
	return nil, fmt.Errorf("attribute %q not found", key)
}

func (attrs *Attrs) SetStaticAttr(key string, value interface{}) {
	attrs.staticAttrs[key] = value
}

func (attrs *Attrs) StaticAttrs() map[string]interface{} {
	return attrs.staticAttrs
}

func (attrs *Attrs) Attr(key string) (interface{}, error) {
	if attrs.dynamicAttrs != nil {
		if val, ok := attrs.dynamicAttrs[key]; ok {
			return val, nil
		}
	}
	return attrs.StaticAttr(key)
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
