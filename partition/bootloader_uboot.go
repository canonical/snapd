//--------------------------------------------------------------------
// Copyright (c) 2014-2015 Canonical Ltd.
//--------------------------------------------------------------------

package partition

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mvo5/goconfigparser"
)

var (
	bootloaderUbootDir        = "/boot/uboot"
	bootloaderUbootConfigFile = "/boot/uboot/uEnv.txt"

	// File created by u-boot itself when
	// bootloaderBootmodeTry == "try" which the
	// successfully booted system must remove to flag to u-boot that
	// this partition is "good".
	bootloaderUbootStampFile = "/boot/uboot/snappy-stamp.txt"

	// the main uEnv.txt u-boot config file sources this snappy
	// boot-specific config file.
	bootloaderUbootEnvFile = "/boot/uboot/snappy-system.txt"
)

const bootloaderNameUboot bootloaderName = "u-boot"

type uboot struct {
	*bootloaderType
}

// Stores a Name and a Value to be added as a name=value pair in a file.
type configFileChange struct {
	Name  string
	Value string
}

// newUboot create a new Grub bootloader object
func newUboot(partition *Partition) bootLoader {
	if !fileExists(bootloaderUbootConfigFile) {
		return nil
	}

	b := newBootLoader(partition)
	if b == nil {
		return nil
	}
	u := uboot{bootloaderType: b}
	u.currentBootPath = path.Join(bootloaderUbootDir, u.currentRootfs)
	u.otherBootPath = path.Join(bootloaderUbootDir, u.otherRootfs)

	return &u
}

func (u *uboot) Name() bootloaderName {
	return bootloaderNameUboot
}

// ToggleRootFS make the U-Boot bootloader switch rootfs's.
//
// Approach:
//
// - Assume the device's installed version of u-boot supports
//   CONFIG_SUPPORT_RAW_INITRD (that allows u-boot to boot a
//   standard initrd+kernel on the fat32 disk partition).
// - Copy the "other" rootfs's kernel+initrd to the boot partition,
//   renaming them in the process to ensure the next boot uses the
//   correct versions.
func (u *uboot) ToggleRootFS() (err error) {

	// If the file exists, update it. Otherwise create it.
	//
	// The file _should_ always exist, but since it's on a writable
	// partition, it's possible the admin removed it by mistake. So
	// recreate to allow the system to boot!
	changes := []configFileChange{
		configFileChange{Name: bootloaderRootfsVar,
			Value: string(u.otherRootfs),
		},
		configFileChange{Name: bootloaderBootmodeVar,
			Value: bootloaderBootmodeTry,
		},
	}

	return modifyNameValueFile(bootloaderUbootEnvFile, changes)
}

func (u *uboot) GetBootVar(name string) (value string, err error) {
	cfg := goconfigparser.New()
	cfg.AllowNoSectionHeader = true
	if err := cfg.ReadFile(bootloaderUbootEnvFile); err != nil {
		return "", nil
	}

	return cfg.Get("", name)
}

func (u *uboot) GetNextBootRootFSName() (label string, err error) {
	value, err := u.GetBootVar(bootloaderRootfsVar)
	if err != nil {
		// should never happen
		return label, err
	}

	return value, err
}

func (u *uboot) GetRootFSName() string {
	return u.currentRootfs
}

func (u *uboot) GetOtherRootFSName() string {
	return u.otherRootfs
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
		if _, err := fmt.Fprintln(writer, line); err != nil {
			return err
		}
	}
	return writer.Flush()
}

// Returns name=value entries from the specified file, removing all
// blank lines and comments.
func getNameValuePairs(file string) (vars []string, err error) {
	lines, err := readLines(file)
	if err != nil {
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

func (u *uboot) MarkCurrentBootSuccessful() (err error) {
	changes := []configFileChange{
		configFileChange{Name: bootloaderBootmodeVar,
			Value: bootloaderBootmodeSuccess,
		},
	}

	if err := modifyNameValueFile(bootloaderUbootEnvFile, changes); err != nil {
		return err
	}

	return os.RemoveAll(bootloaderUbootStampFile)
}

func (u *uboot) SyncBootFiles() (err error) {
	srcDir := u.currentBootPath
	destDir := u.otherBootPath

	// always start from scratch: all files here are owned by us.
	os.RemoveAll(destDir)

	return runCommand("/bin/cp", "-a", srcDir, destDir)
}

func (u *uboot) HandleAssets() (err error) {

	var dirsToRemove map[string]int

	dirsToRemove = make(map[string]int)

	defer func() {
		var dirs []string

		// convert to slice
		for dir := range dirsToRemove {
			dirs = append(dirs, dir)
		}

		// reverse sort to ensure a depth-first approach
		sort.Sort(sort.Reverse(sort.StringSlice(dirs)))

		for _, dir := range dirs {
			if derr := os.RemoveAll(dir); derr != nil {
				// pass error to parent (if none exists already)
				if err == nil {
					err = derr
				}
			}
		}
	}()

	hardware, err := u.partition.hardwareSpec()
	if err != nil {
		return err
	}

	// validate
	if hardware.Bootloader != u.Name() {
		return fmt.Errorf(
			"bootloader is of type %s but hardware spec requires %s",
			u.Name(),
			hardware.Bootloader)
	}

	// validate
	switch hardware.PartitionLayout {
	case "system-AB":
		if !u.partition.dualRootPartitions() {
			return fmt.Errorf(
				"hardware spec requires dual root partitions")
		}
	}

	destDir := u.otherBootPath

	if err := os.MkdirAll(destDir, dirMode); err != nil {
		return err
	}

	// install kernel+initrd
	for _, file := range []string{hardware.Kernel, hardware.Initrd} {

		if file == "" {
			continue
		}

		// expand path
		path := path.Join(u.partition.cacheDir(), file)

		if !fileExists(path) {
			continue
		}

		dir := filepath.Dir(path)
		dirsToRemove[dir] = 1

		if err := runCommand("/bin/cp", file, destDir); err != nil {
			return err
		}
	}

	// install .dtb files
	if fileExists(hardware.DtbDir) {
		dtbDestDir := path.Join(destDir, "dtbs")

		if err := os.MkdirAll(dtbDestDir, dirMode); err != nil {
			return err
		}

		files, err := filepath.Glob(path.Join(hardware.DtbDir, "*"))
		if err != nil {
			return err
		}

		for _, file := range files {
			if err := runCommand("/bin/cp", file, dtbDestDir); err != nil {
				return err
			}
		}
	}

	flashAssetsDir := u.partition.flashAssetsDir()

	if fileExists(flashAssetsDir) {
		// FIXME: we don't currently do anything with the
		// MLO + uImage files since they are not specified in
		// the hardware spec. So for now, just remove them.

		if err := os.RemoveAll(flashAssetsDir); err != nil {
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
	if err := os.Rename(tmpFile, file); err != nil {
		return err
	}

	return nil
}

// Rewrite the specified file, applying the specified set of changes.
// Lines not in the changes slice are left alone.
// If the original file does not contain any of the name entries (from
// the corresponding configFileChange objects), those entries are
// appended to the file.
//
// FIXME: put into utils package
func modifyNameValueFile(file string, changes []configFileChange) (err error) {
	var updated []configFileChange

	lines, err := readLines(file)
	if err != nil {
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
		got := false
		for _, update := range updated {
			if update.Name == change.Name {
				got = true
				break
			}
		}

		if !got {
			// name/value pair did not exist in original
			// file, so append
			lines = append(lines, fmt.Sprintf("%s=%s",
				change.Name, change.Value))
		}
	}

	return atomicFileUpdate(file, lines)
}

func (u *uboot) AdditionalBindMounts() []string {
	// nothing additional to system-boot required on uboot
	return []string{}
}
