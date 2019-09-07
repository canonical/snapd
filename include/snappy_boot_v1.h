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

#define SNAP_BOOTSELECT_VERSION   0x00010001
#define SNAP_BOOTSELECT_SIGNATURE ('S' | ('B' << 8) | ('s' << 16) | ('e' << 24))
#define SNAP_NAME_MAX_LEN (256)
#define HASH_LENGTH (32)
#define SNAP_MODE_LENGTH (8)
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
    char snap_mode[SNAP_MODE_LENGTH];
    /* current core snap revision */
    char snap_core[SNAP_NAME_MAX_LEN];
    /* try core snap revision */
    char snap_try_core[SNAP_NAME_MAX_LEN];
    /* current kernel snap revision */
    char snap_kernel[SNAP_NAME_MAX_LEN];
    /* current kernel snap revision */
    char snap_try_kernel[SNAP_NAME_MAX_LEN];

    /* GADGET assets: current gadget assets revision */
    char snap_gadget[SNAP_NAME_MAX_LEN];
    /* GADGET assets: try gadget assets revision */
    char snap_try_gadget [SNAP_NAME_MAX_LEN];

    /**
     Reboot reason
     Optional parameter to signal bootloader alternative reboot reasons
     e.g. recovery/factory-reset/boot asset update
    */
    char reboot_reason[SNAP_NAME_MAX_LEN];

	/**
      Matrix for mapping of boot img partion to installed kernel snap revision
      At image build time:
        - snap prepare populates:
             - fills matrix first column with bootimage part names based on
               gadget.yaml file where we will support multiple occurrences of the role: bootimg
             - fills boot_part_num with number of actually available boot partitions
        - snapd:
             - when new kernel snap is installed, snap updates mapping in matrix so
               bootloader can pick correct kernel snap to use for boot
             - snap_mode, snap_try_kernel, snap_try_core behaves same way as with u-boot
             - boot partition labels are never modified by snapd at run time
        - bootloader:
             - Finds boot partition to use based on info in matrix and snap_kernel / snap_try_kernel
             - bootloaer does not alter matrix, only alters snap_mode

        [ <bootimg 1 part label> ] [ <currently installed kernel snap revison> ]
        [ <bootimg 2 part label> ] [ <currently installed kernel snap revision> ]
    */
    char bootimg_matrix[SNAP_BOOTIMG_PART_NUM][2][SNAP_NAME_MAX_LEN];

    /* name of the boot image from kernel snap to be used for extraction
       when not defined or empty, default boot.img will be used */
    char bootimg_file_name[SNAP_NAME_MAX_LEN];

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
    char boot_asset_matrix [SNAP_BOOTIMG_PART_NUM][2][SNAP_NAME_MAX_LEN];

    /* unused placeholders for additional parameters to be used  in the future */
    char unused_key_1 [SNAP_NAME_MAX_LEN];
    char unused_key_2 [SNAP_NAME_MAX_LEN];
    char unused_key_3 [SNAP_NAME_MAX_LEN];
    char unused_key_4 [SNAP_NAME_MAX_LEN];

    /* unused array of 10 key - value pairs */
    char key_value_pairs [10][2][SNAP_NAME_MAX_LEN];

    /* crc32 value for structure */
    uint32_t crc32;
} SNAP_BOOT_SELECTION_t;

#endif
