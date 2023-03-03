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

package builtin

// https://www.kernel.org/doc/Documentation/vm/hugetlbpage.txt
// https://www.kernel.org/doc/Documentation/vm/transhuge.txt
// This interface assumes that huge pages are mounted at either:
// - /dev/hugepages (Debian, Ubuntu)
// - /run/hugepages (various documentation)
const hugepagesControlSummary = `allows controlling hugepages`

const hugepagesControlBaseDeclarationSlots = `
  hugepages-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const hugepagesControlConnectedPlugAppArmor = `
# Allow configuring huge pages via /sys or /proc
/sys/kernel/mm/hugepages/{,hugepages-[0-9]*}/* r,
/sys/kernel/mm/hugepages/{,hugepages-[0-9]*}/nr_{hugepages,hugepages_mempolicy,overcommit_hugepages} w,
/sys/devices/system/node/node[0-9]*/hugepages/{,hugepages-[0-9]*}/* r,
/sys/devices/system/node/node[0-9]*/hugepages/{,hugepages-[0-9]*}/nr_hugepages w,
@{PROC}/sys/vm/nr_{hugepages,hugepages_mempolicy,overcommit_hugepages} rw,

# Observe which group can create shm segments using hugetlb pages
@{PROC}/sys/vm/hugetlb_shm_group r,

# Observe allocated huge pages by node (@{PROC}/meminfo already in base abstraction)
/sys/devices/system/node/node[0-9]*/meminfo r,

# hugepages may be controlled via chown/chgrp/chmod. Enforce this with
# owner match
/{dev,run}/hugepages/ r,
owner /{dev,run}/hugepages/{,**} rwk,

# Allow configuring transparent huge pages
/sys/kernel/mm/transparent_hugepage/{,**} r,
/sys/kernel/mm/transparent_hugepage/defrag w,
/sys/kernel/mm/transparent_hugepage/{,shmem_}enabled w,
/sys/kernel/mm/transparent_hugepage/use_zero_page w,
/sys/kernel/mm/transparent_hugepage/khugepaged/{alloc,scan}_sleep_millisecs w,
/sys/kernel/mm/transparent_hugepage/khugepaged/defrag w,
/sys/kernel/mm/transparent_hugepage/khugepaged/max_ptes_{none,swap} w,
/sys/kernel/mm/transparent_hugepage/khugepaged/pages_to_scan w,

# Allow mounting huge tables (for hypervisors like ACRN)
mount options=ro /dev/hugepages,
`

func init() {
	registerIface(&commonInterface{
		name:                  "hugepages-control",
		summary:               hugepagesControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  hugepagesControlBaseDeclarationSlots,
		connectedPlugAppArmor: hugepagesControlConnectedPlugAppArmor,
	})
}
