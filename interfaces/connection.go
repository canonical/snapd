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

type Connection struct {
	plug *ConnectedPlug
	slot *ConnectedSlot
}

type ConnectedPlug struct {
	plugInfo     *snap.PlugInfo
	dynamicAttrs map[string]interface{}
}

type ConnectedSlot struct {
	slotInfo     *snap.SlotInfo
	dynamicAttrs map[string]interface{}
}

func NewConnectedSlot(slot *snap.SlotInfo, dynamicAttrs map[string]interface{}) (*ConnectedSlot, error) {
	return &ConnectedSlot{
		slotInfo:     slot,
		dynamicAttrs: dynamicAttrs,
	}, nil
}

func NewConnectedPlug(plug *snap.PlugInfo, dynamicAttrs map[string]interface{}) (*ConnectedPlug, error) {
	return &ConnectedPlug{
		plugInfo:     plug,
		dynamicAttrs: dynamicAttrs,
	}, nil
}

func (plug *ConnectedPlug) Interface() string {
	return plug.plugInfo.Interface
}

func (plug *ConnectedPlug) Name() string {
	return plug.plugInfo.Name
}

func (plug *ConnectedPlug) Snap() *snap.Info {
	return plug.plugInfo.Snap
}

func (plug *ConnectedPlug) Apps() map[string]*snap.AppInfo {
	return plug.plugInfo.Apps
}

func (plug *ConnectedPlug) Hooks() map[string]*snap.HookInfo {
	return plug.plugInfo.Hooks
}

func (plug *ConnectedPlug) SecurityTags() []string {
	return plug.plugInfo.SecurityTags()
}

func (plug *ConnectedPlug) StaticAttr(key string) (interface{}, error) {
	if val, ok := plug.plugInfo.Attrs[key]; ok {
		return copyRecursive(val)
	}
	return nil, fmt.Errorf("attribute %q not found", key)
}

func (plug *ConnectedPlug) StaticAttrs() (map[string]interface{}, error) {
	return copyAttributes(plug.plugInfo.Attrs)
}

func (plug *ConnectedPlug) Attr(key string) (interface{}, error) {
	if plug.dynamicAttrs != nil {
		if val, ok := plug.dynamicAttrs[key]; ok {
			return val, nil
		}
	}
	return plug.StaticAttr(key)
}

func (plug *ConnectedPlug) Attrs() (map[string]interface{}, error) {
	if plug.dynamicAttrs != nil {
		return plug.dynamicAttrs, nil
	}
	return plug.StaticAttrs()
}

func (plug *ConnectedPlug) SetAttr(key string, value interface{}) error {
	if plug.dynamicAttrs == nil {
		plug.dynamicAttrs = make(map[string]interface{})
	}
	if _, ok := plug.plugInfo.Attrs[key]; ok {
		return fmt.Errorf("attribute %q cannot be overwritten", key)
	}
	plug.dynamicAttrs[key] = value
	return nil
}

func (slot *ConnectedSlot) Interface() string {
	return slot.slotInfo.Interface
}

func (slot *ConnectedSlot) Name() string {
	return slot.slotInfo.Name
}

func (slot *ConnectedSlot) Snap() *snap.Info {
	return slot.slotInfo.Snap
}

func (slot *ConnectedSlot) Apps() map[string]*snap.AppInfo {
	return slot.slotInfo.Apps
}

func (slot *ConnectedSlot) SecurityTags() []string {
	return slot.slotInfo.SecurityTags()
}

func (slot *ConnectedSlot) StaticAttr(key string) (interface{}, error) {
	if val, ok := slot.slotInfo.Attrs[key]; ok {
		return copyRecursive(val)
	}
	return nil, fmt.Errorf("attribute %q not found", key)
}

func (slot *ConnectedSlot) StaticAttrs() (map[string]interface{}, error) {
	return copyAttributes(slot.slotInfo.Attrs)
}

func (slot *ConnectedSlot) Attr(key string) (interface{}, error) {
	if slot.dynamicAttrs != nil {
		if val, ok := slot.dynamicAttrs[key]; ok {
			return val, nil
		}
	}
	return slot.StaticAttr(key)
}

func (slot *ConnectedSlot) Attrs() (map[string]interface{}, error) {
	if slot.dynamicAttrs != nil {
		return slot.dynamicAttrs, nil
	}
	return slot.StaticAttrs()
}

func (slot *ConnectedSlot) SetAttr(key string, value interface{}) error {
	if slot.dynamicAttrs == nil {
		slot.dynamicAttrs = make(map[string]interface{})
	}
	if _, ok := slot.slotInfo.Attrs[key]; ok {
		return fmt.Errorf("attribute %q cannot be overwritten", key)
	}
	slot.dynamicAttrs[key] = value
	return nil
}

func (conn *Connection) Interface() string {
	return conn.plug.plugInfo.Interface
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
		return nil, fmt.Errorf("unsupported attribute type %T, value %q", value, value)
	}
}
