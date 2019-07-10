// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */
package recovery

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

type DiskDevice struct {
	node string
}

func (d *DiskDevice) FindFromPartLabel(label string) error {
	dev, err := getDevByLabel(label)
	if err != nil {
		return err
	}
	logger.Noticef("device is %s", dev)
	d.node, err = getVolumeDevice(dev)
	logger.Noticef("node is %s", d.node)
	return err
}

func (d *DiskDevice) CreatePartition(size uint64, label string) error {
	logger.Noticef("Create partition %q", label)
	if output, err := pipeRun([]byte(fmt.Sprintf(",%d,,,", size/sizeSector)),
		"sfdisk", "--no-reread", "-a", d.node); err != nil {
		return osutil.OutputErr(output, fmt.Errorf("cannot create partition: %s", err))
	}

	// Re-read partition table
	if output, err := exec.Command("partx", "-u", d.node).CombinedOutput(); err != nil {
		return osutil.OutputErr(output, fmt.Errorf("cannot update partition table: %s", err))
	}

	// FIXME: determine partition name in a civilized way
	partdev := d.partDev(4)
	logger.Noticef("Create filesystem on %s", partdev)
	if output, err := exec.Command("mke2fs", "-t", "ext4", "-L", label, partdev).CombinedOutput(); err != nil {
		return osutil.OutputErr(output, fmt.Errorf("cannot create filesystem on %s: %s", partdev, err))
	}

	return nil
}

func (d *DiskDevice) CreateLUKSPartition(size uint64, label string, keyBuffer []byte, cryptdev string) error {
	logger.Noticef("Create partition %q", label)
	cmd := exec.Command("sfdisk", "--no-reread", "-a", d.node)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("cannot get sfdisk stdin: %s", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("cannot create partition: %s", err)
	}
	if _, err := stdin.Write([]byte(fmt.Sprintf(",%d,,,", size/sizeSector))); err != nil {
		return fmt.Errorf("cannot write to sfdisk pipe: %s", err)
	}
	if err := stdin.Close(); err != nil {
		return fmt.Errorf("cannot close fdisk pipe: %s", err)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("cannot run sfdisk: %s", err)
	}

	cryptdev = path.Join("/dev/mapper", cryptdev)

	// Re-read partition table
	if output, err := exec.Command("partx", "-u", d.node).CombinedOutput(); err != nil {
		return osutil.OutputErr(output, fmt.Errorf("cannot update partition table: %s", err))
	}

	// FIXME: determine partition name in a civilized way
	partdev := d.partDev(4)

	// Don't remove this delay, it prevents a kernel crash
	// see https://bugs.launchpad.net/ubuntu/+source/linux/+bug/1835279
	time.Sleep(1 * time.Second)

	// Write key to stdin to avoid creating a temporary file
	logger.Noticef("Create LUKS device on %s", partdev)
	if output, err := pipeRun(keyBuffer, "cryptsetup", "-q", "--type", "luks2", "--key-file", "-",
		"--pbkdf-memory", "100000", "luksFormat", partdev); err != nil {
		return osutil.OutputErr(output, fmt.Errorf("cannot format LUKS device: %s", err))
	}

	time.Sleep(1 * time.Second)

	logger.Noticef("Open LUKS device")
	if output, err := pipeRun(keyBuffer, "sh", "-c", fmt.Sprintf("LD_PRELOAD=/lib/no-udev.so cryptsetup "+
		"--type luks2 --key-file - --pbkdf-memory 100000 open %s %s", partdev,
		path.Base(cryptdev))); err != nil {
		return osutil.OutputErr(output, fmt.Errorf("cannot open LUKS device on %s: %s", partdev, err))
	}

	// FIXME: Ok, now this is ugly. We'll have to see how to handle this properly.
	logger.Noticef("Hack: create LUKS device symlink")
	if err := os.Symlink("../dm-0", cryptdev); err != nil {
		return fmt.Errorf("cannot create LUKS device symlink: %s", err)
	}

	time.Sleep(1 * time.Second)

	// Create filesystem
	logger.Noticef("Create filesystem on %s", cryptdev)
	if output, err := exec.Command("mke2fs", "-t", "ext4", "-L", label, cryptdev).CombinedOutput(); err != nil {
		return osutil.OutputErr(output, fmt.Errorf("cannot create filesystem on %s: %s", cryptdev, err))
	}

	return nil
}

func pipeRun(input []byte, name string, args ...string) (output []byte, err error) {
	cmd := exec.Command(name, args...)
	stdin, err := cmd.StdinPipe()
	var out bytes.Buffer
	// FIXME: use combined output
	cmd.Stderr = &out
	if err != nil {
		return
	}
	if err = cmd.Start(); err != nil {
		return
	}
	n, err := stdin.Write(input)
	if err != nil {
		return
	}
	if n != len(input) {
		err = fmt.Errorf("short write (%d)", n)
		return
	}
	if err = stdin.Close(); err != nil {
		return
	}
	if err = cmd.Wait(); err != nil {
		output = out.Bytes()
		return
	}
	return
}

func createCrypttab(partdev, keyfile, cryptdev string) error {
	buffer := fmt.Sprintf("%s  %s  %s  luks\n", cryptdev, partdev, keyfile)
	if err := ioutil.WriteFile(keyfile, []byte(buffer), 0644); err != nil {
		return err
	}
	return nil
}

func (d *DiskDevice) partDev(num int) string {
	// FIXME
	return d.node + strconv.Itoa(num)
}

//

func getDevByLabel(label string) (string, error) {
	out, err := exec.Command("findfs", "LABEL="+label).Output()
	if err != nil {
		return "", err
	}
	dev := strings.TrimSpace(string(out))
	logger.Debugf("device for label %q: %s", label, dev)
	return dev, nil
}

func getVolumeDevice(part string) (string, error) {
	sysdev := path.Join("/sys/class/block", path.Base(part))
	dev, err := filepath.EvalSymlinks(sysdev)
	if err != nil {
		return "", fmt.Errorf("cannot resolve symlink: %s: %s", sysdev, err)
	}

	devpath := path.Join(path.Dir(dev), "dev")
	f, err := os.Open(devpath)
	if err != nil {
		return "", fmt.Errorf("cannot open %s: %s", devpath, err)
	}
	defer f.Close()

	// Read major and minor block device numbers
	r := bufio.NewReader(f)
	line, _, err := r.ReadLine()
	nums := strings.TrimSpace(string(line))
	if err != nil {
		return "", fmt.Errorf("cannot read numbers: %s", err)
	}

	// Locate block device based on device numbers
	blockdev := path.Join("/dev/block", nums)
	voldev, err := filepath.EvalSymlinks(blockdev)
	if err != nil {
		return "", fmt.Errorf("cannot resolve symlink: %s: %s", blockdev, err)
	}

	return voldev, nil
}
