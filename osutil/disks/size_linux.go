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

	// kernel 5.14.8 source says:
	// #define BLKGETSIZE _IO(0x12,96)	/* return device size /512 (long *arg) */
	partBlocks, err := unix.IoctlGetInt(int(fp.Fd()), unix.BLKGETSIZE)
	if err != nil {
		return 0, err
	}

	return uint64(partBlocks * 512), nil
}
