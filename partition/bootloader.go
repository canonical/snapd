package partition

const (
	BOOTLOADER_TYPE_GRUB = iota
	BOOTLOADER_TYPE_UBOOT
	bootloaderTypeCount
)

type BootLoader interface {
	// Name of the bootloader
	Name() string

	// Returns true if the bootloader type is installed
	Installed() bool

	// Switch bootloader configuration so that the "other" root
	// filesystem partitions will be used on next boot.
	ToggleRootFS() error

	// Retrieve a list of all bootloader name=value pairs set
	// by this program.
	GetAllBootVars() ([]string, error)

	// Return the value of the specified bootloader variable, or "" if
	// not set.
	GetBootVar(name string) (string, error)

	// Set the variable specified by name to the given value
	SetBootVar(name, value string) (error)

	// Remove the specified variable
	ClearBootVar(name string) (currentValue string, err error)

	// Return the name of the partition label corresponding to the
	// rootfs that will be used on next boot.
	GetNextBootRootLabel() (string)

	// Return the name of the partition label for the currently booted
	// root filesystem.
	GetCurrentBootRootLabel() (string)
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
