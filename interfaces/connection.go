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

// Connection represents a connection between a particular plug and slot.
type Connection struct {
	plug *ConnectedPlug
	slot *ConnectedSlot
}

// ConnectedPlug represents a plug that is connected to a slot.
type ConnectedPlug struct {
	plugInfo     *snap.PlugInfo
	staticAttrs  map[string]interface{}
	dynamicAttrs map[string]interface{}
}

// ConnectedSlot represents a slot that is connected to a plug.
type ConnectedSlot struct {
	slotInfo     *snap.SlotInfo
	staticAttrs  map[string]interface{}
	dynamicAttrs map[string]interface{}
}

// NewConnectedSlot creates an object representing a connected slot.
func NewConnectedSlot(slot *snap.SlotInfo, dynamicAttrs map[string]interface{}) *ConnectedSlot {
	return &ConnectedSlot{
		slotInfo:     slot,
		staticAttrs:  copyAttributes(slot.Attrs),
		dynamicAttrs: dynamicAttrs,
	}
}

// NewConnectedPlug creates an object representing a connected plug.
func NewConnectedPlug(plug *snap.PlugInfo, dynamicAttrs map[string]interface{}) *ConnectedPlug {
	return &ConnectedPlug{
		plugInfo:     plug,
		staticAttrs:  copyAttributes(plug.Attrs),
		dynamicAttrs: dynamicAttrs,
	}
}

// Interface returns the name of the interface for this plug.
func (plug *ConnectedPlug) Interface() string {
	return plug.plugInfo.Interface
}

// Name returns the name of this plug.
func (plug *ConnectedPlug) Name() string {
	return plug.plugInfo.Name
}

// Snap returns the snap Info of this plug.
func (plug *ConnectedPlug) Snap() *snap.Info {
	return plug.plugInfo.Snap
}

// Apps returns all the apps associated with this plug.
func (plug *ConnectedPlug) Apps() map[string]*snap.AppInfo {
	return plug.plugInfo.Apps
}

// Hooks returns all the hooks associated with this plug.
func (plug *ConnectedPlug) Hooks() map[string]*snap.HookInfo {
	return plug.plugInfo.Hooks
}

// SecurityTags returns the security tags for this plug.
func (plug *ConnectedPlug) SecurityTags() []string {
	return plug.plugInfo.SecurityTags()
}

// StaticAttr returns a static attribute with the given key, or error if attribute doesn't exist.
func (plug *ConnectedPlug) StaticAttr(key string) (interface{}, error) {
	if val, ok := plug.staticAttrs[key]; ok {
		return val, nil
	}
	return nil, fmt.Errorf("attribute %q not found", key)
}

// StaticAttrs returns all static attributes.
func (plug *ConnectedPlug) StaticAttrs() map[string]interface{} {
	return plug.staticAttrs
}

// Attr returns a dynamic attribute with the given name. It falls back to returning static
// attribute if dynamic one doesn't exist. Error is returned if neither dynamic nor static
// attribute exist.
func (plug *ConnectedPlug) Attr(key string) (interface{}, error) {
	if plug.dynamicAttrs != nil {
		if val, ok := plug.dynamicAttrs[key]; ok {
			return val, nil
		}
	}
	return plug.StaticAttr(key)
}

// SetAttr sets the given dynamic attribute. Error is returned if the key is already used by a static attribute.
func (plug *ConnectedPlug) SetAttr(key string, value interface{}) error {
	if _, ok := plug.staticAttrs[key]; ok {
		return fmt.Errorf("cannot change attribute %q as it was statically specified in the snap details", key)
	}
	if plug.dynamicAttrs == nil {
		plug.dynamicAttrs = make(map[string]interface{})
	}
	plug.dynamicAttrs[key] = value
	return nil
}

// Interface returns the name of the interface for this slot.
func (slot *ConnectedSlot) Interface() string {
	return slot.slotInfo.Interface
}

// Name returns the name of this slot.
func (slot *ConnectedSlot) Name() string {
	return slot.slotInfo.Name
}

// Snap returns the snap Info of this slot.
func (slot *ConnectedSlot) Snap() *snap.Info {
	return slot.slotInfo.Snap
}

// Apps returns all the apps associated with this slot.
func (slot *ConnectedSlot) Apps() map[string]*snap.AppInfo {
	return slot.slotInfo.Apps
}

// Hooks returns all the hooks associated with this slot.
func (slot *ConnectedSlot) Hooks() map[string]*snap.HookInfo {
	return slot.slotInfo.Hooks
}

// SecurityTags returns the security tags for this slot.
func (slot *ConnectedSlot) SecurityTags() []string {
	return slot.slotInfo.SecurityTags()
}

// StaticAttr returns a static attribute with the given key, or error if attribute doesn't exist.
func (slot *ConnectedSlot) StaticAttr(key string) (interface{}, error) {
	if val, ok := slot.staticAttrs[key]; ok {
		return val, nil
	}
	return nil, fmt.Errorf("attribute %q not found", key)
}

// StaticAttrs returns all static attributes.
func (slot *ConnectedSlot) StaticAttrs() map[string]interface{} {
	return slot.staticAttrs
}

// Attr returns a dynamic attribute with the given name. It falls back to returning static
// attribute if dynamic one doesn't exist. Error is returned if neither dynamic nor static
// attribute exist.
func (slot *ConnectedSlot) Attr(key string) (interface{}, error) {
	if slot.dynamicAttrs != nil {
		if val, ok := slot.dynamicAttrs[key]; ok {
			return val, nil
		}
	}
	return slot.StaticAttr(key)
}

// SetAttr sets the given dynamic attribute. Error is returned if the key is already used by a static attribute.
func (slot *ConnectedSlot) SetAttr(key string, value interface{}) error {
	if _, ok := slot.staticAttrs[key]; ok {
		return fmt.Errorf("cannot change attribute %q as it was statically specified in the snap details", key)
	}
	if slot.dynamicAttrs == nil {
		slot.dynamicAttrs = make(map[string]interface{})
	}
	slot.dynamicAttrs[key] = value
	return nil
}

// Interface returns the name of the interface for this connection.
func (conn *Connection) Interface() string {
	return conn.plug.plugInfo.Interface
}

func copyAttributes(value map[string]interface{}) map[string]interface{} {
	return copyRecursive(value).(map[string]interface{})
}

func copyRecursive(value interface{}) interface{} {
	// note: ensure all the mutable types (or types that need a conversion)
	// are handled here.
	switch v := value.(type) {
	case []interface{}:
		arr := make([]interface{}, len(v))
		for i, el := range v {
			arr[i] = copyRecursive(el)
		}
		return arr
	case map[string]interface{}:
		mp := make(map[string]interface{}, len(v))
		for key, item := range v {
			mp[key] = copyRecursive(item)
		}
		return mp
	default:
		return v
	}
}
