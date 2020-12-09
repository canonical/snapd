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

#include <stdint.h>

#ifndef _BOOTLOADER_SNAP_BOOT_COMMON_H
#define _BOOTLOADER_SNAP_BOOT_COMMON_H

#define SNAP_BOOTSELECT_SIGNATURE ('S' | ('B' << 8) | ('s' << 16) | ('e' << 24))
// SNAP_BOOTSELECT_SIGNATURE_RUN is the same as SNAP_BOOTSELECT_SIGNATURE
#define SNAP_BOOTSELECT_SIGNATURE_RUN ('S' | ('B' << 8) | ('s' << 16) | ('e' << 24))

// note SNAP_NAME_MAX_LEN also defines the max length of a recovery system label
#define SNAP_NAME_MAX_LEN (256)
#define HASH_LENGTH (32)
#define SNAP_MODE_TRY "try"
#define SNAP_MODE_TRYING "trying"
#define FACTORY_RESET "factory-reset"

#define SNAP_RECOVERY_MODE_INSTALL "install"
#define SNAP_RECOVERY_MODE_RUN "run"
#define SNAP_RECOVERY_MODE_RECOVER "recover"

/* partition label where boot select structure is stored, for uc20 this is just
 * used for run mode
 */
#define SNAP_BOOTSELECT_PARTITION "snapbootsel"

/* partition label where recovery boot select structure is stored */
#define SNAP_RECOVERYSELECT_PARTITION "snaprecoverysel"

/** maximum number of available bootimg partitions for recovery systems, min 5
 *  NOTE: the number of actual bootimg partitions usable is determined by the
 *  gadget, this just sets the upper bound of maximum number of recovery systems
 *  a gadget could define without needing changes here
 */
#define SNAP_RECOVERY_BOOTIMG_PART_NUM 10

/* number of available bootimg partitions for run mode, min 2 */
#define SNAP_RUN_BOOTIMG_PART_NUM 2

#endif  // _BOOTLOADER_SNAP_BOOT_COMMON_H
