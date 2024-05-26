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

	"golang.org/x/xerrors"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil"
)

const (
	SNAP_BOOTSELECT_VERSION_V1 = 0x00010001
	SNAP_BOOTSELECT_VERSION_V2 = 0x00010010
)

// const SNAP_BOOTSELECT_SIGNATURE ('S' | ('B' << 8) | ('s' << 16) | ('e' << 24))
// value comes from S(Snap)B(Boot)se(select)
const SNAP_BOOTSELECT_SIGNATURE = 0x53 | 0x42<<8 | 0x73<<16 | 0x65<<24

// const SNAP_BOOTSELECT_RECOVERY_SIGNATURE ('S' | ('R' << 8) | ('s' << 16) | ('e' << 24))
// value comes from S(Snap)R(Recovery)se(select)
const SNAP_BOOTSELECT_RECOVERY_SIGNATURE = 0x53 | 0x52<<8 | 0x73<<16 | 0x65<<24

// SNAP_FILE_NAME_MAX_LEN is the maximum size of a C string representing a snap name,
// such as for a kernel snap revision.
const SNAP_FILE_NAME_MAX_LEN = 256

// SNAP_BOOTIMG_PART_NUM  is the number of available boot image partitions
const SNAP_BOOTIMG_PART_NUM = 2

// SNAP_RUN_BOOTIMG_PART_NUM is the number of available boot image partitions
// for uc20 for kernel/try-kernel in run mode
const SNAP_RUN_BOOTIMG_PART_NUM = 2

/** maximum number of available bootimg partitions for recovery systems, min 5
 *  NOTE: the number of actual bootimg partitions usable is determined by the
 *  gadget, this just sets the upper bound of maximum number of recovery systems
 *  a gadget could define without needing changes here
 */
const SNAP_RECOVERY_BOOTIMG_PART_NUM = 10

/* Default boot image file name to be used from kernel snap */
const BOOTIMG_DEFAULT_NAME = "boot.img"

// for accessing the 	Bootimg_matrix
const (
	// the boot image partition label itself
	MATRIX_ROW_PARTITION = 0
	// the value of the boot image partition label mapping (i.e. the kernel
	// revision or the recovery system label, depending on which specific
	// matrix is being operated on)
	MATRIX_ROW_VALUE = 1
)

type Version int

const (
	V1 Version = iota
	V2Run
	V2Recovery
)

// Number returns the Version of the lkenv version as it is encoded in the
func (v Version) Number() uint32 {
	switch v {
	case V1:
		return SNAP_BOOTSELECT_VERSION_V1
	case V2Run, V2Recovery:
		return SNAP_BOOTSELECT_VERSION_V2
	default:
		panic(fmt.Sprintf("unknown lkenv version number: %v", v))
	}
}

// Signature returns the Signature of the lkenv version.
func (v Version) Signature() uint32 {
	switch v {
	case V1, V2Run:
		return SNAP_BOOTSELECT_SIGNATURE
	case V2Recovery:
		return SNAP_BOOTSELECT_RECOVERY_SIGNATURE
	default:
		panic(fmt.Sprintf("unknown lkenv version number: %v", v))
	}
}

type envVariant interface {
	// get returns the value of a key in the env object.
	get(string) string

	// set sets a key to a value in the env object.
	set(string, string)

	// currentCrc32 is a helper method to return the value of the crc32 stored in the
	// environment variable - it is NOT a method to calculate the current value,
	// it is used to store the crc32 for helper methods that validate the crc32
	// independently of what is in the environment.
	currentCrc32() uint32
	// currentVersion is the same kind of helper method as currentCrc32(),
	// always returning the value from the object itself.
	currentVersion() uint32
	// currentSignature is the same kind of helper method as currentCrc32(),
	// always returning the value from the object itself.
	currentSignature() uint32

	// bootImgKernelMatrix returns the boot image matrix from the environment
	// which stores the kernel revisions for the boot image partitions. The boot
	// image matrix is used for various exported methods such as
	// SetBootPartitionKernel(), etc.
	bootImgKernelMatrix() (bootimgMatrixGeneric, error)

	// bootImgRecoverySystemMatrix returns the boot image matrix from the
	// environment which stores the recovery system labels for the boot image
	// partitions. The boot image matrix is used for various recovery system
	// methods such as FindFreeRecoverySystemBootPartition(), etc.
	bootImgRecoverySystemMatrix() (bootimgMatrixGeneric, error)
}

var (
	// the variant implementations must all implement envVariant
	_ = envVariant(&SnapBootSelect_v1{})
	_ = envVariant(&SnapBootSelect_v2_run{})
	_ = envVariant(&SnapBootSelect_v2_recovery{})
)

// Env contains the data of the little kernel environment
type Env struct {
	// path is the primary lkenv object file, it can be a regular file during
	// build time, or it can be a partition device node at run time
	path string
	// pathbak is the backup lkenv object file, it too can either be a regular
	// file during build time, or a partition device node at run time, and it is
	// typically at prepare-image time given by "<path>" + "bak", i.e.
	// $PWD/lk.conf and $PWD/lk.confbak but will be different device nodes for
	// different partitions at runtime.
	pathbak string
	// version is the configured version of the lkenv object from NewEnv.
	version Version
	// variant is the internal implementation of the lkenv object, dependent on
	// the version. It is tracked separately such that we can verify a given
	// variant matches the specified version when loading an lkenv object from
	// disk.
	variant envVariant
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

// NewEnv creates a new lkenv object referencing the primary bootloader
// environment file at path with the specified version. If the specified filed
// is expected to be a valid lkenv object, then the object should be loaded with
// the Load() method, otherwise the lkenv object can be manipulated in memory
// and later written to disk with Save().
func NewEnv(path, backupPath string, version Version) *Env {
	if backupPath == "" {
		// legacy behavior is for the backup file to be the same name/dir, but
		// with "bak" appended to it
		backupPath = path + "bak"
	}
	e := &Env{
		path:    path,
		pathbak: backupPath,
		version: version,
	}

	switch version {
	case V1:
		e.variant = newV1()
	case V2Recovery:
		e.variant = newV2Recovery()
	case V2Run:
		e.variant = newV2Run()
	}
	return e
}

// Load will load the lk bootloader environment from it's configured primary
// environment file, and if that fails it will fallback to trying the backup
// environment file.
func (l *Env) Load() error {
	mylog.Check(l.LoadEnv(l.path))

	return nil
}

type compatErrNotExist struct {
	err error
}

func (e compatErrNotExist) Error() string {
	return e.err.Error()
}

func (e compatErrNotExist) Unwrap() error {
	// for go 1.9 (and 1.10) xerrors compatibility, we check if os.PathError
	// implements Unwrap(), and if not return os.ErrNotExist directly
	if _, ok := e.err.(interface {
		Unwrap() error
	}); !ok {
		return os.ErrNotExist
	}
	return e.err
}

// LoadEnv loads the lk bootloader environment from the specified file. The
// bootloader environment in the referenced file must be of the same version
// that the Env object was created with using NewEnv.
// The returned error may wrap os.ErrNotExist, so instead of using
// os.IsNotExist, callers should use xerrors.Is(err,os.ErrNotExist) instead.
func (l *Env) LoadEnv(path string) error {
	f := mylog.Check2(os.Open(path))
	mylog.Check(

		// TODO: when we drop support for Go 1.9, this code can go away, in Go
		//       1.9 *os.PathError does not implement Unwrap(), and so callers
		//       that try to call xerrors.Is(err,os.ErrNotExist) will fail, so
		//       instead we do our own wrapping first such that when Unwrap() is
		//       called by xerrors.Is() it will see os.ErrNotExist directly when
		//       compiled with a version of Go that does not implement Unwrap()
		//       on os.PathError

		binary.Read(f, binary.LittleEndian, l.variant))

	// validate the version and signatures
	v := l.variant.currentVersion()
	s := l.variant.currentSignature()
	expV := l.version.Number()
	expS := l.version.Signature()

	if expV != v {
		return fmt.Errorf("cannot validate %s: expected version 0x%X, got 0x%X", path, expV, v)
	}

	if expS != s {
		return fmt.Errorf("cannot validate %s: expected signature 0x%X, got 0x%X", path, expS, s)
	}

	// independently calculate crc32 to validate structure
	w := bytes.NewBuffer(nil)
	ss := binary.Size(l.variant)
	w.Grow(ss)
	mylog.Check(binary.Write(w, binary.LittleEndian, l.variant))

	crc := crc32.ChecksumIEEE(w.Bytes()[:ss-4]) // size of crc32 itself at the end of the structure
	if crc != l.variant.currentCrc32() {
		return fmt.Errorf("cannot validate %s: expected checksum 0x%X, got 0x%X", path, crc, l.variant.currentCrc32())
	}
	logger.Debugf("validated crc32 as 0x%X for lkenv loaded from file %s", l.variant.currentCrc32(), path)

	return nil
}

// Save saves the lk bootloader environment to the configured primary
// environment file, and if the backup environment file exists, the backup too.
// Save will also update the CRC32 of the environment when writing the file(s).
func (l *Env) Save() error {
	buf := bytes.NewBuffer(nil)
	ss := binary.Size(l.variant)
	buf.Grow(ss)
	mylog.Check(binary.Write(buf, binary.LittleEndian, l.variant))

	// calculate crc32
	newCrc32 := crc32.ChecksumIEEE(buf.Bytes()[:ss-4])
	logger.Debugf("calculated lk bootloader environment crc32 as 0x%X to save", newCrc32)
	// note for efficiency's sake to avoid re-writing the whole structure, we
	// re-write _just_ the crc32 to w as little-endian
	buf.Truncate(ss - 4)
	binary.Write(buf, binary.LittleEndian, &newCrc32)
	mylog.Check(l.saveEnv(l.path, buf))

	// if there is backup environment file save to it as well
	if osutil.FileExists(l.pathbak) {
		mylog.Check(
			// TODO: if the primary succeeds but saving to the backup fails, we
			// don't return non-nil error here, should we?
			l.saveEnv(l.pathbak, buf))
	}
	return err
}

func (l *Env) saveEnv(path string, buf *bytes.Buffer) error {
	f := mylog.Check2(os.OpenFile(path, os.O_WRONLY, 0660))

	defer f.Close()
	mylog.Check2(f.Write(buf.Bytes()))
	mylog.Check(f.Sync())

	return nil
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

// InitializeBootPartitions sets the boot image partition label names.
// This function should not be used at run time!
// It should be used only at image build time, if partition labels are not
// pre-filled by gadget built, currently it is only used inside snapd for tests.
func (l *Env) InitializeBootPartitions(bootPartLabels ...string) error {
	var matr bootimgMatrixGeneric

	// calculate the min/max limits for bootPartLabels
	var min, max int
	switch l.version {
	case V1, V2Run:
		min = 2
		max = 2
		matr = mylog.Check2(l.variant.bootImgKernelMatrix())
	case V2Recovery:
		min = 1
		max = SNAP_RECOVERY_BOOTIMG_PART_NUM
		matr = mylog.Check2(l.variant.bootImgRecoverySystemMatrix())
	}

	return matr.initializeBootPartitions(bootPartLabels, min, max)
}

// FindFreeKernelBootPartition finds a free boot image partition to be used for
// a new kernel revision. It ignores the currently installed boot image
// partition used for the active kernel
func (l *Env) FindFreeKernelBootPartition(kernel string) (string, error) {
	matr := mylog.Check2(l.variant.bootImgKernelMatrix())

	// the reserved boot image partition value is just the current snap_kernel
	// if it is set (it could be unset at image build time where the lkenv is
	// unset and has no kernel revision values set for the boot image partitions)
	installedKernels := []string{}
	if installedKernel := l.variant.get("snap_kernel"); installedKernel != "" {
		installedKernels = []string{installedKernel}
	}
	return matr.findFreeBootPartition(installedKernels, kernel)
}

// GetKernelBootPartition returns the first found boot image partition label
// that contains a reference to the given kernel revision. If the revision was
// not found, a non-nil error is returned.
func (l *Env) GetKernelBootPartition(kernel string) (string, error) {
	matr := mylog.Check2(l.variant.bootImgKernelMatrix())

	bootPart := mylog.Check2(matr.getBootPartWithValue(kernel))

	return bootPart, nil
}

// SetBootPartitionKernel sets the kernel revision reference for the provided
// boot image partition label. It returns a non-nil err if the provided boot
// image partition label was not found.
func (l *Env) SetBootPartitionKernel(bootpart, kernel string) error {
	matr := mylog.Check2(l.variant.bootImgKernelMatrix())

	return matr.setBootPart(bootpart, kernel)
}

// RemoveKernelFromBootPartition removes from the boot image matrix the
// first found boot image partition that contains a reference to the given
// kernel revision. If the referenced kernel revision was not found, a non-nil
// err is returned, otherwise the reference is removed and nil is returned.
func (l *Env) RemoveKernelFromBootPartition(kernel string) error {
	matr := mylog.Check2(l.variant.bootImgKernelMatrix())

	return matr.dropBootPartValue(kernel)
}

// FindFreeRecoverySystemBootPartition finds a free recovery system boot image
// partition to be used for the recovery kernel from the recovery system. It
// only considers boot image partitions that are currently not set to a recovery
// system to be free.
func (l *Env) FindFreeRecoverySystemBootPartition(recoverySystem string) (string, error) {
	matr := mylog.Check2(l.variant.bootImgRecoverySystemMatrix())

	// when we create a new recovery system partition, we set all current
	// recovery systems as reserved, so first get that list
	currentRecoverySystems := matr.assignedBootPartValues()
	return matr.findFreeBootPartition(currentRecoverySystems, recoverySystem)
}

// SetBootPartitionRecoverySystem sets the recovery system reference for the
// provided boot image partition. It returns a non-nil err if the provided boot
// partition reference was not found.
func (l *Env) SetBootPartitionRecoverySystem(bootpart, recoverySystem string) error {
	matr := mylog.Check2(l.variant.bootImgRecoverySystemMatrix())

	return matr.setBootPart(bootpart, recoverySystem)
}

// GetRecoverySystemBootPartition returns the first found boot image partition
// label that contains a reference to the given recovery system. If the recovery
// system was not found, a non-nil error is returned.
func (l *Env) GetRecoverySystemBootPartition(recoverySystem string) (string, error) {
	matr := mylog.Check2(l.variant.bootImgRecoverySystemMatrix())

	bootPart := mylog.Check2(matr.getBootPartWithValue(recoverySystem))

	return bootPart, nil
}

// RemoveRecoverySystemFromBootPartition removes from the boot image matrix the
// first found boot partition that contains a reference to the given recovery
// system. If the referenced recovery system was not found, a non-nil err is
// returned, otherwise the reference is removed and nil is returned.
func (l *Env) RemoveRecoverySystemFromBootPartition(recoverySystem string) error {
	matr := mylog.Check2(l.variant.bootImgRecoverySystemMatrix())

	return matr.dropBootPartValue(recoverySystem)
}

// GetBootImageName return expected boot image file name in kernel snap. If
// unset, it will return the default boot.img name.
func (l *Env) GetBootImageName() string {
	fn := l.Get("bootimg_file_name")
	if fn != "" {
		return fn
	}
	return BOOTIMG_DEFAULT_NAME
}

// common matrix helper methods which operate on the boot image matrix, which is
// a mapping of boot image partition label to either a kernel revision or a
// recovery system label.

// bootimgMatrixGeneric is a generic slice version of the above two matrix types
// which are both statically sized arrays, and thus not able to be used
// interchangeably while the slice is.
type bootimgMatrixGeneric [][2][SNAP_FILE_NAME_MAX_LEN]byte

// initializeBootPartitions is a test helper method to set all the boot image
// partition labels for a lkenv object, normally this is done by the gadget at
// image build time and not done by snapd, but we do this in tests.
// The min and max arguments are for size checking of the provided array of
// bootPartLabels
func (matr bootimgMatrixGeneric) initializeBootPartitions(bootPartLabels []string, min, max int) error {
	numBootPartLabels := len(bootPartLabels)

	if numBootPartLabels < min || numBootPartLabels > max {
		return fmt.Errorf("invalid number of boot image partitions, expected %d got %d", len(matr), numBootPartLabels)
	}
	for x, label := range bootPartLabels {
		copyString(matr[x][MATRIX_ROW_PARTITION][:], label)
	}
	return nil
}

// dropBootPartValue will remove the specified bootPartValue from the boot image
// matrix - it _only_ deletes the value, not the boot image partition label
// itself, as the boot image partition labels are static for the lifetime of a
// device and should never be changed (as those values correspond to physical
// names of the formatted partitions and we don't yet support repartitioning of
// any kind).
func (matr bootimgMatrixGeneric) dropBootPartValue(bootPartValue string) error {
	for x := range matr {
		if cToGoString(matr[x][MATRIX_ROW_PARTITION][:]) != "" {
			if bootPartValue == cToGoString(matr[x][MATRIX_ROW_VALUE][:]) {
				// clear the string by setting the first element to 0 or NUL
				matr[x][MATRIX_ROW_VALUE][0] = 0
				return nil
			}
		}
	}

	return fmt.Errorf("cannot find %q in boot image partitions", bootPartValue)
}

// setBootPart associates the specified boot image partition label to the
// specified value.
func (matr bootimgMatrixGeneric) setBootPart(bootpart, bootPartValue string) error {
	for x := range matr {
		if bootpart == cToGoString(matr[x][MATRIX_ROW_PARTITION][:]) {
			copyString(matr[x][MATRIX_ROW_VALUE][:], bootPartValue)
			return nil
		}
	}

	return fmt.Errorf("cannot find boot image partition %s", bootpart)
}

// findFreeBootPartition will return a boot image partition that can be
// used for a new value, specifically skipping the reserved values. It may
// return either a boot image partition that does not contain any value or
// a boot image partition that already contains the specified value. The
// reserved argument is typically used for already installed values, such as the
// currently installed kernel snap revision, so that a new try kernel snap does
// not overwrite the existing installed kernel snap.
func (matr bootimgMatrixGeneric) findFreeBootPartition(reserved []string, newValue string) (string, error) {
	for x := range matr {
		bootPartLabel := cToGoString(matr[x][MATRIX_ROW_PARTITION][:])
		// skip boot image partition labels that are unset, for example this may
		// happen if a system only has 3 physical boot image partitions for
		// recovery system kernels, but the same matrix structure has 10 slots
		// and all 3 usable slots are in use by installed reserved recovery
		// systems.
		if bootPartLabel == "" {
			continue
		}

		val := cToGoString(matr[x][MATRIX_ROW_VALUE][:])

		// if the value is exactly the same, as requested return it, this needs
		// to be handled before checking the reserved values since we may
		// sometimes need to find a "free" boot partition for the specific
		// kernel revision that is already installed, thus it will show up in
		// the reserved list, but it will also be newValue
		// this case happens in practice during seeding of kernels on uc16/uc18,
		// where we already extracted the kernel at image build time and we will
		// go to extract the kernel again during seeding
		if val == newValue {
			return bootPartLabel, nil
		}

		// if this value was reserved, skip it
		if strutil.ListContains(reserved, val) {
			continue
		}

		// otherwise consider it to be free, even if it was set to something
		// else - this is because callers should be using reserved to prevent
		// overwriting the wrong boot image partition value
		return bootPartLabel, nil
	}

	return "", fmt.Errorf("cannot find free boot image partition")
}

// assignedBootPartValues returns all boot image partitions values that are set.
func (matr bootimgMatrixGeneric) assignedBootPartValues() []string {
	bootPartValues := make([]string, 0, len(matr))
	for x := range matr {
		bootPartLabel := cToGoString(matr[x][MATRIX_ROW_PARTITION][:])
		if bootPartLabel != "" {
			// now check the value
			bootPartValue := cToGoString(matr[x][MATRIX_ROW_VALUE][:])
			if bootPartValue != "" {
				bootPartValues = append(bootPartValues, bootPartValue)
			}
		}
	}

	return bootPartValues
}

// getBootPartWithValue returns the boot image partition label for the specified value.
// If the boot image partition label does not exist in the matrix, an error will
// be returned.
func (matr bootimgMatrixGeneric) getBootPartWithValue(value string) (string, error) {
	for x := range matr {
		if value == cToGoString(matr[x][MATRIX_ROW_VALUE][:]) {
			return cToGoString(matr[x][MATRIX_ROW_PARTITION][:]), nil
		}
	}

	return "", fmt.Errorf("no boot image partition has value %q", value)
}
