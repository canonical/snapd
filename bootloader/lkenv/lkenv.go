// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package lkenv

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

const SNAP_BOOTSELECT_VERSION = 0x00010001

// const SNAP_BOOTSELECT_SIGNATURE ('S' | ('B' << 8) | ('s' << 16) | ('e' << 24))
const SNAP_BOOTSELECT_SIGNATURE = 0x53 | 0x42<<8 | 0x73<<16 | 0x65<<24
const SNAP_NAME_MAX_LEN = 256

/* number of available boot partitions */
const SNAP_BOOTIMG_PART_NUM = 2

/* Default boot image file name to be used from kernel snap */
const BOOTIMG_DEFAULT_NAME = "boot.img"

// for accessing the 	Bootimg_matrix
const (
	MATRIX_ROW_PARTITION = 0
	MATRIX_ROW_KERNEL    = 1
)

// Env contains the data of the uboot environment
// path can be file or partition device node
type Env struct {
	path    string
	pathbak string
	env     SnapBootSelect_v1
}

// cToGoString convert string in passed byte array into string type
// if string in byte array is not terminated, empty string is returned
func cToGoString(c []byte) string {
	if end := bytes.IndexByte(c, 0); end >= 0 {
		return string(c[:end])
	}
	// no trailing \0 - return ""
	return ""
}

// copyString copy passed string into byte array
// make sure string is terminated
// if string does not fit into byte array, it will be concatenated
func copyString(b []byte, s string) {
	sl := len(s)
	bs := len(b)
	if bs > sl {
		copy(b[:], s)
		b[sl] = 0
	} else {
		copy(b[:bs-1], s)
		b[bs-1] = 0
	}
}

func NewEnv(path string) *Env {
	return &Env{
		path:    path,
		pathbak: path + "bak",
		env: SnapBootSelect_v1{
			Signature: SNAP_BOOTSELECT_SIGNATURE,
			Version:   SNAP_BOOTSELECT_VERSION,
		},
	}
}

func (l *Env) Get(key string) string {
	switch key {
	case "snap_mode":
		return cToGoString(l.env.Snap_mode[:])
	case "snap_kernel":
		return cToGoString(l.env.Snap_kernel[:])
	case "snap_try_kernel":
		return cToGoString(l.env.Snap_try_kernel[:])
	case "snap_core":
		return cToGoString(l.env.Snap_core[:])
	case "snap_try_core":
		return cToGoString(l.env.Snap_try_core[:])
	case "snap_gadget":
		return cToGoString(l.env.Snap_gadget[:])
	case "snap_try_gadget":
		return cToGoString(l.env.Snap_try_gadget[:])
	case "reboot_reason":
		return cToGoString(l.env.Reboot_reason[:])
	case "bootimg_file_name":
		return cToGoString(l.env.Bootimg_file_name[:])
	}
	return ""
}

func (l *Env) Set(key, value string) {
	switch key {
	case "snap_mode":
		copyString(l.env.Snap_mode[:], value)
	case "snap_kernel":
		copyString(l.env.Snap_kernel[:], value)
	case "snap_try_kernel":
		copyString(l.env.Snap_try_kernel[:], value)
	case "snap_core":
		copyString(l.env.Snap_core[:], value)
	case "snap_try_core":
		copyString(l.env.Snap_try_core[:], value)
	case "snap_gadget":
		copyString(l.env.Snap_gadget[:], value)
	case "snap_try_gadget":
		copyString(l.env.Snap_try_gadget[:], value)
	case "reboot_reason":
		copyString(l.env.Reboot_reason[:], value)
	case "bootimg_file_name":
		copyString(l.env.Bootimg_file_name[:], value)
	}
}

// ConfigureBootPartitions set boot partitions label names
// this function should not be used at run time!
// it should be used only at image build time,
// if partition labels are not pre-filled by gadget built
func (l *Env) ConfigureBootPartitions(boot_1, boot_2 string) {
	copyString(l.env.Bootimg_matrix[0][MATRIX_ROW_PARTITION][:], boot_1)
	copyString(l.env.Bootimg_matrix[1][MATRIX_ROW_PARTITION][:], boot_2)
}

// ConfigureBootimgName set boot image file name
// boot image file name is used at kernel extraction time
// this function should not be used at run time!
// it should be used only at image build time
// if default boot.img is not set by gadget built
func (l *Env) ConfigureBootimgName(bootimgName string) {
	copyString(l.env.Bootimg_file_name[:], bootimgName)
}

func (l *Env) Load() error {
	err := l.LoadEnv(l.path)
	if err != nil {
		return l.LoadEnv(l.pathbak)
	}
	return nil
}

func (l *Env) LoadEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("cannot open LK env file: %v", err)
	}

	defer f.Close()
	if err := binary.Read(f, binary.LittleEndian, &l.env); err != nil {
		return fmt.Errorf("cannot read LK env from file: %v", err)
	}

	// calculate crc32 to validate structure
	w := bytes.NewBuffer(nil)
	ss := binary.Size(l.env)
	w.Grow(ss)
	if err := binary.Write(w, binary.LittleEndian, &l.env); err != nil {
		return fmt.Errorf("cannot write LK env to buffer for validation: %v", err)
	}
	if l.env.Version != SNAP_BOOTSELECT_VERSION || l.env.Signature != SNAP_BOOTSELECT_SIGNATURE {
		return fmt.Errorf("cannot validate version/signature for %s, got 0x%X expected 0x%X, got 0x%X expected 0x%X\n", path, l.env.Version, SNAP_BOOTSELECT_VERSION, l.env.Signature, SNAP_BOOTSELECT_SIGNATURE)
	}

	crc := crc32.ChecksumIEEE(w.Bytes()[:ss-4]) // size of crc32 itself at the end of the structure
	if crc != l.env.Crc32 {
		return fmt.Errorf("cannot validate environment checksum %s, got 0x%X expected 0x%X\n", path, crc, l.env.Crc32)
	}
	logger.Debugf("Load: validated crc32 (0x%X)", l.env.Crc32)
	return nil
}

func (l *Env) Save() error {
	logger.Debugf("Save")
	w := bytes.NewBuffer(nil)
	ss := binary.Size(l.env)
	w.Grow(ss)
	if err := binary.Write(w, binary.LittleEndian, &l.env); err != nil {
		return fmt.Errorf("cannot write LK env to buffer for saving: %v", err)
	}
	// calculate crc32
	l.env.Crc32 = crc32.ChecksumIEEE(w.Bytes()[:ss-4])
	logger.Debugf("Save: calculated crc32 (0x%X)", l.env.Crc32)
	w.Truncate(ss - 4)
	binary.Write(w, binary.LittleEndian, &l.env.Crc32)

	err := l.saveEnv(l.path, w)
	if err != nil {
		logger.Debugf("Save: failed to save main environment")
	}
	// if there is backup environment file save to it as well
	if osutil.FileExists(l.pathbak) {
		if err := l.saveEnv(l.pathbak, w); err != nil {
			logger.Debugf("Save: failed to save backup environment %v", err)
		}
	}
	return err
}

func (l *Env) saveEnv(path string, buf *bytes.Buffer) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0660)
	if err != nil {
		return fmt.Errorf("cannot open LK env file for env storing: %v", err)
	}
	defer f.Close()

	if _, err := f.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("cannot write LK env buf to LK env file: %v", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("cannot sync LK env file: %v", err)
	}
	return nil
}

// FindFreeBootPartition find free boot partition to be used for new kernel revision
// - consider kernel snap blob name, if kernel name matches
//   already installed revision, return coresponding partition name
// - protect partition used by kernel_snap, consider other as free
// - consider only boot partitions with defined partition name
func (l *Env) FindFreeBootPartition(kernel string) (string, error) {
	for x := range l.env.Bootimg_matrix {
		bp := cToGoString(l.env.Bootimg_matrix[x][MATRIX_ROW_PARTITION][:])
		if bp != "" {
			k := cToGoString(l.env.Bootimg_matrix[x][MATRIX_ROW_KERNEL][:])
			if k != cToGoString(l.env.Snap_kernel[:]) || k == kernel || k == "" {
				return cToGoString(l.env.Bootimg_matrix[x][MATRIX_ROW_PARTITION][:]), nil
			}
		}
	}
	return "", fmt.Errorf("cannot find free partition for boot image")
}

// SetBootPartition sets the kernel revision reference in the provided boot
// partition reference to the provided kernel revision. It returns a non-nil err
// if the provided boot partition reference was not found.
func (l *Env) SetBootPartition(bootpart, kernel string) error {
	for x := range l.env.Bootimg_matrix {
		if bootpart == cToGoString(l.env.Bootimg_matrix[x][MATRIX_ROW_PARTITION][:]) {
			copyString(l.env.Bootimg_matrix[x][MATRIX_ROW_KERNEL][:], kernel)
			return nil
		}
	}
	return fmt.Errorf("cannot find defined [%s] boot image partition", bootpart)
}

// GetBootPartition returns the first found boot partition that contains a
// reference to the given kernel revision. If the revision was not found, a
// non-nil error is returned.
func (l *Env) GetBootPartition(kernel string) (string, error) {
	for x := range l.env.Bootimg_matrix {
		if kernel == cToGoString(l.env.Bootimg_matrix[x][MATRIX_ROW_KERNEL][:]) {
			return cToGoString(l.env.Bootimg_matrix[x][MATRIX_ROW_PARTITION][:]), nil
		}
	}
	return "", fmt.Errorf("cannot find kernel %q in boot image partitions", kernel)
}

// RemoveKernelRevisionFromBootPartition removes from the boot image matrix the
// first found boot partition that contains a reference to the given kernel
// revision. If the referenced kernel revision was not found, a non-nil err is
// returned, otherwise the reference is removed and nil is returned.
// Note that to persist this change the env must be saved afterwards with Save.
func (l *Env) RemoveKernelRevisionFromBootPartition(kernel string) error {
	for x := range l.env.Bootimg_matrix {
		if "" != cToGoString(l.env.Bootimg_matrix[x][MATRIX_ROW_PARTITION][:]) {
			if kernel == cToGoString(l.env.Bootimg_matrix[x][MATRIX_ROW_KERNEL][:]) {
				l.env.Bootimg_matrix[x][1][MATRIX_ROW_PARTITION] = 0
				return nil
			}
		}
	}
	return fmt.Errorf("cannot find defined [%s] boot image partition", kernel)
}

// GetBootImageName return expected boot image file name in kernel snap
func (l *Env) GetBootImageName() string {
	if "" != cToGoString(l.env.Bootimg_file_name[:]) {
		return cToGoString(l.env.Bootimg_file_name[:])
	}
	return BOOTIMG_DEFAULT_NAME
}
