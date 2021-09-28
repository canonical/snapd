package disks

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// Size returns the size of the given block device, e.g. /dev/sda1 in
// bytes as reported by the kernels BLKGETSIZE ioctl.
func Size(partDevice string) (uint64, error) {
	fp, err := os.Open(partDevice)
	if err != nil {
		return 0, fmt.Errorf("cannot open disk to get size: %v", err)
	}
	defer fp.Close()

	partSize, err := unix.IoctlGetInt(int(fp.Fd()), unix.BLKGETSIZE64)
	if err != nil {
		return 0, err
	}

	return uint64(partSize), nil
}
