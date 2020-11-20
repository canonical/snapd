// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"fmt"
)

/**
 * Following structure has to be kept in sync with c structure defined by
 * include/lk/snappy-boot_v2.h
 * c headerfile is used by bootloader, this ensures sync of the environment
 * between snapd and bootloader

 * when this structure needs to be updated,
 * new version should be introduced instead together with c header file,
 * which is to be adopted by bootloader
 *
 * !!! Support for old version has to be maintained, as it is not guaranteed
 * all existing bootloader would adopt new version!
 */

type SnapBootSelect_v2_recovery struct {
	/* Contains value BOOTSELECT_SIGNATURE defined above */
	Signature uint32
	/* snappy boot select version */
	Version uint32

	/** snapd_recovery_mode is what mode the system will be booted in, one of
	 *  "install", "recover" or "run"
	 */
	Snapd_recovery_mode [SNAP_NAME_MAX_LEN]byte

	/** snapd_recovery_system defines the recovery system label to be used when
	 *  booting the system, it must be defined to one of the values in the
	 *  bootimg matrix below
	 */
	Snapd_recovery_system [SNAP_NAME_MAX_LEN]byte

	/**
	 * Matrix for mapping of recovery system boot img partition to kernel snap
	 *   revisions for those recovery systems
	 *
	 * First column represents boot image partition label (e.g. recov_a, recov_a)
	 *   value are static and should be populated at gadget build time
	 *   or latest at image build time. Values are not further altered at run
	 *   time.
	 * Second column represents the name of the currently installed recovery
	 *   system label there - note that every recovery system has only one
	 *   kernel for it, so this is in effect a proxy for the kernel revision
	 *
	 * The initial value representing initial single recovery system is
	 *   populated at image build time by snapd
	 *
	 * There are SNAP_RECOVER_BOOTIMG_PART_NUM rows in the matrix, representing
	 *   all possible recovery systems on the image.
	 * The following describes how this matrix should be modified at different
	 * stages:
	 *  - at image build time:
	 *    - default recovery system label should be filled into free slot
	 *      (first row, second column)
	 *  - snapd:
	 *    - when new recovery system is being created, snapd cycles
	 *      through matrix to find unused 'boot slot' to be used for new
	 *      recovery system from free slot, first column represents partition
	 *      label to which kernel snap boot image should be extracted. Second
	 *      column is then populated recovery system label.
	 *    - snapd_recovery_mode and snapd_recovery_system are written/used
	 *      normally when transitioning to/from recover/install/run modes
	 *  - bootloader:
	 *    - bootloader reads snapd_recovery_system to determine what label
	 *      should be searched for in the matrix, then finds the corresponding
	 *      partition label for the kernel snap from that recovery system. Then
	 *      snapd_recovery_mode is read and both variables are put onto the
	 *      kernel commandline when booting the linux kernel
	 *    - bootloader NEVER alters this matrix values
	 *
	 * [ <bootimg 1 part label> ] [ <kernel snap revision installed in this boot partition> ]
	 * [ <bootimg 2 part label> ] [ <kernel snap revision installed in this boot partition> ]
	 */
	Bootimg_matrix [SNAP_RECOVER_BOOTIMG_PART_NUM][2][SNAP_NAME_MAX_LEN]byte

	/* name of the boot image from kernel snap to be used for extraction
	when not defined or empty, default boot.img will be used */
	Bootimg_file_name [SNAP_NAME_MAX_LEN]byte

	/* unused placeholders for additional parameters in the future */
	Unused_key_01 [SNAP_NAME_MAX_LEN]byte
	Unused_key_02 [SNAP_NAME_MAX_LEN]byte
	Unused_key_03 [SNAP_NAME_MAX_LEN]byte
	Unused_key_04 [SNAP_NAME_MAX_LEN]byte
	Unused_key_05 [SNAP_NAME_MAX_LEN]byte
	Unused_key_06 [SNAP_NAME_MAX_LEN]byte
	Unused_key_07 [SNAP_NAME_MAX_LEN]byte
	Unused_key_08 [SNAP_NAME_MAX_LEN]byte
	Unused_key_09 [SNAP_NAME_MAX_LEN]byte
	Unused_key_10 [SNAP_NAME_MAX_LEN]byte
	Unused_key_11 [SNAP_NAME_MAX_LEN]byte
	Unused_key_12 [SNAP_NAME_MAX_LEN]byte
	Unused_key_13 [SNAP_NAME_MAX_LEN]byte
	Unused_key_14 [SNAP_NAME_MAX_LEN]byte
	Unused_key_15 [SNAP_NAME_MAX_LEN]byte
	Unused_key_16 [SNAP_NAME_MAX_LEN]byte
	Unused_key_17 [SNAP_NAME_MAX_LEN]byte
	Unused_key_18 [SNAP_NAME_MAX_LEN]byte
	Unused_key_19 [SNAP_NAME_MAX_LEN]byte
	Unused_key_20 [SNAP_NAME_MAX_LEN]byte

	/* unused array of 10 key value pairs */
	Key_value_pairs [10][2][SNAP_NAME_MAX_LEN]byte

	/* crc32 value for structure */
	Crc32 uint32
}

func (v2recovery *SnapBootSelect_v2_recovery) crc32() uint32 {
	return v2recovery.Crc32
}

func (v2recovery *SnapBootSelect_v2_recovery) version() uint32 {
	return v2recovery.Version
}

func (v2recovery *SnapBootSelect_v2_recovery) signature() uint32 {
	return v2recovery.Signature
}

func (v2recovery *SnapBootSelect_v2_recovery) setup() error {
	return nil
}

func (v2recovery *SnapBootSelect_v2_recovery) get(key string) string {
	switch key {
	case "snapd_recovery_mode":
		return cToGoString(v2recovery.Snapd_recovery_mode[:])
	case "snapd_recovery_system":
		return cToGoString(v2recovery.Snapd_recovery_system[:])
	case "bootimg_file_name":
		return cToGoString(v2recovery.Bootimg_file_name[:])
	}
	return ""
}

func (v2recovery *SnapBootSelect_v2_recovery) set(key, value string) {
	switch key {
	case "snapd_recovery_mode":
		copyString(v2recovery.Snapd_recovery_mode[:], value)
	case "snapd_recovery_system":
		copyString(v2recovery.Snapd_recovery_system[:], value)
	case "bootimg_file_name":
		copyString(v2recovery.Bootimg_file_name[:], value)
	}
}

func (v2recovery *SnapBootSelect_v2_recovery) configureBootPartitions(bootPartLabels []string) error {
	numBootPartLabels := len(bootPartLabels)

	// too many
	if numBootPartLabels > SNAP_RECOVER_BOOTIMG_PART_NUM {
		return fmt.Errorf("too many (%d) boot partition labels for v2 lkenv run mode, expected no more than %d", numBootPartLabels, SNAP_RECOVER_BOOTIMG_PART_NUM)
	}
	// too few
	if numBootPartLabels < SNAP_BOOTIMG_PART_NUM {
		return fmt.Errorf("too few (%d) boot partition labels for v2 lkenv run mode, expected at least %d", numBootPartLabels, SNAP_BOOTIMG_PART_NUM)
	}

	// TODO: implement this for tests

	return nil
}

func (v2recovery *SnapBootSelect_v2_recovery) load(path string) error {
	return commonLoad(path, v2recovery, SNAP_BOOTSELECT_VERSION_V2, SNAP_BOOTSELECT_SIGNATURE)
}

// TODO: the following two implementations are very similar to the kernel bootimg
// matrix implementations, but they operate on a differently sized array, perhaps
// we could do something inefficient like copy to a slice of any size and pass
// that to a helper which operates on slices? would be ugly but kind of nice
func (v2recovery *SnapBootSelect_v2_recovery) setRecoveryBootPartition(bootpart string, recoverySystem string) error {
	for x := range v2recovery.Bootimg_matrix {
		if bootpart == cToGoString(v2recovery.Bootimg_matrix[x][MATRIX_ROW_PARTITION][:]) {
			copyString(v2recovery.Bootimg_matrix[x][MATRIX_ROW_RECOVERY_SYSTEM][:], recoverySystem)
			return nil
		}
	}
	return fmt.Errorf("cannot find defined [%s] boot image partition", bootpart)
}

func (v2recovery *SnapBootSelect_v2_recovery) findFreeRecoveryPartition(recoverySystem string) (string, error) {
	for x := range v2recovery.Bootimg_matrix {
		bp := cToGoString(v2recovery.Bootimg_matrix[x][MATRIX_ROW_PARTITION][:])
		if bp != "" {
			sys := cToGoString(v2recovery.Bootimg_matrix[x][MATRIX_ROW_RECOVERY_SYSTEM][:])
			// return this one if it's the exact specified recovery system or if
			// it's empty
			if sys == recoverySystem || sys == "" {
				return cToGoString(v2recovery.Bootimg_matrix[x][MATRIX_ROW_PARTITION][:]), nil
			}
		}
	}
	return "", fmt.Errorf("cannot find free partition for recovery system")
}

// unimplemented for v2 recovery

func (v2recovery *SnapBootSelect_v2_recovery) removeKernelFromBootPart(kernel string) error {
	return fmt.Errorf("internal error: recovery lkenv has no kernel boot partition matrix")
}

func (v2recovery *SnapBootSelect_v2_recovery) setBootPartition(bootpart, kernel string) error {
	return fmt.Errorf("internal error: recovery lkenv has no kernel boot partition matrix")
}

func (v2recovery *SnapBootSelect_v2_recovery) findFreeBootPartition(kernel string) (string, error) {
	return "", fmt.Errorf("internal error: recovery lkenv has no kernel boot partition matrix")
}

func (v2recovery *SnapBootSelect_v2_recovery) getBootPartition(kernel string) (string, error) {
	return "", fmt.Errorf("test function unimplemented for non-v1")
}

type SnapBootSelect_v2_run struct {
	/* Contains value BOOTSELECT_SIGNATURE defined above */
	Signature uint32
	/* snappy boot select version */
	Version uint32

	/* kernel_status, one of: 'empty', "try", "trying" */
	Kernel_status [SNAP_NAME_MAX_LEN]byte
	/* current kernel snap revision */
	Snap_kernel [SNAP_NAME_MAX_LEN]byte
	/* current try kernel snap revision */
	Snap_try_kernel [SNAP_NAME_MAX_LEN]byte

	/* gadget_mode, one of: 'empty', "try", "trying" */
	Gadget_mode [SNAP_NAME_MAX_LEN]byte
	/* GADGET assets: current gadget assets revision */
	Snap_gadget [SNAP_NAME_MAX_LEN]byte
	/* GADGET assets: try gadget assets revision */
	Snap_try_gadget [SNAP_NAME_MAX_LEN]byte

	/**
	 * Matrix for mapping of run mode boot img partition to installed kernel
	 *   snap revision
	 *
	 * First column represents boot image partition label (e.g. boot_a,boot_b )
	 *   value are static and should be populated at gadget built time
	 *   or latest at image build time. Values are not further altered at run
	 *   time.
	 * Second column represents name currently installed kernel snap
	 *   e.g. pi2-kernel_123.snap
	 * initial value representing initial kernel snap revision
	 *   is populated at image build time by snapd
	 *
	 * There are two rows in the matrix, representing current and previous
	 * kernel revision
	 * The following describes how this matrix should be modified at different
	 * stages:
	 *  - snapd in install mode:
	 *    - extracted kernel snap revision name should be filled
	 *      into free slot (first row, second row)
	 *  - snapd in run mode:
	 *    - when new kernel snap revision is being installed, snapd cycles
	 *      through matrix to find unused 'boot slot' to be used for new kernel
	 *      snap revision from free slot, first column represents partition
	 *      label to which kernel snap boot image should be extracted. Second
	 *      column is then populated with kernel snap revision name.
	 *    - kernel_status, snap_try_kernel, snap_try_core behaves same way as
	 *      with u-boot
	 *  - bootloader:
	 *    - bootloader reads kernel_status to determine if snap_kernel or
	 *      snap_try_kernel is used to get kernel snap revision name.
	 *      kernel snap revision is then used to search matrix to determine
	 *      partition label to be used for current boot
	 *    - bootloader NEVER alters this matrix values
	 *
	 * [ <bootimg 1 part label> ] [ <kernel snap revision installed in this boot partition> ]
	 * [ <bootimg 2 part label> ] [ <kernel snap revision installed in this boot partition> ]
	 */
	Bootimg_matrix [SNAP_RUN_BOOTIMG_PART_NUM][2][SNAP_NAME_MAX_LEN]byte

	/* name of the boot image from kernel snap to be used for extraction
	when not defined or empty, default boot.img will be used */
	Bootimg_file_name [SNAP_NAME_MAX_LEN]byte

	/**
	 * gadget assets: Matrix for mapping of gadget asset partitions
	 * Optional boot asset tracking, based on bootloader support
	 * Some boot chains support A/B boot assets for increased robustness
	 * example being A/B TrustExecutionEnvironment
	 * This matrix can be used to track current and try boot assets for
	 * robust updates
	 * Use of Gadget_asset_matrix matches use of Bootimg_matrix
	 *
	 * [ <boot assets 1 part label> ] [ <currently installed assets revision in this partition> ]
	 * [ <boot assets 2 part label> ] [ <currently installed assets revision in this partition> ]
	 */
	Gadget_asset_matrix [SNAP_RUN_BOOTIMG_PART_NUM][2][SNAP_NAME_MAX_LEN]byte

	/* unused placeholders for additional parameters in the future */
	Unused_key_01 [SNAP_NAME_MAX_LEN]byte
	Unused_key_02 [SNAP_NAME_MAX_LEN]byte
	Unused_key_03 [SNAP_NAME_MAX_LEN]byte
	Unused_key_04 [SNAP_NAME_MAX_LEN]byte
	Unused_key_05 [SNAP_NAME_MAX_LEN]byte
	Unused_key_06 [SNAP_NAME_MAX_LEN]byte
	Unused_key_07 [SNAP_NAME_MAX_LEN]byte
	Unused_key_08 [SNAP_NAME_MAX_LEN]byte
	Unused_key_09 [SNAP_NAME_MAX_LEN]byte
	Unused_key_10 [SNAP_NAME_MAX_LEN]byte
	Unused_key_11 [SNAP_NAME_MAX_LEN]byte
	Unused_key_12 [SNAP_NAME_MAX_LEN]byte
	Unused_key_13 [SNAP_NAME_MAX_LEN]byte
	Unused_key_14 [SNAP_NAME_MAX_LEN]byte
	Unused_key_15 [SNAP_NAME_MAX_LEN]byte
	Unused_key_16 [SNAP_NAME_MAX_LEN]byte
	Unused_key_17 [SNAP_NAME_MAX_LEN]byte
	Unused_key_18 [SNAP_NAME_MAX_LEN]byte
	Unused_key_19 [SNAP_NAME_MAX_LEN]byte
	Unused_key_20 [SNAP_NAME_MAX_LEN]byte

	/* unused array of 10 key value pairs */
	Key_value_pairs [10][2][SNAP_NAME_MAX_LEN]byte

	/* crc32 value for structure */
	Crc32 uint32
}

func (v2run *SnapBootSelect_v2_run) crc32() uint32 {
	return v2run.Crc32
}

func (v2run *SnapBootSelect_v2_run) version() uint32 {
	return v2run.Version
}

func (v2run *SnapBootSelect_v2_run) signature() uint32 {
	return v2run.Signature
}

func (v2run *SnapBootSelect_v2_run) setup() error {
	return nil
}

func (v2run *SnapBootSelect_v2_run) get(key string) string {
	switch key {
	case "kernel_status":
		return cToGoString(v2run.Kernel_status[:])
	case "snap_kernel":
		return cToGoString(v2run.Snap_kernel[:])
	case "snap_try_kernel":
		return cToGoString(v2run.Snap_try_kernel[:])
	case "snap_gadget":
		return cToGoString(v2run.Snap_gadget[:])
	case "snap_try_gadget":
		return cToGoString(v2run.Snap_try_gadget[:])
	case "bootimg_file_name":
		return cToGoString(v2run.Bootimg_file_name[:])
	}
	return ""
}

func (v2run *SnapBootSelect_v2_run) set(key, value string) {
	switch key {
	case "kernel_status":
		copyString(v2run.Kernel_status[:], value)
	case "snap_kernel":
		copyString(v2run.Snap_kernel[:], value)
	case "snap_try_kernel":
		copyString(v2run.Snap_try_kernel[:], value)
	case "snap_gadget":
		copyString(v2run.Snap_gadget[:], value)
	case "snap_try_gadget":
		copyString(v2run.Snap_try_gadget[:], value)
	case "bootimg_file_name":
		copyString(v2run.Bootimg_file_name[:], value)
	}
}

func (v2run *SnapBootSelect_v2_run) configureBootPartitions(bootPartLabels []string) error {
	matr, err := commonConfigureBootPartitions(v2run.Bootimg_matrix, bootPartLabels)
	if err != nil {
		return err
	}
	v2run.Bootimg_matrix = matr
	return nil
}

func (v2run *SnapBootSelect_v2_run) load(path string) error {
	return commonLoad(path, v2run, SNAP_BOOTSELECT_VERSION_V2, SNAP_BOOTSELECT_SIGNATURE)
}

func (v2run *SnapBootSelect_v2_run) removeKernelFromBootPart(kernel string) error {
	matr, err := commonRemoveKernelFromBootPart(v2run.Bootimg_matrix, kernel)
	if err != nil {
		return err
	}
	v2run.Bootimg_matrix = matr
	return nil
}

func (v2run *SnapBootSelect_v2_run) setBootPartition(bootpart, kernel string) error {
	matr, err := commonSetBootPartition(v2run.Bootimg_matrix, bootpart, kernel)
	if err != nil {
		return err
	}
	v2run.Bootimg_matrix = matr
	return nil
}

func (v2run *SnapBootSelect_v2_run) findFreeBootPartition(kernel string) (string, error) {
	return commonFindFreeBootPartition(v2run, v2run.Bootimg_matrix, kernel)
}

func (v2run *SnapBootSelect_v2_run) getBootPartition(kernel string) (string, error) {
	return commonGetBootPartition(v2run.Bootimg_matrix, kernel)
}

// unimplemented for v2 run

func (v2run *SnapBootSelect_v2_run) setRecoveryBootPartition(string, string) error {
	return fmt.Errorf("internal error: cannot set recovery system boot partition on non-recovery lkenv")
}

func (v2run *SnapBootSelect_v2_run) findFreeRecoveryPartition(string) (string, error) {
	return "", fmt.Errorf("internal error: cannot find recovery system boot partition on non-recovery lkenv")
}
