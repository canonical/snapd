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

const (
	SNAP_BOOTSELECT_VERSION_V1 = 0x00010001
	SNAP_BOOTSELECT_VERSION_V2 = 0x00010010
)

// const SNAP_BOOTSELECT_SIGNATURE ('S' | ('B' << 8) | ('s' << 16) | ('e' << 24))
const SNAP_BOOTSELECT_SIGNATURE = 0x53 | 0x42<<8 | 0x73<<16 | 0x65<<24
const SNAP_NAME_MAX_LEN = 256

/* number of available boot partitions */
const SNAP_BOOTIMG_PART_NUM = 2

/* number of available boot partitions for uc20 for kernel/try-kernel in run mode */
const SNAP_RUN_BOOTIMG_PART_NUM = 2

/** maximum number of available bootimg partitions for recovery systems, min 5
 *  NOTE: the number of actual bootimg partitions usable is determined by the
 *  gadget, this just sets the upper bound of maximum number of recovery systems
 *  a gadget could define without needing changes here
 */
const SNAP_RECOVER_BOOTIMG_PART_NUM = 10

/* Default boot image file name to be used from kernel snap */
const BOOTIMG_DEFAULT_NAME = "boot.img"

// for accessing the 	Bootimg_matrix
const (
	MATRIX_ROW_PARTITION       = 0
	MATRIX_ROW_KERNEL          = 1
	MATRIX_ROW_RECOVERY_SYSTEM = 1
)

type bootimgKernelMatrix [SNAP_RUN_BOOTIMG_PART_NUM][2][SNAP_NAME_MAX_LEN]byte

type Version int

const (
	V1 Version = iota
	V2Run
	V2Recovery
)

// Env contains the data of the uboot environment
// path can be file or partition device node
type Env struct {
	path            string
	pathbak         string
	version         Version
	env_v1          SnapBootSelect_v1
	env_v2_recovery SnapBootSelect_v2_recovery
	env_v2_run      SnapBootSelect_v2_run
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

func NewEnv(path string, version Version) *Env {
	e := &Env{
		path:    path,
		pathbak: path + "bak",
		version: version,
	}

	switch version {
	case V1:
		e.env_v1 = SnapBootSelect_v1{
			Signature: SNAP_BOOTSELECT_SIGNATURE,
			Version:   SNAP_BOOTSELECT_VERSION_V1,
		}
	case V2Recovery:
		e.env_v2_recovery = SnapBootSelect_v2_recovery{
			Signature: SNAP_BOOTSELECT_SIGNATURE,
			Version:   SNAP_BOOTSELECT_VERSION_V2,
		}
	case V2Run:
		e.env_v2_run = SnapBootSelect_v2_run{
			Signature: SNAP_BOOTSELECT_SIGNATURE,
			Version:   SNAP_BOOTSELECT_VERSION_V2,
		}
	}
	return e
}

// Get returns the value of the key from the environment. If the key specified
// is not supported for the environment, the empty string is returned.
func (l *Env) Get(key string) string {
	switch l.version {
	case V1:
		switch key {
		case "snap_mode":
			return cToGoString(l.env_v1.Snap_mode[:])
		case "snap_kernel":
			return cToGoString(l.env_v1.Snap_kernel[:])
		case "snap_try_kernel":
			return cToGoString(l.env_v1.Snap_try_kernel[:])
		case "snap_core":
			return cToGoString(l.env_v1.Snap_core[:])
		case "snap_try_core":
			return cToGoString(l.env_v1.Snap_try_core[:])
		case "snap_gadget":
			return cToGoString(l.env_v1.Snap_gadget[:])
		case "snap_try_gadget":
			return cToGoString(l.env_v1.Snap_try_gadget[:])
		case "reboot_reason":
			return cToGoString(l.env_v1.Reboot_reason[:])
		case "bootimg_file_name":
			return cToGoString(l.env_v1.Bootimg_file_name[:])
		}

	case V2Recovery:
		switch key {
		case "snapd_recovery_mode":
			return cToGoString(l.env_v2_recovery.Snapd_recovery_mode[:])
		case "snapd_recovery_system":
			return cToGoString(l.env_v2_recovery.Snapd_recovery_system[:])
		case "bootimg_file_name":
			return cToGoString(l.env_v2_recovery.Bootimg_file_name[:])
		}

	case V2Run:
		switch key {
		case "kernel_status":
			return cToGoString(l.env_v2_run.Kernel_status[:])
		case "snap_kernel":
			return cToGoString(l.env_v2_run.Snap_kernel[:])
		case "snap_try_kernel":
			return cToGoString(l.env_v2_run.Snap_try_kernel[:])
		case "snap_gadget":
			return cToGoString(l.env_v2_run.Snap_gadget[:])
		case "snap_try_gadget":
			return cToGoString(l.env_v2_run.Snap_try_gadget[:])
		case "bootimg_file_name":
			return cToGoString(l.env_v2_run.Bootimg_file_name[:])
		}
	}

	return ""
}

// Set assigns the value to the key in the environment. If the key specified is
// not supported for the environment, nothing happens.
func (l *Env) Set(key, value string) {
	switch l.version {
	case V1:
		switch key {
		case "snap_mode":
			copyString(l.env_v1.Snap_mode[:], value)
		case "snap_kernel":
			copyString(l.env_v1.Snap_kernel[:], value)
		case "snap_try_kernel":
			copyString(l.env_v1.Snap_try_kernel[:], value)
		case "snap_core":
			copyString(l.env_v1.Snap_core[:], value)
		case "snap_try_core":
			copyString(l.env_v1.Snap_try_core[:], value)
		case "snap_gadget":
			copyString(l.env_v1.Snap_gadget[:], value)
		case "snap_try_gadget":
			copyString(l.env_v1.Snap_try_gadget[:], value)
		case "reboot_reason":
			copyString(l.env_v1.Reboot_reason[:], value)

		// setting the boot image file name should not be set at run time,
		// it should be used only at image build time if default boot.img is not
		// set when the gadget was built
		case "bootimg_file_name":
			copyString(l.env_v1.Bootimg_file_name[:], value)
		}
	case V2Recovery:
		switch key {
		case "snapd_recovery_mode":
			copyString(l.env_v2_recovery.Snapd_recovery_mode[:], value)
		case "snapd_recovery_system":
			copyString(l.env_v2_recovery.Snapd_recovery_system[:], value)
		case "bootimg_file_name":
			copyString(l.env_v2_recovery.Bootimg_file_name[:], value)
		}
	case V2Run:
		switch key {
		case "kernel_status":
			copyString(l.env_v2_run.Kernel_status[:], value)
		case "snap_kernel":
			copyString(l.env_v2_run.Snap_kernel[:], value)
		case "snap_try_kernel":
			copyString(l.env_v2_run.Snap_try_kernel[:], value)
		case "snap_gadget":
			copyString(l.env_v2_run.Snap_gadget[:], value)
		case "snap_try_gadget":
			copyString(l.env_v2_run.Snap_try_gadget[:], value)
		case "bootimg_file_name":
			copyString(l.env_v2_run.Bootimg_file_name[:], value)
		}
	}
}

// ConfigureBootPartitions set boot partitions label names
// this function should not be used at run time!
// it should be used only at image build time,
// if partition labels are not pre-filled by gadget built
// TODO: this function is currently unused?
func (l *Env) ConfigureBootPartitions(bootPartLabels ...string) error {
	numBootPartLabels := len(bootPartLabels)
	switch l.version {
	case V1:
		if numBootPartLabels != SNAP_BOOTIMG_PART_NUM {
			return fmt.Errorf("invalid number of boot partition labels for v1 lkenv, expected %d got %d", numBootPartLabels, SNAP_BOOTIMG_PART_NUM)
		}
		copyString(l.env_v1.Bootimg_matrix[0][MATRIX_ROW_PARTITION][:], bootPartLabels[0])
		copyString(l.env_v1.Bootimg_matrix[1][MATRIX_ROW_PARTITION][:], bootPartLabels[1])
	case V2Run:
		if numBootPartLabels != SNAP_BOOTIMG_PART_NUM {
			return fmt.Errorf("invalid number of boot partition labels for v2 lkenv run mode, expected %d got %d", numBootPartLabels, SNAP_BOOTIMG_PART_NUM)
		}
		copyString(l.env_v2_run.Bootimg_matrix[0][MATRIX_ROW_PARTITION][:], bootPartLabels[0])
		copyString(l.env_v2_run.Bootimg_matrix[1][MATRIX_ROW_PARTITION][:], bootPartLabels[1])
	case V2Recovery:
		// too many
		if numBootPartLabels > SNAP_RECOVER_BOOTIMG_PART_NUM {
			return fmt.Errorf("too many (%d) boot partition labels for v2 lkenv run mode, expected no more than %d", numBootPartLabels, SNAP_RECOVER_BOOTIMG_PART_NUM)
		}
		// too few
		if numBootPartLabels < SNAP_BOOTIMG_PART_NUM {
			return fmt.Errorf("too few (%d) boot partition labels for v2 lkenv run mode, expected at least %d", numBootPartLabels, SNAP_BOOTIMG_PART_NUM)
		}
	}
	return nil
}

func (l *Env) ConfigureBootimgName(bootimgName string) {
	l.Set("bootimg_file_name", bootimgName)
}

func (l *Env) Load() error {
	err := l.LoadEnv(l.path)
	if err != nil {
		logger.Noticef("cannot load primary bootloader environment: %v\n", err)
		logger.Noticef("attempting to load backup bootloader environment\n")
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
	var envObj interface{}
	switch l.version {
	case V1:
		envObj = &l.env_v1
	case V2Recovery:
		envObj = &l.env_v2_recovery
	case V2Run:
		envObj = &l.env_v2_run
	}
	if err := binary.Read(f, binary.LittleEndian, envObj); err != nil {
		return fmt.Errorf("cannot read LK env from file: %v", err)
	}

	var dataCRC32 uint32
	switch l.version {
	case V1:
		dataCRC32 = l.env_v1.Crc32
	case V2Recovery:
		dataCRC32 = l.env_v2_recovery.Crc32
	case V2Run:
		dataCRC32 = l.env_v2_run.Crc32
	}

	// calculate crc32 to validate structure
	w := bytes.NewBuffer(nil)
	ss := binary.Size(envObj)
	w.Grow(ss)
	if err := binary.Write(w, binary.LittleEndian, envObj); err != nil {
		return fmt.Errorf("cannot write LK env to buffer for validation: %v", err)
	}

	validationErr := "cannot validate %s: got version of 0x%X (expected 0x%X), got signature of 0x%X (expected 0x%X)"
	switch l.version {
	case V1:
		if l.env_v1.Version != SNAP_BOOTSELECT_VERSION_V1 ||
			l.env_v1.Signature != SNAP_BOOTSELECT_SIGNATURE {
			return fmt.Errorf(
				validationErr,
				path,
				l.env_v1.Version,
				SNAP_BOOTSELECT_VERSION_V1,
				l.env_v1.Signature,
				SNAP_BOOTSELECT_SIGNATURE,
			)
		}
	case V2Recovery:
		if l.env_v2_recovery.Version != SNAP_BOOTSELECT_VERSION_V2 ||
			l.env_v2_recovery.Signature != SNAP_BOOTSELECT_SIGNATURE {
			return fmt.Errorf(
				validationErr,
				path,
				l.env_v2_recovery.Version,
				SNAP_BOOTSELECT_VERSION_V1,
				l.env_v2_recovery.Signature,
				SNAP_BOOTSELECT_SIGNATURE,
			)
		}
	case V2Run:
		if l.env_v2_run.Version != SNAP_BOOTSELECT_VERSION_V2 ||
			l.env_v2_run.Signature != SNAP_BOOTSELECT_SIGNATURE {
			return fmt.Errorf(
				validationErr,
				path,
				l.env_v2_run.Version,
				SNAP_BOOTSELECT_VERSION_V1,
				l.env_v2_run.Signature,
				SNAP_BOOTSELECT_SIGNATURE,
			)
		}
	}

	crc := crc32.ChecksumIEEE(w.Bytes()[:ss-4]) // size of crc32 itself at the end of the structure
	if crc != dataCRC32 {
		return fmt.Errorf("cannot validate environment checksum %s, got 0x%X expected 0x%X", path, crc, dataCRC32)
	}
	logger.Debugf("Load: validated crc32 (0x%X)", dataCRC32)
	return nil
}

func (l *Env) Save() error {
	var envObj interface{}
	switch l.version {
	case V1:
		envObj = &l.env_v1
	case V2Recovery:
		envObj = &l.env_v2_recovery
	case V2Run:
		envObj = &l.env_v2_run
	}

	w := bytes.NewBuffer(nil)
	ss := binary.Size(envObj)
	w.Grow(ss)
	if err := binary.Write(w, binary.LittleEndian, envObj); err != nil {
		return fmt.Errorf("cannot write LK env to buffer for saving: %v", err)
	}
	// calculate crc32
	newCrc32 := crc32.ChecksumIEEE(w.Bytes()[:ss-4])
	logger.Debugf("Save: calculated crc32 (0x%X)", newCrc32)
	// note for efficiency's sake to avoid re-writing the whole structure, we
	// re-write _just_ the crc32 w as little-endian
	w.Truncate(ss - 4)
	binary.Write(w, binary.LittleEndian, &newCrc32)

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

// FindFreeBootPartition finds a free boot partition to be used for new kernel
// revision. It roughly does:
// - consider kernel snap blob name, if kernel name matches
//   already installed revision, return coresponding partition name
// - protect partition used by kernel_snap, consider other as free
// - consider only boot partitions with defined partition name
func (l *Env) FindFreeBootPartition(kernel string) (string, error) {
	var matr bootimgKernelMatrix
	switch l.version {
	case V1:
		matr = l.env_v1.Bootimg_matrix
	case V2Run:
		matr = l.env_v2_run.Bootimg_matrix
	case V2Recovery:
		return "", fmt.Errorf("internal error: recovery lkenv has no kernel boot partition matrix")
	}
	for x := range matr {
		bp := cToGoString(matr[x][MATRIX_ROW_PARTITION][:])
		if bp != "" {
			k := cToGoString(matr[x][MATRIX_ROW_KERNEL][:])
			// return this one if it's not the current snap_kernel, if it's the
			// exactly specified kernel, or if it's empty
			if k != l.Get("snap_kernel") || k == kernel || k == "" {
				return cToGoString(matr[x][MATRIX_ROW_PARTITION][:]), nil
			}
		}
	}
	return "", fmt.Errorf("cannot find free partition for boot image")
}

func (l *Env) FindFreeRecoverySystemPartition(recoverySystem string) (string, error) {
	if l.version != V2Recovery {
		return "", fmt.Errorf("internal error: cannot find recovery system boot partition on non-recovery lkenv")
	}

	for x := range l.env_v2_recovery.Bootimg_matrix {
		bp := cToGoString(l.env_v2_recovery.Bootimg_matrix[x][MATRIX_ROW_PARTITION][:])
		if bp != "" {
			sys := cToGoString(l.env_v2_recovery.Bootimg_matrix[x][MATRIX_ROW_RECOVERY_SYSTEM][:])
			// return this one if it's the exact specified recovery system or if
			// it's empty
			if sys == recoverySystem || sys == "" {
				return cToGoString(l.env_v2_recovery.Bootimg_matrix[x][MATRIX_ROW_PARTITION][:]), nil
			}
		}
	}
	return "", fmt.Errorf("cannot find free partition for recovery system")

}

// SetRecoverySystemBootPartition sets the recovery system reference in the
// provided boot partition reference to the provided recovery system. It returns
// a non-nil err if the provided boot partition reference was not found.
func (l *Env) SetRecoverySystemBootPartition(bootpart, recoverySystem string) error {
	if l.version != V2Recovery {
		return fmt.Errorf("internal error: cannot set recovery system boot partition on non-recovery lkenv")
	}
	for x := range l.env_v2_recovery.Bootimg_matrix {
		if bootpart == cToGoString(l.env_v2_recovery.Bootimg_matrix[x][MATRIX_ROW_PARTITION][:]) {
			copyString(l.env_v2_recovery.Bootimg_matrix[x][MATRIX_ROW_RECOVERY_SYSTEM][:], recoverySystem)
			return nil
		}
	}
	return fmt.Errorf("cannot find defined [%s] boot image partition", bootpart)
}

// GetBootPartition returns the first found boot partition that contains a
// reference to the given kernel revision. If the revision was not found, a
// non-nil error is returned.
func (l *Env) GetBootPartition(kernel string) (string, error) {
	var matr bootimgKernelMatrix
	switch l.version {
	case V1:
		matr = l.env_v1.Bootimg_matrix
	default:
		panic("test function unimplemented for non-v1")
	}
	for x := range matr {
		if kernel == cToGoString(matr[x][MATRIX_ROW_KERNEL][:]) {
			return cToGoString(matr[x][MATRIX_ROW_PARTITION][:]), nil
		}
	}
	return "", fmt.Errorf("cannot find kernel %q in boot image partitions", kernel)
}

// SetBootPartition sets the kernel revision reference in the provided boot
// partition reference to the provided kernel revision. It returns a non-nil err
// if the provided boot partition reference was not found.
func (l *Env) SetBootPartition(bootpart, kernel string) error {
	var matr bootimgKernelMatrix
	switch l.version {
	case V1:
		matr = l.env_v1.Bootimg_matrix
	case V2Run:
		matr = l.env_v2_run.Bootimg_matrix
	case V2Recovery:
		return fmt.Errorf("internal error: recovery lkenv has no kernel boot partition matrix")
	}
	for x := range matr {
		if bootpart == cToGoString(matr[x][MATRIX_ROW_PARTITION][:]) {
			switch l.version {
			case V1:
				copyString(l.env_v1.Bootimg_matrix[x][MATRIX_ROW_KERNEL][:], kernel)
			case V2Run:
				copyString(l.env_v2_run.Bootimg_matrix[x][MATRIX_ROW_KERNEL][:], kernel)
			}

			return nil
		}
	}
	return fmt.Errorf("cannot find defined [%s] boot image partition", bootpart)
}

// RemoveKernelRevisionFromBootPartition removes from the boot image matrix the
// first found boot partition that contains a reference to the given kernel
// revision. If the referenced kernel revision was not found, a non-nil err is
// returned, otherwise the reference is removed and nil is returned.
// Note that to persist this change the env must be saved afterwards with Save.
func (l *Env) RemoveKernelRevisionFromBootPartition(kernel string) error {
	var matr bootimgKernelMatrix
	switch l.version {
	case V1:
		matr = l.env_v1.Bootimg_matrix
	case V2Run:
		matr = l.env_v2_run.Bootimg_matrix
	case V2Recovery:
		return fmt.Errorf("internal error: recovery lkenv has no kernel boot partition matrix")
	}

	for x := range matr {
		if "" != cToGoString(matr[x][MATRIX_ROW_PARTITION][:]) {
			if kernel == cToGoString(matr[x][MATRIX_ROW_KERNEL][:]) {
				switch l.version {
				case V1:
					l.env_v1.Bootimg_matrix[x][1][MATRIX_ROW_PARTITION] = 0
				case V2Run:
					l.env_v2_run.Bootimg_matrix[x][1][MATRIX_ROW_PARTITION] = 0
				}

				return nil
			}
		}
	}
	return fmt.Errorf("cannot find defined [%s] boot image partition", kernel)
}

// GetBootImageName return expected boot image file name in kernel snap
func (l *Env) GetBootImageName() string {
	fn := l.Get("bootimg_file_name")
	if fn != "" {
		return fn
	}
	return BOOTIMG_DEFAULT_NAME
}
