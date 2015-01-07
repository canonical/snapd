package partition

const (
	BOOTLOADER_TYPE_GRUB = iota
	BOOTLOADER_TYPE_UBOOT
	bootloaderTypeCount
)

const (
	// bootloader variable used to denote which rootfs to boot from
	// FIXME: preferred new name
	// BOOTLOADER_UBOOT_ROOTFS_VAR = "snappy_rootfs_label"
	BOOTLOADER_ROOTFS_VAR = "snappy_ab"

	// bootloader variable used to determine if boot was successful.
	// Set to 'try' initially, and then changed to 'regular' by the
	// system when the boot reaches the required sequence point.
	BOOTLOADER_BOOTMODE_VAR = "snappy_mode"

	// Initial and final values
	BOOTLOADER_BOOTMODE_VAR_START_VALUE = "try"
	BOOTLOADER_BOOTMODE_VAR_END_VALUE   = "default"
)

type BootLoader interface {
	// Name of the bootloader
	Name() string

	// Returns true if the bootloader type is installed
	Installed() bool

	// Switch bootloader configuration so that the "other" root
	// filesystem partition will be used on next boot.
	ToggleRootFS() error

	// Retrieve a list of all bootloader name=value pairs set
	// by this program.
	GetAllBootVars() ([]string, error)

	// Return the value of the specified bootloader variable, or "" if
	// not set.
	GetBootVar(name string) (string, error)

	// Set the variable specified by name to the given value
	SetBootVar(name, value string) error

	// Remove the specified variable
	ClearBootVar(name string) (currentValue string, err error)

	// Return the name of the partition label corresponding to the
	// rootfs that will be used on _next_ boot. Note that there is
	// no corresponding GetCurrentBootRootLabel() since that is
	// handled via BlockDevice.
	GetNextBootRootLabel() (string, error)

	// Update the bootloader configuration to mark the
	// currently-booted rootfs as having booted successfully.
	MarkCurrentBootSuccessful() error
}

func DetermineBootLoader(p *Partition) BootLoader {

	bootloaders := []BootLoader{&Uboot{p}, &Grub{p}}

	for _, b := range bootloaders {
		if b.Installed() == true {
			return b
		}
	}

	return nil
}
