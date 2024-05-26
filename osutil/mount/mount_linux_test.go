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

package mount_test

import (
	"syscall"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil/mount"
)

func Test(t *testing.T) { TestingT(t) }

type mountSuite struct{}

var _ = Suite(&mountSuite{})

func (s *mountSuite) TestMountFlagsToOpts(c *C) {
	// Known flags are converted to symbolic names.
	opts, unknown := mount.MountFlagsToOpts(syscall.MS_REMOUNT |
		syscall.MS_BIND | syscall.MS_REC | syscall.MS_RDONLY | syscall.MS_SHARED |
		syscall.MS_SLAVE | syscall.MS_PRIVATE | syscall.MS_UNBINDABLE)
	c.Check(opts, DeepEquals, []string{
		"MS_REMOUNT", "MS_BIND", "MS_REC",
		"MS_RDONLY", "MS_SHARED", "MS_SLAVE", "MS_PRIVATE", "MS_UNBINDABLE",
	})
	c.Check(unknown, Equals, 0)
	// Unknown flags are retained and returned.
	opts, unknown = mount.MountFlagsToOpts(1 << 24)
	c.Check(opts, DeepEquals, []string(nil))
	c.Check(unknown, Equals, 1<<24)
	// Known and unknown flags work in tandem.
	opts, unknown = mount.MountFlagsToOpts(syscall.MS_BIND | 1<<24)
	c.Check(opts, DeepEquals, []string{"MS_BIND"})
	c.Check(unknown, Equals, 1<<24)
}

func (s *mountSuite) TestUnmountFlagsToOpts(c *C) {
	// Known flags are converted to symbolic names.
	const UMOUNT_NOFOLLOW = 8
	opts, unknown := mount.UnmountFlagsToOpts(syscall.MNT_FORCE |
		syscall.MNT_DETACH | syscall.MNT_EXPIRE | UMOUNT_NOFOLLOW)
	c.Check(opts, DeepEquals, []string{
		"UMOUNT_NOFOLLOW", "MNT_FORCE",
		"MNT_DETACH", "MNT_EXPIRE",
	})
	c.Check(unknown, Equals, 0)
	// Unknown flags are retained and returned.
	opts, unknown = mount.UnmountFlagsToOpts(1 << 24)
	c.Check(opts, DeepEquals, []string(nil))
	c.Check(unknown, Equals, 1<<24)
	// Known and unknown flags work in tandem.
	opts, unknown = mount.UnmountFlagsToOpts(syscall.MNT_DETACH | 1<<24)
	c.Check(opts, DeepEquals, []string{"MNT_DETACH"})
	c.Check(unknown, Equals, 1<<24)
}

var (
	benchResultOpt     []string
	benchResultUnknown int
)

func benchUnmount(flags int, b *testing.B) {
	var opts []string
	var unknown int
	for n := 0; n < b.N; n++ {
		opts, unknown = mount.UnmountFlagsToOpts(flags)
	}
	benchResultOpt = opts
	benchResultUnknown = unknown
}

func benchMount(flags int, b *testing.B) {
	var opts []string
	var unknown int
	for n := 0; n < b.N; n++ {
		opts, unknown = mount.MountFlagsToOpts(flags)
	}
	benchResultOpt = opts
	benchResultUnknown = unknown
}

func BenchmarkUnmountFlagsToOptsAllMissing(b *testing.B) { benchUnmount(1<<24, b) }
func BenchmarkUnmountFlagsToOptsMixed(b *testing.B)      { benchUnmount(syscall.MNT_DETACH|1<<24, b) }
func BenchmarkUnmountFlagsToOptsAllPresent(b *testing.B) {
	const UMOUNT_NOFOLLOW = 8
	benchUnmount(syscall.MNT_FORCE|
		syscall.MNT_DETACH|syscall.MNT_EXPIRE|UMOUNT_NOFOLLOW, b)
}

func BenchmarkMountFlagsToOptsAllMissing(b *testing.B) { benchMount(1<<24, b) }

func BenchmarkMountFlagsToOptsAllPresent(b *testing.B) {
	benchMount(syscall.MS_REMOUNT|
		syscall.MS_BIND|syscall.MS_REC|syscall.MS_RDONLY|syscall.MS_SHARED|
		syscall.MS_SLAVE|syscall.MS_PRIVATE|syscall.MS_UNBINDABLE, b)
}
func BenchmarkMountFlagsToOptsMixed(b *testing.B) { benchMount(syscall.MS_BIND|1<<24, b) }

func BenchmarkMountFlagsToOptsTypical(b *testing.B) {
	benchMount(syscall.MS_BIND, b)
}
