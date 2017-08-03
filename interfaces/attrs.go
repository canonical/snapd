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

	"github.com/snapcore/snapd/snap"
)

type PlugData struct {
	plug         *Plug
	dynamicAttrs map[string]interface{}
}

type SlotData struct {
	slot         *Slot
	dynamicAttrs map[string]interface{}
}

func newSlotData(slot *Slot, dynamicAttrs map[string]interface{}) *SlotData {
	return &SlotData{
		slot:         slot,
		dynamicAttrs: dynamicAttrs,
	}
}

func newPlugData(plug *Plug, dynamicAttrs map[string]interface{}) *PlugData {
	return &PlugData{
		plug:         plug,
		dynamicAttrs: dynamicAttrs,
	}
}

func (attrs *PlugData) Interface() string {
	return attrs.plug.Interface
}

func (attrs *PlugData) Name() string {
	return attrs.plug.Name
}

func (attrs *PlugData) Snap() *snap.Info {
	return attrs.plug.Snap
}

func (attrs *PlugData) Apps() map[string]*snap.AppInfo {
	return attrs.plug.Apps
}

func (attrs *PlugData) StaticAttr(key string) (interface{}, error) {
	if val, ok := attrs.plug.Attrs[key]; ok {
		return val, nil
	}
	return nil, fmt.Errorf("attribute %q not found", key)
}

func (attrs *PlugData) SetStaticAttr(key string, value interface{}) {
	attrs.plug.Attrs[key] = value
}

func (attrs *PlugData) StaticAttrs() map[string]interface{} {
	return attrs.plug.Attrs
}

func (attrs *PlugData) Attr(key string) (interface{}, error) {
	if attrs.dynamicAttrs != nil {
		if val, ok := attrs.dynamicAttrs[key]; ok {
			return val, nil
		}
	}
	return attrs.StaticAttr(key)
}

func (attrs *PlugData) Attrs() (map[string]interface{}, error) {
	if attrs.dynamicAttrs == nil {
		return nil, fmt.Errorf("dynamic attributes not initialized")
	}
	return attrs.dynamicAttrs, nil
}

func (attrs *PlugData) SetAttr(key string, value interface{}) error {
	if attrs.dynamicAttrs == nil {
		return fmt.Errorf("dynamic attributes not initialized")
	}
	attrs.dynamicAttrs[key] = value
	return nil
}

func (attrs *SlotData) Interface() string {
	return attrs.slot.Interface
}

func (attrs *SlotData) Name() string {
	return attrs.slot.Name
}

func (attrs *SlotData) Snap() *snap.Info {
	return attrs.slot.Snap
}

func (attrs *SlotData) StaticAttr(key string) (interface{}, error) {
	if val, ok := attrs.slot.Attrs[key]; ok {
		return val, nil
	}
	return nil, fmt.Errorf("attribute %q not found", key)
}

func (attrs *SlotData) SetStaticAttr(key string, value interface{}) {
	attrs.slot.Attrs[key] = value
}

func (attrs *SlotData) StaticAttrs() map[string]interface{} {
	return attrs.slot.Attrs
}

func (attrs *SlotData) Attr(key string) (interface{}, error) {
	if attrs.dynamicAttrs != nil {
		if val, ok := attrs.dynamicAttrs[key]; ok {
			return val, nil
		}
	}
	return attrs.StaticAttr(key)
}

func (attrs *SlotData) Attrs() (map[string]interface{}, error) {
	if attrs.dynamicAttrs == nil {
		return nil, fmt.Errorf("dynamic attributes not initialized")
	}
	return attrs.dynamicAttrs, nil
}

func (attrs *SlotData) SetAttr(key string, value interface{}) error {
	if attrs.dynamicAttrs == nil {
		return fmt.Errorf("dynamic attributes not initialized")
	}
	attrs.dynamicAttrs[key] = value
	return nil
}
