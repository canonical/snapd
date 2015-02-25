package snappy

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

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

	additionalFile := getHWAccessJSONFile(snapname)
	if err := atomicWriteFile(additionalFile, out, 0640); err != nil {
		return err
	}

	return nil
}

func regenerateAppArmorRulesImpl() error {
	return exec.Command(aaClickHookCmd, "-f").Run()
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
			newWritePath = append(newWritePath, device)
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
