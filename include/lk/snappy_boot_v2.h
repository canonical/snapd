/**
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

#include "snappy_boot_common.h"

#ifndef _BOOTLOADER_SNAP_BOOT_V2_H
#define _BOOTLOADER_SNAP_BOOT_V2_H

#define SNAP_BOOTSELECT_VERSION_V2 0x00010010
#define SNAP_BOOTSELECT_SIGNATURE_RECOVERY ('S' | ('R' << 8) | ('s' << 16) | ('e' << 24))

// device lock states
#define DEVICE_STATE_UNKNOW   0  // initial device state at first boot
#define DEVICE_STATE_UNLOCKED 1  // device unlocked
#define DEVICE_STATE_LOCKED   2  // device locked

/* snappy bootselect partition format structure for run mode */
typedef struct SNAP_RUN_BOOT_SELECTION {
    /* Should always contain value of SNAP_BOOTSELECT_SIGNATURE_RUN defined in common.h */
    uint32_t signature;
    /* Should always contain value of SNAP_BOOTSELECT_VERSION_V2 */
    uint32_t version;

    /* kernel_status, one of: 'empty', "try", "trying" */
    char kernel_status[SNAP_NAME_MAX_LEN];
    /* current kernel snap revision */
    char snap_kernel[SNAP_NAME_MAX_LEN];
    /* current try kernel snap revision */
    char snap_try_kernel[SNAP_NAME_MAX_LEN];

    /* gadget_mode, one of: 'empty', "try", "trying" */
    char gadget_mode[SNAP_NAME_MAX_LEN];
    /* GADGET assets: current gadget assets revision */
    char snap_gadget[SNAP_NAME_MAX_LEN];
    /* GADGET assets: try gadget assets revision */
    char snap_try_gadget[SNAP_NAME_MAX_LEN];

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
    char bootimg_matrix[SNAP_RUN_BOOTIMG_PART_NUM][2][SNAP_NAME_MAX_LEN];

    /* name of the boot image from kernel snap to be used for extraction
       when not defined or empty, default boot.img will be used */
    char bootimg_file_name[SNAP_NAME_MAX_LEN];

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
    char gadget_asset_matrix[SNAP_RUN_BOOTIMG_PART_NUM][2][SNAP_NAME_MAX_LEN];

    /* unused placeholders for additional parameters to be used  in the future */
    char unused_key_01[SNAP_NAME_MAX_LEN];
    char unused_key_02[SNAP_NAME_MAX_LEN];
    char unused_key_03[SNAP_NAME_MAX_LEN];
    char unused_key_04[SNAP_NAME_MAX_LEN];
    char unused_key_05[SNAP_NAME_MAX_LEN];
    char unused_key_06[SNAP_NAME_MAX_LEN];
    char unused_key_07[SNAP_NAME_MAX_LEN];
    char unused_key_08[SNAP_NAME_MAX_LEN];
    char unused_key_09[SNAP_NAME_MAX_LEN];
    char unused_key_10[SNAP_NAME_MAX_LEN];
    char unused_key_11[SNAP_NAME_MAX_LEN];
    char unused_key_12[SNAP_NAME_MAX_LEN];
    char unused_key_13[SNAP_NAME_MAX_LEN];
    char unused_key_14[SNAP_NAME_MAX_LEN];
    char unused_key_15[SNAP_NAME_MAX_LEN];
    char unused_key_16[SNAP_NAME_MAX_LEN];
    char unused_key_17[SNAP_NAME_MAX_LEN];
    char unused_key_18[SNAP_NAME_MAX_LEN];
    char unused_key_19[SNAP_NAME_MAX_LEN];
    char unused_key_20[SNAP_NAME_MAX_LEN];

    /* unused array of 10 key - value pairs */
    char key_value_pairs[10][2][SNAP_NAME_MAX_LEN];

    /* crc32 value for structure */
    uint32_t crc32;
} SNAP_RUN_BOOT_SELECTION_t;

/* snappy bootselect partition format structure for recovery*/
typedef struct SNAP_RECOVERY_BOOT_SELECTION {
    /* Should always contain value of SNAP_BOOTSELECT_SIGNATURE_RECOVERY defined above */
    uint32_t signature;
    /* Should always contain value of SNAP_BOOTSELECT_VERSION_V2 */
    uint32_t version;

    /** snapd_recovery_mode is what mode the system will be booted in, one of
     *  "install", "recover" or "run"
     */
    char snapd_recovery_mode[SNAP_NAME_MAX_LEN];

    /** snapd_recovery_system defines the recovery system label to be used when
     *  booting the system, it must be defined to one of the values in the
     *  bootimg matrix below
     */
    char snapd_recovery_system[SNAP_NAME_MAX_LEN];

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
     * There are SNAP_RECOVERY_BOOTIMG_PART_NUM rows in the matrix, representing
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
    char bootimg_matrix[SNAP_RECOVERY_BOOTIMG_PART_NUM][2][SNAP_NAME_MAX_LEN];

    /* name of the boot image from kernel snap to be used for extraction
       when not defined or empty, default boot.img will be used */
    char bootimg_file_name[SNAP_NAME_MAX_LEN];

    /** try_recovery_system contains the label of a recovery system to be
     *  tried. This entry is completely transparent to the bootloader and is
     *  only modified by snapd or snap-bootstrap.
     */
    char try_recovery_system[SNAP_NAME_MAX_LEN];

    /** recovery_system_status contains the status of a tried recovery
     *  systems, which is one of "", "try", "tried". This entry is completely
     *  transparent to the bootloader and is only modified by snapd or
     *  snap-bootstrap
     */
    char recovery_system_status[SNAP_NAME_MAX_LEN];

    /** device_lock_state contains the lock state of the device. It is used by the
     * bootloader to track device lock changes. When lock state changes, device goes
     * automatically to install mode. This entry is completely transparent
     * to the snapd and is only modified by bootloader.
     * Only first char in the aray is used (device_lock_state[0])
     * Permitted values:
     *  0: DEVICE_STATE_UNKNOW:   initial value at first boot.
     *          This is changed by the bootloader to reflect actual device state.
     *  1: DEVICE_STATE_UNLOCKED: unlocked device
     *  2: DEVICE_STATE_LOCKED:   locked device
     */
    char device_lock_state[SNAP_NAME_MAX_LEN];

    /* unused placeholders for additional parameters to be used  in the future */
    char unused_key_01[SNAP_NAME_MAX_LEN];
    char unused_key_02[SNAP_NAME_MAX_LEN];
    char unused_key_03[SNAP_NAME_MAX_LEN];
    char unused_key_04[SNAP_NAME_MAX_LEN];
    char unused_key_05[SNAP_NAME_MAX_LEN];
    char unused_key_06[SNAP_NAME_MAX_LEN];
    char unused_key_07[SNAP_NAME_MAX_LEN];
    char unused_key_08[SNAP_NAME_MAX_LEN];
    char unused_key_09[SNAP_NAME_MAX_LEN];
    char unused_key_10[SNAP_NAME_MAX_LEN];
    char unused_key_11[SNAP_NAME_MAX_LEN];
    char unused_key_12[SNAP_NAME_MAX_LEN];
    char unused_key_13[SNAP_NAME_MAX_LEN];
    char unused_key_14[SNAP_NAME_MAX_LEN];
    char unused_key_15[SNAP_NAME_MAX_LEN];
    char unused_key_16[SNAP_NAME_MAX_LEN];
    char unused_key_17[SNAP_NAME_MAX_LEN];

    /* unused array of 10 key - value pairs */
    char key_value_pairs[10][2][SNAP_NAME_MAX_LEN];

    /* crc32 value for structure */
    uint32_t crc32;
} SNAP_RECOVERY_BOOT_SELECTION_t;

#endif  // _BOOTLOADER_SNAP_BOOT_V2_H
