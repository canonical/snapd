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
	"fmt"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/seccomp"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

const posixMQSummary = `allows access to POSIX message queues`

const posixMQBaseDeclarationSlots = `
  posix-mq:
    allow-installation: true
    deny-auto-connection: true
`

const posixMQBaseDeclarationPlugs = `
  posix-mq:
    allow-installation: true
    allow-connection:
      slot-attributes:
        posix-mq: $PLUG(posix-mq)
    allow-auto-connection:
      slot-publisher-id:
        - $PLUG_PUBLISHER_ID
      slot-attributes:
        posix-mq: $PLUG(posix-mq)
`

const posixMQPermanentSlotSecComp = `
mq_open
mq_getsetattr
mq_unlink
mq_notify
mq_timedreceive
mq_timedsend
`

var posixMQPlugPermissions = []string{
	"open",
	"associate", /* alias for open */
	"read",
	"receive", /* alias for read */
	"write",
	"send", /* alias for write */
	"create",
	"delete",
	"destroy", /* alias for delete */
	"setattr",
	"getattr",
}

var posixMQDefaultPlugPermissions = []string{
	"read",
	"write",
}

// Ensure that the name matches the criteria from the mq_overview man page:
//   Each message queue is identified by a name of the form /somename;
//   that is, a null-terminated string of up to NAME_MAX (i.e., 255)
//   characters consisting of an initial slash, followed by one or more
//   characters, none of which are slashes.
var posixMQNamePattern = regexp.MustCompile(`^/[^/]{1,255}$`)

type posixMQInterface struct {
	commonInterface
}

func (iface *posixMQInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              posixMQSummary,
		BaseDeclarationSlots: posixMQBaseDeclarationSlots,
		BaseDeclarationPlugs: posixMQBaseDeclarationPlugs,
	}
}

func (iface *posixMQInterface) Name() string {
	return "posix-mq"
}

func (iface *posixMQInterface) checkPosixMQAppArmorSupport() error {
	if apparmor_sandbox.ProbedLevel() == apparmor_sandbox.Unsupported {
		/* AppArmor is not supported at all; no need to add rules */
		return nil
	}
	if features, err := apparmor_sandbox.ParserFeatures(); err == nil {
		if !strutil.ListContains(features, "mqueue") {
			return fmt.Errorf("AppArmor does not support POSIX message queues - cannot setup or connect interfaces")
		}
	} else {
		return err
	}
	return nil
}

func (iface *posixMQInterface) isValidPermission(perm string) bool {
	for _, validPerm := range posixMQPlugPermissions {
		if perm == validPerm {
			return true
		}
	}
	return false
}

func (iface *posixMQInterface) validatePermissionList(perms []string, name string) error {
	for _, perm := range perms {
		if !iface.isValidPermission(perm) {
			return fmt.Errorf("posix-mq slot %s permission \"%s\" not valid, must be one of %v", name, perm, posixMQPlugPermissions)
		}
	}

	return nil
}

func (iface *posixMQInterface) getPermissions(attrs interfaces.Attrer, name string) ([]string, error) {
	var perms []string

	if err := attrs.Attr("permissions", &perms); err != nil {
		/* If the permissions have not been specified, use the defaults */
		perms = posixMQDefaultPlugPermissions
	}

	if err := iface.validatePermissionList(perms, name); err != nil {
		return nil, err
	}

	return perms, nil
}

func (iface *posixMQInterface) getPath(attrs interfaces.Attrer, name string) (string, error) {
	if pathAttr, isSet := attrs.Lookup("path"); isSet {
		if path, ok := pathAttr.(string); ok {
			return path, nil
		}
		return "", fmt.Errorf(`posix-mq slot %s "path" attribute must be a string, not %v`, name, pathAttr)
	}
	return "", fmt.Errorf(`posix-mq slot %s has missing "path" attribute`, name)
}

func (iface *posixMQInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	if err := iface.checkPosixMQAppArmorSupport(); err != nil {
		return err
	}

	/* Ensure that the given permissions are valid */
	if _, err := iface.getPermissions(plug, plug.Name); err != nil {
		return err
	}

	return nil
}

func (iface *posixMQInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	if err := iface.checkPosixMQAppArmorSupport(); err != nil {
		return err
	}

	/* Ensure that the given permissions are valid */
	if _, err := iface.getPermissions(slot, slot.Name); err != nil {
		return err
	}

	/* Ensure that the given path has a valid POSIX MQ name */
	if path, err := iface.getPath(slot, slot.Name); err == nil {
		if posixMQNamePattern.MatchString(path) {
			return nil
		}
		return fmt.Errorf("posix-mq path must conform to the POSIX message queue name specifications (see `man mq_overview`)")
	} else {
		return err
	}
}

func (iface *posixMQInterface) AutoConnect(plug *snap.PlugInfo, slot *snap.SlotInfo) bool {
	return true
}

func (iface *posixMQInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	if !implicitSystemPermanentSlot(slot) {
		if path, err := iface.getPath(slot, slot.Name); err == nil {
			spec.AddSnippet(fmt.Sprintf(`# POSIX Message Queue management
mqueue (create delete getattr setattr read write) %s,
`, path))
		} else {
			return err
		}
	}
	return nil
}

func (iface *posixMQInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if path, err := iface.getPath(slot, slot.Name()); err == nil {
		spec.AddSnippet(fmt.Sprintf(`# POSIX Message Queue slot communication
mqueue (create delete getattr setattr) %s,
`, path))
	} else {
		return err
	}
	return nil
}

func (iface *posixMQInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if path, err := iface.getPath(slot, slot.Name()); err == nil {
		if perms, err := iface.getPermissions(slot, slot.Name()); err == nil {
			aaPerms := strings.Join(perms, " ")
			spec.AddSnippet(fmt.Sprintf(`# POSIX Message Queue plug communication
mqueue (%s) %s,
`, aaPerms, path))
		} else {
			return err
		}
	} else {
		return err
	}
	return nil
}

func (iface *posixMQInterface) SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(posixMQPermanentSlotSecComp)
	return nil
}

func (iface *posixMQInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if perms, err := iface.getPermissions(slot, slot.Name()); err == nil {
		var syscalls = []string{
			// always allow this function
			"mq_open",
		}
		for _, perm := range perms {
			switch perm {
			case "read":
				fallthrough
			case "receive":
				syscalls = append(syscalls, "mq_timedreceive")
				syscalls = append(syscalls, "mq_notify")
				break
			case "write":
				fallthrough
			case "send":
				syscalls = append(syscalls, "mq_timedsend")
				break
			case "delete":
				fallthrough
			case "destroy":
				syscalls = append(syscalls, "mq_unlink")
				break
			case "setattr":
				fallthrough
			case "getattr":
				syscalls = append(syscalls, "mq_getsetattr")
				break
			default:
				continue // no syscall needed
			}
		}
		spec.AddSnippet(strings.Join(syscalls, "\n"))
	} else {
		return err
	}
	return nil
}

func init() {
	registerIface(&posixMQInterface{})
}
