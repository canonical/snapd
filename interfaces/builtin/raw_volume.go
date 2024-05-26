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

package builtin

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
)

const rawVolumeSummary = `allows read/write access to specific disk partition`

// raw-volume grants full access to a particular disk partition. Since the
// volume is device-specific, it is desirable to limit the plugging snap's
// connection (eg to avoid situations of intending to grant access to a 'data'
// disk on one device but granting access to a 'system' disk on another).
// Therefore, require a snap declaration for connecting the interface at all.
const rawVolumeBaseDeclarationSlots = `
  raw-volume:
    allow-installation:
      slot-snap-type:
        - core
        - gadget
    deny-connection: true
    deny-auto-connection: true
`

// Only allow disk device partitions; not loop, ram, CDROM, generic SCSI,
// network, tape, raid, etc devices
const rawVolumeConnectedPlugAppArmorPath = `
# Description: can access disk partition read/write
%s rw,

# needed for write access
capability sys_admin,

# allow read access to sysfs and udev for block devices
@{PROC}/devices r,
/run/udev/data/b[0-9]*:[0-9]* r,
/sys/block/ r,
/sys/devices/**/block/** r,

# Allow to use mkfs utils to format partitions
/{,usr/}sbin/mke2fs ixr,
/{,usr/}sbin/mkfs.fat ixr,
`

// The type for this interface
type rawVolumeInterface struct{}

// Getter for the name of this interface
func (iface *rawVolumeInterface) Name() string {
	return "raw-volume"
}

func (iface *rawVolumeInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              rawVolumeSummary,
		BaseDeclarationSlots: rawVolumeBaseDeclarationSlots,
	}
}

func (iface *rawVolumeInterface) String() string {
	return iface.Name()
}

// https://www.kernel.org/doc/Documentation/admin-guide/devices.txt
//
// For now, only list common devices and skip the following:
// - Acorn MFM mfma-mfmb
// - ACSI ada-adp
// - Parallel port IDE pda-pdd
// - Parallel port ATAPI pf0-3
// - USB block device uba-ubz
//
// The '0' partition number (eg, hda0) is omitted since it refers to the whole
// disk.

// IDE, MFM, RLL hda-hdt, 1-63 partitions:
const hdPat = `hd[a-t]([1-9]|[1-5][0-9]|6[0-3])`

// SCSI sda-sdiv, 1-15 partitions:
const sdPat = `sd([a-z]|[a-h][a-z]|i[a-v])([1-9]|1[0-5])`

// I2O i2o/hda-hddx, 1-15 partitions:
const i2oPat = `i2o/hd([a-z]|[a-c][a-z]|d[a-x])([1-9]|1[0-5])`

// MMC mmcblk0-999, 1-63 partitions (number of partitions is kernel cmdline
// configurable. Ubuntu uses 32, so use 64 for headroom):
const mmcPat = `mmcblk([0-9]|[1-9][0-9]{1,2})p([1-9]|[1-5][0-9]|6[0-3])`

// NVMe nvme0-99, 1-63 partitions with 1-63 optional namespaces:
const nvmePat = `nvme([0-9]|[1-9][0-9])(n([1-9]|[1-5][0-9]|6[0-3])){0,1}p([1-9]|[1-5][0-9]|6[0-3])`

// virtio vda-vdz, 1-63 partitions:
const vdPat = `vd[a-z]([1-9]|[1-5][0-9]|6[0-3])`

var rawVolumePartitionPattern = regexp.MustCompile(fmt.Sprintf("^/dev/(%s|%s|%s|%s|%s|%s)$", hdPat, sdPat, i2oPat, mmcPat, nvmePat, vdPat))

const invalidDeviceNodeSlotPathErrFmt = "slot %q path attribute must be a valid device node"

// Check validity of the defined slot
func (iface *rawVolumeInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	_ := mylog.Check2(verifySlotPathAttribute(&interfaces.SlotRef{Snap: slot.Snap.InstanceName(), Name: slot.Name}, slot, rawVolumePartitionPattern, invalidDeviceNodeSlotPathErrFmt))
	return err
}

func (iface *rawVolumeInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	cleanedPath := mylog.Check2(verifySlotPathAttribute(slot.Ref(), slot, rawVolumePartitionPattern, invalidDeviceNodeSlotPathErrFmt))

	spec.AddSnippet(fmt.Sprintf(rawVolumeConnectedPlugAppArmorPath, cleanedPath))

	return nil
}

func (iface *rawVolumeInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	cleanedPath := mylog.Check2(verifySlotPathAttribute(slot.Ref(), slot, rawVolumePartitionPattern, invalidDeviceNodeSlotPathErrFmt))

	spec.TagDevice(fmt.Sprintf(`KERNEL=="%s"`, strings.TrimPrefix(cleanedPath, "/dev/")))

	return nil
}

func (iface *rawVolumeInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// Allow what is allowed in the declarations
	return true
}

func init() {
	registerIface(&rawVolumeInterface{})
}
