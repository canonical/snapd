/**
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

#ifndef _BOOTLOADER_SNAP_BOOT_V1_H
#define _BOOTLOADER_SNAP_BOOT_V1_H

#define SNAP_BOOTSELECT_VERSION 0x00010001
#define SNAP_BOOTSELECT_SIGNATURE ('S' | ('B' << 8) | ('s' << 16) | ('e' << 24))
#define SNAP_NAME_MAX_LEN (256)
#define HASH_LENGTH (32)
#define SNAP_MODE_TRY "try"
#define SNAP_MODE_TRYING "trying"
#define FACTORY_RESET "factory-reset"

/* partition label where boot select structure is stored */
#define SNAP_BOOTSELECT_PARTITION "snapbootsel"

/* number of available bootimg partitions, min 2 */
#define SNAP_BOOTIMG_PART_NUM 2

/* snappy bootselect partition format structure */
typedef struct SNAP_BOOT_SELECTION {
    /* Contains value BOOTSELECT_SIGNATURE defined above */
    uint32_t signature;
    /* snappy boot select version */
    uint32_t version;

    /* snap_mode, one of: 'empty', "try", "trying" */
    char snap_mode[SNAP_NAME_MAX_LEN];
    /* current core snap revision */
    char snap_core[SNAP_NAME_MAX_LEN];
    /* try core snap revision */
    char snap_try_core[SNAP_NAME_MAX_LEN];
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
     * Reboot reason
     * optional parameter to signal bootloader alternative reboot reasons
     * e.g. recovery/factory-reset/boot asset update
     */
    char reboot_reason[SNAP_NAME_MAX_LEN];

    /**
     * Matrix for mapping of boot img partition to installed kernel snap revision
     *
     * First column represents boot image partition label (e.g. boot_a,boot_b )
     *   value are static and should be populated at gadget built time
     *   or latest at image build time. Values are not further altered at run time.
     * Second column represents name currently installed kernel snap
     *   e.g. pi2-kernel_123.snap
     * initial value representing initial kernel snap revision
     *   is populated at image build time by snapd
     *
     * There are two rows in the matrix, representing current and previous kernel revision
     * following describes how this matrix should be modified at different stages:
     *  - at image build time:
     *    - extracted kernel snap revision name should be filled
     *      into free slot (first row, second column)
     *  - snapd:
     *    - when new kernel snap revision is being installed, snapd cycles through
     *      matrix to find unused 'boot slot' to be used for new kernel snap revision
     *      from free slot, first column represents partition label to which kernel
     *      snap boot image should be extracted. Second column is then populated with
     *      kernel snap revision name.
     *    - snap_mode, snap_try_kernel, snap_try_core behaves same way as with u-boot
     *  - bootloader:
     *    - bootloader reads snap_mode to determine if snap_kernel or snap_kernel is used
     *      to get kernel snap revision name
     *      kernel snap revision is then used to search matrix to determine
     *      partition label to be used for current boot
     *    - bootloader NEVER alters this matrix values
     *
     * [ <bootimg 1 part label> ] [ <kernel snap revision installed in this boot partition> ]
     * [ <bootimg 2 part label> ] [ <kernel snap revision installed in this boot partition> ]
     */
    char bootimg_matrix[SNAP_BOOTIMG_PART_NUM][2][SNAP_NAME_MAX_LEN];

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
    char gadget_asset_matrix[SNAP_BOOTIMG_PART_NUM][2][SNAP_NAME_MAX_LEN];

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
} SNAP_BOOT_SELECTION_t;

#endif
