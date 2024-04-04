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
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/metautil"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

const posixMQSummary = `allows access to POSIX message queues`

// This interface is super-privileged
const posixMQBaseDeclarationSlots = `
  posix-mq:
    allow-installation: false
    deny-connection: true
    deny-auto-connection: true
`

// Paths can only be specified by the slot and a slot needs to exist for the
// plug to connect to, so the plug does not need to be marked super-privileged
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
mq_timedreceive_time64
mq_timedsend
mq_timedsend_time64
`

var posixMQPlugPermissions = []string{
	"open",
	"read",
	"write",
	"create",
	"delete",
}

var posixMQDefaultPlugPermissions = []string{
	"read",
	"write",
}

// Ensure that the name matches the criteria from the mq_overview man page:
//
//	Each message queue is identified by a name of the form /somename;
//	that is, a null-terminated string of up to NAME_MAX (i.e., 255)
//	characters consisting of an initial slash, followed by one or more
//	characters, none of which are slashes.
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
		// AppArmor is not supported at all; no need to add rules
		return nil
	}

	features, err := apparmor_sandbox.ParserFeatures()
	if err != nil {
		return err
	}

	if !strutil.ListContains(features, "mqueue") {
		return fmt.Errorf("AppArmor does not support POSIX message queues - cannot setup or connect interfaces")
	}

	return nil
}

func (iface *posixMQInterface) validatePermissionList(perms []string, name string) error {
	for _, perm := range perms {
		if !strutil.ListContains(posixMQPlugPermissions, perm) {
			return fmt.Errorf("posix-mq slot permission \"%s\" not valid, must be one of %v", perm, posixMQPlugPermissions)
		}
	}

	return nil
}

func (iface *posixMQInterface) validatePermissionsAttr(permsAttr interface{}) ([]string, error) {
	var perms []string
	permsList, ok := permsAttr.([]interface{})

	if !ok {
		return nil, fmt.Errorf(`posix-mq slot "permissions" attribute must be a list of strings, not %v`, permsAttr)
	}

	// Ensure that each permission in the list is a string
	for _, i := range permsList {
		perm, ok := i.(string)
		if !ok {
			return nil, fmt.Errorf(`each posix-mq slot permission must be a string, not %v`, permsAttr)
		}
		perms = append(perms, perm)
	}

	return perms, nil
}

func (iface *posixMQInterface) getPermissions(attrs interfaces.Attrer, name string) ([]string, error) {
	var perms []string

	err := attrs.Attr("permissions", &perms)
	switch {
	case errors.Is(err, snap.AttributeNotFoundError{}):
		// If the permissions have not been specified, use the defaults
		perms = posixMQDefaultPlugPermissions
	case err != nil:
		return nil, err
	}

	if err := iface.validatePermissionList(perms, name); err != nil {
		return nil, err
	}

	return perms, nil
}

func (iface *posixMQInterface) getPaths(attrs interfaces.Attrer, name string) ([]string, error) {
	var pathList []string
	var pathStr string

	// The path attribute can either be a string or an array of strings
	err := attrs.Attr("path", &pathStr)
	switch {
	case errors.Is(err, snap.AttributeNotFoundError{}):
		return nil, fmt.Errorf(`posix-mq slot requires the "path" attribute`)
	case errors.Is(err, metautil.AttributeNotCompatibleError{}):
		// If the attribute exists but reading it as a string didn't work, try reading it as an array
		if err = attrs.Attr("path", &pathList); err != nil {
			// If that didn't work, the attribute is an invalid type
			return nil, err
		}
	case err != nil:
		return nil, err
	default:
		// If the path is a single string, turn it into an array
		pathList = append(pathList, pathStr)
	}

	if len(pathList) == 0 {
		return nil, fmt.Errorf(`posix-mq slot requires at least one value in the "path" attribute`)
	}

	for i, path := range pathList {
		if len(path) == 0 {
			return nil, fmt.Errorf(`posix-mq slot "path" attribute values cannot be empty`)
		}

		// Path must begin with a /
		if path[0] != '/' {
			path = "/" + path
			pathList[i] = path
		}

		if err := iface.validatePath(name, path); err != nil {
			return nil, err
		}
	}

	return pathList, nil
}

func (iface *posixMQInterface) validatePath(name, path string) error {
	if !posixMQNamePattern.MatchString(path) {
		return fmt.Errorf(`posix-mq "path" attribute must conform to the POSIX message queue name specifications (see "man mq_overview"): %v`, path)
	}

	if err := apparmor_sandbox.ValidateNoAppArmorRegexp(path); err != nil {
		return fmt.Errorf(`posix-mq "path" attribute is invalid: %v"`, path)
	}

	if !cleanSubPath(path) {
		return fmt.Errorf(`posix-mq "path" attribute is not a clean path: %q`, path)
	}

	return nil
}

func (iface *posixMQInterface) checkPosixMQAttr(name string, attrs *map[string]interface{}) error {
	posixMQAttr, isSet := (*attrs)["posix-mq"]
	posixMQ, ok := posixMQAttr.(string)
	if isSet && !ok {
		return fmt.Errorf(`posix-mq "posix-mq" attribute must be a string, not %v`, (*attrs)["posix-mq"])
	}
	if posixMQ == "" {
		if *attrs == nil {
			*attrs = make(map[string]interface{})
		}
		// posix-mq attribute defaults to name if unspecified
		(*attrs)["posix-mq"] = name
	}

	return nil
}

func (iface *posixMQInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	if err := iface.checkPosixMQAppArmorSupport(); err != nil {
		return err
	}

	if err := iface.checkPosixMQAttr(plug.Name, &plug.Attrs); err != nil {
		return err
	}

	// Plugs don't have any path or permission arguments to validate;
	// everything is configured by the slot

	return nil
}

func (iface *posixMQInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	if err := iface.checkPosixMQAppArmorSupport(); err != nil {
		return err
	}

	if err := iface.checkPosixMQAttr(slot.Name, &slot.Attrs); err != nil {
		return err
	}

	// Only ensure that the given permissions are valid, don't use them here
	if _, err := iface.getPermissions(slot, slot.Name); err != nil {
		return err
	}

	// Only ensure that the given path is valid, don't use it here
	if _, err := iface.getPaths(slot, slot.Name); err != nil {
		return err
	}

	return nil
}

func (iface *posixMQInterface) generateSnippet(name, plugOrSlot string, permissions, paths []string) string {
	var snippet strings.Builder
	aaPerms := strings.Join(permissions, " ")

	snippet.WriteString(fmt.Sprintf("  # POSIX Message Queue %s: %s\n", plugOrSlot, name))
	for _, path := range paths {
		snippet.WriteString(fmt.Sprintf("  mqueue (%s) \"%s\",\n", aaPerms, path))
	}

	return snippet.String()
}

func (iface *posixMQInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	if implicitSystemPermanentSlot(slot) {
		return nil
	}

	paths, err := iface.getPaths(slot, slot.Name)
	if err != nil {
		return err
	}

	// Slots always have all permissions enabled for the given message queue path
	snippet := iface.generateSnippet(slot.Name, "slot", posixMQPlugPermissions, paths)
	spec.AddSnippet(snippet)

	return nil
}

func (iface *posixMQInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	paths, err := iface.getPaths(slot, slot.Name())
	if err != nil {
		return err
	}

	perms, err := iface.getPermissions(slot, slot.Name())
	if err != nil {
		return err
	}

	// Always allow "open"
	if !strutil.ListContains(perms, "open") {
		perms = append(perms, "open")
	}

	snippet := iface.generateSnippet(plug.Name(), "plug", perms, paths)
	spec.AddSnippet(snippet)

	return nil
}

func (iface *posixMQInterface) SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(posixMQPermanentSlotSecComp)
	return nil
}

func (iface *posixMQInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	perms, err := iface.getPermissions(slot, slot.Name())
	if err != nil {
		return err
	}

	var syscalls = []string{
		// Always allow these functions
		"mq_open",
		"mq_getsetattr",
	}

	for _, perm := range perms {
		// Only these permissions have associated syscalls
		switch perm {
		case "read":
			syscalls = append(syscalls, "mq_timedreceive")
			syscalls = append(syscalls, "mq_timedreceive_time64")
			syscalls = append(syscalls, "mq_notify")
		case "write":
			syscalls = append(syscalls, "mq_timedsend")
			syscalls = append(syscalls, "mq_timedsend_time64")
		case "delete":
			syscalls = append(syscalls, "mq_unlink")
		}
	}
	spec.AddSnippet(strings.Join(syscalls, "\n"))

	return nil
}

func init() {
	registerIface(&posixMQInterface{})
}
