package disks

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/osutil"
)

// Size returns the size of the given block device, e.g. /dev/sda1 in
// bytes as reported by the kernels BLKGETSIZE ioctl.
func Size(partDevice string) (uint64, error) {
	// This code is not using the ioctl unix.BLKGETSIZE64 because
	// on 32bit systems it's a pain to get a 64bit value from a ioctl.
	raw, err := exec.Command("blockdev", "--getsz", partDevice).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("cannot get disk size: %v", osutil.OutputErr(raw, err))
	}
	output := strings.TrimSpace(string(raw))
	partBlocks, err := strconv.ParseUint(output, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("cannot parse disk size output: %v", err)
	}

	return uint64(partBlocks) * 512, nil
}
