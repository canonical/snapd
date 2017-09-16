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

type Attributes interface {
	SetStaticAttr(key string, value interface{})
	StaticAttr(key string) (interface{}, error)
	StaticAttrs() map[string]interface{}
	Attr(key string) (interface{}, error)
	Attrs() (map[string]interface{}, error)
	SetAttr(key string, value interface{}) error
}

type plugSlotData struct {
	data         *snap.PlugSlotData
	dynamicAttrs map[string]interface{}
}

func (attrs *plugSlotData) Interface() string {
	return attrs.data.Interface
}

func (attrs *plugSlotData) Name() string {
	return attrs.data.Name
}

func (attrs *plugSlotData) Snap() *snap.Info {
	return attrs.data.Snap
}

func (attrs *plugSlotData) Apps() map[string]*snap.AppInfo {
	return attrs.data.Apps
}

func (attrs *plugSlotData) StaticAttr(key string) (interface{}, error) {
	if val, ok := attrs.data.Attrs[key]; ok {
		return val, nil
	}
	return nil, fmt.Errorf("attribute %q not found", key)
}

func (attrs *plugSlotData) SetStaticAttr(key string, value interface{}) {
	if attrs.data.Attrs == nil {
		attrs.data.Attrs = make(map[string]interface{})
	}
	attrs.data.Attrs[key] = value
}

func (attrs *plugSlotData) StaticAttrs() map[string]interface{} {
	return attrs.data.Attrs
}

func (attrs *plugSlotData) Attr(key string) (interface{}, error) {
	if attrs.dynamicAttrs != nil {
		if val, ok := attrs.dynamicAttrs[key]; ok {
			return val, nil
		}
	}
	return attrs.StaticAttr(key)
}

func (attrs *plugSlotData) Attrs() (map[string]interface{}, error) {
	if attrs.dynamicAttrs == nil {
		return nil, fmt.Errorf("dynamic attributes not initialized")
	}
	return attrs.dynamicAttrs, nil
}

func (attrs *plugSlotData) SetAttr(key string, value interface{}) error {
	if attrs.dynamicAttrs == nil {
		return fmt.Errorf("dynamic attributes not initialized")
	}
	if _, ok := attrs.data.Attrs[key]; ok {
		return fmt.Errorf("attribute %q cannot be overwritten", key)
	}
	attrs.dynamicAttrs[key] = value
	return nil
}

type PlugData struct {
	plug *snap.PlugInfo
	plugSlotData
}

func NewPlugData(plug *snap.PlugInfo, dynamicAttrs map[string]interface{}) *PlugData {
	return &PlugData{
		plug: plug,
		plugSlotData: plugSlotData{
			data:         &plug.PlugSlotData,
			dynamicAttrs: dynamicAttrs,
		},
	}
}

func (attrs *PlugData) Ref() PlugRef {
	return PlugRef{Snap: attrs.data.Snap.Name(), Name: attrs.data.Name}
}

func (attrs *PlugData) SecurityTags() []string {
	return attrs.plug.SecurityTags()
}

type SlotData struct {
	slot *snap.SlotInfo
	plugSlotData
}

func NewSlotData(slot *snap.SlotInfo, dynamicAttrs map[string]interface{}) *SlotData {
	return &SlotData{
		slot: slot,
		plugSlotData: plugSlotData{
			data:         &slot.PlugSlotData,
			dynamicAttrs: dynamicAttrs,
		},
	}
}

func (attrs *SlotData) Ref() SlotRef {
	return SlotRef{Snap: attrs.slot.Snap.Name(), Name: attrs.slot.Name}
}

func (attrs *SlotData) SecurityTags() []string {
	return attrs.slot.SecurityTags()
}
