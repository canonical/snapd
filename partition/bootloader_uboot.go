package partition

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	BOOTLOADER_UBOOT_CONFIG_FILE = "/boot/uEnv.txt"

	// the main uEnv.txt u-boot config file sources this snappy
	// boot-specific config file.
	BOOTLOADER_UBOOT_ENV_FILE = "snappy-system.txt"
)

type Uboot struct {
	partition *Partition
}

// Stores a Name and a Value to be added as a name=value pair in a file.
type ConfigFileChange struct {
	Name     string
	Value    string
}

func (u *Uboot) Name() string {
	return "u-boot"
}

func (u *Uboot) Installed() bool {
	// crude heuristic
	err := FileExists(BOOTLOADER_UBOOT_CONFIG_FILE)

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

	var kernel       string
	var initrd       string

	if kernel, err = u.getKernel(); err != nil {
		return err
	}

	if initrd, err = u.getInitrd(); err != nil {
		return err
	}

	other := u.partition.otherRootPartition()
	label := other.name

	// FIXME: current naming scheme
	dir := string(label[len(label)-1])
	// FIXME: preferred naming scheme
	//dir := label

	bootDir := fmt.Sprintf("/boot/%s", dir)

	if err = os.MkdirAll(bootDir, DIR_MODE); err != nil {
		return err
	}

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

	// FIXME: current
	value := dir
	// FIXME: preferred
	//value := label

	// If the file exists, update it. Otherwise create it.
	//
	// The file _should_ always exist, but since it's on a writable
	// partition, it's possible the admin removed it by mistake. So
	// recreate to allow the system to boot!
	changes := []ConfigFileChange{
		ConfigFileChange{Name: BOOTLOADER_ROOTFS_VAR,
				  Value: value,
		},
		ConfigFileChange{Name: BOOTLOADER_BOOTMODE_VAR,
				  Value: BOOTLOADER_BOOTMODE_VAR_START_VALUE,
		},
	}

	return modifyNameValueFile(BOOTLOADER_UBOOT_ENV_FILE, changes)
}

func (u *Uboot) GetAllBootVars() (vars []string, err error) {
	return getNameValuePairs(BOOTLOADER_UBOOT_ENV_FILE)
}

func (u *Uboot) GetBootVar(name string) (value string, err error) {
	var vars []string

	vars, err = u.GetAllBootVars()

	if err != nil {
		return value, err
	}

	for _, pair := range vars {
		fields := strings.Split(string(pair), "=")

		if fields[0] == name {
			return fields[1], err
		}
	}

	return value, err
}

func (u *Uboot) SetBootVar(name, value string) (err error) {
	var lines []string

	if lines, err = readLines(BOOTLOADER_UBOOT_ENV_FILE); err != nil {
		return err
	}

	new := fmt.Sprintf("%s=%s", name, value)
	lines = append(lines, new)

	// Rewrite the file
	return atomicFileUpdate(BOOTLOADER_UBOOT_ENV_FILE, lines)
}

func (u *Uboot) ClearBootVar(name string) (currentValue string, err error) {
	var saved []string
	var lines []string

	// XXX: note that we do not call GetAllBootVars() since that
	// strips all comments (which we want to retain).
	if lines, err = readLines(BOOTLOADER_UBOOT_ENV_FILE); err != nil {
		return currentValue, err
	}

	for _, line := range lines {
		fields := strings.Split(string(line), "=")
		if fields[0] == name {
			currentValue = fields[1]
		} else {
			saved = append(saved, line)
		}
	}

	// Rewrite the file, excluding the name to clear
	return currentValue, atomicFileUpdate(BOOTLOADER_UBOOT_ENV_FILE, saved)
}

func (u *Uboot) GetNextBootRootLabel() (label string, err error) {
	var value string

	if value, err = u.GetBootVar(BOOTLOADER_ROOTFS_VAR); err != nil {
		// should never happen
		return label, err
	}

	return value, err
}

// FIXME: put into utils package
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

// FIXME: put into utils package
func writeLines(lines []string, path string) (err error) {

	file, err := os.Create(path)

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

// Returns name=value entries from the specified file, removing all
// blank lines and comments.
func getNameValuePairs(file string) (vars []string, err error) {
	var lines []string

	if lines, err = readLines(file); err != nil {
		return vars, err
	}

	for _, line := range lines {
		// ignore blank lines
		if line == "" || line == "\n" {
			continue
		}

		// ignore comment lines
		if strings.HasPrefix(line, "#") {
			continue
		}

		if strings.Index(line, "=") != -1 {
			vars = append(vars, line)
		}
	}

	return vars, err
}

// Returns full path to kernel on the other partition
func (u *Uboot) getKernel() (path string, err error) {

	files, err := filepath.Glob(fmt.Sprintf("%s/boot/vmlinuz-*",
		u.partition.MountTarget))

	if err != nil {
		return path, err
	}

	if len(files) < 1 {
		return path, errors.New("Failed to find kernel")
	}

	path = files[0]

	return path, err
}

// Returns full path to initrd / initramfs on the other partition
func (u *Uboot) getInitrd() (path string, err error) {

	files, err := filepath.Glob(fmt.Sprintf("%s/boot/initrd.img-*",
		u.partition.MountTarget))

	if err != nil {
		return path, err
	}

	if len(files) < 1 {
		return path, errors.New("Failed to find initrd")
	}

	path = files[0]

	return path, err
}

func (u *Uboot) MarkCurrentBootSuccessful() (err error) {
	changes := []ConfigFileChange{
		ConfigFileChange{Name: BOOTLOADER_BOOTMODE_VAR,
			Value: BOOTLOADER_BOOTMODE_VAR_END_VALUE,
		},
	}

	return modifyNameValueFile(BOOTLOADER_UBOOT_ENV_FILE, changes)
}

// Write lines to file atomically. File does not have to preexist.
// FIXME: put into utils package
func atomicFileUpdate(file string, lines []string) (err error) {
	tmpFile := fmt.Sprintf("%s.NEW", file)

	if err := writeLines(lines, tmpFile); err != nil {
		return err
	}

	// atomic update
	if err = os.Rename(tmpFile, file); err != nil {
		return err
	}

	return err
}

// Rewrite the specified file, applying the specified set of changes.
// Lines not in the changes slice are left alone.
// If the original file does not contain any of the name entries (from
// the corresponding ConfigFileChange objects), those entries are
// appended to the file.
//
// FIXME: put into utils package
func modifyNameValueFile(file string, changes []ConfigFileChange) (err error) {
	var lines []string
	var updated []ConfigFileChange

	if lines, err = readLines(file); err != nil {
		return err
	}

	var new []string

	for _, line := range lines {
		for _, change := range changes {
			if strings.HasPrefix(line, fmt.Sprintf("%s=", change.Name)) {
				line = fmt.Sprintf("%s=%s", change.Name, change.Value)
				updated = append(updated, change)
			}
		}
		new = append(new, line)
	}

	lines = new

	for _, change := range changes {
		var got bool = false
		for _, update := range updated {
			if update.Name == change.Name {
				got = true
				break
			}
		}

		if got == false {
			// name/value pair did not exist in original
			// file, so append
			lines = append(lines, fmt.Sprintf("%s=%s",
				       change.Name, change.Value))
		}
	}

	return atomicFileUpdate(file, lines)
}
