// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snappy

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/osutil"
)

const udevDataGlob = "/run/udev/data/*"

// return the yaml filename to add to the security yaml
func getHWAccessYamlFile(snapname string) string {
	return filepath.Join(dirs.SnapAppArmorAdditionalDir, fmt.Sprintf("%s.hwaccess.yaml", snapname))
}

// Return true if the device string is a valid device
func validDevice(device string) bool {
	validPrefixes := []string{"/dev/", "/sys/devices/", "/sys/class/gpio/"}

	for _, s := range validPrefixes {
		if strings.HasPrefix(device, s) {
			return true
		}
	}

	return false
}

func validDeviceForUdev(device string) bool {
	validPrefixes := []string{"/dev/"}

	for _, s := range validPrefixes {
		if strings.HasPrefix(device, s) {
			return true
		}
	}

	return false
}

func readHWAccessYamlFile(snapname string) (SecurityOverrideDefinition, error) {
	var appArmorAdditional SecurityOverrideDefinition

	additionalFile := getHWAccessYamlFile(snapname)
	f, err := os.Open(additionalFile)
	if err != nil {
		return appArmorAdditional, err
	}

	content, err := ioutil.ReadAll(f)
	if err != nil {
		return appArmorAdditional, err
	}
	if err := yaml.Unmarshal(content, &appArmorAdditional); err != nil {
		return appArmorAdditional, err
	}

	return appArmorAdditional, nil
}

func writeHWAccessYamlFile(snapname string, appArmorAdditional SecurityOverrideDefinition) error {
	if len(appArmorAdditional.WritePaths) == 0 {
		appArmorAdditional.ReadPaths = nil
	} else {
		appArmorAdditional.ReadPaths = []string{udevDataGlob}
	}
	out, err := yaml.Marshal(appArmorAdditional)
	if err != nil {
		return err
	}

	additionalFile := getHWAccessYamlFile(snapname)
	if !osutil.FileExists(filepath.Dir(additionalFile)) {
		if err := os.MkdirAll(filepath.Dir(additionalFile), 0755); err != nil {
			return err
		}
	}
	if err := osutil.AtomicWriteFile(additionalFile, out, 0640, 0); err != nil {
		return err
	}

	return nil
}

func regenerateAppArmorRulesImpl(snapname string) error {
	err := regeneratePolicyForSnap(snapname)
	if err != nil {
		return err
	}

	return nil
}

func udevRulesPathForSnap(snapName string) string {
	// use 70- here so that its read before the Gadget rules
	return filepath.Join(dirs.SnapUdevRulesDir, fmt.Sprintf("70-snappy_hwassign_%s.rules", snapName))
}

func addUdevRulesForSnap(snapname string, newRules []string) error {
	udevRulesFile := udevRulesPathForSnap(snapname)

	rules, err := ioutil.ReadFile(udevRulesFile)
	if nil != err && !os.IsNotExist(err) {
		return err
	}

	// At this point either rules variable contains some rules if the
	// file exists, or it is nil if the file does not exist yet.
	// In both cases, updatedRules will have the right content.
	for _, newRule := range newRules {
		rules = append(rules, newRule...)
	}
	if err := osutil.AtomicWriteFile(udevRulesFile, rules, 0644, 0); nil != err {
		return err
	}

	return nil
}

func writeUdevRuleForDeviceCgroup(snapname, device string) error {
	os.MkdirAll(dirs.SnapUdevRulesDir, 0755)

	// the device cgroup/launcher etc support only the apps level,
	// not a binary/service or version, so if we get a full
	// appname_binary-or-service_version string we need to split that
	if strings.Contains(snapname, "_") {
		l := strings.Split(snapname, "_")
		snapname = l[0]
	}
	// If there's a dedicated .developer then parse it and use that as the developer
	// to look for in the loop below. In other cases, just ignore developer
	// altogether.
	//
	// NOTE: snapname stays as "$snap.$developer" so that hw-unassign doesn't have
	// to be changed. This is all meant to be removed anyway.
	name := snapname
	developer := ""
	if strings.Contains(snapname, ".") {
		l := strings.Split(snapname, ".")
		name, developer = l[0], l[1]
	}
	devicePath := filepath.Base(device)

	installed, err := (&Overlord{}).Installed()
	if err != nil {
		return err
	}
	var acls []string
	for _, snap := range installed {
		if snap.Name() == name && (developer == "" || snap.Developer() == developer) {
			for _, app := range snap.Apps() {
				acl := fmt.Sprintf(`KERNEL=="%v", TAG:="snappy-assign", ENV{SNAPPY_APP}:="%s"`+"\n",
					devicePath, fmt.Sprintf("%s.%s", snap.Name(), app.Name))
				acls = append(acls, acl)
			}
		}
	}
	sort.Strings(acls)
	if err = addUdevRulesForSnap(snapname, acls); err != nil {
		return err
	}

	return activateGadgetHardwareUdevRules()
}

var regenerateAppArmorRules = regenerateAppArmorRulesImpl

// AddHWAccess allows the given snap package to access the given hardware
// device
func AddHWAccess(snapname, device string) error {
	if !validDevice(device) {
		return ErrInvalidHWDevice
	}

	// LP: #1499087 - ensure that the snapname is not mixed up with
	//                an appid, the "_" is reserved for that
	if strings.Contains(snapname, "_") {
		return ErrPackageNotFound
	}

	// check if there is anything apparmor related to add to
	globExpr := filepath.Join(dirs.SnapAppArmorDir, fmt.Sprintf("%s_*", snapname))
	matches, err := filepath.Glob(globExpr)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		return ErrPackageNotFound
	}

	// read .additional file, its ok if the file does not exist (yet)
	appArmorAdditional, err := readHWAccessYamlFile(snapname)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// check for dupes, please golang make this simpler
	for _, p := range appArmorAdditional.WritePaths {
		if p == device {
			return ErrHWAccessAlreadyAdded
		}
	}
	// add the new write path
	appArmorAdditional.WritePaths = append(appArmorAdditional.WritePaths, device)

	// and write the data out
	err = writeHWAccessYamlFile(snapname, appArmorAdditional)
	if err != nil {
		return err
	}

	// add udev rule for device cgroup
	if validDeviceForUdev(device) {
		if err := writeUdevRuleForDeviceCgroup(snapname, device); err != nil {
			return err
		}
	}

	// re-generate apparmor fules
	return regenerateAppArmorRules(snapname)
}

// ListHWAccess returns a list of hardware-device strings that the snap
// can access
func ListHWAccess(snapname string) ([]string, error) {
	appArmorAdditional, err := readHWAccessYamlFile(snapname)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return appArmorAdditional.WritePaths, nil
}

func removeUdevRuleForSnap(snapname, device string) error {
	udevRulesFile := udevRulesPathForSnap(snapname)

	file, err := os.Open(udevRulesFile)
	if nil != err && !os.IsNotExist(err) {
		return err
	}

	// Get the full list of rules to keep
	var rulesToKeep []string
	scanner := bufio.NewScanner(file)
	devicePattern := "\"" + filepath.Base(device) + "\""

	for scanner.Scan() {
		rule := scanner.Text()
		if "" != rule && !strings.Contains(rule, devicePattern) {
			rulesToKeep = append(rulesToKeep, rule)
		}
	}
	file.Close()

	// Update the file with the remaining rules or delete it
	// if there is not any rule left.
	if 0 < len(rulesToKeep) {
		// Appending the []string list of rules in a single
		// string to convert it later in []byte
		var out string
		for _, rule := range rulesToKeep {
			out = out + rule + "\n"
		}

		if err := osutil.AtomicWriteFile(udevRulesFile, []byte(out), 0644, 0); nil != err {
			return err
		}
	} else {
		if err := os.Remove(udevRulesFile); nil != err {
			return err
		}
	}

	return nil
}

// RemoveHWAccess allows the given snap package to access the given hardware
// device
func RemoveHWAccess(snapname, device string) error {
	if !validDevice(device) {
		return ErrInvalidHWDevice
	}

	appArmorAdditional, err := readHWAccessYamlFile(snapname)
	if err != nil {
		return err
	}

	// remove write path, please golang make this easier!
	newWritePaths := []string{}
	for _, p := range appArmorAdditional.WritePaths {
		if p != device {
			newWritePaths = append(newWritePaths, p)
		}
	}
	if len(newWritePaths) == len(appArmorAdditional.WritePaths) {
		return ErrHWAccessRemoveNotFound
	}
	appArmorAdditional.WritePaths = newWritePaths

	// and write it out again
	err = writeHWAccessYamlFile(snapname, appArmorAdditional)
	if err != nil {
		return err
	}

	if err = removeUdevRuleForSnap(snapname, device); nil != err {
		return err
	}

	if err := activateGadgetHardwareUdevRules(); err != nil {
		return err
	}

	// re-generate apparmor rules
	return regenerateAppArmorRules(snapname)
}

// RemoveAllHWAccess removes all hw access from the given snap.
func RemoveAllHWAccess(snapname string) error {
	for _, fn := range []string{
		udevRulesPathForSnap(snapname),
		getHWAccessYamlFile(snapname),
	} {
		if err := os.Remove(fn); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	return regenerateAppArmorRules(snapname)
}
