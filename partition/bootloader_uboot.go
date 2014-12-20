package partition

import (
	"fmt"
	"strings"
	"os"
	"bufio"
	"path/filepath"
	"errors"
)

type Uboot struct {
	partition *Partition
}

func (u *Uboot) Name() string {
	return "u-boot"
}

func (u *Uboot) Installed() bool {
	// crude heuristic
	err := FileExists("/boot/uEnv.txt")

	if err == nil {
		return true
	}

	return false
}

// Make the U-Boot bootloader switch rootfs's.
//
// Approach:
//
// - Assume the device's installed version of u-boot supports
//   CONFIG_SUPPORT_RAW_INITRD (that allows u-boot to boot a
//   standard initrd+kernel on the fat32 disk partition).
// - Copy the "other" rootfs's kernel+initrd to the boot partition,
//   renaming them in the process to ensure the next boot uses the
//   correct versions.
func (u *Uboot) ToggleRootFS() (err error) {

	// the main uEnv.txt u-boot config file sources this snappy
	// boot-specific config file.
	snappy_uenv_file := "snappy-system.txt"

	// u-boot variable used to denote which rootfs to boot from
	// FIXME: preferred new name
	// rootfs_var := "snappy_rootfs_label"
	rootfsVar := "snappy_ab"

	files, err := filepath.Glob(fmt.Sprintf("%s/boot/vmlinuz-*",
		u.partition.MountTarget))
	if err != nil {
		return err
	}

	if len(files) < 1 {
		return errors.New("Failed to find kernel")
	}

	kernel := files[0]

	files, err = filepath.Glob(fmt.Sprintf("%s/boot/initrd.img-*",
		u.partition.MountTarget))
	if err != nil {
		return err
	}

	if len(files) < 1 {
		return errors.New("Failed to find initrd")
	}

	initrd := files[0]

	other := u.partition.OtherRootPartition()
	label := other.name

	// FIXME: current naming scheme
	dir := string(label[len(label)-1])
	// FIXME: preferred naming scheme
	//dir := label

	bootDir := fmt.Sprintf("/boot/%s", dir)

	if err = os.MkdirAll(bootDir, DIR_MODE); err != nil {
		return err
	}

	uenvPath := fmt.Sprintf("/boot/%s", snappy_uenv_file)
	kernelDest := fmt.Sprintf("%s/vmlinuz", bootDir)
	initrdDest := fmt.Sprintf("%s/initrd.img", bootDir)

	// install the kernel into the boot partition
	if err = RunCommand([]string{"/bin/cp", kernel, kernelDest}); err != nil {
		return err
	}

	// install the initramfs into the boot partition
	if err = RunCommand([]string{"/bin/cp", initrd, initrdDest}); err != nil {
		return err
	}

	var lines []string

	err = FileExists(uenvPath)

	name := rootfsVar

	// FIXME: current
	value := dir
	// FIXME: preferred
	//value := label

	// If the file exists, update it. Otherwise create it.
	// The file _should_ always exist, but since it's on a writable
	// partition, it's possible the admin removed it by mistake. So
	// recreate to allow the system to boot!
	if err == nil {
		if lines, err = readLines(uenvPath); err != nil {
			return err
		}

		var new []string

		// update the u-boot configuration. Note that we only
		// change the line we care about. Remember - this file
		// is writable so might contain comments added by the
		// admin, etc.
		for _, line := range lines {
			if strings.HasPrefix(line, rootfsVar) {
				// toggle
				line = fmt.Sprintf("%s=%s", name, value)
			}

			new = append(new, line)
		}

		lines = new

	} else {
		line := fmt.Sprintf("%s=%s", name, value)
		lines = append(lines, line)
	}

	tmpFile := fmt.Sprintf("%s.NEW", uenvPath)

	if err := writeLines(lines, tmpFile); err != nil {
		return err
	}

	// atomic update
	if err = os.Rename(tmpFile, uenvPath); err != nil {
		return err
	}

	return err
}

func (u *Uboot) GetAllBootVars() (vars []string, err error) {
	// FIXME
	return vars, err
}

func (u *Uboot) GetBootVar(name string) (value string) {
	// FIXME
	return value
}

func (u *Uboot) SetBootVar(name, value string) (err error) {
	// FIXME
	return err
}

func (u *Uboot) ClearBootVar(name string) (currentValue string, err error) {
	// FIXME
	return currentValue, err
}

func (u *Uboot) GetNextBootRootLabel() (label string) {
	// FIXME
	return label
}

func (u *Uboot) GetCurrentBootRootLabel() (label string) {
	// FIXME
	return label
}

func readLines(path string) (lines []string, err error) {

	file, err := os.Open(path)

	if err != nil {
		return nil, err
	}

	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	return lines, scanner.Err()
}

func writeLines(lines []string, path string) (err error) {

	file, err := os.Create(path);

	if err != nil {
		return err
	}

	defer file.Close()

	writer := bufio.NewWriter(file)

	for _, line := range lines {
		if _, err = fmt.Fprintln(writer, line); err != nil {
			return err
		}
	}
	return writer.Flush()
}
