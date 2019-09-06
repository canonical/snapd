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
const SNAP_MODE_LENGTH = 8

/* number of available boot partitions */
const SNAP_BOOTIMG_PART_NUM = 2

/* Default boot image file name to be used from kernel snap */
const BOOTIMG_DEFAULT_NAME = "boot.img"

// for accessing the 	Bootimg_matrix
const (
	MATRIX_ROW_PARTITION = 0
	MATRIX_ROW_KERNEL    = 1
)

/**
 * following structure has to be kept in sync with c structure defined by
 * include/snappy-boot_v1.h
 * c headerfile is used by bootloader, this ensures sync of  the environment
 * between snapd and bootloader

 * when this structure needs to be updated,
 * new version should be introduced instead together with c header file,
 * which is to be adopted by bootloader
 *
 * !!! Support for old version has to be maintained, as it is not guaranteed
 * all existing bootloader would adopt new version!
 */
type SnapBootSelect_v1 struct {
	/* Contains value BOOTSELECT_SIGNATURE defined above */
	Signature uint32
	/* snappy boot select version */
	Version uint32

	/* snap_mode, one of: 'empty', "try", "trying" */
	Snap_mode [SNAP_MODE_LENGTH]byte
	/* current core snap revision */
	Snap_core [SNAP_NAME_MAX_LEN]byte
	/* try core snap revision */
	Snap_try_core [SNAP_NAME_MAX_LEN]byte
	/* current kernel snap revision */
	Snap_kernel [SNAP_NAME_MAX_LEN]byte
	/* current kernel snap revision */
	Snap_try_kernel [SNAP_NAME_MAX_LEN]byte

	/* GADGET assets: current gadget assets revision */
	Snap_gadget [SNAP_NAME_MAX_LEN]byte
	/* GADGET assets: try gadget assets revision */
	Snap_try_gadget [SNAP_NAME_MAX_LEN]byte

	/**
	Reboot reason
	Optional parameter to signal bootloader alternative reboot reasons
	e.g. recovery/factory-reset/boot asset update
	*/
	Reboot_reason [SNAP_NAME_MAX_LEN]byte

	/**
	 Matrix for mapping of boot img partion to installed kernel snap revision
	 At image build time:
			 - snap prepare populates boot args as for other bootloaders
			 - first column with bootimage part names can be filled at gadget builds
			   or in the future by snap prepare image time
			 - ExtractKernelAssets fills mapping for initial boot image
	 snapd:
			 - when new kernel snap is installed, ExtractKernelAssets updates mapping
			   in matrix for bootloader to pick correct kernel snap to use for boot
			 - snap_mode, snap_try_kernel, snap_try_core behaves same way as with u-boot
			 - boot partition labels are never modified by snapd at run time
	 bootloader:
			 - Finds boot partition to use based on info in matrix and snap_kernel / snap_try_kernel
			 - bootloader does not alter matrix, only alters snap_mode

	 [ <bootimg 1 part label> ] [ <currently installed kernel snap revison> ]
	 [ <bootimg 2 part label> ] [ <currently installed kernel snap revision> ]
	*/
	Bootimg_matrix [SNAP_BOOTIMG_PART_NUM][2][SNAP_NAME_MAX_LEN]byte

	/* name of the boot image from kernel snap to be used for extraction
	   when not defined or empty, default boot.img will be used */
	Bootimg_file_name [SNAP_NAME_MAX_LEN]byte

	/**
	GADGET assets: Matrix for mapping of gadget asset partions
	Optional boot asset tracking, based on bootloader support
	Some boot chains support A/B boot assets for increased robustness
	example being A/B TrustExecutionEnvironment
	This matrix can be used to track current and try boot assets for
	robust updates

	[ <boot assets 1 part label> ] [ <currently installed assets revison> ]
	[ <boot assets 2 part label> ] [ <currently installed assets revision> ]
	*/
	Boot_asset_matrix [SNAP_BOOTIMG_PART_NUM][2][SNAP_NAME_MAX_LEN]byte

	/* unused placeholders for additional parameters in the future */
	Unused_key_1 [SNAP_NAME_MAX_LEN]byte
	Unused_key_2 [SNAP_NAME_MAX_LEN]byte
	Unused_key_3 [SNAP_NAME_MAX_LEN]byte
	Unused_key_4 [SNAP_NAME_MAX_LEN]byte

	/* unused array of 10 key value pairs */
	Kye_value_pairs [10][2][SNAP_NAME_MAX_LEN]byte

	/* crc32 value for structure */
	Crc32 uint32
}

// Env contains the data of the uboot environment
// path can be file or partition device node
type Env struct {
	path    string
	pathbak string
	env     SnapBootSelect_v1
}

//helper function to trim string from byte array to actual length
func cToGoString(c []byte) string {
	if end := bytes.IndexByte(c, 0); end >= 0 {
		return string(c[:end])
	}
	// no trailing \0 - return ""
	return ""
}

// helper function to copy string into array and making sure it's terminated
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
	// osutil.FileExists(path + "bak")
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
	}
}

// Configure partition labels for used boot partitions
// this should not be used at run time, it should be used
// once at image build time, if part labels are not filled by gadget build
func (l *Env) ConfigureBootPartitions(boot_1, boot_2 string) {
	copyString(l.env.Bootimg_matrix[0][MATRIX_ROW_PARTITION][:], boot_1)
	copyString(l.env.Bootimg_matrix[1][MATRIX_ROW_PARTITION][:], boot_2)
}

// Configure boot image file name to be used at kernel extraction time
// this should not be used at run time, it should be used
// once at image build time if default boot.img is not used
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

	err := l.SaveEnv(l.path, w)
	if err != nil {
		logger.Debugf("Save: failed to save main environment")
	}
	// if there is backup environment file save to it as well
	if osutil.FileExists(l.pathbak) {
		if err := l.SaveEnv(l.pathbak, w); err != nil {
			logger.Debugf("Save: failed to save backup environment %v", err)
		}
	}
	return err
}

func (l *Env) SaveEnv(path string, buf *bytes.Buffer) error {
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

// Find first free boot partition to be used
// - consider kernel snap blob name, if already used simply return used part name
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

// sets kernel revision to defined boot partition partition
func (l *Env) SetBootPartition(bootpart, kernel string) error {
	for x := range l.env.Bootimg_matrix {
		if bootpart == cToGoString(l.env.Bootimg_matrix[x][MATRIX_ROW_PARTITION][:]) {
			copyString(l.env.Bootimg_matrix[x][MATRIX_ROW_KERNEL][:], kernel)
			return nil
		}
	}
	return fmt.Errorf("cannot find defined [%s] boot image partition", bootpart)
}

func (l *Env) GetBootPartition(kernel string) (string, error) {
	for x := range l.env.Bootimg_matrix {
		if kernel == cToGoString(l.env.Bootimg_matrix[x][MATRIX_ROW_KERNEL][:]) {
			return cToGoString(l.env.Bootimg_matrix[x][MATRIX_ROW_PARTITION][:]), nil
		}
	}
	return "", fmt.Errorf("cannot find kernel %q in boot image partitions", kernel)
}

// frees boot partition with given kernel revision
// ignored if it cannot find given kernel revision
func (l *Env) FreeBootPartition(kernel string) (bool, error) {
	for x := range l.env.Bootimg_matrix {
		if "" != cToGoString(l.env.Bootimg_matrix[x][MATRIX_ROW_PARTITION][:]) {
			if kernel == cToGoString(l.env.Bootimg_matrix[x][MATRIX_ROW_KERNEL][:]) {
				l.env.Bootimg_matrix[x][1][MATRIX_ROW_PARTITION] = 0
				return true, nil
			}
		}
	}
	return false, fmt.Errorf("cannot find defined [%s] boot image partition", kernel)
}

// return name of boot.img from kernel snap to used
func (l *Env) GetBootImageName() string {
	if "" != cToGoString(l.env.Bootimg_file_name[:]) {
		return cToGoString(l.env.Bootimg_file_name[:])
	}
	return BOOTIMG_DEFAULT_NAME
}
