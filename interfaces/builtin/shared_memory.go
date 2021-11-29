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
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
)

const sharedMemorySummary = `allows two snaps to use predefined shared memory objects`

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

	// TODO: allow "*" as a globbing character; figure out if more AARE should be allowed
	if err := apparmor.ValidateNoAppArmorRegexp(path); err != nil {
		return fmt.Errorf("shared-memory interface path is invalid: %v", err)
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
	parseError := func(key string, value interface{}) error {
		return fmt.Errorf(`shared-memory %q attribute must be a list of strings, not "%v"`, key, value)
	}
	attr, ok := attrer.Lookup(key)
	if !ok {
		return nil, nil
	}

	attrList, ok := attr.([]interface{})
	if !ok || len(attrList) == 0 {
		return nil, parseError(key, attr)
	}

	stringList := make([]string, 0, len(attrList))
	for _, value := range attrList {
		s, ok := value.(string)
		if !ok {
			return nil, parseError(key, attrList)
		}
		stringList = append(stringList, s)
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
