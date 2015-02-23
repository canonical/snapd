package snappy

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type appArmorAdditionalJSON struct {
	WritePath []string `json:"write_path"`
}

func getHWAccessJSONFile(snapname string) string {
	return filepath.Join(snapAppArmorDir, fmt.Sprintf("%s.json.additional", snapname))
}

// AddHWAccess allows the given snap package to access the given hardware
// device
func AddHWAccess(snapname, device string) error {
	if !strings.HasPrefix(device, "/dev") && !strings.HasPrefix(device, "/sys/devices") {
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

	// update .additional file
	var appArmorAdditional appArmorAdditionalJSON
	// merge existing file
	additionalFile := getHWAccessJSONFile(snapname)
	if _, err = os.Stat(additionalFile); err == nil {
		f, _ := os.Open(additionalFile)
		dec := json.NewDecoder(f)
		if err := dec.Decode(&appArmorAdditional); err != nil {
			return err
		}
	}

	// add new write path
	appArmorAdditional.WritePath = append(appArmorAdditional.WritePath, device)
	out, err := json.MarshalIndent(appArmorAdditional, "", "  ")
	if err != nil {
		return err
	}

	// and write it out
	if err := ioutil.WriteFile(additionalFile, out, 0640); err != nil {
		return err
	}

	// re-generate apparmor fules
	cmd := exec.Command(aaClickHookCmd, "-f")
	return cmd.Run()
}

// ListHWAccess returns a list of hardware-device strings that the snap
// can access
func ListHWAccess(snapname string) ([]string, error) {

	var appArmorAdditional appArmorAdditionalJSON
	additionalFile := getHWAccessJSONFile(snapname)
	f, err := os.Open(additionalFile)
	if err != nil {
		return []string{}, err
	}

	dec := json.NewDecoder(f)
	if err := dec.Decode(&appArmorAdditional); err != nil {
		return []string{}, err
	}

	return appArmorAdditional.WritePath, nil
}

// RemoveHWAccess allows the given snap package to access the given hardware
// device
func RemoveHWAccess(snapname, device string) error {
	if !strings.HasPrefix(device, "/dev") && !strings.HasPrefix(device, "/sys/devices") {
		return ErrInvalidHWDevice
	}

	// update .additional file
	additionalFile := getHWAccessJSONFile(snapname)
	if _, err := os.Stat(additionalFile); err != nil {
		return err
	}

	f, _ := os.Open(additionalFile)
	dec := json.NewDecoder(f)
	var appArmorAdditional appArmorAdditionalJSON
	if err := dec.Decode(&appArmorAdditional); err != nil {
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
		return fmt.Errorf("Can not find '%s' access for '%s'", device, snapname)
	}
	appArmorAdditional.WritePath = newWritePath

	// and write it out again
	out, err := json.MarshalIndent(appArmorAdditional, "", "  ")
	if err != nil {
		return err
	}

	// and write it out
	if err := ioutil.WriteFile(additionalFile, out, 0640); err != nil {
		return err
	}

	// re-generate apparmor fules
	cmd := exec.Command(aaClickHookCmd, "-f")
	return cmd.Run()
}
