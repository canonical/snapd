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
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"launchpad.net/snappy/helpers"
)

var aaClickHookCmd = "aa-clickhook"

type appArmorAdditionalJSON struct {
	WritePath []string `json:"write_path"`
}

// return the json filename to add to the security json
func getHWAccessJSONFile(snapname string) string {
	return filepath.Join(snapAppArmorDir, fmt.Sprintf("%s.json.additional", snapname))
}

// Return true if the device string is a valid device
func validDevice(device string) bool {
	return strings.HasPrefix(device, "/dev") || strings.HasPrefix(device, "/sys/devices")
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
	if err := exec.Command(aaClickHookCmd, "-f").Run(); err != nil {
		if exitCode, err := helpers.ExitCode(err); err != nil {
			return &ErrHookFailed{
				cmd:      aaClickHookCmd,
				exitCode: exitCode,
			}
		}
		return err
	}

	return nil
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

	// re-generate apparmor fules
	return regenerateAppArmorRules()
}
