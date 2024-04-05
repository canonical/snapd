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
	"strings"

	"github.com/snapcore/snapd/interfaces/utils"
	"github.com/snapcore/snapd/metautil"
	"github.com/snapcore/snapd/snap"
)

// Connection represents a connection between a particular plug and slot.
type Connection struct {
	Plug *ConnectedPlug
	Slot *ConnectedSlot
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

// Attrer is an interface with Attr getter method common
// to ConnectedSlot, ConnectedPlug, PlugInfo and SlotInfo types.
type Attrer interface {
	// Attr returns attribute value for given path, or an error. Dotted paths are supported.
	Attr(path string, value interface{}) error
	// Lookup returns attribute value for given path, or false. Dotted paths are supported.
	Lookup(path string) (value interface{}, ok bool)
}

func lookupAttr(staticAttrs map[string]interface{}, dynamicAttrs map[string]interface{}, path string) (interface{}, bool) {
	var v interface{}
	comps := strings.FieldsFunc(path, func(r rune) bool { return r == '.' })
	if len(comps) == 0 {
		return nil, false
	}
	if _, ok := dynamicAttrs[comps[0]]; ok {
		v = dynamicAttrs
	} else {
		v = staticAttrs
	}

	for _, comp := range comps {
		m, ok := v.(map[string]interface{})
		if !ok {
			return nil, false
		}
		v, ok = m[comp]
		if !ok {
			return nil, false
		}
	}

	return v, true
}

func getAttribute(snapName string, ifaceName string, staticAttrs map[string]interface{}, dynamicAttrs map[string]interface{}, path string, val interface{}) error {
	v, ok := lookupAttr(staticAttrs, dynamicAttrs, path)
	if !ok {
		err := fmt.Errorf("snap %q does not have attribute %q for interface %q", snapName, path, ifaceName)
		return snap.AttributeNotFoundError{Err: err}
	}

	return metautil.SetValueFromAttribute(snapName, ifaceName, path, v, val)
}

// NewConnectedSlot creates an object representing a connected slot.
func NewConnectedSlot(slot *snap.SlotInfo, staticAttrs, dynamicAttrs map[string]interface{}) *ConnectedSlot {
	var static map[string]interface{}
	if staticAttrs != nil {
		static = staticAttrs
	} else {
		static = slot.Attrs
	}
	return &ConnectedSlot{
		slotInfo:     slot,
		staticAttrs:  utils.CopyAttributes(static),
		dynamicAttrs: utils.NormalizeInterfaceAttributes(dynamicAttrs).(map[string]interface{}),
	}
}

// NewConnectedPlug creates an object representing a connected plug.
func NewConnectedPlug(plug *snap.PlugInfo, staticAttrs, dynamicAttrs map[string]interface{}) *ConnectedPlug {
	var static map[string]interface{}
	if staticAttrs != nil {
		static = staticAttrs
	} else {
		static = plug.Attrs
	}
	return &ConnectedPlug{
		plugInfo:     plug,
		staticAttrs:  utils.CopyAttributes(static),
		dynamicAttrs: utils.NormalizeInterfaceAttributes(dynamicAttrs).(map[string]interface{}),
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

// StaticAttr returns a static attribute with the given key, or error if attribute doesn't exist.
func (plug *ConnectedPlug) StaticAttr(key string, val interface{}) error {
	return getAttribute(plug.Snap().InstanceName(), plug.Interface(), plug.staticAttrs, nil, key, val)
}

// StaticAttrs returns all static attributes.
func (plug *ConnectedPlug) StaticAttrs() map[string]interface{} {
	return utils.CopyAttributes(plug.staticAttrs)
}

// DynamicAttrs returns all dynamic attributes.
func (plug *ConnectedPlug) DynamicAttrs() map[string]interface{} {
	return utils.CopyAttributes(plug.dynamicAttrs)
}

// Attr returns a dynamic attribute with the given name. It falls back to returning static
// attribute if dynamic one doesn't exist. Error is returned if neither dynamic nor static
// attribute exist.
func (plug *ConnectedPlug) Attr(key string, val interface{}) error {
	return getAttribute(plug.Snap().InstanceName(), plug.Interface(), plug.staticAttrs, plug.dynamicAttrs, key, val)
}

func (plug *ConnectedPlug) Lookup(path string) (interface{}, bool) {
	return lookupAttr(plug.staticAttrs, plug.dynamicAttrs, path)
}

// SetAttr sets the given dynamic attribute. Error is returned if the key is already used by a static attribute.
func (plug *ConnectedPlug) SetAttr(key string, value interface{}) error {
	if _, ok := plug.staticAttrs[key]; ok {
		return fmt.Errorf("cannot change attribute %q as it was statically specified in the snap details", key)
	}
	if plug.dynamicAttrs == nil {
		plug.dynamicAttrs = make(map[string]interface{})
	}
	plug.dynamicAttrs[key] = utils.NormalizeInterfaceAttributes(value)
	return nil
}

// Ref returns the PlugRef for this plug.
func (plug *ConnectedPlug) Ref() *PlugRef {
	return &PlugRef{Snap: plug.Snap().InstanceName(), Name: plug.Name()}
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

// StaticAttr returns a static attribute with the given key, or error if attribute doesn't exist.
func (slot *ConnectedSlot) StaticAttr(key string, val interface{}) error {
	return getAttribute(slot.Snap().InstanceName(), slot.Interface(), slot.staticAttrs, nil, key, val)
}

// StaticAttrs returns all static attributes.
func (slot *ConnectedSlot) StaticAttrs() map[string]interface{} {
	return utils.CopyAttributes(slot.staticAttrs)
}

// DynamicAttrs returns all dynamic attributes.
func (slot *ConnectedSlot) DynamicAttrs() map[string]interface{} {
	return utils.CopyAttributes(slot.dynamicAttrs)
}

// Attr returns a dynamic attribute with the given name. It falls back to returning static
// attribute if dynamic one doesn't exist. Error is returned if neither dynamic nor static
// attribute exist.
func (slot *ConnectedSlot) Attr(key string, val interface{}) error {
	return getAttribute(slot.Snap().InstanceName(), slot.Interface(), slot.staticAttrs, slot.dynamicAttrs, key, val)
}

func (slot *ConnectedSlot) Lookup(path string) (interface{}, bool) {
	return lookupAttr(slot.staticAttrs, slot.dynamicAttrs, path)
}

// SetAttr sets the given dynamic attribute. Error is returned if the key is already used by a static attribute.
func (slot *ConnectedSlot) SetAttr(key string, value interface{}) error {
	if _, ok := slot.staticAttrs[key]; ok {
		return fmt.Errorf("cannot change attribute %q as it was statically specified in the snap details", key)
	}
	if slot.dynamicAttrs == nil {
		slot.dynamicAttrs = make(map[string]interface{})
	}
	slot.dynamicAttrs[key] = utils.NormalizeInterfaceAttributes(value)
	return nil
}

// Ref returns the SlotRef for this slot.
func (slot *ConnectedSlot) Ref() *SlotRef {
	return &SlotRef{Snap: slot.Snap().InstanceName(), Name: slot.Name()}
}

// Interface returns the name of the interface for this connection.
func (conn *Connection) Interface() string {
	return conn.Plug.plugInfo.Interface
}
