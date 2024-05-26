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
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/interfaces/utils"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

const customDeviceSummary = `provides access to custom devices specified via the gadget snap`

const customDeviceBaseDeclarationSlots = `
  custom-device:
    allow-installation: false
    allow-connection:
      plug-attributes:
        custom-device: $SLOT(custom-device)
    deny-auto-connection: true
`

var (
	// A cryptic, uninformative error message that we use only on impossible code paths
	customDeviceInternalError = errors.New(`custom-device interface internal error`)

	// Validating regexp for filesystem paths
	customDevicePathRegexp = regexp.MustCompile(`^/[^"@]*$`)

	// Validating regexp for udev device names.
	// We forbid:
	// - `|`: it's valid for udev, but more work for us
	// - `{}`: have a special meaning for AppArmor
	// - `"`: it's just dangerous (both for udev and AppArmor)
	// - `\`: also dangerous
	customDeviceUDevDeviceRegexp = regexp.MustCompile(`^/dev/[^"|{}\\]+$`)

	// Validating regexp for udev tag values (all but kernel devices)
	customDeviceUDevValueRegexp = regexp.MustCompile(`^[^"{}\\]+$`)
)

// customDeviceInterface allows sharing customDevice between snaps
type customDeviceInterface struct{}

func (iface *customDeviceInterface) validateFilePath(path string, attrName string) error {
	if !customDevicePathRegexp.MatchString(path) {
		return fmt.Errorf(`custom-device %q path must start with / and cannot contain special characters: %q`, attrName, path)
	}

	if !cleanSubPath(path) {
		return fmt.Errorf(`custom-device %q path is not clean: %q`, attrName, path)
	}

	const allowCommas = true
	mylog.Check2(utils.NewPathPattern(path, allowCommas))

	// We don't allow "**" because that's an AppArmor specific globbing pattern
	// which we don't want to expose in our API contract.
	if strings.Contains(path, "**") {
		return fmt.Errorf(`custom-device %q path contains invalid glob pattern "**"`, attrName)
	}

	return nil
}

func (iface *customDeviceInterface) validateDevice(path string, attrName string) error {
	// The device must satisfy udev's device name rules and generic path rules
	if !customDeviceUDevDeviceRegexp.MatchString(path) {
		return fmt.Errorf(`custom-device %q path must start with /dev/ and cannot contain special characters: %q`, attrName, path)
	}
	mylog.Check(iface.validateFilePath(path, attrName))

	return nil
}

func (iface *customDeviceInterface) validatePaths(attrName string, paths []string) error {
	for _, path := range paths {
		mylog.Check(iface.validateFilePath(path, attrName))
	}

	return nil
}

func (iface *customDeviceInterface) validateUDevValue(value interface{}) error {
	stringValue, ok := value.(string)
	if !ok {
		return fmt.Errorf(`value "%v" is not a string`, value)
	}

	if !customDeviceUDevValueRegexp.MatchString(stringValue) {
		return fmt.Errorf(`value "%v" contains invalid characters`, stringValue)
	}

	return nil
}

func (iface *customDeviceInterface) validateUDevValueMap(value interface{}) error {
	valueMap, ok := value.(map[string]interface{})
	if !ok {
		return fmt.Errorf(`value "%v" is not a map`, value)
	}

	for key, val := range valueMap {
		if !customDeviceUDevValueRegexp.MatchString(key) {
			return fmt.Errorf(`key "%v" contains invalid characters`, key)
		}
		mylog.Check(iface.validateUDevValue(val))

	}

	return nil
}

func (iface *customDeviceInterface) validateKernelMatchesOneDeviceBasename(kernelVal string, devices []string) error {
	matches := make([]string, 0)
	for _, devicePath := range devices {
		if kernelVal != filepath.Base(devicePath) {
			continue
		}
		matches = append(matches, devicePath)
	}
	switch len(matches) {
	case 0:
		return fmt.Errorf(`%q does not match any specified device`, kernelVal)
	case 1:
		return nil
	default:
		return fmt.Errorf(`%q matches more than one specified device: %q`, kernelVal, matches)
	}
}

func (iface *customDeviceInterface) validateUDevTaggingRule(rule map[string]interface{}, devices []string) error {
	hasKernelTag := false
	for key, value := range rule {
		switch key {
		case "subsystem":
			mylog.Check(iface.validateUDevValue(value))
		case "kernel":
			hasKernelTag = true
			mylog.Check(iface.validateUDevValue(value))

			kernelVal := value.(string)
			// The kernel device name must match the full path of
			// one of the given devices, stripped of the leading
			// /dev/, or it must be the basename of a device path.
			if strutil.ListContains(devices, "/dev/"+kernelVal) {
				break
			}
			mylog.
				// Not a full path, so check if it matches the basename
				// of a device path, and not more than one.
				Check(iface.validateKernelMatchesOneDeviceBasename(kernelVal, devices))
		case "attributes", "environment":
			mylog.Check(iface.validateUDevValueMap(value))
		default:
			mylog.Check(errors.New(`unknown tag`))
		}
	}

	if !hasKernelTag {
		return errors.New(`custom-device udev tagging rule missing mandatory "kernel" key`)
	}

	return nil
}

func (iface *customDeviceInterface) Name() string {
	return "custom-device"
}

func (iface *customDeviceInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              customDeviceSummary,
		BaseDeclarationSlots: customDeviceBaseDeclarationSlots,
	}
}

func (iface *customDeviceInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	if slot.Attrs == nil {
		slot.Attrs = make(map[string]interface{})
	}
	customDeviceAttr, isSet := slot.Attrs["custom-device"]
	customDevice, ok := customDeviceAttr.(string)
	if isSet && !ok {
		return fmt.Errorf(`custom-device "custom-device" attribute must be a string, not %v`,
			customDeviceAttr)
	}
	if customDevice == "" {
		// custom-device defaults to "slot" name if unspecified
		slot.Attrs["custom-device"] = slot.Name
	}

	var devices []string
	mylog.Check(slot.Attr("devices", &devices))
	if err != nil && !errors.Is(err, snap.AttributeNotFoundError{}) {
		return err
	}
	for _, device := range devices {
		mylog.Check(iface.validateDevice(device, "devices"))
	}

	var readDevices []string
	mylog.Check(slot.Attr("read-devices", &readDevices))
	if err != nil && !errors.Is(err, snap.AttributeNotFoundError{}) {
		return err
	}
	for _, device := range readDevices {
		mylog.Check(iface.validateDevice(device, "read-devices"))

		if strutil.ListContains(devices, device) {
			return fmt.Errorf(`cannot specify path %q both in "devices" and "read-devices" attributes`, device)
		}
	}

	allDevices := devices
	allDevices = append(allDevices, readDevices...)

	// validate files
	var filesMap map[string][]string
	mylog.Check(slot.Attr("files", &filesMap))
	if err != nil && !errors.Is(err, snap.AttributeNotFoundError{}) {
		return err
	}
	for key, val := range filesMap {
		switch key {
		case "read":
			mylog.Check(iface.validatePaths("read", val))

		case "write":
			mylog.Check(iface.validatePaths("write", val))

		default:
			return fmt.Errorf(`cannot specify %q in "files" section, only "read" and "write" allowed`, key)
		}
	}

	if len(allDevices) == 0 && len(filesMap) == 0 {
		return fmt.Errorf("cannot use custom-device slot without any files or devices")
	}

	var udevTaggingRules []map[string]interface{}
	mylog.Check(slot.Attr("udev-tagging", &udevTaggingRules))
	if err != nil && !errors.Is(err, snap.AttributeNotFoundError{}) {
		return err
	}
	for _, udevTaggingRule := range udevTaggingRules {
		mylog.Check(iface.validateUDevTaggingRule(udevTaggingRule, allDevices))
	}

	return nil
}

func (iface *customDeviceInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	customDeviceAttr, isSet := plug.Attrs["custom-device"]
	customDevice, ok := customDeviceAttr.(string)
	if isSet && !ok {
		return fmt.Errorf(`custom-device "custom-device" attribute must be a string, not %v`,
			plug.Attrs["custom-device"])
	}
	if customDevice == "" {
		if plug.Attrs == nil {
			plug.Attrs = make(map[string]interface{})
		}
		// custom-device defaults to "plug" name if unspecified
		plug.Attrs["custom-device"] = plug.Name
	}

	return nil
}

func (iface *customDeviceInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	snippet := &bytes.Buffer{}
	emitRule := func(paths []string, permissions string) {
		for _, path := range paths {
			fmt.Fprintf(snippet, "\"%s\" %s,\n", path, permissions)
		}
	}

	// get all attributes without validation, since that was done before;
	// should an error occur, we'll simply not write any rule.

	var devicePaths []string
	_ = slot.Attr("devices", &devicePaths)
	emitRule(devicePaths, "rw")

	var readDevicePaths []string
	_ = slot.Attr("read-devices", &readDevicePaths)
	emitRule(readDevicePaths, "r")

	var filesMap map[string][]string
	mylog.Check(slot.Attr("files", &filesMap))
	if err != nil && !errors.Is(err, snap.AttributeNotFoundError{}) {
		return err
	}
	for key, val := range filesMap {
		perm := ""
		switch key {
		case "read":
			perm = "r"
		case "write":
			perm = "rw"
		default:
			return fmt.Errorf(`cannot specify %q in "files" section, only "read" and "write" allowed`, key)
		}

		emitRule(val, perm)
	}

	spec.AddSnippet(snippet.String())
	return nil
}

// extractStringMapAttribute looks up the given key in the container, and
// returns its value as a map[string]string.
// No validation is performed, since it already occurred before connecting the
// interface.
func (iface *customDeviceInterface) extractStringMapAttribute(container map[string]interface{}, key string) map[string]string {
	valueMap, ok := container[key].(map[string]interface{})
	if !ok {
		return nil
	}

	stringMap := make(map[string]string, len(valueMap))
	for key, value := range valueMap {
		stringMap[key] = value.(string)
	}

	return stringMap
}

func (iface *customDeviceInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// Collect all the device paths specified in either the "devices" or
	// "read-devices" attributes.
	var devicePaths []string
	_ = slot.Attr("devices", &devicePaths)
	var readDevicePaths []string
	_ = slot.Attr("read-devices", &readDevicePaths)
	allDevicePaths := devicePaths
	allDevicePaths = append(allDevicePaths, readDevicePaths...)

	// Create a map in which will store udev rules indexed by device name
	deviceRules := make(map[string]string, len(allDevicePaths))

	const placeholderRule string = "<placeholder>"

	// Generate a placeholder udev rule for each device; we put them into a
	// map indexed by the device name, so that we can overwrite the entry
	// later with a specified rule, or create default rules if no
	// "udev-tagging" rules are explicitly given.
	for _, devicePath := range allDevicePaths {
		if strings.HasPrefix(devicePath, "/dev/") {
			deviceName := devicePath[len("/dev/"):]
			deviceRules[deviceName] = placeholderRule
		}
	}

	// Generate udev rules from the "udev-tagging" attribute; note that these
	// rules might override the simpler KERNEL=="<device>" rules we computed
	// above -- that's fine.
	var udevTaggingRules []map[string]interface{}
	_ = slot.Attr("udev-tagging", &udevTaggingRules)
	for _, udevTaggingRule := range udevTaggingRules {
		rule := &bytes.Buffer{}

		deviceName, ok := udevTaggingRule["kernel"].(string)
		if !ok {
			return customDeviceInternalError
		}

		fmt.Fprintf(rule, `KERNEL=="%s"`, deviceName)

		if subsystem, ok := udevTaggingRule["subsystem"].(string); ok {
			fmt.Fprintf(rule, `, SUBSYSTEM=="%s"`, subsystem)
		}

		environment := iface.extractStringMapAttribute(udevTaggingRule, "environment")
		for variable, value := range environment {
			fmt.Fprintf(rule, `, ENV{%s}=="%s"`, variable, value)
		}

		attributes := iface.extractStringMapAttribute(udevTaggingRule, "attributes")
		for variable, value := range attributes {
			fmt.Fprintf(rule, `, ATTR{%s}=="%s"`, variable, value)
		}

		deviceRules[deviceName] = rule.String()
	}

	// Now write all the rules
	for deviceName, rule := range deviceRules {
		if rule != placeholderRule {
			spec.TagDevice(rule)
			continue
		}

		baseName := filepath.Base(deviceName)

		defaultRule := fmt.Sprintf(`KERNEL=="%s"`, deviceName)
		defaultBaseNameRule := fmt.Sprintf(`KERNEL=="%s"`, baseName)

		if baseName == deviceName {
			spec.TagDevice(defaultRule)
			continue
		}

		baseNameRule, exists := deviceRules[baseName]
		if !exists {
			// There is no rule for the basename, so emit a default
			// rule for both the full path and basename.
			spec.TagDevice(defaultRule)
			spec.TagDevice(defaultBaseNameRule)
			continue
		}

		if baseNameRule != placeholderRule && !strutil.ListContains(allDevicePaths, "/dev/"+baseName) {
			// There is a user-defined rule for the basename of the
			// device path, and there is not a device whose path
			// is /dev/<basename>, so that rule should apply to
			// the device given by this full path. Thus, do not
			// emit a default rule for this device name.
			// validateUDevTaggingRule() already checked that the
			// basename rule only applies to only one device.
			logger.Noticef(`custom-device: applying "udev-tagging" rule with kernel "%s" to device "/dev/%s", since no device with path "/dev/%s"`, baseName, deviceName, baseName)
			continue
		}

		spec.TagDevice(defaultRule)
	}

	return nil
}

func (iface *customDeviceInterface) AutoConnect(plug *snap.PlugInfo, slot *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&customDeviceInterface{})
}
