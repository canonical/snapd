package snappy

import (
	"fmt"
	"io/ioutil"
	"os/exec"
	"path/filepath"
)

func makeAdditionalJSON(device string) string {
	s := fmt.Sprintf(`{
  "write_path": [
    "%s"
  ]
}`, device)
	return s
}

func AddHWAccess(snapname, device string) error {
	globExpr := filepath.Join(snapAppArmorDir, fmt.Sprintf("%s_*.json", snapname))
	matches, err := filepath.Glob(globExpr)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		return ErrPackageNotFound
	}

	for _, match := range matches {
		additionalFile := match + ".additional"
		if err := ioutil.WriteFile(additionalFile, []byte(makeAdditionalJSON(device)), 0640); err != nil {
			return err
		}
	}

	// re-generate apparmor fules
	cmd := exec.Command(aaClickHookCmd, "-f")
	return cmd.Run()
}
