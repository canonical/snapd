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
	BOOTLOADER_UBOOT_DIR         = "/boot/uboot"
	BOOTLOADER_UBOOT_CONFIG_FILE = "/boot/uboot/uEnv.txt"

	// File created by u-boot itself when
	// BOOTLOADER_BOOTMODE_VAR_START_VALUE == "try" which the
	// successfully booted system must remove to flag to u-boot that
	// this partition is "good".
	BOOTLOADER_UBOOT_STAMP_FILE = "/boot/uboot/snappy-stamp.txt"

	// the main uEnv.txt u-boot config file sources this snappy
	// boot-specific config file.
	BOOTLOADER_UBOOT_ENV_FILE = "snappy-system.txt"

)

type Uboot struct {
	partition       *Partition
	currentLabel     string
	otherLabel       string
	currentBootPath  string
	otherBootPath    string
}

// Stores a Name and a Value to be added as a name=value pair in a file.
type ConfigFileChange struct {
	Name     string
	Value    string
}

// Create a new Grub bootloader object
func NewUboot(partition *Partition) *Uboot {
	u := new(Uboot)
	u.partition = partition

	current := u.partition.rootPartition()
	other   := u.partition.otherRootPartition()

	u.currentLabel = current.name
	u.otherLabel   = other.name

	// each rootfs partition has a corresponding u-boot directory named
	// from the last character of the partition name ('a' or 'b').
	currentPartition := u.currentLabel[len(u.currentLabel) - 1]
	otherPartition   := u.otherLabel[len(u.otherLabel) - 1]

	u.currentBootPath = fmt.Sprintf("%s/%s",
		BOOTLOADER_UBOOT_DIR, currentPartition)

	u.otherBootPath = fmt.Sprintf("%s/%s",
		BOOTLOADER_UBOOT_DIR, otherPartition)

	return u
}

func (u *Uboot) Name() string {
	return "uboot"
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

	var kernel string
	var initrd string

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

	bootDir := fmt.Sprintf("%s/%s", BOOTLOADER_UBOOT_DIR, dir)

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

	err = modifyNameValueFile(BOOTLOADER_UBOOT_ENV_FILE, changes)
	if err != nil {
		return err
	}

	return os.RemoveAll(BOOTLOADER_UBOOT_STAMP_FILE)
}

func (u *Uboot) SyncBootFiles() (err error) {
	srcDir := u.currentBootPath
	destDir := u.otherBootPath

	// always start from scratch: all files here are owned by us.
	os.RemoveAll(destDir)

	return RunCommand([]string{"/bin/cp", "-a", srcDir, destDir})
}

func (u *Uboot) HandleAssets() (err error) {

	assetsDir := u.partition.assetsDir()
	flashAssetsDir := u.partition.flashAssetsDir()

	destDir := u.otherBootPath

	err = os.MkdirAll(destDir, DIR_MODE)
	if err != nil {
		return err
	}

	if err = FileExists(assetsDir); err == nil {

		kernel := fmt.Sprintf("%s/vmlinuz", assetsDir)
		initrd := fmt.Sprintf("%s/initrd.img", assetsDir)
		dtbDir := fmt.Sprintf("%s/dtbs/", assetsDir)

		// install kernel+initrd
		for _, file := range []string{kernel, initrd} {
			if err = FileExists(file); err != nil {
				continue
			}
			err = RunCommand([]string{"/bin/cp", file, destDir})
			if err != nil {
				return err
			}
		}

		// install .dtb files
		if err = FileExists(dtbDir); err == nil {
			dtbDestDir := fmt.Sprintf("%s/dtbs", destDir)

			err = os.MkdirAll(dtbDestDir, DIR_MODE)
			if err != nil {
				return err
			}

			files, err := filepath.Glob(fmt.Sprintf("%s/*", dtbDir))
			if err != nil {
				return err
			}

			for _, file := range files {
				err = RunCommand([]string{"/bin/cp", file, dtbDestDir})
				if err != nil {
					return err
				}
			}
		}

		// remove the original unpack directory
		if err = os.RemoveAll(assetsDir); err != nil {
			return err
		}
	}

	if err = FileExists(flashAssetsDir); err == nil {
		// FIXME: we don't currently do anything with the
		// MLO + uImage files yet, but we should!!

		err = os.RemoveAll(flashAssetsDir)
		if err != nil {
			return err
		}
	}

	return err
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
