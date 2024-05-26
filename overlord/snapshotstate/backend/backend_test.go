// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2020 Canonical Ltd
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

package backend_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"crypto"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/overlord/snapshotstate/backend"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type snapshotSuite struct {
	testutil.BaseTest
	root      string
	restore   []func()
	tarPath   string
	isTesting bool
}

// silly wrappers to get better failure messages
type (
	isTestingSuite struct{ snapshotSuite }
	noTestingSuite struct{ snapshotSuite }
)

var (
	_ = check.Suite(&isTestingSuite{snapshotSuite{isTesting: true}})
	_ = check.Suite(&noTestingSuite{snapshotSuite{isTesting: false}})
)

// tie gocheck into testing
func TestSnapshot(t *testing.T) { check.TestingT(t) }

type tableT struct {
	dir     string
	name    string
	content string
}

func table(si snap.PlaceInfo, homeDir string) []tableT {
	return []tableT{
		{
			dir:     si.DataDir(),
			name:    "foo",
			content: "versioned system canary\n",
		}, {
			dir:     si.CommonDataDir(),
			name:    "bar",
			content: "common system canary\n",
		}, {
			dir:     si.UserDataDir(homeDir, nil),
			name:    "ufoo",
			content: "versioned user canary\n",
		}, {
			dir:     si.UserCommonDataDir(homeDir, nil),
			name:    "ubar",
			content: "common user canary\n",
		},
	}
}

func (s *snapshotSuite) SetUpTest(c *check.C) {
	s.BaseTest.SetUpTest(c)
	s.root = c.MkDir()

	dirs.SetRootDir(s.root)

	si := snap.MinimalPlaceInfo("hello-snap", snap.R(42))

	for _, t := range table(si, filepath.Join(dirs.GlobalRootDir, "home/snapuser")) {
		c.Check(os.MkdirAll(t.dir, 0755), check.IsNil)
		c.Check(os.WriteFile(filepath.Join(t.dir, t.name), []byte(t.content), 0644), check.IsNil)
	}

	cur := mylog.Check2(user.Current())
	c.Assert(err, check.IsNil)

	s.restore = append(s.restore, backend.MockUserLookup(func(username string) (*user.User, error) {
		if username != "snapuser" {
			return nil, user.UnknownUserError(username)
		}
		rv := *cur
		rv.Username = username
		rv.HomeDir = filepath.Join(dirs.GlobalRootDir, "home/snapuser")
		return &rv, nil
	}),
		backend.MockIsTesting(s.isTesting),
	)

	s.tarPath = mylog.Check2(exec.LookPath("tar"))
	c.Assert(err, check.IsNil)
}

func (s *snapshotSuite) TearDownTest(c *check.C) {
	dirs.SetRootDir("")
	for _, restore := range s.restore {
		restore()
	}
}

func hashkeys(snapshot *client.Snapshot) (keys []string) {
	for k := range snapshot.SHA3_384 {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	return keys
}

func (s *snapshotSuite) TestLastSnapshotID(c *check.C) {
	// LastSnapshotSetID is happy without any snapshots
	setID := mylog.Check2(backend.LastSnapshotSetID())
	c.Assert(err, check.IsNil)
	c.Check(setID, check.Equals, uint64(0))

	// create snapshots dir and test snapshots
	os.MkdirAll(dirs.SnapshotsDir, os.ModePerm)
	for _, name := range []string{
		"9_some-snap-1.zip", "1234_not-a-snapshot", "12_other-snap.zip", "3_foo.zip",
	} {
		c.Assert(os.WriteFile(filepath.Join(dirs.SnapshotsDir, name), []byte{}, 0644), check.IsNil)
	}
	setID = mylog.Check2(backend.LastSnapshotSetID())
	c.Assert(err, check.IsNil)
	c.Check(setID, check.Equals, uint64(12))
}

func (s *snapshotSuite) TestLastSnapshotIDErrorOnDirNames(c *check.C) {
	// we need snapshots dir, otherwise LastSnapshotSetID exits early.
	c.Assert(os.MkdirAll(dirs.SnapshotsDir, os.ModePerm), check.IsNil)

	defer backend.MockDirNames(func(*os.File, int) ([]string, error) {
		return nil, fmt.Errorf("fail")
	})()
	setID := mylog.Check2(backend.LastSnapshotSetID())
	c.Assert(err, check.ErrorMatches, "fail")
	c.Check(setID, check.Equals, uint64(0))
}

func (s *snapshotSuite) TestIsSnapshotFilename(c *check.C) {
	tests := []struct {
		name  string
		valid bool
		setID uint64
	}{
		{"1_foo.zip", true, 1},
		{"14_hello-world_6.4_29.zip", true, 14},
		{"1_.zip", false, 0},
		{"1_foo.zip.bak", false, 0},
		{"foo_1_foo.zip", false, 0},
		{"foo_bar_baz.zip", false, 0},
		{"", false, 0},
		{"1_", false, 0},
	}

	for _, t := range tests {
		ok, setID := backend.IsSnapshotFilename(t.name)
		c.Check(ok, check.Equals, t.valid, check.Commentf("fail: %s", t.name))
		c.Check(setID, check.Equals, t.setID, check.Commentf("fail: %s", t.name))
	}
}

func (s *snapshotSuite) TestIterBailsIfContextDone(c *check.C) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	triedToOpenDir := false
	defer backend.MockOsOpen(func(string) (*os.File, error) {
		triedToOpenDir = true
		return nil, nil // deal with it
	})()
	mylog.Check(backend.Iter(ctx, nil))
	c.Check(err, check.Equals, context.Canceled)
	c.Check(triedToOpenDir, check.Equals, false)
}

func (s *snapshotSuite) TestIterBailsIfContextDoneMidway(c *check.C) {
	ctx, cancel := context.WithCancel(context.Background())
	triedToOpenDir := false
	defer backend.MockOsOpen(func(string) (*os.File, error) {
		triedToOpenDir = true
		return os.Open(os.DevNull)
	})()
	readNames := 0
	defer backend.MockDirNames(func(*os.File, int) ([]string, error) {
		readNames++
		cancel()
		return []string{"hello"}, nil
	})()
	triedToOpenSnapshot := false
	defer backend.MockOpen(func(string, uint64) (*backend.Reader, error) {
		triedToOpenSnapshot = true
		return nil, nil
	})()
	mylog.Check(backend.Iter(ctx, nil))
	c.Check(err, check.Equals, context.Canceled)
	c.Check(triedToOpenDir, check.Equals, true)
	// bails as soon as
	c.Check(readNames, check.Equals, 1)
	c.Check(triedToOpenSnapshot, check.Equals, false)
}

func (s *snapshotSuite) TestIterReturnsOkIfSnapshotsDirNonexistent(c *check.C) {
	triedToOpenDir := false
	defer backend.MockOsOpen(func(string) (*os.File, error) {
		triedToOpenDir = true
		return nil, os.ErrNotExist
	})()
	mylog.Check(backend.Iter(context.Background(), nil))
	c.Check(err, check.IsNil)
	c.Check(triedToOpenDir, check.Equals, true)
}

func (s *snapshotSuite) TestIterBailsIfSnapshotsDirFails(c *check.C) {
	triedToOpenDir := false
	defer backend.MockOsOpen(func(string) (*os.File, error) {
		triedToOpenDir = true
		return nil, os.ErrInvalid
	})()
	mylog.Check(backend.Iter(context.Background(), nil))
	c.Check(err, check.ErrorMatches, "cannot open snapshots directory: invalid argument")
	c.Check(triedToOpenDir, check.Equals, true)
}

func (s *snapshotSuite) TestIterWarnsOnOpenErrorIfSnapshotNil(c *check.C) {
	logbuf, restore := logger.MockLogger()
	defer restore()
	triedToOpenDir := false
	defer backend.MockOsOpen(func(string) (*os.File, error) {
		triedToOpenDir = true
		return new(os.File), nil
	})()
	readNames := 0
	defer backend.MockDirNames(func(*os.File, int) ([]string, error) {
		readNames++
		if readNames > 1 {
			return nil, io.EOF
		}
		return []string{"1_hello.zip"}, nil
	})()
	triedToOpenSnapshot := false
	defer backend.MockOpen(func(string, uint64) (*backend.Reader, error) {
		triedToOpenSnapshot = true
		return nil, os.ErrInvalid
	})()

	calledF := false
	f := func(snapshot *backend.Reader) error {
		calledF = true
		return nil
	}
	mylog.Check(backend.Iter(context.Background(), f))
	// snapshot open errors are not failures:
	c.Check(err, check.IsNil)
	c.Check(triedToOpenDir, check.Equals, true)
	c.Check(readNames, check.Equals, 2)
	c.Check(triedToOpenSnapshot, check.Equals, true)
	c.Check(logbuf.String(), check.Matches, `(?m).* Cannot open snapshot "1_hello.zip": invalid argument.`)
	c.Check(calledF, check.Equals, false)
}

func (s *snapshotSuite) TestIterCallsFuncIfSnapshotNotNil(c *check.C) {
	logbuf, restore := logger.MockLogger()
	defer restore()
	triedToOpenDir := false
	defer backend.MockOsOpen(func(string) (*os.File, error) {
		triedToOpenDir = true
		return new(os.File), nil
	})()
	readNames := 0
	defer backend.MockDirNames(func(*os.File, int) ([]string, error) {
		readNames++
		if readNames > 1 {
			return nil, io.EOF
		}
		return []string{"1_hello.zip"}, nil
	})()
	triedToOpenSnapshot := false
	defer backend.MockOpen(func(string, uint64) (*backend.Reader, error) {
		triedToOpenSnapshot = true
		// NOTE non-nil reader, and error, returned
		r := backend.Reader{}
		r.SetID = 1
		r.Broken = "xyzzy"
		return &r, os.ErrInvalid
	})()

	calledF := false
	f := func(snapshot *backend.Reader) error {
		c.Check(snapshot.Broken, check.Equals, "xyzzy")
		calledF = true
		return nil
	}
	mylog.Check(backend.Iter(context.Background(), f))
	// snapshot open errors are not failures:
	c.Check(err, check.IsNil)
	c.Check(triedToOpenDir, check.Equals, true)
	c.Check(readNames, check.Equals, 2)
	c.Check(triedToOpenSnapshot, check.Equals, true)
	c.Check(logbuf.String(), check.Equals, "")
	c.Check(calledF, check.Equals, true)
}

func (s *snapshotSuite) TestIterReportsCloseError(c *check.C) {
	logbuf, restore := logger.MockLogger()
	defer restore()
	triedToOpenDir := false
	defer backend.MockOsOpen(func(string) (*os.File, error) {
		triedToOpenDir = true
		return new(os.File), nil
	})()
	readNames := 0
	defer backend.MockDirNames(func(*os.File, int) ([]string, error) {
		readNames++
		if readNames > 1 {
			return nil, io.EOF
		}
		return []string{"42_hello.zip"}, nil
	})()
	triedToOpenSnapshot := false
	defer backend.MockOpen(func(string, uint64) (*backend.Reader, error) {
		triedToOpenSnapshot = true
		r := backend.Reader{}
		r.SetID = 42
		return &r, nil
	})()

	calledF := false
	f := func(snapshot *backend.Reader) error {
		c.Check(snapshot.SetID, check.Equals, uint64(42))
		calledF = true
		return nil
	}
	mylog.Check(backend.Iter(context.Background(), f))
	// snapshot close errors _are_ failures (because they're completely unexpected):
	c.Check(err, check.Equals, os.ErrInvalid)
	c.Check(triedToOpenDir, check.Equals, true)
	c.Check(readNames, check.Equals, 1) // never gets to read another one
	c.Check(triedToOpenSnapshot, check.Equals, true)
	c.Check(logbuf.String(), check.Equals, "")
	c.Check(calledF, check.Equals, true)
}

func readerForFilename(fname string, c *check.C) *backend.Reader {
	var snapname string
	var id uint64
	fn := strings.TrimSuffix(filepath.Base(fname), ".zip")
	_ := mylog.Check2(fmt.Sscanf(fn, "%d_%s", &id, &snapname))
	c.Assert(err, check.IsNil, check.Commentf(fn))
	f := mylog.Check2(os.Open(os.DevNull))
	c.Assert(err, check.IsNil, check.Commentf(fn))
	return &backend.Reader{
		File: f,
		Snapshot: client.Snapshot{
			SetID: id,
			Snap:  snapname,
		},
	}
}

func (s *snapshotSuite) TestIterIgnoresSnapshotsWithInvalidNames(c *check.C) {
	logbuf, restore := logger.MockLogger()
	defer restore()

	defer backend.MockOsOpen(func(string) (*os.File, error) {
		return new(os.File), nil
	})()
	readNames := 0
	defer backend.MockDirNames(func(*os.File, int) ([]string, error) {
		readNames++
		if readNames > 1 {
			return nil, io.EOF
		}
		return []string{
			"_foo.zip",
			"43_bar.zip",
			"foo_bar.zip",
			"bar.",
		}, nil
	})()
	defer backend.MockOpen(func(fname string, setID uint64) (*backend.Reader, error) {
		return readerForFilename(fname, c), nil
	})()

	var calledF int
	f := func(snapshot *backend.Reader) error {
		calledF++
		c.Check(snapshot.SetID, check.Equals, uint64(43))
		return nil
	}
	mylog.Check(backend.Iter(context.Background(), f))
	c.Check(err, check.IsNil)
	c.Check(logbuf.String(), check.Equals, "")
	c.Check(calledF, check.Equals, 1)
}

func (s *snapshotSuite) TestIterSetIDoverride(c *check.C) {
	if os.Geteuid() == 0 {
		c.Skip("this test cannot run as root (runuser will fail)")
	}
	logger.SimpleSetup(nil)

	epoch := snap.E("42*")
	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "hello-snap", Revision: snap.R(42), SnapID: "hello-id"}, Version: "v1.33", Epoch: epoch}
	cfg := map[string]interface{}{"some-setting": false}

	shw := mylog.Check2(backend.Save(context.TODO(), 12, info, cfg, []string{"snapuser"}, nil, nil))
	c.Assert(err, check.IsNil)
	c.Check(shw.SetID, check.Equals, uint64(12))

	snapshotPath := filepath.Join(dirs.SnapshotsDir, "12_hello-snap_v1.33_42.zip")
	c.Check(backend.Filename(shw), check.Equals, snapshotPath)
	c.Check(hashkeys(shw), check.DeepEquals, []string{"archive.tgz", "user/snapuser.tgz"})

	// rename the snapshot, verify that set id from the filename is used by the reader.
	c.Assert(os.Rename(snapshotPath, filepath.Join(dirs.SnapshotsDir, "33_hello.zip")), check.IsNil)

	var calledF int
	f := func(snapshot *backend.Reader) error {
		calledF++
		c.Check(snapshot.SetID, check.Equals, uint64(uint(33)))
		c.Check(snapshot.Snap, check.Equals, "hello-snap")
		return nil
	}

	c.Assert(backend.Iter(context.Background(), f), check.IsNil)
	c.Check(calledF, check.Equals, 1)
}

func (s *snapshotSuite) TestList(c *check.C) {
	logbuf, restore := logger.MockLogger()
	defer restore()
	defer backend.MockOsOpen(func(string) (*os.File, error) { return new(os.File), nil })()

	readNames := 0
	defer backend.MockDirNames(func(*os.File, int) ([]string, error) {
		readNames++
		if readNames > 4 {
			return nil, io.EOF
		}
		return []string{
			fmt.Sprintf("%d_foo.zip", readNames),
			fmt.Sprintf("%d_bar.zip", readNames),
			fmt.Sprintf("%d_baz.zip", readNames),
		}, nil
	})()
	defer backend.MockOpen(func(fn string, setID uint64) (*backend.Reader, error) {
		var id uint64
		var snapname string
		c.Assert(strings.HasSuffix(fn, ".zip"), check.Equals, true)
		fn = strings.TrimSuffix(filepath.Base(fn), ".zip")
		_ := mylog.Check2(fmt.Sscanf(fn, "%d_%s", &id, &snapname))
		c.Assert(err, check.IsNil, check.Commentf(fn))
		f := mylog.Check2(os.Open(os.DevNull))
		c.Assert(err, check.IsNil, check.Commentf(fn))
		return &backend.Reader{
			File: f,
			Snapshot: client.Snapshot{
				SetID:    id,
				Snap:     snapname,
				SnapID:   "id-for-" + snapname,
				Version:  "v1.0-" + snapname,
				Revision: snap.R(int(id)),
			},
		}, nil
	})()

	type tableT struct {
		setID     uint64
		snapnames []string
		numSets   int
		numShots  int
		predicate func(*client.Snapshot) bool
	}
	table := []tableT{
		{0, nil, 4, 12, nil},
		{0, []string{"foo"}, 4, 4, func(snapshot *client.Snapshot) bool { return snapshot.Snap == "foo" }},
		{1, nil, 1, 3, func(snapshot *client.Snapshot) bool { return snapshot.SetID == 1 }},
		{2, []string{"bar"}, 1, 1, func(snapshot *client.Snapshot) bool { return snapshot.Snap == "bar" && snapshot.SetID == 2 }},
		{0, []string{"foo", "bar"}, 4, 8, func(snapshot *client.Snapshot) bool { return snapshot.Snap == "foo" || snapshot.Snap == "bar" }},
	}

	for i, t := range table {
		comm := check.Commentf("%d: %d/%v", i, t.setID, t.snapnames)
		// reset
		readNames = 0
		logbuf.Reset()

		sets := mylog.Check2(backend.List(context.Background(), t.setID, t.snapnames))
		c.Check(err, check.IsNil, comm)
		c.Check(readNames, check.Equals, 5, comm)
		c.Check(logbuf.String(), check.Equals, "", comm)
		c.Check(sets, check.HasLen, t.numSets, comm)
		nShots := 0
		fnTpl := filepath.Join(dirs.SnapshotsDir, "%d_%s_%s_%s.zip")
		for j, ss := range sets {
			for k, snapshot := range ss.Snapshots {
				comm := check.Commentf("%d: %d/%v #%d/%d", i, t.setID, t.snapnames, j, k)
				if t.predicate != nil {
					c.Check(t.predicate(snapshot), check.Equals, true, comm)
				}
				nShots++
				fn := fmt.Sprintf(fnTpl, snapshot.SetID, snapshot.Snap, snapshot.Version, snapshot.Revision)
				c.Check(backend.Filename(snapshot), check.Equals, fn, comm)
				c.Check(snapshot.SnapID, check.Equals, "id-for-"+snapshot.Snap)
			}
		}
		c.Check(nShots, check.Equals, t.numShots)
	}
}

func (s *snapshotSuite) TestAddDirToZipBails(c *check.C) {
	snapshot := &client.Snapshot{SetID: 42, Snap: "a-snap", Revision: snap.R(5)}

	oldVal := os.Getenv("SNAPD_DEBUG")
	c.Assert(os.Setenv("SNAPD_DEBUG", "1"), check.IsNil)
	defer func() {
		os.Setenv("SNAPD_DEBUG", oldVal)
	}()

	buf, restore := logger.MockLogger()
	defer restore()
	savingUserData := false
	// note as the zip is nil this would panic if it didn't bail
	c.Check(backend.AddSnapDirToZip(nil, snapshot, nil, "", "an/entry", filepath.Join(s.root, "nonexistent"), savingUserData, nil), check.IsNil)
	c.Check(backend.AddSnapDirToZip(nil, snapshot, nil, "", "an/entry", "/etc/passwd", savingUserData, nil), check.IsNil)
	c.Check(buf.String(), check.Matches, "(?m).* is does not exist.*")
}

func (s *snapshotSuite) TestAddDirToZipTarFails(c *check.C) {
	rev := snap.R(5)
	d := filepath.Join(s.root, rev.String())
	c.Assert(os.MkdirAll(filepath.Join(d, "bar"), 0755), check.IsNil)
	c.Assert(os.MkdirAll(filepath.Join(s.root, "common"), 0755), check.IsNil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	savingUserData := false
	c.Assert(backend.AddSnapDirToZip(ctx, &client.Snapshot{Revision: rev}, z, "", "an/entry", s.root, savingUserData, nil), check.ErrorMatches, ".* context canceled")
}

func (s *snapshotSuite) TestAddDirToZip(c *check.C) {
	rev := snap.R(5)
	d := filepath.Join(s.root, rev.String())
	c.Assert(os.MkdirAll(filepath.Join(d, "bar"), 0755), check.IsNil)
	c.Assert(os.MkdirAll(filepath.Join(s.root, "common"), 0755), check.IsNil)
	c.Assert(os.WriteFile(filepath.Join(d, "bar", "baz"), []byte("hello\n"), 0644), check.IsNil)

	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	snapshot := &client.Snapshot{
		SHA3_384: map[string]string{},
		Revision: rev,
	}
	savingUserData := false
	c.Assert(backend.AddSnapDirToZip(context.Background(), snapshot, z, "", "an/entry", s.root, savingUserData, nil), check.IsNil)
	z.Close() // write out the central directory

	c.Check(snapshot.SHA3_384, check.HasLen, 1)
	c.Check(snapshot.SHA3_384["an/entry"], check.HasLen, 96)
	c.Check(snapshot.Size > 0, check.Equals, true) // actual size most likely system-dependent
	br := bytes.NewReader(buf.Bytes())
	r := mylog.Check2(zip.NewReader(br, int64(br.Len())))
	c.Assert(err, check.IsNil)
	c.Check(r.File, check.HasLen, 1)
	c.Check(r.File[0].Name, check.Equals, "an/entry")
}

func (s *snapshotSuite) TestAddDirToZipExclusions(c *check.C) {
	d := filepath.Join(s.root, "x1")
	c.Assert(os.MkdirAll(d, 0755), check.IsNil)

	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	snapshot := &client.Snapshot{
		SHA3_384: map[string]string{},
		Revision: snap.R("x1"),
	}
	defer z.Close()

	var tarArgs []string
	restore := backend.MockTarAsUser(func(username string, args ...string) *exec.Cmd {
		// We care only about the exclusion arguments in this test
		tarArgs = nil
		for _, arg := range args {
			if strings.HasPrefix(arg, "--exclude=") {
				tarArgs = append(tarArgs, arg)
			}
		}
		// We only care about being called with the right arguments
		return exec.Command("false")
	})
	defer restore()

	for _, testData := range []struct {
		excludes       []string
		savingUserData bool
		expectedArgs   []string
	}{
		{
			[]string{"$SNAP_DATA/file"},
			false,
			[]string{"--exclude=x1/file"},
		},
		{
			// user data, but vars are for system data: they must be ignored
			[]string{"$SNAP_DATA/a", "$SNAP_COMMON_DATA/b"}, true, nil,
		},
		{
			// system data, but vars are for system data: they must be ignored
			[]string{"$SNAP_USER_DATA/a", "$SNAP_USER_COMMON/b"}, false, nil,
		},
		{
			// system data
			[]string{"$SNAP_DATA/one", "$SNAP_COMMON/two"},
			false,
			[]string{"--exclude=x1/one", "--exclude=common/two"},
		},
		{
			// user data
			[]string{"$SNAP_USER_DATA/file", "$SNAP_USER_COMMON/test"},
			true,
			[]string{"--exclude=x1/file", "--exclude=common/test"},
		},
		{
			// mixed case
			[]string{"$SNAP_USER_DATA/1", "$SNAP_DATA/2", "$SNAP_COMMON/3", "$SNAP_DATA/4"},
			false,
			[]string{"--exclude=x1/2", "--exclude=common/3", "--exclude=x1/4"},
		},
	} {
		testLabel := check.Commentf("%s/%v", testData.excludes, testData.savingUserData)
		mylog.Check(backend.AddSnapDirToZip(context.Background(), snapshot, z, "", "an/entry", s.root, testData.savingUserData, testData.excludes))
		c.Check(err, check.ErrorMatches, "tar failed.*")
		c.Check(tarArgs, check.DeepEquals, testData.expectedArgs, testLabel)
	}
}

func (s *snapshotSuite) TestHappyRoundtrip(c *check.C) {
	s.testHappyRoundtrip(c, "marker")
}

func (s *snapshotSuite) TestHappyRoundtripNoCommon(c *check.C) {
	for _, t := range table(snap.MinimalPlaceInfo("hello-snap", snap.R(42)), filepath.Join(dirs.GlobalRootDir, "home/snapuser")) {
		if _, d := filepath.Split(t.dir); d == "common" {
			c.Assert(os.RemoveAll(t.dir), check.IsNil)
		}
	}
	s.testHappyRoundtrip(c, "marker")
}

func (s *snapshotSuite) TestHappyRoundtripNoRev(c *check.C) {
	for _, t := range table(snap.MinimalPlaceInfo("hello-snap", snap.R(42)), filepath.Join(dirs.GlobalRootDir, "home/snapuser")) {
		if _, d := filepath.Split(t.dir); d == "42" {
			c.Assert(os.RemoveAll(t.dir), check.IsNil)
		}
	}
	s.testHappyRoundtrip(c, "../common/marker")
}

func (s *snapshotSuite) testHappyRoundtrip(c *check.C, marker string) {
	if os.Geteuid() == 0 {
		c.Skip("this test cannot run as root (runuser will fail)")
	}
	logger.SimpleSetup(nil)

	epoch := snap.E("42*")
	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "hello-snap", Revision: snap.R(42), SnapID: "hello-id"}, Version: "v1.33", Epoch: epoch}
	cfg := map[string]interface{}{"some-setting": false}
	shID := uint64(12)

	statExcludes := []string{"$SNAP_USER_DATA/exclude", "$SNAP_USER_COMMON/exclude"}
	dynExcludes := []string{"$SNAP_DATA/exclude", "$SNAP_COMMON/exclude"}
	mergedExcludes := append(statExcludes, dynExcludes...)
	statSnapshotOpts := &snap.SnapshotOptions{Exclude: statExcludes}
	dynSnapshotOpts := &snap.SnapshotOptions{Exclude: dynExcludes}

	var readSnapshotYamlCalled int
	defer backend.MockReadSnapshotYaml(func(si *snap.Info) (*snap.SnapshotOptions, error) {
		readSnapshotYamlCalled++
		c.Check(si, check.DeepEquals, info)
		return statSnapshotOpts, nil
	})()

	shw := mylog.Check2(backend.Save(context.TODO(), shID, info, cfg, []string{"snapuser"}, dynSnapshotOpts, nil))
	c.Assert(err, check.IsNil)
	c.Check(shw.SetID, check.Equals, shID)
	c.Check(shw.Snap, check.Equals, info.InstanceName())
	c.Check(shw.SnapID, check.Equals, info.SnapID)
	c.Check(shw.Version, check.Equals, info.Version)
	c.Check(shw.Epoch, check.DeepEquals, epoch)
	c.Check(shw.Revision, check.Equals, info.Revision)
	c.Check(shw.Conf, check.DeepEquals, cfg)
	c.Check(shw.Auto, check.Equals, false)
	c.Check(shw.Options, check.DeepEquals, dynSnapshotOpts)
	c.Check(backend.Filename(shw), check.Equals, filepath.Join(dirs.SnapshotsDir, "12_hello-snap_v1.33_42.zip"))
	c.Check(hashkeys(shw), check.DeepEquals, []string{"archive.tgz", "user/snapuser.tgz"})
	c.Check(statSnapshotOpts.Exclude, check.DeepEquals, mergedExcludes)
	c.Check(readSnapshotYamlCalled, check.Equals, 1)

	shs := mylog.Check2(backend.List(context.TODO(), 0, nil))
	c.Assert(err, check.IsNil)
	c.Assert(shs, check.HasLen, 1)
	c.Assert(shs[0].Snapshots, check.HasLen, 1)

	shr := mylog.Check2(backend.Open(backend.Filename(shw), backend.ExtractFnameSetID))
	c.Assert(err, check.IsNil)
	defer shr.Close()

	for label, sh := range map[string]*client.Snapshot{"open": &shr.Snapshot, "list": shs[0].Snapshots[0]} {
		comm := check.Commentf("%q", label)
		c.Check(sh.SetID, check.Equals, shID, comm)
		c.Check(sh.Snap, check.Equals, info.InstanceName(), comm)
		c.Check(sh.SnapID, check.Equals, info.SnapID, comm)
		c.Check(sh.Version, check.Equals, info.Version, comm)
		c.Check(sh.Epoch, check.DeepEquals, epoch)
		c.Check(sh.Revision, check.Equals, info.Revision, comm)
		c.Check(sh.Conf, check.DeepEquals, cfg, comm)
		c.Check(sh.SHA3_384, check.DeepEquals, shw.SHA3_384, comm)
		c.Check(sh.Auto, check.Equals, false)
		c.Check(sh.Options, check.DeepEquals, dynSnapshotOpts)
	}
	c.Check(shr.Name(), check.Equals, filepath.Join(dirs.SnapshotsDir, "12_hello-snap_v1.33_42.zip"))
	c.Check(shr.Check(context.TODO(), nil), check.IsNil)

	newroot := c.MkDir()
	c.Assert(os.MkdirAll(filepath.Join(newroot, "home/snapuser"), 0755), check.IsNil)
	dirs.SetRootDir(newroot)

	diff := func() *exec.Cmd {
		cmd := exec.Command("diff", "-urN", "-x*.zip", s.root, newroot)
		// cmd.Stdout = os.Stdout
		// cmd.Stderr = os.Stderr
		return cmd
	}

	for i := 0; i < 3; i++ {
		comm := check.Commentf("%d", i)
		// validity check
		c.Check(diff().Run(), check.NotNil, comm)

		// restore leaves things like they were (again and again)
		rs := mylog.Check2(shr.Restore(context.TODO(), snap.R(0), nil, logger.Debugf, nil))
		c.Assert(err, check.IsNil, comm)
		rs.Cleanup()
		c.Check(diff().Run(), check.IsNil, comm)

		// dirty it -> no longer like it was
		c.Check(os.WriteFile(filepath.Join(info.DataDir(), marker), []byte("scribble\n"), 0644), check.IsNil, comm)
	}
}

func (s *snapshotSuite) TestOpenSetIDoverride(c *check.C) {
	if os.Geteuid() == 0 {
		c.Skip("this test cannot run as root (runuser will fail)")
	}
	logger.SimpleSetup(nil)

	epoch := snap.E("42*")
	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "hello-snap", Revision: snap.R(42), SnapID: "hello-id"}, Version: "v1.33", Epoch: epoch}
	cfg := map[string]interface{}{"some-setting": false}

	shw := mylog.Check2(backend.Save(context.TODO(), 12, info, cfg, []string{"snapuser"}, nil, nil))
	c.Assert(err, check.IsNil)
	c.Check(shw.SetID, check.Equals, uint64(12))

	c.Check(backend.Filename(shw), check.Equals, filepath.Join(dirs.SnapshotsDir, "12_hello-snap_v1.33_42.zip"))
	c.Check(hashkeys(shw), check.DeepEquals, []string{"archive.tgz", "user/snapuser.tgz"})

	shr := mylog.Check2(backend.Open(backend.Filename(shw), 99))
	c.Assert(err, check.IsNil)
	defer shr.Close()

	c.Check(shr.SetID, check.Equals, uint64(99))
}

func (s *snapshotSuite) TestRestoreRoundtripDifferentRevision(c *check.C) {
	if os.Geteuid() == 0 {
		c.Skip("this test cannot run as root (runuser will fail)")
	}
	logger.SimpleSetup(nil)

	epoch := snap.E("42*")
	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "hello-snap", Revision: snap.R(42), SnapID: "hello-id"}, Version: "v1.33", Epoch: epoch}
	shID := uint64(12)

	shw := mylog.Check2(backend.Save(context.TODO(), shID, info, nil, []string{"snapuser"}, nil, nil))
	c.Assert(err, check.IsNil)
	c.Check(shw.Revision, check.Equals, info.Revision)

	shr := mylog.Check2(backend.Open(backend.Filename(shw), backend.ExtractFnameSetID))
	c.Assert(err, check.IsNil)
	defer shr.Close()

	c.Check(shr.Revision, check.Equals, info.Revision)
	c.Check(shr.Name(), check.Equals, filepath.Join(dirs.SnapshotsDir, "12_hello-snap_v1.33_42.zip"))

	// move the expected data to its expected place
	for _, dir := range []string{
		filepath.Join(s.root, "home", "snapuser", "snap", "hello-snap"),
		filepath.Join(dirs.SnapDataDir, "hello-snap"),
	} {
		c.Check(os.Rename(filepath.Join(dir, "42"), filepath.Join(dir, "17")), check.IsNil)
	}

	newroot := c.MkDir()
	c.Assert(os.MkdirAll(filepath.Join(newroot, "home", "snapuser"), 0755), check.IsNil)
	dirs.SetRootDir(newroot)

	diff := func() *exec.Cmd {
		cmd := exec.Command("diff", "-urN", "-x*.zip", s.root, newroot)
		// cmd.Stdout = os.Stdout
		// cmd.Stderr = os.Stderr
		return cmd
	}

	// validity check
	c.Check(diff().Run(), check.NotNil)

	// restore leaves things like they were, but in the new dir
	rs := mylog.Check2(shr.Restore(context.TODO(), snap.R("17"), nil, logger.Debugf, nil))
	c.Assert(err, check.IsNil)
	rs.Cleanup()
	c.Check(diff().Run(), check.IsNil)
}

func (s *snapshotSuite) TestPickUserWrapperRunuser(c *check.C) {
	n := 0
	defer backend.MockExecLookPath(func(s string) (string, error) {
		n++
		if s != "runuser" {
			c.Fatalf(`expected to get "runuser", got %q`, s)
		}
		return "/sbin/runuser", nil
	})()

	c.Check(backend.PickUserWrapper(), check.Equals, "/sbin/runuser")
	c.Check(n, check.Equals, 1)
}

func (s *snapshotSuite) TestPickUserWrapperSudo(c *check.C) {
	n := 0
	defer backend.MockExecLookPath(func(s string) (string, error) {
		n++
		if n == 1 {
			if s != "runuser" {
				c.Fatalf(`expected to get "runuser" first, got %q`, s)
			}
			return "", errors.New("no such thing")
		}
		if s != "sudo" {
			c.Fatalf(`expected to get "sudo" next, got %q`, s)
		}
		return "/usr/bin/sudo", nil
	})()

	c.Check(backend.PickUserWrapper(), check.Equals, "/usr/bin/sudo")
	c.Check(n, check.Equals, 2)
}

func (s *snapshotSuite) TestPickUserWrapperNothing(c *check.C) {
	n := 0
	defer backend.MockExecLookPath(func(s string) (string, error) {
		n++
		return "", errors.New("no such thing")
	})()

	c.Check(backend.PickUserWrapper(), check.Equals, "")
	c.Check(n, check.Equals, 2)
}

func (s *snapshotSuite) TestMaybeRunuserHappyRunuser(c *check.C) {
	uid := sys.UserID(0)
	defer backend.MockSysGeteuid(func() sys.UserID { return uid })()
	defer backend.SetUserWrapper("/sbin/runuser")()
	logbuf, restore := logger.MockLogger()
	defer restore()

	c.Check(backend.TarAsUser("test", "--bar"), check.DeepEquals, &exec.Cmd{
		Path: "/sbin/runuser",
		Args: []string{"/sbin/runuser", "-u", "test", "--", "tar", "--bar"},
	})
	c.Check(backend.TarAsUser("root", "--bar"), check.DeepEquals, &exec.Cmd{
		Path: s.tarPath,
		Args: []string{"tar", "--bar"},
	})
	uid = 42
	c.Check(backend.TarAsUser("test", "--bar"), check.DeepEquals, &exec.Cmd{
		Path: s.tarPath,
		Args: []string{"tar", "--bar"},
	})
	c.Check(logbuf.String(), check.Equals, "")
}

func (s *snapshotSuite) TestMaybeRunuserHappySudo(c *check.C) {
	uid := sys.UserID(0)
	defer backend.MockSysGeteuid(func() sys.UserID { return uid })()
	defer backend.SetUserWrapper("/usr/bin/sudo")()
	logbuf, restore := logger.MockLogger()
	defer restore()

	cmd := backend.TarAsUser("test", "--bar")
	c.Check(cmd, check.DeepEquals, &exec.Cmd{
		Path: "/usr/bin/sudo",
		Args: []string{"/usr/bin/sudo", "-u", "test", "--", "tar", "--bar"},
	})
	c.Check(backend.TarAsUser("root", "--bar"), check.DeepEquals, &exec.Cmd{
		Path: s.tarPath,
		Args: []string{"tar", "--bar"},
	})
	uid = 42
	c.Check(backend.TarAsUser("test", "--bar"), check.DeepEquals, &exec.Cmd{
		Path: s.tarPath,
		Args: []string{"tar", "--bar"},
	})
	c.Check(logbuf.String(), check.Equals, "")
}

func (s *snapshotSuite) TestMaybeRunuserNoHappy(c *check.C) {
	uid := sys.UserID(0)
	defer backend.MockSysGeteuid(func() sys.UserID { return uid })()
	defer backend.SetUserWrapper("")()
	logbuf, restore := logger.MockLogger()
	defer restore()

	c.Check(backend.TarAsUser("test", "--bar"), check.DeepEquals, &exec.Cmd{
		Path: s.tarPath,
		Args: []string{"tar", "--bar"},
	})
	c.Check(backend.TarAsUser("root", "--bar"), check.DeepEquals, &exec.Cmd{
		Path: s.tarPath,
		Args: []string{"tar", "--bar"},
	})
	uid = 42
	c.Check(backend.TarAsUser("test", "--bar"), check.DeepEquals, &exec.Cmd{
		Path: s.tarPath,
		Args: []string{"tar", "--bar"},
	})
	c.Check(strings.TrimSpace(logbuf.String()), check.Matches, ".* No user wrapper found.*")
}

func (s *snapshotSuite) TestImport(c *check.C) {
	tempdir := c.MkDir()

	// create snapshot export file
	tarFile1 := path.Join(tempdir, "exported1.snapshot")
	mylog.Check(createTestExportFile(tarFile1, &createTestExportFlags{exportJSON: true}))
	c.Check(err, check.IsNil)

	// create an exported snapshot with missing export.json
	tarFile2 := path.Join(tempdir, "exported2.snapshot")
	mylog.Check(createTestExportFile(tarFile2, &createTestExportFlags{}))
	c.Check(err, check.IsNil)

	// create invalid exported file
	tarFile3 := path.Join(tempdir, "exported3.snapshot")
	mylog.Check(os.WriteFile(tarFile3, []byte("invalid"), 0755))
	c.Check(err, check.IsNil)

	// create an exported snapshot with a directory
	tarFile4 := path.Join(tempdir, "exported4.snapshot")
	flags := &createTestExportFlags{
		exportJSON: true,
		withDir:    true,
	}
	mylog.Check(createTestExportFile(tarFile4, flags))
	c.Check(err, check.IsNil)

	// create an exported snapshot with parent path element
	tarFile5 := path.Join(tempdir, "exported5.snapshot")
	flags = &createTestExportFlags{
		exportJSON: true,
		withParent: true,
	}
	mylog.Check(createTestExportFile(tarFile5, flags))
	c.Check(err, check.IsNil)

	type tableT struct {
		setID      uint64
		filename   string
		inProgress bool
		error      string
	}

	table := []tableT{
		{14, tarFile1, false, ""},
		{14, tarFile2, false, "cannot import snapshot 14: no export.json file in uploaded data"},
		{14, tarFile3, false, "cannot import snapshot 14: cannot read snapshot import: unexpected EOF"},
		{14, tarFile4, false, "cannot import snapshot 14: unexpected directory in import file"},
		{14, tarFile5, false, "cannot import snapshot 14: invalid filename in import file"},
		{14, tarFile1, true, "cannot import snapshot 14: already in progress for this set id"},
	}

	for i, t := range table {
		comm := check.Commentf("%d: %d %s", i, t.setID, t.filename)
		mylog.Check(

			// reset
			os.RemoveAll(dirs.SnapshotsDir))
		c.Assert(err, check.IsNil, comm)
		importingFile := filepath.Join(dirs.SnapshotsDir, fmt.Sprintf("%d_importing", t.setID))
		if t.inProgress {
			mylog.Check(os.MkdirAll(dirs.SnapshotsDir, 0700))
			c.Assert(err, check.IsNil, comm)
			mylog.Check(os.WriteFile(importingFile, nil, 0644))
			c.Assert(err, check.IsNil)
		} else {
			mylog.Check(os.RemoveAll(importingFile))
			c.Assert(err, check.IsNil, comm)
		}

		f := mylog.Check2(os.Open(t.filename))
		c.Assert(err, check.IsNil, comm)
		defer f.Close()

		snapNames := mylog.Check2(backend.Import(context.Background(), t.setID, f, nil))
		if t.error != "" {
			c.Check(err, check.ErrorMatches, t.error, comm)
			continue
		}
		c.Check(err, check.IsNil, comm)
		sort.Strings(snapNames)
		c.Check(snapNames, check.DeepEquals, []string{"bar", "baz", "foo"})

		dir := mylog.Check2(os.Open(dirs.SnapshotsDir))
		c.Assert(err, check.IsNil, comm)
		defer dir.Close()
		names := mylog.Check2(dir.Readdirnames(100))
		c.Assert(err, check.IsNil, comm)
		c.Check(len(names), check.Equals, 3, comm)
	}
}

func (s *snapshotSuite) TestImportCheckError(c *check.C) {
	mylog.Check(os.MkdirAll(dirs.SnapshotsDir, 0755))
	c.Assert(err, check.IsNil)

	// create snapshot export file
	tarFile1 := path.Join(c.MkDir(), "exported1.snapshot")
	flags := &createTestExportFlags{
		exportJSON:      true,
		corruptChecksum: true,
	}
	mylog.Check(createTestExportFile(tarFile1, flags))
	c.Assert(err, check.IsNil)

	f := mylog.Check2(os.Open(tarFile1))
	c.Assert(err, check.IsNil)
	_ = mylog.Check2(backend.Import(context.Background(), 14, f, nil))
	c.Assert(err, check.ErrorMatches, `cannot import snapshot 14: validation failed for .+/14_foo_1.0_199.zip": snapshot entry "archive.tgz" expected hash \(d5ef563…\) does not match actual \(6655519…\)`)
}

func (s *snapshotSuite) TestImportDuplicated(c *check.C) {
	mylog.Check(os.MkdirAll(dirs.SnapshotsDir, 0755))
	c.Assert(err, check.IsNil)

	ctx := context.TODO()

	epoch := snap.E("42*")
	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "hello-snap", Revision: snap.R(42), SnapID: "hello-id"}, Version: "v1.33", Epoch: epoch}
	shID := uint64(12)

	shw := mylog.Check2(backend.Save(ctx, shID, info, nil, []string{"snapuser"}, nil, nil))
	c.Assert(err, check.IsNil)

	export := mylog.Check2(backend.NewSnapshotExport(ctx, shw.SetID))
	c.Assert(err, check.IsNil)
	mylog.Check(export.Init())
	c.Assert(err, check.IsNil)

	buf := bytes.NewBuffer(nil)
	c.Assert(export.StreamTo(buf), check.IsNil)
	c.Check(buf.Len(), check.Equals, int(export.Size()))

	// now import it
	_ = mylog.Check2(backend.Import(ctx, 123, buf, nil))
	dupErr, ok := err.(backend.DuplicatedSnapshotImportError)
	c.Assert(ok, check.Equals, true)
	c.Assert(dupErr, check.DeepEquals, backend.DuplicatedSnapshotImportError{SetID: shID, SnapNames: []string{"hello-snap"}})
}

func (s *snapshotSuite) TestImportExportRoundtrip(c *check.C) {
	mylog.Check(os.MkdirAll(dirs.SnapshotsDir, 0755))
	c.Assert(err, check.IsNil)

	ctx := context.TODO()

	epoch := snap.E("42*")
	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "hello-snap", Revision: snap.R(42), SnapID: "hello-id"}, Version: "v1.33", Epoch: epoch}
	cfg := map[string]interface{}{"some-setting": false}
	shID := uint64(12)

	shw := mylog.Check2(backend.Save(ctx, shID, info, cfg, []string{"snapuser"}, nil, nil))
	c.Assert(err, check.IsNil)
	c.Check(shw.SetID, check.Equals, shID)

	c.Check(backend.Filename(shw), check.Equals, filepath.Join(dirs.SnapshotsDir, "12_hello-snap_v1.33_42.zip"))
	c.Check(hashkeys(shw), check.DeepEquals, []string{"archive.tgz", "user/snapuser.tgz"})

	export := mylog.Check2(backend.NewSnapshotExport(ctx, shw.SetID))
	c.Assert(err, check.IsNil)
	mylog.Check(export.Init())
	c.Assert(err, check.IsNil)

	buf := bytes.NewBuffer(nil)
	c.Assert(export.StreamTo(buf), check.IsNil)
	c.Check(buf.Len(), check.Equals, int(export.Size()))

	// now import it
	c.Assert(os.Remove(filepath.Join(dirs.SnapshotsDir, "12_hello-snap_v1.33_42.zip")), check.IsNil)

	names := mylog.Check2(backend.Import(ctx, 123, buf, nil))
	c.Assert(err, check.IsNil)
	c.Check(names, check.DeepEquals, []string{"hello-snap"})

	sets := mylog.Check2(backend.List(ctx, 0, nil))
	c.Assert(err, check.IsNil)
	c.Assert(sets, check.HasLen, 1)
	c.Check(sets[0].ID, check.Equals, uint64(123))

	rdr := mylog.Check2(backend.Open(filepath.Join(dirs.SnapshotsDir, "123_hello-snap_v1.33_42.zip"), backend.ExtractFnameSetID))
	defer rdr.Close()
	c.Check(err, check.IsNil)
	c.Check(rdr.SetID, check.Equals, uint64(123))
	c.Check(rdr.Snap, check.Equals, "hello-snap")
	c.Check(rdr.IsValid(), check.Equals, true)
}

func (s *snapshotSuite) TestEstimateSnapshotSize(c *check.C) {
	for _, t := range []struct {
		snapDir string
		opts    *dirs.SnapDirOptions
	}{
		{dirs.UserHomeSnapDir, nil},
		{dirs.UserHomeSnapDir, &dirs.SnapDirOptions{HiddenSnapDataDir: false}},
		{dirs.HiddenSnapDataHomeDir, &dirs.SnapDirOptions{HiddenSnapDataDir: true}},
	} {
		s.testEstimateSnapshotSize(c, t.snapDir, t.opts)
	}
}

func (s *snapshotSuite) testEstimateSnapshotSize(c *check.C, snapDataDir string, opts *dirs.SnapDirOptions) {
	restore := backend.MockUsersForUsernames(func(usernames []string, _ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{{HomeDir: filepath.Join(s.root, "home/user1")}}, nil
	})
	defer restore()

	info := &snap.Info{
		SuggestedName: "foo",
		SideInfo: snap.SideInfo{
			Revision: snap.R(7),
		},
	}

	snapData := []string{
		"/var/snap/foo/7/somedatadir",
		"/var/snap/foo/7/otherdata",
		"/var/snap/foo/7",
		"/var/snap/foo/common",
		"/var/snap/foo/common/a",
		filepath.Join("/home/user1", snapDataDir, "foo/7/somedata"),
		filepath.Join("/home/user1", snapDataDir, "foo/common"),
	}
	var data []byte
	var expected int
	for _, d := range snapData {
		data = append(data, 0)
		expected += len(data)
		c.Assert(os.MkdirAll(filepath.Join(s.root, d), 0755), check.IsNil)
		c.Assert(os.WriteFile(filepath.Join(s.root, d, "somefile"), data, 0644), check.IsNil)
	}

	sz := mylog.Check2(backend.EstimateSnapshotSize(info, nil, opts))
	c.Assert(err, check.IsNil)
	c.Check(sz, check.Equals, uint64(expected))
}

func (s *snapshotSuite) TestEstimateSnapshotSizeEmpty(c *check.C) {
	restore := backend.MockUsersForUsernames(func(usernames []string, _ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{{HomeDir: filepath.Join(s.root, "home/user1")}}, nil
	})
	defer restore()

	info := &snap.Info{
		SuggestedName: "foo",
		SideInfo: snap.SideInfo{
			Revision: snap.R(7),
		},
	}

	snapData := []string{
		"/var/snap/foo/common",
		"/var/snap/foo/7",
		"/home/user1/snap/foo/7",
		"/home/user1/snap/foo/common",
	}
	for _, d := range snapData {
		c.Assert(os.MkdirAll(filepath.Join(s.root, d), 0755), check.IsNil)
	}

	sz := mylog.Check2(backend.EstimateSnapshotSize(info, nil, nil))
	c.Assert(err, check.IsNil)
	c.Check(sz, check.Equals, uint64(0))
}

func (s *snapshotSuite) TestEstimateSnapshotPassesUsernames(c *check.C) {
	var gotUsernames []string
	restore := backend.MockUsersForUsernames(func(usernames []string, _ *dirs.SnapDirOptions) ([]*user.User, error) {
		gotUsernames = usernames
		return nil, nil
	})
	defer restore()

	info := &snap.Info{
		SuggestedName: "foo",
		SideInfo: snap.SideInfo{
			Revision: snap.R(7),
		},
	}

	_ := mylog.Check2(backend.EstimateSnapshotSize(info, []string{"user1", "user2"}, nil))
	c.Assert(err, check.IsNil)
	c.Check(gotUsernames, check.DeepEquals, []string{"user1", "user2"})
}

func (s *snapshotSuite) TestEstimateSnapshotSizeNotDataDirs(c *check.C) {
	restore := backend.MockUsersForUsernames(func(usernames []string, _ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{{HomeDir: filepath.Join(s.root, "home/user1")}}, nil
	})
	defer restore()

	info := &snap.Info{
		SuggestedName: "foo",
		SideInfo:      snap.SideInfo{Revision: snap.R(7)},
	}

	sz := mylog.Check2(backend.EstimateSnapshotSize(info, nil, nil))
	c.Assert(err, check.IsNil)
	c.Check(sz, check.Equals, uint64(0))
}

func (s *snapshotSuite) TestExportTwice(c *check.C) {
	// use mocking done in snapshotSuite.SetUpTest
	info := &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "hello-snap",
			Revision: snap.R(42),
			SnapID:   "hello-id",
		},
		Version: "v1.33",
	}
	// create a snapshot
	shID := uint64(12)
	_ := mylog.Check2(backend.Save(context.TODO(), shID, info, nil, []string{"snapuser"}, nil, nil))
	c.Assert(err, check.IsNil)

	// content.json + num_files + export.json + footer
	expectedSize := int64(1024 + 4*512 + 1024 + 2*512)
	// do on export at the start of the epoch
	restore := backend.MockTimeNow(func() time.Time { return time.Time{} })
	defer restore()
	// export once
	buf := bytes.NewBuffer(nil)
	ctx := context.Background()
	se := mylog.Check2(backend.NewSnapshotExport(ctx, shID))
	c.Assert(err, check.IsNil)
	mylog.Check(se.Init())
	c.Assert(err, check.IsNil)
	c.Check(se.Size(), check.Equals, expectedSize)
	mylog.
		// and we can stream the data
		Check(se.StreamTo(buf))
	c.Check(err, check.IsNil)
	c.Check(buf.Len(), check.Equals, int(expectedSize))

	// and again to ensure size does not change when exported again
	//
	// Note that moving beyond year 2242 will change the tar format
	// used by the go internal tar and that will make the size actually
	// change.
	restore = backend.MockTimeNow(func() time.Time { return time.Date(2242, 1, 1, 12, 0, 0, 0, time.UTC) })
	defer restore()
	se2 := mylog.Check2(backend.NewSnapshotExport(ctx, shID))
	c.Assert(err, check.IsNil)
	mylog.Check(se2.Init())
	c.Assert(err, check.IsNil)
	c.Check(se2.Size(), check.Equals, expectedSize)
	// and we can stream the data
	buf.Reset()
	mylog.Check(se2.StreamTo(buf))
	c.Assert(err, check.IsNil)
	c.Check(buf.Len(), check.Equals, int(expectedSize))
}

func (s *snapshotSuite) TestExportUnhappy(c *check.C) {
	se := mylog.Check2(backend.NewSnapshotExport(context.Background(), 5))
	c.Assert(err, check.ErrorMatches, "no snapshot data found for 5")
	c.Assert(se, check.IsNil)
}

type createTestExportFlags struct {
	exportJSON      bool
	withDir         bool
	withParent      bool
	corruptChecksum bool
}

func createTestExportFile(filename string, flags *createTestExportFlags) error {
	tf := mylog.Check2(os.Create(filename))

	defer tf.Close()
	tw := tar.NewWriter(tf)
	defer tw.Close()

	for _, s := range []string{"foo", "bar", "baz"} {
		fname := fmt.Sprintf("5_%s_1.0_199.zip", s)

		buf := bytes.NewBuffer(nil)
		zipW := zip.NewWriter(buf)
		defer zipW.Close()

		sha := map[string]string{}

		// create test archive.tgz
		archiveWriter := mylog.Check2(zipW.CreateHeader(&zip.FileHeader{Name: "archive.tgz"}))

		var sz osutil.Sizer
		hasher := crypto.SHA3_384.New()
		out := io.MultiWriter(archiveWriter, hasher, &sz)
		mylog.Check2(out.Write([]byte(s)))

		if flags.corruptChecksum {
			hasher.Write([]byte{0})
		}
		sha["archive.tgz"] = fmt.Sprintf("%x", hasher.Sum(nil))

		snapshot := backend.MockSnapshot(5, s, snap.Revision{N: 199}, sz.Size(), sha)

		// create meta.json
		metaWriter := mylog.Check2(zipW.Create("meta.json"))

		hasher = crypto.SHA3_384.New()
		enc := json.NewEncoder(io.MultiWriter(metaWriter, hasher))
		mylog.Check(enc.Encode(snapshot))

		// write meta.sha3_384
		metaSha3Writer := mylog.Check2(zipW.Create("meta.sha3_384"))

		fmt.Fprintf(metaSha3Writer, "%x\n", hasher.Sum(nil))
		zipW.Close()

		hdr := &tar.Header{
			Name: fname,
			Mode: 0644,
			Size: int64(buf.Len()),
		}
		mylog.Check(tw.WriteHeader(hdr))
		mylog.Check2(tw.Write(buf.Bytes()))

	}

	if flags.withDir {
		hdr := &tar.Header{
			Name:     dirs.SnapshotsDir,
			Mode:     0700,
			Size:     int64(0),
			Typeflag: tar.TypeDir,
		}
		mylog.Check(tw.WriteHeader(hdr))
		mylog.Check2(tw.Write([]byte("")))

	}

	if flags.withParent {
		hdr := &tar.Header{
			Name: dirs.SnapshotsDir + "/../../2_foo",
			Mode: 0644,
			Size: int64(0),
		}
		mylog.Check(tw.WriteHeader(hdr))
		mylog.Check2(tw.Write([]byte("")))

	}

	if flags.exportJSON {
		exp := fmt.Sprintf(`{"format":1, "date":"%s"}`, time.Now().Format(time.RFC3339))
		hdr := &tar.Header{
			Name: "export.json",
			Mode: 0644,
			Size: int64(len(exp)),
		}
		mylog.Check(tw.WriteHeader(hdr))
		mylog.Check2(tw.Write([]byte(exp)))

	}

	return nil
}

func makeMockSnapshotZipContent(c *check.C) []byte {
	buf := bytes.NewBuffer(nil)
	zipW := zip.NewWriter(buf)

	// create test archive.tgz
	archiveWriter := mylog.Check2(zipW.CreateHeader(&zip.FileHeader{Name: "archive.tgz"}))
	c.Assert(err, check.IsNil)
	_ = mylog.Check2(archiveWriter.Write([]byte("mock archive.tgz content")))
	c.Assert(err, check.IsNil)

	// create test meta.json
	archiveWriter = mylog.Check2(zipW.CreateHeader(&zip.FileHeader{Name: "meta.json"}))
	c.Assert(err, check.IsNil)
	_ = mylog.Check2(archiveWriter.Write([]byte("{}")))
	c.Assert(err, check.IsNil)

	zipW.Close()
	return buf.Bytes()
}

func (s *snapshotSuite) TestIterWithMockedSnapshotFiles(c *check.C) {
	mylog.Check(os.MkdirAll(dirs.SnapshotsDir, 0755))
	c.Assert(err, check.IsNil)

	fn := "1_hello_1.0_x1.zip"
	mylog.Check(os.WriteFile(filepath.Join(dirs.SnapshotsDir, fn), makeMockSnapshotZipContent(c), 0644))
	c.Assert(err, check.IsNil)

	callbackCalled := 0
	f := func(snapshot *backend.Reader) error {
		callbackCalled++
		return nil
	}
	mylog.Check(backend.Iter(context.Background(), f))
	c.Check(err, check.IsNil)
	c.Check(callbackCalled, check.Equals, 1)

	// now pretend we are importing snapshot id 1
	callbackCalled = 0
	fn = "1_importing"
	mylog.Check(os.WriteFile(filepath.Join(dirs.SnapshotsDir, fn), nil, 0644))
	c.Assert(err, check.IsNil)
	mylog.

		// and while importing Iter() does not call the callback
		Check(backend.Iter(context.Background(), f))
	c.Check(err, check.IsNil)
	c.Check(callbackCalled, check.Equals, 0)
}

func (s *snapshotSuite) TestCleanupAbandonedImports(c *check.C) {
	mylog.Check(os.MkdirAll(dirs.SnapshotsDir, 0755))
	c.Assert(err, check.IsNil)

	// create 2 snapshot IDs 1,2
	snapshotFiles := map[int][]string{}
	for i := 1; i < 3; i++ {
		fn := fmt.Sprintf("%d_hello_%d.0_x1.zip", i, i)
		p := filepath.Join(dirs.SnapshotsDir, fn)
		snapshotFiles[i] = append(snapshotFiles[i], p)
		mylog.Check(os.WriteFile(p, makeMockSnapshotZipContent(c), 0644))
		c.Assert(err, check.IsNil)

		fn = fmt.Sprintf("%d_olleh_%d.0_x1.zip", i, i)
		p = filepath.Join(dirs.SnapshotsDir, fn)
		snapshotFiles[i] = append(snapshotFiles[i], p)
		mylog.Check(os.WriteFile(p, makeMockSnapshotZipContent(c), 0644))
		c.Assert(err, check.IsNil)
	}

	// pretend setID 2 has a import file which means which means that
	// an import was started in the past but did not complete
	fn := "2_importing"
	mylog.Check(os.WriteFile(filepath.Join(dirs.SnapshotsDir, fn), nil, 0644))
	c.Assert(err, check.IsNil)

	// run cleanup
	cleaned := mylog.Check2(backend.CleanupAbandonedImports())
	c.Check(cleaned, check.Equals, 1)
	c.Check(err, check.IsNil)

	// id1 untouched
	c.Check(snapshotFiles[1][0], testutil.FilePresent)
	c.Check(snapshotFiles[1][1], testutil.FilePresent)
	// id2 cleaned
	c.Check(snapshotFiles[2][0], testutil.FileAbsent)
	c.Check(snapshotFiles[2][1], testutil.FileAbsent)
}

func (s *snapshotSuite) TestCleanupAbandonedImportsFailMany(c *check.C) {
	restore := backend.MockFilepathGlob(func(string) ([]string, error) {
		return []string{
			"/var/lib/snapd/snapshots/NaN_importing",
			"/var/lib/snapd/snapshots/11_importing",
			"/var/lib/snapd/snapshots/22_importing",
		}, nil
	})
	defer restore()

	_ := mylog.Check2(backend.CleanupAbandonedImports())
	c.Assert(err, check.ErrorMatches, `cannot cleanup imports:
- cannot determine snapshot id from "/var/lib/snapd/snapshots/NaN_importing"
- cannot cancel import for set id 11:
 - remove /.*/var/lib/snapd/snapshots/11_importing: no such file or directory
- cannot cancel import for set id 22:
 - remove /.*/var/lib/snapd/snapshots/22_importing: no such file or directory`)
}

func (s *snapshotSuite) TestMultiError(c *check.C) {
	me2 := backend.NewMultiError("deeper nested wrongness", []error{
		fmt.Errorf("some error in level 2"),
	})
	me1 := backend.NewMultiError("nested wrongness", []error{
		fmt.Errorf("some error in level 1"),
		me2,
		fmt.Errorf("other error in level 1"),
	})
	me := backend.NewMultiError("many things went wrong", []error{
		fmt.Errorf("some normal error"),
		me1,
	})

	c.Check(me, check.ErrorMatches, `many things went wrong:
- some normal error
- nested wrongness:
 - some error in level 1
 - deeper nested wrongness:
  - some error in level 2
 - other error in level 1`)

	// do it again
	c.Check(me, check.ErrorMatches, `many things went wrong:
- some normal error
- nested wrongness:
 - some error in level 1
 - deeper nested wrongness:
  - some error in level 2
 - other error in level 1`)
}

func (s *snapshotSuite) TestMultiErrorCycle(c *check.C) {
	errs := []error{nil, fmt.Errorf("e5")}
	me5 := backend.NewMultiError("he5", errs)
	// very hard to happen in practice
	errs[0] = me5
	me4 := backend.NewMultiError("he4", []error{me5})
	me3 := backend.NewMultiError("he3", []error{me4})
	me2 := backend.NewMultiError("he3", []error{me3})
	me1 := backend.NewMultiError("he1", []error{me2})
	me := backend.NewMultiError("he", []error{me1})

	c.Check(me, check.ErrorMatches, `he:
- he1:
 - he3:
  - he3:
   - he4:
    - he5:
     - he5:
      - he5:
       - he5:
        - circular or too deep error nesting \(max 8\)\?!
        - e5
       - e5
      - e5
     - e5`)
}

func (s *snapshotSuite) TestSnapshotExportContentHash(c *check.C) {
	ctx := context.TODO()
	info := &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "hello-snap",
			Revision: snap.R(42),
			SnapID:   "hello-id",
		},
		Version: "v1.33",
	}
	shID := uint64(12)
	shw := mylog.Check2(backend.Save(ctx, shID, info, nil, []string{"snapuser"}, nil, nil))
	c.Check(err, check.IsNil)

	// now export it
	export := mylog.Check2(backend.NewSnapshotExport(ctx, shw.SetID))
	c.Assert(err, check.IsNil)
	c.Check(export.ContentHash(), check.HasLen, sha256.Size)

	// and check that exporting it again leads to the same content hash
	export2 := mylog.Check2(backend.NewSnapshotExport(ctx, shw.SetID))
	c.Assert(err, check.IsNil)
	c.Check(export.ContentHash(), check.DeepEquals, export2.ContentHash())

	// but changing the snapshot changes the content hash
	info = &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "hello-snap",
			Revision: snap.R(9999),
			SnapID:   "hello-id",
		},
		Version: "v1.33",
	}
	shw = mylog.Check2(backend.Save(ctx, shID, info, nil, []string{"snapuser"}, nil, nil))
	c.Check(err, check.IsNil)

	export3 := mylog.Check2(backend.NewSnapshotExport(ctx, shw.SetID))
	c.Assert(err, check.IsNil)
	c.Check(export.ContentHash(), check.Not(check.DeepEquals), export3.ContentHash())
}
