// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
)

func makeMockMountInfo(c *C, content string) string {
	fn := filepath.Join(c.MkDir(), "mountinfo")
	err := ioutil.WriteFile(fn, []byte(content), 0644)
	c.Assert(err, IsNil)
	return fn
}

func (s *SnapSuite) TestAutoImportAssertsHappy(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	fakeAssertData := []byte("my-assertion")

	n := 0
	total := 2
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/assertions")
			postData, err := ioutil.ReadAll(r.Body)
			c.Assert(err, IsNil)
			c.Check(postData, DeepEquals, fakeAssertData)
			fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done"}}`)
			n++
		case 1:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/create-user")
			postData, err := ioutil.ReadAll(r.Body)
			c.Assert(err, IsNil)
			c.Check(string(postData), Equals, `{"sudoer":true,"known":true}`)

			fmt.Fprintln(w, `{"type": "sync", "result": [{"username": "foo"}]}`)
			n++
		default:
			c.Fatalf("unexpected request: %v (expected %d got %d)", r, total, n)
		}

	})

	fakeAssertsFn := filepath.Join(c.MkDir(), "auto-import.assert")
	err := ioutil.WriteFile(fakeAssertsFn, fakeAssertData, 0644)
	c.Assert(err, IsNil)

	mockMountInfoFmt := `
24 0 8:18 / %s rw,relatime shared:1 - ext4 /dev/sdb2 rw,errors=remount-ro,data=ordered`
	content := fmt.Sprintf(mockMountInfoFmt, filepath.Dir(fakeAssertsFn))
	restore = snap.MockMountInfoPath(makeMockMountInfo(c, content))
	defer restore()

	logbuf, restore := logger.MockLogger()
	defer restore()

	rest, err := snap.Parser().ParseArgs([]string{"auto-import"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Equals, `created user "foo"`+"\n")
	// matches because we may get a:
	//   "WARNING: cannot create syslog logger\n"
	// in the output
	c.Check(logbuf.String(), Matches, fmt.Sprintf("(?ms).*imported %s\n", fakeAssertsFn))
	c.Check(n, Equals, total)
}

func (s *SnapSuite) TestAutoImportAssertsNotImportedFromLoop(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	fakeAssertData := []byte("bad-assertion")

	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		// assertion is ignored, nothing is posted to this endpoint
		panic("not reached")
	})

	fakeAssertsFn := filepath.Join(c.MkDir(), "auto-import.assert")
	err := ioutil.WriteFile(fakeAssertsFn, fakeAssertData, 0644)
	c.Assert(err, IsNil)

	mockMountInfoFmtWithLoop := `
24 0 8:18 / %s rw,relatime shared:1 - squashfs /dev/loop1 rw,errors=remount-ro,data=ordered`
	content := fmt.Sprintf(mockMountInfoFmtWithLoop, filepath.Dir(fakeAssertsFn))
	restore = snap.MockMountInfoPath(makeMockMountInfo(c, content))
	defer restore()

	rest, err := snap.Parser().ParseArgs([]string{"auto-import"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestAutoImportCandidatesHappy(c *C) {
	dirs := make([]string, 4)
	args := make([]interface{}, len(dirs))
	files := make([]string, len(dirs))
	for i := range dirs {
		dirs[i] = c.MkDir()
		args[i] = dirs[i]
		files[i] = filepath.Join(dirs[i], "auto-import.assert")
		err := ioutil.WriteFile(files[i], nil, 0644)
		c.Assert(err, IsNil)
	}

	mockMountInfoFmtWithLoop := `
too short
24 0 8:18 / %[1]s rw,relatime foo ext3 /dev/meep2 no,separator
24 0 8:18 / %[2]s rw,relatime - ext3 /dev/meep2 rw,errors=remount-ro,data=ordered
24 0 8:18 / %[3]s rw,relatime opt:1 - ext4 /dev/meep3 rw,errors=remount-ro,data=ordered
24 0 8:18 / %[4]s rw,relatime opt:1 opt:2 - ext2 /dev/meep1 rw,errors=remount-ro,data=ordered
`

	content := fmt.Sprintf(mockMountInfoFmtWithLoop, args...)
	restore := snap.MockMountInfoPath(makeMockMountInfo(c, content))
	defer restore()

	l, err := snap.AutoImportCandidates()
	c.Check(err, IsNil)
	c.Check(l, DeepEquals, files[1:])
}

func (s *SnapSuite) TestAutoImportAssertsHappyNotOnClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	fakeAssertData := []byte("my-assertion")
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Errorf("auto-import on classic is disabled, but something tried to do a %q with %s", r.Method, r.URL.Path)
	})

	fakeAssertsFn := filepath.Join(c.MkDir(), "auto-import.assert")
	err := ioutil.WriteFile(fakeAssertsFn, fakeAssertData, 0644)
	c.Assert(err, IsNil)

	mockMountInfoFmt := `
24 0 8:18 / %s rw,relatime shared:1 - ext4 /dev/sdb2 rw,errors=remount-ro,data=ordered`
	content := fmt.Sprintf(mockMountInfoFmt, filepath.Dir(fakeAssertsFn))
	restore = snap.MockMountInfoPath(makeMockMountInfo(c, content))
	defer restore()

	rest, err := snap.Parser().ParseArgs([]string{"auto-import"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "auto-import is disabled on classic\n")
}

func (s *SnapSuite) TestAutoImportIntoSpool(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	logbuf, restore := logger.MockLogger()
	defer restore()

	fakeAssertData := []byte("good-assertion")

	// ensure we can not connect
	snap.ClientConfig.BaseURL = "can-not-connect-to-this-url"

	fakeAssertsFn := filepath.Join(c.MkDir(), "auto-import.assert")
	err := ioutil.WriteFile(fakeAssertsFn, fakeAssertData, 0644)
	c.Assert(err, IsNil)

	mockMountInfoFmt := `
24 0 8:18 / %s rw,relatime shared:1 - squashfs /dev/sc1 rw,errors=remount-ro,data=ordered`
	content := fmt.Sprintf(mockMountInfoFmt, filepath.Dir(fakeAssertsFn))
	restore = snap.MockMountInfoPath(makeMockMountInfo(c, content))
	defer restore()

	rest, err := snap.Parser().ParseArgs([]string{"auto-import"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Equals, "")
	// matches because we may get a:
	//   "WARNING: cannot create syslog logger\n"
	// in the output
	c.Check(logbuf.String(), Matches, "(?ms).*queuing for later.*\n")

	files, err := ioutil.ReadDir(dirs.SnapAssertsSpoolDir)
	c.Assert(err, IsNil)
	c.Check(files, HasLen, 1)
	c.Check(files[0].Name(), Equals, "iOkaeet50rajLvL-0Qsf2ELrTdn3XIXRIBlDewcK02zwRi3_TJlUOTl9AaiDXmDn.assert")
}

func (s *SnapSuite) TestAutoImportFromSpoolHappy(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	fakeAssertData := []byte("my-assertion")

	n := 0
	total := 2
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/assertions")
			postData, err := ioutil.ReadAll(r.Body)
			c.Assert(err, IsNil)
			c.Check(postData, DeepEquals, fakeAssertData)
			fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done"}}`)
			n++
		case 1:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/create-user")
			postData, err := ioutil.ReadAll(r.Body)
			c.Assert(err, IsNil)
			c.Check(string(postData), Equals, `{"sudoer":true,"known":true}`)

			fmt.Fprintln(w, `{"type": "sync", "result": [{"username": "foo"}]}`)
			n++
		default:
			c.Fatalf("unexpected request: %v (expected %d got %d)", r, total, n)
		}

	})

	fakeAssertsFn := filepath.Join(dirs.SnapAssertsSpoolDir, "1234343")
	err := os.MkdirAll(filepath.Dir(fakeAssertsFn), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(fakeAssertsFn, fakeAssertData, 0644)
	c.Assert(err, IsNil)

	logbuf, restore := logger.MockLogger()
	defer restore()

	rest, err := snap.Parser().ParseArgs([]string{"auto-import"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Equals, `created user "foo"`+"\n")
	// matches because we may get a:
	//   "WARNING: cannot create syslog logger\n"
	// in the output
	c.Check(logbuf.String(), Matches, fmt.Sprintf("(?ms).*imported %s\n", fakeAssertsFn))
	c.Check(n, Equals, total)

	c.Check(osutil.FileExists(fakeAssertsFn), Equals, false)
}

func (s *SnapSuite) TestAutoImportIntoSpoolUnhappyTooBig(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	_, restoreLogger := logger.MockLogger()
	defer restoreLogger()

	// fake data is bigger than the default assertion limit
	fakeAssertData := make([]byte, 641*1024)

	// ensure we can not connect
	snap.ClientConfig.BaseURL = "can-not-connect-to-this-url"

	fakeAssertsFn := filepath.Join(c.MkDir(), "auto-import.assert")
	err := ioutil.WriteFile(fakeAssertsFn, fakeAssertData, 0644)
	c.Assert(err, IsNil)

	mockMountInfoFmt := `
24 0 8:18 / %s rw,relatime shared:1 - squashfs /dev/sc1 rw,errors=remount-ro,data=ordered`
	content := fmt.Sprintf(mockMountInfoFmt, filepath.Dir(fakeAssertsFn))
	restore = snap.MockMountInfoPath(makeMockMountInfo(c, content))
	defer restore()

	_, err = snap.Parser().ParseArgs([]string{"auto-import"})
	c.Assert(err, ErrorMatches, "cannot queue .*, file size too big: 656384")
}
