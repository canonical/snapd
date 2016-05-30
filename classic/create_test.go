// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package classic

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

type CreateTestSuite struct {
	testutil.BaseTest

	imageReader    io.Reader
	runInChroot    [][]string
	getgrnamCalled []string
}

var _ = Suite(&CreateTestSuite{})

func (t *CreateTestSuite) SetUpTest(c *C) {
	t.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())

	// mock the chroot handler
	origRunInChroot := runInChroot
	t.AddCleanup(func() { runInChroot = origRunInChroot })
	runInChroot = func(chroot string, cmd ...string) error {
		t.runInChroot = append(t.runInChroot, cmd)
		return nil
	}

	// create some content for the webserver
	r := makeMockLxdTarball(c)
	t.AddCleanup(func() { r.Close() })
	t.imageReader = r

	// ensure getgrnam is called
	getgrnamOrig := getgrnam
	getgrnam = func(name string) (osutil.Group, error) {
		t.getgrnamCalled = append(t.getgrnamCalled, name)
		return osutil.Group{}, nil
	}
	t.AddCleanup(func() { getgrnam = getgrnamOrig })
}

func makeMockLxdIndexSystem() string {
	arch := arch.UbuntuArchitecture()

	s := fmt.Sprintf(`
ubuntu;xenial;otherarch;default;20151126_03:49;/images/ubuntu/xenial/armhf/default/20151126_03:49/
ubuntu;%s;%s;default;20151126_03:49;/images/ubuntu/CODENAME/ARCH/default/20151126_03:49/
`, release.ReleaseInfo.Codename, arch)

	return s
}

func makeMockRoot(c *C) string {
	mockRoot := c.MkDir()
	for _, d := range []string{"/etc", "/run", "/usr/sbin"} {
		err := os.MkdirAll(filepath.Join(mockRoot, d), 0755)
		c.Assert(err, IsNil)
	}
	for _, d := range []string{"/etc/nsswitch.conf", "/etc/apt/sources.list"} {
		dst := filepath.Join(mockRoot, d)
		err := os.MkdirAll(filepath.Dir(dst), 0755)
		c.Assert(err, IsNil)
		err = ioutil.WriteFile(dst, nil, 0644)
		c.Assert(err, IsNil)
	}
	resolvconf := filepath.Join(dirs.GlobalRootDir, "/run/resolvconf/resolv.conf")
	err := os.MkdirAll(filepath.Dir(resolvconf), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(resolvconf, nil, 0644)
	c.Assert(err, IsNil)

	return mockRoot
}

func makeMockLxdTarball(c *C) io.ReadCloser {
	mockRoot := makeMockRoot(c)

	tar := filepath.Join(c.MkDir(), "foo.tar")
	cmd := exec.Command("tar", "-C", mockRoot, "-cf", tar, ".")
	err := cmd.Run()
	c.Assert(err, IsNil)

	f, err := os.Open(tar)
	c.Assert(err, IsNil)
	return f
}

func (t *CreateTestSuite) makeMockLxdServer(c *C) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.RequestURI {
		case "/meta/1.0/index-system":
			s := makeMockLxdIndexSystem()
			fmt.Fprintf(w, s)
		case "/images/ubuntu/CODENAME/ARCH/default/20151126_03:49/rootfs.tar.xz":
			io.Copy(w, t.imageReader)
		default:
			http.NotFound(w, r)
		}
	}))
	t.AddCleanup(func() { ts.Close() })

	origLxdBaseURL := lxdBaseURL
	lxdBaseURL = ts.URL
	t.AddCleanup(func() { lxdBaseURL = origLxdBaseURL })
}

func (t *CreateTestSuite) TestDownloadFileFailsCorrectly(c *C) {
	t.makeMockLxdServer(c)

	failURL := lxdBaseURL + "/not-exists"
	_, err := downloadFile(failURL, nil)
	c.Assert(err, ErrorMatches, fmt.Sprintf("failed to download %s: 404", failURL))
}

func (t *CreateTestSuite) TestCreate(c *C) {
	t.makeMockLxdServer(c)

	err := Create(&progress.NullProgress{})
	c.Assert(err, IsNil)
	c.Assert(t.runInChroot, DeepEquals, [][]string{
		{"deluser", "ubuntu"},
		{"apt-get", "install", "-y", "libnss-extrausers"},
	})
	c.Assert(t.getgrnamCalled, DeepEquals, []string{"sudo"})
	for _, canary := range []string{"/etc/nsswitch.conf", "/etc/hosts", "/usr/sbin/policy-rc.d"} {
		c.Assert(osutil.FileExists(filepath.Join(dirs.ClassicDir, canary)), Equals, true)
	}
	leftovers, err := filepath.Glob(filepath.Join(os.TempDir(), "classic*"))
	c.Assert(err, IsNil)
	c.Assert(leftovers, HasLen, 0)
}

func (t *CreateTestSuite) TestCreateFailDestroys(c *C) {
	t.makeMockLxdServer(c)
	t.imageReader = strings.NewReader("its all broken")

	err := Create(&progress.NullProgress{})
	c.Assert(err, ErrorMatches, `(?m)failed to unpack .*`)
	c.Assert(osutil.FileExists(dirs.ClassicDir), Equals, false)
}
