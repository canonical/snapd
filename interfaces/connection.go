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
	"reflect"

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
	// FIXME temporary
	Attrs map[string]interface{}
}

// ConnectedSlot represents a slot that is connected to a plug.
type ConnectedSlot struct {
	slotInfo     *snap.SlotInfo
	staticAttrs  map[string]interface{}
	dynamicAttrs map[string]interface{}
	// FIXME temporary
	Attrs map[string]interface{}
}

// Attrer is an interface with Attr getter method common
// to ConnectedSlot, ConnectedPlug, PlugInfo and SlotInfo types.
type Attrer interface {
	Attr(key string, val interface{}) error
}

func getAttribute(snapName string, ifaceName string, staticAttrs map[string]interface{}, dynamicAttrs map[string]interface{}, key string, val interface{}) error {
	var v interface{}
	var ok bool

	v, ok = dynamicAttrs[key]
	if !ok {
		v, ok = staticAttrs[key]
	}

	if !ok {
		return fmt.Errorf("snap %q does not have attribute %q for interface %q", snapName, key, ifaceName)
	}

	rt := reflect.TypeOf(val)
	if rt.Kind() != reflect.Ptr || val == nil {
		return fmt.Errorf("internal error: cannot get %q attribute of interface %q with non-pointer value", key, ifaceName)
	}

	if reflect.TypeOf(v) != rt.Elem() {
		return fmt.Errorf("snap %q has interface %q with invalid value type for %q attribute", snapName, ifaceName, key)
	}
	rv := reflect.ValueOf(val)
	rv.Elem().Set(reflect.ValueOf(v))
	return nil
}

// NewConnectedSlot creates an object representing a connected slot.
func NewConnectedSlot(slot *snap.SlotInfo, dynamicAttrs map[string]interface{}) *ConnectedSlot {
	return &ConnectedSlot{
		slotInfo:     slot,
		staticAttrs:  copyAttributes(slot.Attrs),
		dynamicAttrs: normalize(dynamicAttrs).(map[string]interface{}),
		Attrs:        slot.Attrs, // FIXME: temporary
	}
}

// NewConnectedPlug creates an object representing a connected plug.
func NewConnectedPlug(plug *snap.PlugInfo, dynamicAttrs map[string]interface{}) *ConnectedPlug {
	return &ConnectedPlug{
		plugInfo:     plug,
		staticAttrs:  copyAttributes(plug.Attrs),
		dynamicAttrs: normalize(dynamicAttrs).(map[string]interface{}),
		Attrs:        plug.Attrs, // FIXME: temporary
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
func (plug *ConnectedPlug) StaticAttr(key string, val interface{}) error {
	return getAttribute(plug.Snap().Name(), plug.Interface(), plug.staticAttrs, nil, key, val)
}

// StaticAttrs returns all static attributes.
func (plug *ConnectedPlug) StaticAttrs() map[string]interface{} {
	return plug.staticAttrs
}

// Attr returns a dynamic attribute with the given name. It falls back to returning static
// attribute if dynamic one doesn't exist. Error is returned if neither dynamic nor static
// attribute exist.
func (plug *ConnectedPlug) Attr(key string, val interface{}) error {
	return getAttribute(plug.Snap().Name(), plug.Interface(), plug.staticAttrs, plug.dynamicAttrs, key, val)
}

// SetAttr sets the given dynamic attribute. Error is returned if the key is already used by a static attribute.
func (plug *ConnectedPlug) SetAttr(key string, value interface{}) error {
	if _, ok := plug.staticAttrs[key]; ok {
		return fmt.Errorf("cannot change attribute %q as it was statically specified in the snap details", key)
	}
	if plug.dynamicAttrs == nil {
		plug.dynamicAttrs = make(map[string]interface{})
	}
	plug.dynamicAttrs[key] = normalize(value)
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
func (slot *ConnectedSlot) StaticAttr(key string, val interface{}) error {
	return getAttribute(slot.Snap().Name(), slot.Interface(), slot.staticAttrs, nil, key, val)
}

// StaticAttrs returns all static attributes.
func (slot *ConnectedSlot) StaticAttrs() map[string]interface{} {
	return slot.staticAttrs
}

// Attr returns a dynamic attribute with the given name. It falls back to returning static
// attribute if dynamic one doesn't exist. Error is returned if neither dynamic nor static
// attribute exist.
func (slot *ConnectedSlot) Attr(key string, val interface{}) error {
	return getAttribute(slot.Snap().Name(), slot.Interface(), slot.staticAttrs, slot.dynamicAttrs, key, val)
}

// SetAttr sets the given dynamic attribute. Error is returned if the key is already used by a static attribute.
func (slot *ConnectedSlot) SetAttr(key string, value interface{}) error {
	if _, ok := slot.staticAttrs[key]; ok {
		return fmt.Errorf("cannot change attribute %q as it was statically specified in the snap details", key)
	}
	if slot.dynamicAttrs == nil {
		slot.dynamicAttrs = make(map[string]interface{})
	}
	slot.dynamicAttrs[key] = normalize(value)
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
	}
	return value
}

func normalize(value interface{}) interface{} {
	// Normalize ints/floats using their 64-bit variants.
	// That kind of normalization happens in normalizeYamlValue(..) for static attributes
	// when the yaml is loaded, but it needs to be done here as well because we're also
	// dealing with dynamic attributes set by the code of interfaces.
	switch v := value.(type) {
	case int:
		return int64(v)
	case float32:
		return float64(v)
	case []interface{}:
		for i, el := range v {
			v[i] = normalize(el)
		}
	case map[string]interface{}:
		for key, item := range v {
			v[key] = normalize(item)
		}
	}
	return value
}
