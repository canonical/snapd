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
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/logger"
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
	cmd := exec.Command("sfdisk", "--no-reread", "-a", d.node)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("cannot create partition: %s", err)
	}
	stdin.Write([]byte(fmt.Sprintf(",%d,,,", size/sizeSector)))
	stdin.Close()
	cmd.Wait()

	// Re-read partition table
	if err := exec.Command("partx", "-u", d.node).Run(); err != nil {
		return fmt.Errorf("cannot update partition table: %s", err)
	}

	// FIXME: determine partition name in a civilized way
	partdev := d.partDev(4)
	logger.Noticef("Create filesystem on %s", partdev)
	if err := exec.Command("mke2fs", "-t", "ext4", "-L", label, partdev).Run(); err != nil {
		return fmt.Errorf("cannot create filesystem on %s: %s", partdev, err)
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
