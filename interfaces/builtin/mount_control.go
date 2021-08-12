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
	"fmt"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/utils"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd"
)

const mountControlSummary = `allows creating transient and persistent mounts`

const mountControlBaseDeclarationSlots = `
  mount-control:
    allow-installation:
      slot-snap-type:
        - core
        - gadget
    deny-connection: true
`

const mountControlConnectedPlugSecComp = `
# Description: Allow mount and umount syscall access.
mount
umount
umount2
`

var allowedMountOptions = []string{
	"async",
	"atime",
	"bind",
	"diratime",
	"dirsync",
	"iversion",
	"lazytime",
	"mand",
	"nofail",
	"noiversion",
	"nomand",
	"noatime",
	"nodev",
	"nodiratime",
	"noexec",
	"nolazytime",
	"norelatime",
	"nosuid",
	"nostrictatime",
	"nouser",
	"relatime",
	"strictatime",
	"sync",
	"ro",
	"rw",
}

// mountControlInterface allows creating transient and persistent mounts
type mountControlInterface struct {
	commonInterface
}

// TODO: review the regexp
// We prohibit expressions containing a comma, as that is used by AppArmor to
// separate rules; otherwise, one could specify a path like
//   $SNAP/foo, /** rw
// and get read-write access to the whole filesystem.
var whereRegexp = regexp.MustCompile(`^(/media|\$SNAP|\$SNAP_COMMON|\$SNAP_DATA)/[^,]*$`)

type MountInfo struct {
	what       string
	where      string
	persistent bool
	types      []string
	options    []string
}

func parseStringList(mountEntry map[string]interface{}, fieldName string) ([]string, error) {
	var list []string
	value, ok := mountEntry[fieldName]
	if ok {
		interfaceList, ok := value.([]interface{})
		if !ok {
			return nil, fmt.Errorf(`mount-control "mount::%s" must be an array of strings (got %q)`, fieldName, value)
		}
		for _, iface := range interfaceList {
			valueString, ok := iface.(string)
			if !ok {
				return nil, fmt.Errorf(`mount-control "mount::%s" element not a string (%q)`, fieldName, iface)
			}
			list = append(list, valueString)
		}
	}
	return list, nil
}

func enumerateMounts(plug interfaces.Attrer, fn func(mountInfo *MountInfo) error) error {
	mountAttr, ok := plug.Lookup("mount")
	if !ok {
		return nil
	}
	mounts, ok := mountAttr.([]interface{})
	if !ok {
		return fmt.Errorf(`mount-control "mount" attribute must be an array`)
	}

	for _, m := range mounts {
		mount, ok := m.(map[string]interface{})
		if !ok {
			return fmt.Errorf(`mount-control "mount" attribute is not a dictionary`)
		}

		what, ok := mount["what"].(string)
		if !ok {
			return fmt.Errorf(`mount-control "mount::what" must be a string`)
		}

		where, ok := mount["where"].(string)
		if !ok {
			return fmt.Errorf(`mount-control "mount::where" must be a string`)
		}

		persistent := false
		persistentValue, ok := mount["persistent"]
		if ok {
			if persistent, ok = persistentValue.(bool); !ok {
				return fmt.Errorf(`mount-control "mount::persistent" must be a boolean`)
			}
		}

		types, err := parseStringList(mount, "type")
		if err != nil {
			return err
		}

		options, err := parseStringList(mount, "options")
		if err != nil {
			return err
		}

		// TODO: parse other fields

		mountInfo := &MountInfo{
			what:       what,
			where:      where,
			persistent: persistent,
			types:      types,
			options:    options,
		}

		if err := fn(mountInfo); err != nil {
			return err
		}
	}

	return nil
}

func validateWhatAttr(what string) error {
	if what[0] != '/' {
		return fmt.Errorf(`mount-control "what" attribute must be an absolute path`)
	}

	if _, err := utils.NewPathPattern(what); err != nil {
		return fmt.Errorf(`mount-control "what" attribute error: %v`, err)
	}

	// TODO add more checks?
	return nil
}

func validateWhereAttr(where string) error {
	if !whereRegexp.MatchString(where) {
		return fmt.Errorf(`mount-control "where" attribute is invalid`)
	}

	if _, err := utils.NewPathPattern(where); err != nil {
		return fmt.Errorf(`mount-control "where" attribute error: %v`, err)
	}

	// TODO add more checks?
	return nil
}

func validateMountOptions(options []string) error {
	for _, o := range options {
		if !strutil.ListContains(allowedMountOptions, o) {
			return fmt.Errorf(`mount-control option unrecognized or forbidden: %q`, o)
		}
	}
	return nil
}

func validateMountInfo(mountInfo *MountInfo) error {
	if err := validateWhatAttr(mountInfo.what); err != nil {
		return err
	}

	if err := validateWhereAttr(mountInfo.where); err != nil {
		return err
	}

	if err := validateMountOptions(mountInfo.options); err != nil {
		return err
	}

	// TODO: validate mount type

	return nil
}

func (iface *mountControlInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	// We are not really sure what is the exact minimum systemd version that
	// we support, but we know for sure that 204 (used in Trusty) does not
	// work.
	if systemdVersion, err := systemd.Version(); err != nil || systemdVersion <= 204 {
		if err != nil {
			return err
		} else {
			return fmt.Errorf("systemd version %d is too old for mount-control interface",
				systemdVersion)
		}
	}

	if err := enumerateMounts(plug, validateMountInfo); err != nil {
		return err
	}

	return nil
}

func (iface *mountControlInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	mountControlSnippet := bytes.NewBuffer(nil)
	emit := func(f string, args ...interface{}) {
		fmt.Fprintf(mountControlSnippet, f, args...)
	}
	snapInfo := plug.Snap()

	numAddedEntries := 0
	enumerateMounts(plug, func(mountInfo *MountInfo) error {
		if numAddedEntries == 0 {
			fmt.Fprintf(mountControlSnippet, `
  # Rules added by the mount-control interface
  capability sys_admin,  # for mount

  owner @{PROC}/@{pid}/mounts r,

  /{,usr/}bin/mount ixr,
  /{,usr/}bin/umount ixr,
  # mount/umount (via libmount) track some mount info in these files
  /run/mount/utab* wrlk,
`)
		}

		source := snapInfo.ExpandSnapVariables(mountInfo.what)
		target := snapInfo.ExpandSnapVariables(mountInfo.where)

		typeRule := ""
		if len(mountInfo.types) > 0 {
			typeRule = "fstype=(" + strings.Join(mountInfo.types, ",") + ")"
		}

		options := strings.Join(mountInfo.options, ",")

		emit("  mount %s options=(%s) %s -> %s,\n", typeRule, options, source, target)
		emit("  umount %s,\n", target)
		numAddedEntries++
		return nil
	})

	spec.AddSnippet(mountControlSnippet.String())
	return nil
}

func (iface *mountControlInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	return true
}

func init() {
	registerIface(&mountControlInterface{
		commonInterface: commonInterface{
			name:                 "mount-control",
			summary:              mountControlSummary,
			baseDeclarationSlots: mountControlBaseDeclarationSlots,
			implicitOnCore:       true,
			implicitOnClassic:    true,
			connectedPlugSecComp: mountControlConnectedPlugSecComp,
		},
	})
}
