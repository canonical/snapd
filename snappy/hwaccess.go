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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"launchpad.net/snappy/helpers"
)

const udevDataGlob = "/run/udev/data/*"

var aaClickHookCmd = "aa-clickhook"

type appArmorAdditionalJSON struct {
	WritePath []string `json:"write_path,omitempty"`
	ReadPath  []string `json:"read_path,omitempty"`
}

// return the json filename to add to the security json
func getHWAccessJSONFile(snapname string) string {
	return filepath.Join(snapAppArmorDir, fmt.Sprintf("%s.json.additional", snapname))
}

// Return true if the device string is a valid device
func validDevice(device string) bool {
	validPrefixes := []string{"/dev", "/sys/devices", "/sys/class/gpio"}

	for _, s := range validPrefixes {
		if strings.HasPrefix(device, s) {
			return true
		}
	}

	return false
}

func readHWAccessJSONFile(snapname string) (appArmorAdditionalJSON, error) {
	var appArmorAdditional appArmorAdditionalJSON

	additionalFile := getHWAccessJSONFile(snapname)
	f, err := os.Open(additionalFile)
	if err != nil {
		return appArmorAdditional, err
	}

	dec := json.NewDecoder(f)
	if err := dec.Decode(&appArmorAdditional); err != nil {
		return appArmorAdditional, err
	}

	return appArmorAdditional, nil
}

func writeHWAccessJSONFile(snapname string, appArmorAdditional appArmorAdditionalJSON) error {
	if len(appArmorAdditional.WritePath) == 0 {
		appArmorAdditional.ReadPath = nil
	} else {
		appArmorAdditional.ReadPath = []string{udevDataGlob}
	}
	out, err := json.MarshalIndent(appArmorAdditional, "", "  ")
	if err != nil {
		return err
	}
	// append final newline
	out = append(out, '\n')

	additionalFile := getHWAccessJSONFile(snapname)
	if err := helpers.AtomicWriteFile(additionalFile, out, 0640); err != nil {
		return err
	}

	return nil
}

func regenerateAppArmorRulesImpl() error {
	if output, err := exec.Command(aaClickHookCmd, "-f").CombinedOutput(); err != nil {
		if exitCode, err := helpers.ExitCode(err); err == nil {
			return &ErrApparmorGenerate{
				ExitCode: exitCode,
				Output:   output,
			}
		}
		return err
	}

	return nil
}

func udevRulesPathForPart(partid string) string {
	// use 70- here so that its read before the OEM rules
	return filepath.Join(snapUdevRulesDir, fmt.Sprintf("70-snappy_hwassign_%s.rules", partid))
}

func addUdevRuleForSnap(snapname, newRule string) error {
	udevRulesFile := udevRulesPathForPart(snapname)

	rules, err := ioutil.ReadFile(udevRulesFile)
	if nil != err && !os.IsNotExist(err) {
		return err
	}

	// At this point either rules variable contains some rules if the
	// file exists, or it is nil if the file does not exist yet.
	// In both cases, updatedRules will have the right content.
	updatedRules := append(rules, newRule...)

	if err := helpers.AtomicWriteFile(udevRulesFile, updatedRules, 0644); nil != err {
		return err
	}

	return nil
}

func writeUdevRuleForDeviceCgroup(snapname, device string) error {
	os.MkdirAll(snapUdevRulesDir, 0755)

	// the device cgroup/launcher etc support only the apps level,
	// not a binary/service or version, so if we get a full
	// appname_binary-or-service_version string we need to split that
	if strings.Contains(snapname, "_") {
		l := strings.Split(snapname, "_")
		snapname = l[0]
	}

	acl := fmt.Sprintf(`
KERNEL=="%v", TAG:="snappy-assign", ENV{SNAPPY_APP}:="%s"
`, filepath.Base(device), snapname)

	if err := addUdevRuleForSnap(snapname, acl); err != nil {
		return err
	}

	return activateOemHardwareUdevRules()
}

var regenerateAppArmorRules = regenerateAppArmorRulesImpl

// AddHWAccess allows the given snap package to access the given hardware
// device
func AddHWAccess(snapname, device string) error {
	if !validDevice(device) {
		return ErrInvalidHWDevice
	}

	// check if there is anything apparmor related to add to
	globExpr := filepath.Join(snapAppArmorDir, fmt.Sprintf("%s_*.json", snapname))
	matches, err := filepath.Glob(globExpr)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		return ErrPackageNotFound
	}

	// read .additional file, its ok if the file does not exist (yet)
	appArmorAdditional, err := readHWAccessJSONFile(snapname)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// check for dupes, please golang make this simpler
	for _, p := range appArmorAdditional.WritePath {
		if p == device {
			return ErrHWAccessAlreadyAdded
		}
	}
	// add the new write path
	appArmorAdditional.WritePath = append(appArmorAdditional.WritePath, device)

	// and write the data out
	err = writeHWAccessJSONFile(snapname, appArmorAdditional)
	if err != nil {
		return err
	}

	// add udev rule for device cgroup
	if err := writeUdevRuleForDeviceCgroup(snapname, device); err != nil {
		return err
	}

	// re-generate apparmor fules
	return regenerateAppArmorRules()
}

// ListHWAccess returns a list of hardware-device strings that the snap
// can access
func ListHWAccess(snapname string) ([]string, error) {
	appArmorAdditional, err := readHWAccessJSONFile(snapname)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return appArmorAdditional.WritePath, nil
}

func removeUdevRuleForSnap(snapname, device string) error {
	udevRulesFile := udevRulesPathForPart(snapname)

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

		if err := helpers.AtomicWriteFile(udevRulesFile, []byte(out), 0644); nil != err {
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

	appArmorAdditional, err := readHWAccessJSONFile(snapname)
	if err != nil {
		return err
	}

	// remove write path, please golang make this easier!
	newWritePath := []string{}
	for _, p := range appArmorAdditional.WritePath {
		if p != device {
			newWritePath = append(newWritePath, p)
		}
	}
	if len(newWritePath) == len(appArmorAdditional.WritePath) {
		return ErrHWAccessRemoveNotFound
	}
	appArmorAdditional.WritePath = newWritePath

	// and write it out again
	err = writeHWAccessJSONFile(snapname, appArmorAdditional)
	if err != nil {
		return err
	}

	if err = removeUdevRuleForSnap(snapname, device); nil != err {
		return err
	}

	if err := activateOemHardwareUdevRules(); err != nil {
		return err
	}

	// re-generate apparmor rules
	return regenerateAppArmorRules()
}

// RemoveAllHWAccess removes all hw access from the given snap.
func RemoveAllHWAccess(snapname string) error {
	for _, fn := range []string{
		udevRulesPathForPart(snapname),
		getHWAccessJSONFile(snapname),
	} {
		if err := os.Remove(fn); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	return regenerateAppArmorRules()
}
