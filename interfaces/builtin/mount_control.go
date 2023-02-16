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
	"regexp"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/utils"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd"
)

const mountControlSummary = `allows creating transient and persistent mounts`

const mountControlBaseDeclarationPlugs = `
  mount-control:
    allow-installation: false
    deny-auto-connection: true
`

const mountControlBaseDeclarationSlots = `
  mount-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-connection: true
`

var mountAttrTypeError = errors.New(`mount-control "mount" attribute must be a list of dictionaries`)

const mountControlConnectedPlugSecComp = `
# Description: Allow mount and umount syscall access. No filtering here, as we
# rely on AppArmor to filter the mount operations.
mount
umount
umount2
`

// The reason why this list is not shared with osutil.MountOptsToCommonFlags or
// other parts of the codebase is that this one only contains the options which
// have been deemed safe and have been vetted by the security team.
var allowedMountOptions = []string{
	"async",
	"atime",
	"bind",
	"diratime",
	"dirsync",
	"iversion",
	"lazytime",
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

// A few mount flags are special in that if they are specified, the filesystem
// type is ignored. We list them here, and we will ensure that the plug
// declaration does not specify a type, if any of them is present among the
// options.
var optionsWithoutFsType = []string{
	"bind",
	// Note: the following flags should also fall into this list, but we are
	// not currently allowing them (and don't plan to):
	// - "make-private"
	// - "make-shared"
	// - "make-slave"
	// - "make-unbindable"
	// - "move"
	// - "remount"
}

// List of filesystem types to allow if the plug declaration does not
// explicitly specify a filesystem type.
var defaultFSTypes = []string{
	"aufs",
	"autofs",
	"btrfs",
	"ext2",
	"ext3",
	"ext4",
	"hfs",
	"iso9660",
	"jfs",
	"msdos",
	"ntfs",
	"ramfs",
	"reiserfs",
	"squashfs",
	"tmpfs",
	"ubifs",
	"udf",
	"ufs",
	"vfat",
	"zfs",
	"xfs",
}

// The filesystems in the following list were considered either dangerous or
// not relevant for this interface:
var disallowedFSTypes = []string{
	"bpf",
	"cgroup",
	"cgroup2",
	"debugfs",
	"devpts",
	"ecryptfs",
	"hugetlbfs",
	"overlayfs",
	"proc",
	"securityfs",
	"sysfs",
	"tracefs",
}

// mountControlInterface allows creating transient and persistent mounts
type mountControlInterface struct {
	commonInterface
}

// The "what" and "where" attributes end up in the AppArmor profile, surrounded
// by double quotes; to ensure that a malicious snap cannot inject arbitrary
// rules by specifying something like
//
//	where: $SNAP_DATA/foo", /** rw, #
//
// which would generate a profile line like:
//
//	mount options=() "$SNAP_DATA/foo", /** rw, #"
//
// (which would grant read-write access to the whole filesystem), it's enough
// to exclude the `"` character: without it, whatever is written in the
// attribute will not be able to escape being treated like a pattern.
//
// To be safe, there's more to be done: the pattern also needs to be valid, as
// a malformed one (for example, a pattern having an unmatched `}`) would cause
// apparmor_parser to fail loading the profile. For this situation, we use the
// PathPattern interface to validate the pattern.
//
// Besides that, we are also excluding the `@` character, which is used to mark
// AppArmor variables (tunables): when generating the profile we lack the
// knowledge of which variables have been defined, so it's safer to exclude
// them.
// The what attribute regular expression here is intentionally permissive of
// nearly any path, and due to the super-privileged nature of this interface it
// is expected that sensible values of what are enforced by the store manual
// review queue and security teams.
var (
	whatRegexp  = regexp.MustCompile(`^(none|/[^"@]*)$`)
	whereRegexp = regexp.MustCompile(`^(\$SNAP_COMMON|\$SNAP_DATA)?/[^\$"@]+$`)
)

// Excluding spaces and other characters which might allow constructing a
// malicious string like
//
//	auto) options=() /malicious/content /var/lib/snapd/hostfs/...,\n mount fstype=(
var typeRegexp = regexp.MustCompile(`^[a-z0-9]+$`)

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
			return nil, fmt.Errorf(`mount-control "%s" must be an array of strings (got %q)`, fieldName, value)
		}
		for i, iface := range interfaceList {
			valueString, ok := iface.(string)
			if !ok {
				return nil, fmt.Errorf(`mount-control "%s" element %d not a string (%q)`, fieldName, i+1, iface)
			}
			list = append(list, valueString)
		}
	}
	return list, nil
}

func enumerateMounts(plug interfaces.Attrer, fn func(mountInfo *MountInfo) error) error {
	var mounts []map[string]interface{}
	err := plug.Attr("mount", &mounts)
	if err != nil && !errors.Is(err, snap.AttributeNotFoundError{}) {
		return mountAttrTypeError
	}

	for _, mount := range mounts {
		what, ok := mount["what"].(string)
		if !ok {
			return fmt.Errorf(`mount-control "what" must be a string`)
		}

		where, ok := mount["where"].(string)
		if !ok {
			return fmt.Errorf(`mount-control "where" must be a string`)
		}

		persistent := false
		persistentValue, ok := mount["persistent"]
		if ok {
			if persistent, ok = persistentValue.(bool); !ok {
				return fmt.Errorf(`mount-control "persistent" must be a boolean`)
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

func validateWhatAttr(mountInfo *MountInfo) error {
	what := mountInfo.what

	// with "functionfs" the "what" can essentially be anything, see
	// https://www.kernel.org/doc/html/latest/usb/functionfs.html
	if len(mountInfo.types) == 1 && mountInfo.types[0] == "functionfs" {
		if err := apparmor_sandbox.ValidateNoAppArmorRegexp(what); err != nil {
			return fmt.Errorf(`cannot use mount-control "what" attribute: %w`, err)
		}
		return nil
	}

	if !whatRegexp.MatchString(what) {
		return fmt.Errorf(`mount-control "what" attribute is invalid: must start with / and not contain special characters`)
	}

	if !cleanSubPath(what) {
		return fmt.Errorf(`mount-control "what" pattern is not clean: %q`, what)
	}

	if _, err := utils.NewPathPattern(what); err != nil {
		return fmt.Errorf(`mount-control "what" setting cannot be used: %v`, err)
	}

	// "what" must be set to "none" iff the type is "tmpfs"
	isTmpfs := len(mountInfo.types) == 1 && mountInfo.types[0] == "tmpfs"
	if mountInfo.what == "none" {
		if !isTmpfs {
			return errors.New(`mount-control "what" attribute can be "none" only with "tmpfs"`)
		}
	} else if isTmpfs {
		return fmt.Errorf(`mount-control "what" attribute must be "none" with "tmpfs"; found %q instead`, mountInfo.what)
	}

	return nil
}

func validateWhereAttr(where string) error {
	if !whereRegexp.MatchString(where) {
		return fmt.Errorf(`mount-control "where" attribute must start with $SNAP_COMMON, $SNAP_DATA or / and not contain special characters`)
	}

	if !cleanSubPath(where) {
		return fmt.Errorf(`mount-control "where" pattern is not clean: %q`, where)
	}

	if _, err := utils.NewPathPattern(where); err != nil {
		return fmt.Errorf(`mount-control "where" setting cannot be used: %v`, err)
	}

	return nil
}

func validateMountTypes(types []string) error {
	includesTmpfs := false
	for _, t := range types {
		if !typeRegexp.MatchString(t) {
			return fmt.Errorf(`mount-control filesystem type invalid: %q`, t)
		}
		if strutil.ListContains(disallowedFSTypes, t) {
			return fmt.Errorf(`mount-control forbidden filesystem type: %q`, t)
		}
		if t == "tmpfs" {
			includesTmpfs = true
		}
	}

	if includesTmpfs && len(types) > 1 {
		return errors.New(`mount-control filesystem type "tmpfs" cannot be listed with other types`)
	}
	return nil
}

func validateMountOptions(options []string) error {
	if len(options) == 0 {
		return errors.New(`mount-control "options" cannot be empty`)
	}
	for _, o := range options {
		if !strutil.ListContains(allowedMountOptions, o) {
			return fmt.Errorf(`mount-control option unrecognized or forbidden: %q`, o)
		}
	}
	return nil
}

// Find the first option which is incompatible with a FS type declaration
func optionIncompatibleWithFsType(options []string) string {
	for _, o := range options {
		if strutil.ListContains(optionsWithoutFsType, o) {
			return o
		}
	}
	return ""
}

func validateMountInfo(mountInfo *MountInfo) error {
	if err := validateWhatAttr(mountInfo); err != nil {
		return err
	}

	if err := validateWhereAttr(mountInfo.where); err != nil {
		return err
	}

	if err := validateMountTypes(mountInfo.types); err != nil {
		return err
	}

	if err := validateMountOptions(mountInfo.options); err != nil {
		return err
	}

	// Check if any options are incompatible with specifying a FS type
	fsExclusiveOption := optionIncompatibleWithFsType(mountInfo.options)
	if fsExclusiveOption != "" && len(mountInfo.types) > 0 {
		return fmt.Errorf(`mount-control option %q is incompatible with specifying filesystem type`, fsExclusiveOption)
	}

	// Until we have a clear picture of how this should work, disallow creating
	// persistent mounts into $SNAP_DATA
	if mountInfo.persistent && strings.HasPrefix(mountInfo.where, "$SNAP_DATA") {
		return errors.New(`mount-control "persistent" attribute cannot be used to mount onto $SNAP_DATA`)
	}

	return nil
}

func (iface *mountControlInterface) BeforeConnectPlug(plug *interfaces.ConnectedPlug) error {
	// The systemd.ListMountUnits() method works by issuing the command
	// "systemctl show *.mount", but globbing was only added in systemd v209.
	if err := systemd.EnsureAtLeast(209); err != nil {
		return err
	}

	hasMountEntries := false
	err := enumerateMounts(plug, func(mountInfo *MountInfo) error {
		hasMountEntries = true
		return validateMountInfo(mountInfo)
	})
	if err != nil {
		return err
	}

	if !hasMountEntries {
		return mountAttrTypeError
	}

	return nil
}

func (iface *mountControlInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	mountControlSnippet := bytes.NewBuffer(nil)
	emit := func(f string, args ...interface{}) {
		fmt.Fprintf(mountControlSnippet, f, args...)
	}
	snapInfo := plug.Snap()

	emit(`
  # Rules added by the mount-control interface
  capability sys_admin,  # for mount

  owner @{PROC}/@{pid}/mounts r,
  owner @{PROC}/@{pid}/mountinfo r,
  owner @{PROC}/self/mountinfo r,

  /{,usr/}bin/mount ixr,
  /{,usr/}bin/umount ixr,
  # mount/umount (via libmount) track some mount info in these files
  /run/mount/utab* wrlk,
`)

	// No validation is occurring here, as it was already performed in
	// BeforeConnectPlug()
	enumerateMounts(plug, func(mountInfo *MountInfo) error {

		source := mountInfo.what
		target := mountInfo.where
		if target[0] == '$' {
			matches := whereRegexp.FindStringSubmatchIndex(target)
			if matches == nil || len(matches) < 4 {
				// This cannot really happen, as the string wouldn't pass the validation
				return fmt.Errorf(`internal error: "where" fails to match regexp: %q`, mountInfo.where)
			}
			// the first two elements in "matches" are the boundaries of the whole
			// string; the next two are the boundaries of the first match, which is
			// what we care about as it contains the environment variable we want
			// to expand:
			variableStart, variableEnd := matches[2], matches[3]
			variable := target[variableStart:variableEnd]
			expanded := snapInfo.ExpandSnapVariables(variable)
			target = expanded + target[variableEnd:]
		}

		var typeRule string
		if optionIncompatibleWithFsType(mountInfo.options) != "" {
			// In this rule the FS type will not match unless it's empty
			typeRule = ""
		} else {
			var types []string
			if len(mountInfo.types) > 0 {
				types = mountInfo.types
			} else {
				types = defaultFSTypes
			}
			typeRule = "fstype=(" + strings.Join(types, ",") + ")"
		}

		options := strings.Join(mountInfo.options, ",")

		emit("  mount %s options=(%s) \"%s\" -> \"%s{,/}\",\n", typeRule, options, source, target)
		emit("  umount \"%s{,/}\",\n", target)
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
			baseDeclarationPlugs: mountControlBaseDeclarationPlugs,
			baseDeclarationSlots: mountControlBaseDeclarationSlots,
			implicitOnCore:       true,
			implicitOnClassic:    true,
			connectedPlugSecComp: mountControlConnectedPlugSecComp,
		},
	})
}
