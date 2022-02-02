// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package builtin

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/snap"
)

const sharedMemorySummary = `allows two snaps to use predefined shared memory objects`

// The plug side of shared-memory implements auto-connect to a matching slot -
// this is permitted even though the interface is super-privileged because using
// a slot requires a store declaration anyways so just declaring a plug will not
// grant access unless a slot was also granted at some point.
const sharedMemoryBaseDeclarationPlugs = `
  shared-memory:
    allow-installation: true
    allow-connection:
      slot-attributes:
        shared-memory: $PLUG(shared-memory)
    allow-auto-connection:
      slot-publisher-id:
        - $PLUG_PUBLISHER_ID
      slot-attributes:
        shared-memory: $PLUG(shared-memory)
`

// shared-memory slots are super-privileged and thus denied to any snap except
// those that get a store declaration to do so, but the intent is for
// application or gadget snaps to use the slot much like the content interface.
const sharedMemoryBaseDeclarationSlots = `
  shared-memory:
    allow-installation: false
    deny-connection: true
    deny-auto-connection: true
`

func validateSharedMemoryPath(path string) error {
	if len(path) == 0 {
		return fmt.Errorf("shared-memory interface path is empty")
	}

	if strings.TrimSpace(path) != path {
		return fmt.Errorf("shared-memory interface path has leading or trailing spaces: %q", path)
	}

	// allow specifically only "*" globbing character, but disallow all other
	// AARE characters

	// same as from ValidateNoAppArmorRegexp, but with globbing
	const aareWithoutGlob = `?[]{}^"` + "\x00"
	if strings.ContainsAny(path, aareWithoutGlob) {
		return fmt.Errorf("shared-memory interface path is invalid: %q contains a reserved apparmor char from %s", path, aareWithoutGlob)
	}

	// in addition to only allowing "*", we don't want to allow double "**"
	// because "**" can traverse sub-directories as well which we don't want
	if strings.Contains(path, "**") {
		return fmt.Errorf("shared-memory interface path is invalid: %q contains ** which is unsupported", path)
	}

	// TODO: consider whether we should remove this check and allow full SHM path
	if strings.Contains(path, "/") {
		return fmt.Errorf("shared-memory interface path should not contain '/': %q", path)
	}

	// The check above protects from most unclean paths, but one could still specify ".."
	if !cleanSubPath(path) {
		return fmt.Errorf("shared-memory interface path is not clean: %q", path)
	}

	return nil
}

func stringListAttribute(attrer interfaces.Attrer, key string) ([]string, error) {
	var stringList []string
	err := attrer.Attr(key, &stringList)
	if err != nil && !errors.Is(err, snap.AttributeNotFoundError{}) {
		value, _ := attrer.Lookup(key)
		return nil, fmt.Errorf(`shared-memory %q attribute must be a list of strings, not "%v"`, key, value)
	}

	return stringList, nil
}

// sharedMemoryInterface allows sharing sharedMemory between snaps
type sharedMemoryInterface struct{}

func (iface *sharedMemoryInterface) Name() string {
	return "shared-memory"
}

func (iface *sharedMemoryInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              sharedMemorySummary,
		BaseDeclarationPlugs: sharedMemoryBaseDeclarationPlugs,
		BaseDeclarationSlots: sharedMemoryBaseDeclarationSlots,
		AffectsPlugOnRefresh: true,
	}
}

func (iface *sharedMemoryInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	sharedMemoryAttr, isSet := slot.Attrs["shared-memory"]
	sharedMemory, ok := sharedMemoryAttr.(string)
	if isSet && !ok {
		return fmt.Errorf(`shared-memory "shared-memory" attribute must be a string, not %v`,
			slot.Attrs["shared-memory"])
	}
	if sharedMemory == "" {
		if slot.Attrs == nil {
			slot.Attrs = make(map[string]interface{})
		}
		// shared-memory defaults to "slot" name if unspecified
		slot.Attrs["shared-memory"] = slot.Name
	}

	readPaths, err := stringListAttribute(slot, "read")
	if err != nil {
		return err
	}

	writePaths, err := stringListAttribute(slot, "write")
	if err != nil {
		return err
	}

	// We perform the same validation for read-only and writable paths, so
	// let's just put them all in the same array
	allPaths := append(readPaths, writePaths...)
	if len(allPaths) == 0 {
		return errors.New(`shared memory interface requires at least a valid "read" or "write" attribute`)
	}

	for _, path := range allPaths {
		if err := validateSharedMemoryPath(path); err != nil {
			return err
		}
	}

	return nil
}

type sharedMemorySnippetType int

const (
	snippetForSlot sharedMemorySnippetType = iota
	snippetForPlug
)

func writeSharedMemoryPaths(w io.Writer, slot *interfaces.ConnectedSlot,
	snippetType sharedMemorySnippetType) {
	emitWritableRule := func(path string) {
		// Ubuntu 14.04 uses /run/shm instead of the most common /dev/shm
		fmt.Fprintf(w, "\"/{dev,run}/shm/%s\" rwk,\n", path)
	}

	// All checks were already done in BeforePrepare{Plug,Slot}
	writePaths, _ := stringListAttribute(slot, "write")
	for _, path := range writePaths {
		emitWritableRule(path)
	}
	readPaths, _ := stringListAttribute(slot, "read")
	for _, path := range readPaths {
		if snippetType == snippetForPlug {
			// grant read-only access
			fmt.Fprintf(w, "\"/{dev,run}/shm/%s\" r,\n", path)
		} else {
			// the slot must still be granted write access, because the "read"
			// and "write" attributes are meant to affect the plug only
			emitWritableRule(path)
		}
	}
}

func (iface *sharedMemoryInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	sharedMemoryAttr, isSet := plug.Attrs["shared-memory"]
	sharedMemory, ok := sharedMemoryAttr.(string)
	if isSet && !ok {
		return fmt.Errorf(`shared-memory "shared-memory" attribute must be a string, not %v`,
			plug.Attrs["shared-memory"])
	}
	if sharedMemory == "" {
		if plug.Attrs == nil {
			plug.Attrs = make(map[string]interface{})
		}
		// shared-memory defaults to "plug" name if unspecified
		plug.Attrs["shared-memory"] = plug.Name
	}

	return nil
}

func (iface *sharedMemoryInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	sharedMemorySnippet := &bytes.Buffer{}
	writeSharedMemoryPaths(sharedMemorySnippet, slot, snippetForPlug)
	spec.AddSnippet(sharedMemorySnippet.String())
	return nil
}

func (iface *sharedMemoryInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	sharedMemorySnippet := &bytes.Buffer{}
	writeSharedMemoryPaths(sharedMemorySnippet, slot, snippetForSlot)
	spec.AddSnippet(sharedMemorySnippet.String())
	return nil
}

func (iface *sharedMemoryInterface) AutoConnect(plug *snap.PlugInfo, slot *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&sharedMemoryInterface{})
}
