// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package snap

type systemUsername struct {
	Id uint32
	// List of snap IDs which are allowed to declare this user or nil if no
	// such restriction exists
	AllowedSnapIds []string
}

// SupportedSystemUsernames for now contains the hardcoded list of system
// users (and implied system group of same name) that snaps may specify. This
// will eventually be moved out of here into the store.
//
// Since the snap is mounted read-only and to avoid problems associated with
// different systems using different uids and gids for the same user name and
// group name, snapd will create system-usernames where 'scope' is not
// 'external' (currently snapd only supports 'scope: shared') with the
// following characteristics:
//
//   - uid and gid shall match for the specified system-username
//   - a snapd-allocated [ug]id for a user/group name shall never change
//   - snapd should avoid [ug]ids that are known to overlap with uid ranges of
//     common use cases and user namespace container managers so that DAC and
//     AppArmor owner match work as intended.
//   - [ug]id shall be < 2^31 to avoid (at least) broken devpts kernel code
//   - [ug]id shall be >= 524288 (0x00080000) to give plenty of room for large
//     sites, default uid/gid ranges for docker (231072-296608), LXD installs
//     that setup a default /etc/sub{uid,gid} (100000-165536) and podman whose
//     tutorials reference setting up a specific default user and range
//     (100000-165536)
//   - [ug]id shall be < 1,000,000 and > 1,001,000,000 (ie, 1,000,000 subordinate
//     uid with 1,000,000,000 range) to avoid overlapping with LXD's minimum and
//     maximum id ranges. LXD allows for any id range >= 65536 and doesn't
//     perform any [ug]id overlap detection with current users
//   - [ug]ids assigned by snapd initially will fall within a 65536 (2^16) range
//     (see below) where the first [ug]id in the range has the 16 lower bits all
//     set to zero. This allows snapd to conveniently be bitwise aligned, follows
//     sensible conventions (see https://systemd.io/UIDS-GIDS.html) but also
//     potentially discoverable by systemd-nspawn (it assigns a different 65536
//     range to each container. Its allocation algorithm is not sequential and
//     may choose anything within its range that isn't already allocated. It's
//     detection algorithm includes (effectively) performing a getpwent()
//     operation on CANDIDATE_UID & 0XFFFF0000 and selecting another range if it
//     is assigned).
//
// What [ug]id range(s) should snapd use?
//
// While snapd does not employ user namespaces, it will operate on systems with
// container managers that do and will assign from a range of [ug]ids. It is
// desirable that snapd assigns [ug]ids that minimally conflict with the system
// and other software (potential conflicts with admin-assigned ranges in
// /etc/subuid and /etc/subgid cannot be avoided, but can be documented as well
// as detected/logged). Overlapping with container managers is non-fatal for
// snapd and the container, but introduces the possibility that a uid in the
// container matches a uid a snap is using, which is undesirable in terms of
// security (eg, DoS via ulimit, same ownership of files between container and
// snap (even if the other's files are otherwise inaccessible), etc).
//
// snapd shall assign [ug]ids from range(s) of 65536 where the lowest value in
// the range has the 16 lower bits all set to zero (initially just one range,
// but snapd can add more as needed).
//
// To avoid [ug]id overlaps, snapd shall only assign [ug]ids >= 524288
// (0x00080000) and <= 983040 (0x000F0000, ie the first 65536 range under LXD's
// minimum where the lower 16 bits are all zeroes). While [ug]ids >= 1001062400
// (0x3BAB0000, the first 65536 range above LXD's maximum where the lower 16
// bits are all zeroes) would also avoid overlap, considering nested containers
// (eg, LXD snap runs a container that runs a container that runs snapd),
// choosing >= 1001062400 would mean that the admin would need to increase the
// LXD id range for these containers for snapd to be allowed to create its
// [ug]ids in the deeply nested containers. The requirements would both be an
// administrative burden and artificially limit the number of deeply nested
// containers the host could have.
//
// Looking at the LSB and distribution defaults for login.defs, we can observe
// uids and gids in the system's initial 65536 range (ie, 0-65536):
//
//   - 0-99        LSB-suggested statically assigned range (eg, root, daemon,
//     etc)
//   - 0           mandatory 'root' user
//   - 100-499     LSB-suggested dynamically assigned range for system users
//     (distributions often prefer a higher range, see below)
//   - 500-999     typical distribution default for dynamically assigned range
//     for system users (some distributions use a smaller
//     SYS_[GU]ID_MIN)
//   - 1000-60000  typical distribution default for dynamically assigned range
//     for regular users
//   - 65535 (-1)  should not be assigned since '-1' might be evaluated as this
//     with set[ug]id* and chown families of functions
//   - 65534 (-2)  nobody/nogroup user for NFS/etc [ug]id anonymous squashing
//   - 65519-65533 systemd recommended reserved range for site-local anonymous
//     additions, etc
//
// To facilitate potential future use cases within the 65536 range snapd will
// assign from, snapd will only assign from the following subset of ranges
// relative to the range minimum (ie, its 'base' which has the lower 16 bits
// all set to zero):
//
// - 60500-60999 'scope: shared' system-usernames
// - 61000-65519 'scope: private' system-usernames
//
// Since the first [ug]id range must be >= 524288 and <= 983040 (see above) and
// following the above guide for system-usernames [ug]ids within this 65536
// range, the lowest 'scope: shared' user in this range is 584788 (0x0008EC54).
//
// Since this number is within systemd-nspawn's range of 524288-1879048191
// (0x00080000-0x6FFFFFFF), the number's lower 16 bits are not all zeroes so
// systemd-nspawn won't detect this allocation and could potentially assign the
// 65536 range starting at 0x00080000 to a container. snapd will therefore also
// create the 'snapd-range-524288-root' user and group with [ug]id 524288 to
// work within systemd-nspawn's collision detection. This user/group will not
// be assigned to snaps at this time.
//
// In short (phew!), use the following:
//
// $ snappy-debug.id-range 524288 # 0x00080000
// Host range:              524288-589823 (00080000-0008ffff; 0-65535)
// LSB static range:        524288-524387 (00080000-00080063; 0-99)
// Useradd system range:    524788-525287 (000801f4-000803e7; 500-999)
// Useradd regular range:   525288-584288 (000803e8-0008ea60; 1000-60000)
// Snapd system range:      584788-585287 (0008ec54-0008ee47; 60500-60999)
// Snapd private range:     585288-589807 (0008ee48-0008ffef; 61000-65519)
//
// Snapd is of course free to add more ranges (eg, 589824 (0x00090000)) with
// new snapd-range-<base>-root users, or to allocate differently within its
// 65536 range in the future (sequentially assigned [ug]ids are not required),
// but for now start very regimented to avoid as many problems as possible.
//
// References:
// https://forum.snapcraft.io/t/multiple-users-and-groups-in-snaps/
// https://systemd.io/UIDS-GIDS.html
// https://docs.docker.com/engine/security/userns-remap/
// https://github.com/lxc/lxd/blob/master/doc/userns-idmap.md
var SupportedSystemUsernames = map[string]systemUsername{
	// deprecated: snaps should use the "_daemon_" name below
	"snap_daemon": {Id: 584788},
	"snap_microk8s": {Id: 584789, AllowedSnapIds: []string{
		"EaXqgt1lyCaxKaQCU349mlodBkDCXRcg", // microk8s
	}},
	"snap_aziotedge": {Id: 584790, AllowedSnapIds: []string{
		"8neFt3wtSaWGgIbEepgIJcEZ3fnz7Lwt", // azure-iot-edge
	}},
	"snap_aziotdu": {Id: 584791, AllowedSnapIds: []string{
		"KzF67Mv8CeQBdUdrGaKU2sZVEiICWBg1", // deviceupdate-agent
	}},
	"_daemon_": {Id: 584792},
}
