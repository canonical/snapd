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

package main_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	main "github.com/snapcore/snapd/cmd/snap-bootstrap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type doSystemdMountSuite struct {
	testutil.BaseTest
}

var _ = Suite(&doSystemdMountSuite{})

func (s *doSystemdMountSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func (s *doSystemdMountSuite) TestDoSystemdMountUnhappy(c *C) {
	cmd := testutil.MockCommand(c, "systemd-mount", `
echo "mocked error"
exit 1
`)
	defer cmd.Restore()
	mylog.Check(main.DoSystemdMount("something", "somewhere only we know", nil))
	c.Assert(err, ErrorMatches, "mocked error")
}

func (s *doSystemdMountSuite) TestDoSystemdMount(c *C) {
	testStart := time.Now()

	tt := []struct {
		what             string
		where            string
		opts             *main.SystemdMountOptions
		timeNowTimes     []time.Time
		isMountedReturns []bool
		expErr           string
		comment          string
	}{
		{
			what:             "/dev/sda3",
			where:            "/run/mnt/data",
			timeNowTimes:     []time.Time{testStart, testStart},
			isMountedReturns: []bool{true},
			comment:          "happy default",
		},
		{
			what:  "tmpfs",
			where: "/run/mnt/data",
			opts: &main.SystemdMountOptions{
				Tmpfs: true,
			},
			timeNowTimes:     []time.Time{testStart, testStart},
			isMountedReturns: []bool{true},
			comment:          "happy tmpfs",
		},
		{
			what:  "tmpfs",
			where: "/run/mnt/data",
			opts: &main.SystemdMountOptions{
				NeedsFsck: true,
			},
			timeNowTimes:     []time.Time{testStart, testStart},
			isMountedReturns: []bool{true},
			comment:          "happy fsck",
		},
		{
			what:  "tmpfs",
			where: "/run/mnt/data",
			opts: &main.SystemdMountOptions{
				Ephemeral: true,
			},
			timeNowTimes:     []time.Time{testStart, testStart},
			isMountedReturns: []bool{true},
			comment:          "happy initramfs ephemeral",
		},
		{
			what:  "tmpfs",
			where: "/run/mnt/data",
			opts: &main.SystemdMountOptions{
				NoWait: true,
			},
			comment: "happy no wait",
		},
		{
			what:             "what",
			where:            "where",
			timeNowTimes:     []time.Time{testStart, testStart, testStart, testStart.Add(2 * time.Minute)},
			isMountedReturns: []bool{false, false},
			expErr:           "timed out after 1m30s waiting for mount what on where",
			comment:          "times out waiting for mount to appear",
		},
		{
			what:  "what",
			where: "where",
			opts: &main.SystemdMountOptions{
				Tmpfs:     true,
				NeedsFsck: true,
			},
			expErr:  "cannot mount \"what\" at \"where\": impossible to fsck a tmpfs",
			comment: "invalid tmpfs + fsck",
		},
		{
			what:  "tmpfs",
			where: "/run/mnt/data",
			opts: &main.SystemdMountOptions{
				NoSuid: true,
			},
			timeNowTimes:     []time.Time{testStart, testStart},
			isMountedReturns: []bool{true},
			comment:          "happy nosuid",
		},
		{
			what:  "tmpfs",
			where: "/run/mnt/data",
			opts: &main.SystemdMountOptions{
				Bind: true,
			},
			timeNowTimes:     []time.Time{testStart, testStart},
			isMountedReturns: []bool{true},
			comment:          "happy bind",
		},
		{
			what:  "tmpfs",
			where: "/run/mnt/data",
			opts: &main.SystemdMountOptions{
				Umount: true,
			},
			timeNowTimes:     []time.Time{testStart, testStart},
			isMountedReturns: []bool{false},
			comment:          "happy umount",
		},
		{
			what:  "tmpfs",
			where: "/run/mnt/data",
			opts: &main.SystemdMountOptions{
				NoSuid: true,
				Bind:   true,
			},
			timeNowTimes:     []time.Time{testStart, testStart},
			isMountedReturns: []bool{true},
			comment:          "happy nosuid+bind",
		},
		{
			what:  "/run/mnt/data/some.snap",
			where: "/run/mnt/base",
			opts: &main.SystemdMountOptions{
				ReadOnly: true,
			},
			timeNowTimes:     []time.Time{testStart, testStart},
			isMountedReturns: []bool{true},
			comment:          "happy ro",
		},
	}

	for _, t := range tt {
		comment := Commentf(t.comment)

		var cleanups []func()

		opts := t.opts
		if opts == nil {
			opts = &main.SystemdMountOptions{}
		}
		dirs.SetRootDir(c.MkDir())
		cleanups = append(cleanups, func() { dirs.SetRootDir("") })

		cmd := testutil.MockCommand(c, "systemd-mount", ``)
		cleanups = append(cleanups, cmd.Restore)

		timeCalls := 0
		restore := main.MockTimeNow(func() time.Time {
			timeCalls++
			c.Assert(timeCalls <= len(t.timeNowTimes), Equals, true, comment)
			if timeCalls > len(t.timeNowTimes) {
				c.Errorf("too many time.Now calls (%d)", timeCalls)
				// we want the test to fail at some point and not run forever, so
				// move time way forward to make it for sure time out
				return testStart.Add(10000 * time.Hour)
			}
			return t.timeNowTimes[timeCalls-1]
		})
		cleanups = append(cleanups, restore)

		cleanups = append(cleanups, func() {
			c.Assert(timeCalls, Equals, len(t.timeNowTimes), comment)
		})

		isMountedCalls := 0
		restore = main.MockOsutilIsMounted(func(where string) (bool, error) {
			isMountedCalls++
			c.Assert(isMountedCalls <= len(t.isMountedReturns), Equals, true, comment)
			if isMountedCalls > len(t.isMountedReturns) {
				e := fmt.Sprintf("too many osutil.IsMounted calls (%d)", isMountedCalls)
				c.Errorf(e)
				// we want the test to fail at some point and not run forever, so
				// move time way forward to make it for sure time out
				return false, fmt.Errorf(e)
			}
			return t.isMountedReturns[isMountedCalls-1], nil
		})
		cleanups = append(cleanups, restore)

		cleanups = append(cleanups, func() {
			c.Assert(isMountedCalls, Equals, len(t.isMountedReturns), comment)
		})
		mylog.Check(main.DoSystemdMount(t.what, t.where, t.opts))
		if t.expErr != "" {
			c.Assert(err, ErrorMatches, t.expErr)
		} else {

			c.Assert(len(cmd.Calls()), Equals, 1)
			call := cmd.Calls()[0]
			args := []string{
				"systemd-mount", t.what, t.where, "--no-pager", "--no-ask-password",
			}
			if opts.Umount {
				args = []string{
					"systemd-mount", t.where, "--umount", "--no-pager", "--no-ask-password",
				}
			}
			c.Assert(call[:len(args)], DeepEquals, args)

			foundTypeTmpfs := false
			foundFsckYes := false
			foundFsckNo := false
			foundNoBlock := false
			foundBeforeInitrdfsTarget := false
			foundNoSuid := false
			foundBind := false
			foundReadOnly := false
			foundPrivate := false

			for _, arg := range call[len(args):] {
				switch {
				case arg == "--type=tmpfs":
					foundTypeTmpfs = true
				case arg == "--fsck=yes":
					foundFsckYes = true
				case arg == "--fsck=no":
					foundFsckNo = true
				case arg == "--no-block":
					foundNoBlock = true
				case arg == "--property=Before=initrd-fs.target":
					foundBeforeInitrdfsTarget = true
				case strings.HasPrefix(arg, "--options="):
					for _, opt := range strings.Split(strings.TrimPrefix(arg, "--options="), ",") {
						switch opt {
						case "nosuid":
							foundNoSuid = true
						case "bind":
							foundBind = true
						case "ro":
							foundReadOnly = true
						case "private":
							foundPrivate = true
						default:
							c.Logf("Option '%s' unexpected", opt)
							c.Fail()
						}
					}
				default:
					c.Logf("Argument '%s' unexpected", arg)
					c.Fail()
				}
			}
			c.Assert(foundTypeTmpfs, Equals, opts.Tmpfs)
			c.Assert(foundFsckYes, Equals, opts.NeedsFsck)
			c.Assert(foundFsckNo, Equals, !opts.NeedsFsck)
			c.Assert(foundNoBlock, Equals, opts.NoWait)
			c.Assert(foundBeforeInitrdfsTarget, Equals, !opts.Ephemeral)
			c.Assert(foundNoSuid, Equals, opts.NoSuid)
			c.Assert(foundBind, Equals, opts.Bind)
			c.Assert(foundReadOnly, Equals, opts.ReadOnly)
			c.Assert(foundPrivate, Equals, opts.Private)

			// check that the overrides are present if opts.Ephemeral is false,
			// or check the overrides are not present if opts.Ephemeral is true
			for _, initrdUnit := range []string{
				"initrd-fs.target",
				"local-fs.target",
			} {
				mountUnit := systemd.EscapeUnitNamePath(t.where)
				fname := fmt.Sprintf("snap_bootstrap_%s.conf", mountUnit)
				unitFile := filepath.Join(dirs.GlobalRootDir, "/run/systemd/system", initrdUnit+".d", fname)
				if opts.Ephemeral {
					c.Assert(unitFile, testutil.FileAbsent)
				} else {
					c.Assert(unitFile, testutil.FileEquals, fmt.Sprintf(`[Unit]
Wants=%[1]s
`, mountUnit+".mount"))
				}
			}
		}

		for _, r := range cleanups {
			r()
		}
	}
}
