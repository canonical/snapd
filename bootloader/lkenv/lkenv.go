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

type bootimgKernelMatrix [SNAP_BOOTIMG_PART_NUM][2][SNAP_NAME_MAX_LEN]byte

type Version int

const (
	V1 Version = iota
	V2Run
	V2Recovery
)

var (
	validationErr = "cannot validate %s: got version of 0x%X (expected 0x%X), got signature of 0x%X (expected 0x%X)"
)

type envVariant interface {
	// TODO: setup necessary ?
	// setup sets up the env object
	setup() error
	// get returns the value of a key in the env object
	get(string) string
	// set sets a key to a value in the env object
	set(string, string)
	// configureBootPartitions is a helper method for tests to setup an env
	configureBootPartitions(bootPartLabels []string) error
	// load reads the file into the env object and validates it
	load(string) error

	// crc32 is a helper method to return the value of the crc32 stored in the
	// environment variable - it is NOT a method to calculate the current value,
	// it is used to store the crc32 for helper methods that validate the crc32
	// independently of what is in the environment
	crc32() uint32

	version() uint32
	signature() uint32

	// the following functions are only for v1 and v2 run

	// removeKernelFromBootPart finds the boot partition with the kernel
	// reference and removes the reference from that boot partition
	removeKernelFromBootPart(string) error

	// setBootPartition
	setBootPartition(string, string) error
	findFreeBootPartition(string) (string, error)

	// these functions are only for v2 recovery
	setRecoveryBootPartition(string, string) error
	findFreeRecoveryPartition(string) (string, error)

	// TODO: this is a test only function ?
	getBootPartition(string) (string, error)
}

var (
	_ = envVariant(&SnapBootSelect_v1{})
	_ = envVariant(&SnapBootSelect_v2_run{})
	_ = envVariant(&SnapBootSelect_v2_recovery{})
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
	variant         envVariant
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
		// TODO: move the initial assignment of members to setup() ?
		e.variant = &SnapBootSelect_v1{
			Signature: SNAP_BOOTSELECT_SIGNATURE,
			Version:   SNAP_BOOTSELECT_VERSION_V1,
		}
	case V2Recovery:
		e.variant = &SnapBootSelect_v2_recovery{
			Signature: SNAP_BOOTSELECT_SIGNATURE,
			Version:   SNAP_BOOTSELECT_VERSION_V2,
		}
	case V2Run:
		e.variant = &SnapBootSelect_v2_run{
			Signature: SNAP_BOOTSELECT_SIGNATURE,
			Version:   SNAP_BOOTSELECT_VERSION_V2,
		}
	}
	return e
}

// Get returns the value of the key from the environment. If the key specified
// is not supported for the environment, the empty string is returned.
func (l *Env) Get(key string) string {
	return l.variant.get(key)
}

// Set assigns the value to the key in the environment. If the key specified is
// not supported for the environment, nothing happens.
func (l *Env) Set(key, value string) {
	l.variant.set(key, value)
}

// ConfigureBootPartitions set boot partitions label names
// this function should not be used at run time!
// it should be used only at image build time,
// if partition labels are not pre-filled by gadget built
// TODO: this function is currently unused?
func (l *Env) ConfigureBootPartitions(bootPartLabels ...string) error {
	return l.variant.configureBootPartitions(bootPartLabels)
}

// ConfigureBootimgName configures the filename of the bootimg that is extracted
// for the kernel when preparing the image. It should only be used if the
// bootimg filename was not configured when building the gadget.
// This is not to be used at runtime!
func (l *Env) ConfigureBootimgName(bootimgName string) {
	l.Set("bootimg_file_name", bootimgName)
}

// Load will load the lk bootloader environment from it's configured primary
// environment file, and if that fails it will fallback to trying the backup
// environment file.
func (l *Env) Load() error {
	err := l.LoadEnv(l.path)
	if err != nil {
		logger.Noticef("cannot load primary bootloader environment: %v\n", err)
		logger.Noticef("attempting to load backup bootloader environment\n")
		return l.LoadEnv(l.pathbak)
	}
	return nil
}

// LoadEnv loads the lk bootloader environment from the specified file.
func (l *Env) LoadEnv(path string) error {
	return l.variant.load(path)
}

func commonSerialize(v interface{}) (*bytes.Buffer, error) {
	w := bytes.NewBuffer(nil)
	ss := binary.Size(v)
	w.Grow(ss)
	if err := binary.Write(w, binary.LittleEndian, v); err != nil {
		return nil, fmt.Errorf("cannot write LK env to buffer for saving: %v", err)
	}

	// calculate crc32
	newCrc32 := crc32.ChecksumIEEE(w.Bytes()[:ss-4])
	logger.Debugf("calculated lk bootloader environment crc32: 0x%X", newCrc32)
	// note for efficiency's sake to avoid re-writing the whole structure, we
	// re-write _just_ the crc32 w as little-endian
	w.Truncate(ss - 4)
	binary.Write(w, binary.LittleEndian, &newCrc32)
	return w, nil
}

// Save saves the lk bootloader environment to the configured primary
// environment file, and if the backup environment file exists, the backup too.
func (l *Env) Save() error {
	buf, err := commonSerialize(l.variant)
	if err != nil {
		return err
	}

	err = l.saveEnv(l.path, buf)
	if err != nil {
		logger.Noticef("failed to save primary bootloader environment: %v", err)
	}
	// if there is backup environment file save to it as well
	if osutil.FileExists(l.pathbak) {
		// TODO: if the primary succeeds but saving to the backup fails, we
		// don't return non-nil error here, should we?
		if err := l.saveEnv(l.pathbak, buf); err != nil {
			logger.Noticef("failed to save backup environment: %v", err)
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
	return l.variant.findFreeBootPartition(kernel)
}

// FindFreeRecoverySystemPartition finds a free recovery system boot partition
// to be used for the recovery kernel from the recovery system. It follows the
// same internal logic as FindFreeBootPartition, but only operates on V2
// recovery environments.
func (l *Env) FindFreeRecoverySystemPartition(recoverySystem string) (string, error) {
	return l.variant.findFreeRecoveryPartition(recoverySystem)
}

// SetRecoverySystemBootPartition sets the recovery system reference in the
// provided boot partition reference to the provided recovery system. It returns
// a non-nil err if the provided boot partition reference was not found.
func (l *Env) SetRecoverySystemBootPartition(bootpart, recoverySystem string) error {
	return l.variant.setRecoveryBootPartition(bootpart, recoverySystem)
}

// GetBootPartition returns the first found boot partition that contains a
// reference to the given kernel revision. If the revision was not found, a
// non-nil error is returned.
func (l *Env) GetBootPartition(kernel string) (string, error) {
	return l.variant.getBootPartition(kernel)
}

// SetBootPartition sets the kernel revision reference in the provided boot
// partition reference to the provided kernel revision. It returns a non-nil err
// if the provided boot partition reference was not found.
func (l *Env) SetBootPartition(bootpart, kernel string) error {
	return l.variant.setBootPartition(bootpart, kernel)
}

// RemoveKernelRevisionFromBootPartition removes from the boot image matrix the
// first found boot partition that contains a reference to the given kernel
// revision. If the referenced kernel revision was not found, a non-nil err is
// returned, otherwise the reference is removed and nil is returned.
// Note that to persist this change the env must be saved afterwards with Save.
func (l *Env) RemoveKernelRevisionFromBootPartition(kernel string) error {
	return l.variant.removeKernelFromBootPart(kernel)
}

// GetBootImageName return expected boot image file name in kernel snap
func (l *Env) GetBootImageName() string {
	fn := l.Get("bootimg_file_name")
	if fn != "" {
		return fn
	}
	return BOOTIMG_DEFAULT_NAME
}

// common matrix helper methods which take the matrix as input, then return an
// updated version to be re-assigned to the original struct

func commonConfigureBootPartitions(matr bootimgKernelMatrix, bootPartLabels []string) (bootimgKernelMatrix, error) {
	numBootPartLabels := len(bootPartLabels)

	if numBootPartLabels != SNAP_BOOTIMG_PART_NUM {
		return matr, fmt.Errorf("invalid number of boot partition labels, expected %d got %d", SNAP_BOOTIMG_PART_NUM, numBootPartLabels)
	}
	copyString(matr[0][MATRIX_ROW_PARTITION][:], bootPartLabels[0])
	copyString(matr[1][MATRIX_ROW_PARTITION][:], bootPartLabels[1])
	return matr, nil
}

func commonRemoveKernelFromBootPart(matr bootimgKernelMatrix, kernel string) (bootimgKernelMatrix, error) {
	for x := range matr {
		if "" != cToGoString(matr[x][MATRIX_ROW_PARTITION][:]) {
			if kernel == cToGoString(matr[x][MATRIX_ROW_KERNEL][:]) {
				matr[x][1][MATRIX_ROW_PARTITION] = 0
				return matr, nil
			}
		}
	}

	return matr, fmt.Errorf("cannot find kernel %q in boot image partitions", kernel)
}

func commonSetBootPartition(matr bootimgKernelMatrix, bootpart, kernel string) (bootimgKernelMatrix, error) {
	for x := range matr {
		if bootpart == cToGoString(matr[x][MATRIX_ROW_PARTITION][:]) {
			copyString(matr[x][MATRIX_ROW_KERNEL][:], kernel)
			return matr, nil
		}
	}

	return matr, fmt.Errorf("cannot find defined [%s] boot image partition", bootpart)
}

func commonFindFreeBootPartition(env envVariant, matr bootimgKernelMatrix, kernel string) (string, error) {
	for x := range matr {
		bp := cToGoString(matr[x][MATRIX_ROW_PARTITION][:])
		if bp != "" {
			k := cToGoString(matr[x][MATRIX_ROW_KERNEL][:])
			// return this one if it's not the current snap_kernel, if it's the
			// exactly specified kernel, or if it's empty
			if k != env.get("snap_kernel") || k == kernel || k == "" {
				return cToGoString(matr[x][MATRIX_ROW_PARTITION][:]), nil
			}
		}
	}

	return "", fmt.Errorf("cannot find free partition for boot image")
}

func commonGetBootPartition(matr bootimgKernelMatrix, kernel string) (string, error) {
	for x := range matr {
		if kernel == cToGoString(matr[x][MATRIX_ROW_KERNEL][:]) {
			return cToGoString(matr[x][MATRIX_ROW_PARTITION][:]), nil
		}
	}

	return "", fmt.Errorf("cannot find kernel %q in boot image partitions", kernel)
}

func commonLoad(path string, env envVariant, expVers, expSign uint32) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("cannot open LK env file: %v", err)
	}

	if err := binary.Read(f, binary.LittleEndian, env); err != nil {
		return fmt.Errorf("cannot read LK env from file: %v", err)
	}

	// cache the crc32 from the file we just read
	originalCRC32 := env.crc32()

	// independently calculate crc32 to validate structure
	w := bytes.NewBuffer(nil)
	ss := binary.Size(env)
	w.Grow(ss)
	if err := binary.Write(w, binary.LittleEndian, env); err != nil {
		return fmt.Errorf("cannot write LK env to buffer for validation: %v", err)
	}

	if env.version() != expVers ||
		env.signature() != expSign {
		return fmt.Errorf(
			validationErr,
			path,
			env.version(),
			expVers,
			env.signature(),
			expSign,
		)
	}

	crc := crc32.ChecksumIEEE(w.Bytes()[:ss-4]) // size of crc32 itself at the end of the structure
	if crc != originalCRC32 {
		return fmt.Errorf("cannot validate environment checksum %s, got 0x%X expected 0x%X", path, crc, originalCRC32)
	}
	logger.Debugf("Load: validated crc32 (0x%X)", originalCRC32)

	return nil
}
