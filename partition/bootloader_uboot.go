package partition

import (
	"fmt"
	"strings"
	"os"
	"os/exec"
	"bufio"
	"path/filepath"
)

type UbootBootLoader struct {
	partition *Partition
}

func (u *UbootBootLoader) Name() string {
	return "u-boot"
}

func (u *UbootBootLoader) Installed() bool {
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
func (u *UbootBootLoader) ToggleRootFS(p *Partition) (err error) {

	// save
	u.partition = p

	// the main uEnv.txt u-boot config file sources this snappy
	// boot-specific config file.
	snappy_uenv_file := "snappy-system.txt"

	// u-boot variable used to denote which rootfs to boot from
	// FIXME: preferred new name
	// rootfs_var := "snappy_rootfs_label"
	rootfsVar := "snappy_ab"

	files, err := filepath.Glob(fmt.Sprintf("%s/boot/vmlinuz-*", p.MountTarget))
	if err != nil {
		return err
	}

	kernel := files[0]

	files, err = filepath.Glob(fmt.Sprintf("%s/boot/initrd.img-*", p.MountTarget))
	if err != nil {
		return err
	}

	initrd := files[0]

	other := p.OtherRootPartition()
	label := other.name
	dir := label[len(label)-1]

	bootDir := fmt.Sprintf("/boot/%s", dir)

	uenvPath := fmt.Sprintf("%s/%s", bootDir, snappy_uenv_file)
	kernelDest := fmt.Sprintf("%s/vmlinuz", bootDir)
	initrdDest := fmt.Sprintf("%s/initrd.img", bootDir)

	// install the kernel into the boot partition
	cmd := exec.Command("/bin/cp", kernel, kernelDest)
	if err = cmd.Run(); err != nil {
		return err
	}

	// install the initramfs into the boot partition
	cmd = exec.Command("/bin/cp", initrd, initrdDest)
	if err = cmd.Run(); err != nil {
		return err
	}

	var lines []string

	err = FileExists(uenvPath)

    // If the file exists, update it. Otherwise create it.
    // The file _should_ always exist, but since it's on a writable
    // partition, it's possible the admin removed it by mistake. So
    // recreate to allow the system to boot!
    if err == nil {

        if lines, err = readLines(uenvPath); err != nil {
            return err
        }

        var new []string

        // update the u-boot configuration
        for _, line := range lines {
            if strings.HasPrefix(line, rootfsVar) {
                // toggle
                line = fmt.Sprintf("%s=%s", rootfsVar, label)
            }

            new = append(new, line)
        }

        lines = new

    } else {
        line := fmt.Sprintf("%s=%s", rootfsVar, label)
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

func (u *UbootBootLoader) GetAllBootVars() (vars []string, err error) {
	// FIXME
	return vars, err
}

func (u *UbootBootLoader) GetBootVar(name string) (value string) {
	// FIXME
	return value
}

func (u *UbootBootLoader) SetBootVar(name, value string) (err error) {
	// FIXME
	return err
}

func (u *UbootBootLoader) ClearBootVar(name string) (currentValue string, err error) {
	// FIXME
	return currentValue, err
}

func (u *UbootBootLoader) GetNextBootRootLabel() (label string) {
	// FIXME
	return label
}

func (u *UbootBootLoader) GetCurrentBootRootLabel() (label string) {
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
