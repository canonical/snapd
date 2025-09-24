// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	"iter"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
)

const usbGadgetSummary = `allows access to the usb gadget API`

const usbGadgetBaseDeclarationPlugs = `
  usb-gadget:
    allow-installation: false
    deny-auto-connection: true
`

const usbGadgetBaseDeclarationSlots = `
  usb-gadget:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const usbGadgetConnectedPlugSecComp = `
# Description: Allow mount and umount syscall access. No filtering here, as we
# rely on AppArmor to filter the mount operations.
mount
umount
umount2
`

// usbGadgetInterface allows creating transient and persistent mounts
type usbGadgetInterface struct {
	commonInterface
}

type ffsMountInfo struct {
	name       string
	where      string
	persistent bool
}

// Until we have a clear picture of how this should work, disallow creating
// persistent mounts into $SNAP_DATA or $SNAP_USER_DATA
func validatePersistentWhere(where string) error {
	if strings.HasPrefix(where, "$SNAP_DATA") ||
		strings.HasPrefix(where, "$SNAP_USER_DATA") {
		return errors.New(`usb-gadget "persistent" attribute cannot be used to mount onto $SNAP_DATA or $SNAP_USER_DATA`)
	}
	return nil
}

func (mi *ffsMountInfo) validate() error {
	// for ffs the name is the name of the function, which is not a path so we
	// just need to ensure it doesn't contain any AppArmor regex characters.
	if err := validateNoAppArmorRegexpWithError(`cannot use usb-gadget "name" attribute`, mi.name); err != nil {
		return err
	}

	// Reuse the where validation from mount-control, as the semantics are the same
	if err := validateWhereAttr(mi.where); err != nil {
		return err
	}

	if mi.persistent {
		if err := validatePersistentWhere(mi.where); err != nil {
			return err
		}
	}
	return nil
}

func ffsMounts(plug interfaces.Attrer) ([]map[string]any, error) {
	var mounts []map[string]any
	err := plug.Attr("ffs-mounts", &mounts)
	if err != nil && !errors.Is(err, snap.AttributeNotFoundError{}) {
		return nil, mountAttrTypeError
	}
	return mounts, nil
}

func enumerateFFSMounts(mounts []map[string]any) iter.Seq2[*ffsMountInfo, error] {
	return func(yield func(*ffsMountInfo, error) bool) {
		for _, mount := range mounts {
			name, ok := mount["name"].(string)
			if !ok {
				yield(nil, fmt.Errorf(`usb-gadget FunctionFS mount "name" must be a string`))
				return
			}

			where, ok := mount["where"].(string)
			if !ok {
				yield(nil, fmt.Errorf(`usb-gadget FunctionFS mount "where" must be a string`))
				return
			}

			persistent := false
			persistentValue, ok := mount["persistent"]
			if ok {
				if persistent, ok = persistentValue.(bool); !ok {
					yield(nil, fmt.Errorf(`usb-gadget FunctionFS mount "persistent" must be a boolean`))
					return
				}
			}

			mountInfo := &ffsMountInfo{
				name:       name,
				where:      where,
				persistent: persistent,
			}

			if !yield(mountInfo, nil) {
				return
			}
		}
	}
}

func (iface *usbGadgetInterface) BeforeConnectPlug(plug *interfaces.ConnectedPlug) error {
	// The systemd.ListMountUnits() method works by issuing the command
	// "systemctl show *.mount", but globbing was only added in systemd v209.
	if err := systemd.EnsureAtLeast(209); err != nil {
		return err
	}

	mounts, err := ffsMounts(plug)
	if err != nil {
		return err
	}

	enumerateFFSMounts(mounts)(func(m *ffsMountInfo, merr error) bool {
		if merr != nil {
			err = merr
			return false
		}
		if err2 := m.validate(); err2 != nil {
			err = err2
			return false
		}
		return true
	})
	return err
}

func (iface *usbGadgetInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	usbGadgetSnippet := bytes.NewBuffer(nil)
	emit := func(f string, args ...any) {
		fmt.Fprintf(usbGadgetSnippet, f, args...)
	}
	snapInfo := plug.Snap()

	// emit rules required generally for the usb-gadget interface
	emit(`
  # https://www.kernel.org/doc/Documentation/usb/gadget_configfs.txt
  # Allow creating new gadgets under usb_gadget, which is creating
  # new directories
  /sys/kernel/config/usb_gadget/ rw,
  # Allow creating sub-directories, symlinks and files under those
  # directories
  /sys/kernel/config/usb_gadget/** rw,

  # Allow access to UDC (USB Device Controller)
  /sys/class/udc/ r,
`)

	mounts, err := ffsMounts(plug)
	if err != nil {
		return err
	}
	if len(mounts) > 0 {
		// add required rules necessary to actually perform the ffs mounts
		emit(`
  # Rules added by the usb-gadget interface
  # due to ffs mount-support
  capability sys_admin,  # for mount

  owner @{PROC}/@{pid}/mounts r,
  owner @{PROC}/@{pid}/mountinfo r,
  owner @{PROC}/self/mountinfo r,

  /{,usr/}bin/mount ixr,
  /{,usr/}bin/umount ixr,
  # mount/umount (via libmount) track some mount info in these files
  # deny this for /run as /run comes from the host
  deny /run/mount/utab* wrlk,
`)

		// No validation is occurring here, as it was already performed in
		// BeforeConnectPlug()
		var err error
		enumerateFFSMounts(mounts)(func(m *ffsMountInfo, _ error) bool {
			source := m.name
			target, e := expandMountWhereVariable(m.where, snapInfo)
			if e != nil {
				// should never happen, should have been caught
				err = e
				return false
			}

			// mount -t functionfs <name> <where>
			emit("  mount fstype=(functionfs) \"%s\" -> \"%s\",\n", source, target)
			emit("  umount \"%s\",\n", target)
			emit("  %s rw,", target)
			return true
		})
		if err != nil {
			return err
		}
	}

	spec.AddSnippet(usbGadgetSnippet.String())
	return nil
}

func (iface *usbGadgetInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	mounts, err := ffsMounts(plug)
	if err != nil {
		return err
	}
	if len(mounts) > 0 {
		spec.AddSnippet(usbGadgetConnectedPlugSecComp)
	}
	return nil
}

func init() {
	registerIface(&usbGadgetInterface{
		commonInterface: commonInterface{
			name:                 "usb-gadget",
			summary:              usbGadgetSummary,
			baseDeclarationPlugs: usbGadgetBaseDeclarationPlugs,
			baseDeclarationSlots: usbGadgetBaseDeclarationSlots,
			implicitOnCore:       true,
		},
	})
}
