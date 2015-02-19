//--------------------------------------------------------------------
// Copyright (c) 2014-2015 Canonical Ltd.
//--------------------------------------------------------------------

package partition

const (
	// bootloader variable used to denote which rootfs to boot from
	// FIXME: preferred new name
	// BOOTLOADER_UBOOT_ROOTFS_VAR = "snappy_rootfs_label"
	bootloaderRootfsVar = "snappy_ab"

	// bootloader variable used to determine if boot was successful.
	// Set to 'try' initially, and then changed to 'regular' by the
	// system when the boot reaches the required sequence point.
	bootloaderBootmodeVar = "snappy_mode"

	// Initial and final values
	bootloaderBootmodeTry     = "try"
	bootloaderBootmodeSuccess = "default"
)

type bootloaderName string

type bootLoader interface {
	// Name of the bootloader
	Name() bootloaderName

	// Switch bootloader configuration so that the "other" root
	// filesystem partition will be used on next boot.
	ToggleRootFS() error

	// Hook function called before system-image starts downloading
	// and applying archives that allows files to be copied between
	// partitions.
	SyncBootFiles() error

	// Install any hardware-specific files that system-image
	// downloaded.
	HandleAssets() error

	// Return the value of the specified bootloader variable
	GetBootVar(name string) (string, error)

	// Return the 1-character name corresponding to the
	// rootfs currently being used.
	GetRootFSName() string

	// Return the 1-character name corresponding to the
	// other rootfs.
	GetOtherRootFSName() string

	// Return the 1-character name corresponding to the
	// rootfs that will be used on _next_ boot.
	//
	// XXX: Note the distinction between this method and
	// GetOtherRootFSName(): the latter corresponds to the other
	// partition, whereas the value returned by this method is
	// queried directly from the bootloader.
	GetNextBootRootFSName() (string, error)

	// Update the bootloader configuration to mark the
	// currently-booted rootfs as having booted successfully.
	MarkCurrentBootSuccessful() error

	// Return the additional required chroot bind mounts for this bootloader
	AdditionalBindMounts() []string
}

type bootloaderType struct {
	partition *Partition

	// each rootfs partition has a corresponding u-boot directory named
	// from the last character of the partition name ('a' or 'b').
	currentRootfs string
	otherRootfs   string
}

// Factory method that returns a new bootloader for the given partition
func getBootloader(p *Partition) (bootloader bootLoader, err error) {
	// try uboot
	if uboot := newUboot(p); uboot != nil {
		return uboot, nil
	}

	// no, try grub
	if grub := newGrub(p); grub != nil {
		return grub, nil
	}

	// no, weeeee
	return nil, ErrBootloader
}

func newBootLoader(partition *Partition) *bootloaderType {
	b := new(bootloaderType)

	b.partition = partition

	currentLabel := partition.rootPartition().name

	// FIXME: is this the right thing to do? i.e. what should we do
	//        on a single partition system?
	if partition.otherRootPartition() == nil {
		return nil
	}
	otherLabel := partition.otherRootPartition().name

	b.currentRootfs = string(currentLabel[len(currentLabel)-1])
	b.otherRootfs = string(otherLabel[len(otherLabel)-1])

	return b
}

// Return true if the next boot will use the other rootfs
// partition.
func isNextBootOther(bootloader bootLoader) bool {
	value, err := bootloader.GetBootVar(bootloaderBootmodeVar)
	if err != nil {
		return false
	}

	if value != bootloaderBootmodeTry {
		return false
	}

	fsname, err := bootloader.GetNextBootRootFSName()
	if err != nil {
		return false
	}

	if fsname == bootloader.GetOtherRootFSName() {
		return true
	}

	return false
}
