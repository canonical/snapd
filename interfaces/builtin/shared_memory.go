// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"path/filepath"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

const sharedMemorySummary = `allows two snaps to use predefined shared memory objects`

// The plug side of shared-memory can operate in two modes: if the
// private attribute is set to true, then it can be connected to the
// implicit system slot to be given a private version of /dev/shm.
//
// For a plug without that attribute set, it will connect to a
// matching application snap slot - this is permitted even though the
// interface is super-privileged because using a slot requires a store
// declaration anyways so just declaring a plug will not grant access
// unless a slot was also granted at some point.
const sharedMemoryBaseDeclarationPlugs = `
  shared-memory:
    allow-connection:
      -
        plug-attributes:
          private: false
        slot-attributes:
          shared-memory: $PLUG(shared-memory)
      -
        plug-attributes:
          private: true
        slot-snap-type:
          - core
    allow-auto-connection:
      -
        plug-attributes:
          private: false
        slot-publisher-id:
          - $PLUG_PUBLISHER_ID
        slot-attributes:
          shared-memory: $PLUG(shared-memory)
      -
        plug-attributes:
          private: true
        slot-snap-type:
          - core
`

// shared-memory slots can appear either as an implicit system slot,
// or as a slot on an application snap.
//
// The implicit version of the slot is intended to auto-connect with
// plugs that have the private attribute set to true.
//
// Slots on app snaps connect to non-private plugs. They are are
// super-privileged and thus denied to any snap except those that get
// a store declaration to do so, but the intent is for application or
// gadget snaps to use the slot much like the content interface.
const sharedMemoryBaseDeclarationSlots = `
  shared-memory:
    allow-installation:
      slot-snap-type:
        - app
        - gadget
        - core
    deny-installation:
      slot-snap-type:
        - app
        - gadget
    deny-auto-connection: true
`

const sharedMemoryPrivateConnectedPlugAppArmor = `
# Description: Allow access to everything in private /dev/shm
"/dev/shm/**" mrwlkix,
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
		ImplicitOnCore:       true,
		ImplicitOnClassic:    true,
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

	readPaths := mylog.Check2(stringListAttribute(slot, "read"))

	writePaths := mylog.Check2(stringListAttribute(slot, "write"))

	// We perform the same validation for read-only and writable paths, so
	// let's just put them all in the same array
	allPaths := append(readPaths, writePaths...)
	if len(allPaths) == 0 {
		return errors.New(`shared memory interface requires at least a valid "read" or "write" attribute`)
	}

	for _, path := range allPaths {
		mylog.Check(validateSharedMemoryPath(path))
	}

	return nil
}

type sharedMemorySnippetType int

const (
	snippetForSlot sharedMemorySnippetType = iota
	snippetForPlug
)

func writeSharedMemoryPaths(w io.Writer, slot *interfaces.ConnectedSlot,
	snippetType sharedMemorySnippetType,
) {
	emitWritableRule := func(path string) {
		// Ubuntu 14.04 uses /run/shm instead of the most common /dev/shm
		fmt.Fprintf(w, "\"/{dev,run}/shm/%s\" mrwlk,\n", path)
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
	privateAttr, isPrivateSet := plug.Attrs["private"]
	private, ok := privateAttr.(bool)
	if isPrivateSet && !ok {
		return fmt.Errorf(`shared-memory "private" attribute must be a bool, not %v`, privateAttr)
	}
	if plug.Attrs == nil {
		plug.Attrs = make(map[string]interface{})
	}
	plug.Attrs["private"] = private

	sharedMemoryAttr, isSet := plug.Attrs["shared-memory"]
	sharedMemory, ok := sharedMemoryAttr.(string)
	if isSet && !ok {
		return fmt.Errorf(`shared-memory "shared-memory" attribute must be a string, not %v`,
			plug.Attrs["shared-memory"])
	}
	if private {
		if isSet {
			return fmt.Errorf(`shared-memory "shared-memory" attribute must not be set together with "private: true"`)
		}
		// A private shared-memory plug cannot coexist with
		// other shared-memory plugs/slots.
		for _, other := range plug.Snap.Plugs {
			if other != plug && other.Interface == "shared-memory" {
				return fmt.Errorf(`shared-memory plug with "private: true" set cannot be used with other shared-memory plugs`)
			}
		}
		for _, other := range plug.Snap.Slots {
			if other.Interface == "shared-memory" {
				return fmt.Errorf(`shared-memory plug with "private: true" set cannot be used with shared-memory slots`)
			}
		}
	} else {
		if sharedMemory == "" {
			// shared-memory defaults to "plug" name if unspecified
			plug.Attrs["shared-memory"] = plug.Name
		}
	}

	return nil
}

func (iface *sharedMemoryInterface) isPrivate(plug *interfaces.ConnectedPlug) (bool, error) {
	// Note that private may not be set even if
	// "SanitizePlugsSlots()" is called (which in turn calls
	// BeforePreparePlug() which will set this).
	//
	// The code-path is an upgrade from snapd 2.54.4 where
	// shared-memory did not have the "private" attribute
	// yet. Then the ConnectedPlug data is written into the
	// interface repo without this attribute and on regeneration
	// of security profiles the connectedPlug is loaded from the
	// interface repository in the state and not from the
	// snap.yaml so this attribute is missing.
	var private bool
	if mylog.Check(plug.Attr("private", &private)); err != nil && !errors.Is(err, snap.AttributeNotFoundError{}) {
		return false, err
	}
	return private, nil
}

func (iface *sharedMemoryInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	private := mylog.Check2(iface.isPrivate(plug))

	if private {
		spec.AddSnippet(sharedMemoryPrivateConnectedPlugAppArmor)
		spec.AddUpdateNSf(`  # Private /dev/shm
  /dev/ r,
  /dev/shm/{,**} rw,
  mount options=(bind, rw) /dev/shm/snap.%s/ -> /dev/shm/,
  umount /dev/shm/,`, plug.Snap().InstanceName())
	} else {
		sharedMemorySnippet := &bytes.Buffer{}
		writeSharedMemoryPaths(sharedMemorySnippet, slot, snippetForPlug)
		spec.AddSnippet(sharedMemorySnippet.String())
	}
	return nil
}

func (iface *sharedMemoryInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if slot.Snap().Type() == snap.TypeOS || slot.Snap().Type() == snap.TypeSnapd {
		return nil
	}

	sharedMemorySnippet := &bytes.Buffer{}
	writeSharedMemoryPaths(sharedMemorySnippet, slot, snippetForSlot)
	spec.AddSnippet(sharedMemorySnippet.String())
	return nil
}

func (iface *sharedMemoryInterface) MountConnectedPlug(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	private := mylog.Check2(iface.isPrivate(plug))

	if !private {
		return nil
	}

	devShm := filepath.Join(dirs.GlobalRootDir, "/dev/shm")
	if osutil.IsSymlink(devShm) {
		return fmt.Errorf(`shared-memory plug with "private: true" cannot be connected if %q is a symlink`, devShm)
	}

	return spec.AddMountEntry(osutil.MountEntry{
		Name:    filepath.Join(devShm, "snap."+plug.Snap().InstanceName()),
		Dir:     "/dev/shm",
		Options: []string{"bind", "rw"},
	})
}

func (iface *sharedMemoryInterface) AutoConnect(plug *snap.PlugInfo, slot *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&sharedMemoryInterface{})
}
