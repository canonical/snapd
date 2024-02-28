// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/advisor"
	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/dirs"
)

type sillyFinder struct{}

func mkSillyFinder() (advisor.Finder, error) {
	return &sillyFinder{}, nil
}

func (sf *sillyFinder) FindCommand(command string) ([]advisor.Command, error) {
	switch command {
	case "hello":
		return []advisor.Command{
			{Snap: "hello", Command: "hello"},
			{Snap: "hello-wcm", Command: "hello"},
		}, nil
	case "error-please":
		return nil, fmt.Errorf("get failed")
	default:
		return nil, nil
	}
}

func (sf *sillyFinder) FindPackage(pkgName string) (*advisor.Package, error) {
	switch pkgName {
	case "hello":
		return &advisor.Package{Snap: "hello", Summary: "summary for hello"}, nil
	case "error-please":
		return nil, fmt.Errorf("find-pkg failed")
	default:
		return nil, nil
	}
}

func (*sillyFinder) Close() error { return nil }

func (s *SnapSuite) TestAdviseCommandHappyText(c *C) {
	restore := advisor.ReplaceCommandsFinder(mkSillyFinder)
	defer restore()

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"advise-snap", "--command", "hello"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Assert(s.Stdout(), Equals, `
Command "hello" not found, but can be installed with:

sudo snap install hello
sudo snap install hello-wcm

See 'snap info <snap name>' for additional versions.

`)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestAdviseCommandHappyJSON(c *C) {
	restore := advisor.ReplaceCommandsFinder(mkSillyFinder)
	defer restore()

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"advise-snap", "--command", "--format=json", "hello"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Assert(s.Stdout(), Equals, `[{"Snap":"hello","Command":"hello"},{"Snap":"hello-wcm","Command":"hello"}]`+"\n")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestAdviseCommandDumpDb(c *C) {
	dirs.SetRootDir(c.MkDir())
	c.Assert(os.MkdirAll(dirs.SnapCacheDir, 0755), IsNil)
	defer dirs.SetRootDir("")

	db, err := advisor.Create()
	if errors.Is(err, advisor.ErrNotSupported) {
		c.Skip("bolt is not supported")
	}
	c.Assert(err, IsNil)
	c.Assert(db.AddSnap("foo", "1.0", "foo summary", []string{"foo", "bar"}), IsNil)
	c.Assert(db.Commit(), IsNil)

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"advise-snap", "--dump-db"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Assert(s.Stderr(), Equals, "")
	c.Assert(s.Stdout(), Matches, `bar foo 1.0\nfoo foo 1.0\n`)
}

func (s *SnapSuite) TestAdviseCommandMisspellText(c *C) {
	restore := advisor.ReplaceCommandsFinder(mkSillyFinder)
	defer restore()

	for _, misspelling := range []string{"helo", "0hello", "hell0", "hello0"} {
		err := snap.AdviseCommand(misspelling, "pretty")
		c.Assert(err, IsNil)
		c.Assert(s.Stdout(), Equals, fmt.Sprintf(`
Command "%s" not found, did you mean:

 command "hello" from snap "hello"
 command "hello" from snap "hello-wcm"

See 'snap info <snap name>' for additional versions.

`, misspelling))
		c.Assert(s.Stderr(), Equals, "")

		s.stdout.Reset()
		s.stderr.Reset()
	}
}

func (s *SnapSuite) TestAdviseFromAptIntegrationNoAptPackage(c *C) {
	restore := advisor.ReplaceCommandsFinder(mkSillyFinder)
	defer restore()

	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	c.Assert(err, IsNil)

	os.Setenv("APT_HOOK_SOCKET", strconv.Itoa(int(fds[1])))
	// note we don't close fds[1] ourselves; adviseViaAptHook might, or we might leak it
	// (we don't close it here to avoid accidentally closing an arbitrary file descriptor that reused the number)

	done := make(chan bool, 1)
	go func() {
		f := os.NewFile(uintptr(fds[0]), "advise-sock")
		conn, err := net.FileConn(f)
		c.Assert(err, IsNil)
		defer conn.Close()
		defer f.Close()

		// handshake
		_, err = conn.Write([]byte(`{"jsonrpc":"2.0","method":"org.debian.apt.hooks.hello","id":0,"params":{"versions":["0.1"]}}` + "\n\n"))
		c.Assert(err, IsNil)

		// reply from snap
		r := bufio.NewReader(conn)
		buf, _, err := r.ReadLine()
		c.Assert(err, IsNil)
		c.Assert(string(buf), Equals, `{"jsonrpc":"2.0","id":0,"result":{"version":"0.1"}}`)
		// plus empty line
		buf, _, err = r.ReadLine()
		c.Assert(err, IsNil)
		c.Assert(string(buf), Equals, ``)

		// payload
		_, err = conn.Write([]byte(`{"jsonrpc":"2.0","method":"org.debian.apt.hooks.install.fail","params":{"command":"install","search-terms":["aws-cli"],"unknown-packages":["hello"],"packages":[]}}` + "\n\n"))
		c.Assert(err, IsNil)

		// bye
		_, err = conn.Write([]byte(`{"jsonrpc":"2.0","method":"org.debian.apt.hooks.bye","params":{}}` + "\n\n"))
		c.Assert(err, IsNil)

		done <- true
	}()

	cmd := snap.CmdAdviseSnap()
	cmd.FromApt = true
	err = cmd.Execute(nil)
	c.Assert(err, IsNil)
	c.Assert(s.Stdout(), Equals, `
No apt package "hello", but there is a snap with that name.
Try "snap install hello"

`)
	c.Assert(s.Stderr(), Equals, "")
	c.Assert(<-done, Equals, true)
}

func (s *SnapSuite) TestReadRpc(c *C) {
	rpc := strings.Replace(`
{
    "jsonrpc": "2.0",
    "method": "org.debian.apt.hooks.install.pre-prompt",
    "params": {
        "command": "install",
        "packages": [
            {
                "architecture": "amd64",
                "automatic": false,
                "id": 38033,
                "mode": "install",
                "name": "hello",
                "versions": {
                    "candidate": {
                        "architecture": "amd64",
                        "id": 22712,
                        "pin": 500,
                        "version": "4:17.12.3-1ubuntu1"
                    },
                    "install": {
                        "architecture": "amd64",
                        "id": 22712,
                        "pin": 500,
                        "version": "4:17.12.3-1ubuntu1"
                    }
                }
            },
            {
                "architecture": "amd64",
                "automatic": true,
                "id": 38202,
                "mode": "install",
                "name": "hello-kpart",
                "versions": {
                    "candidate": {
                        "architecture": "amd64",
                        "id": 22713,
                        "pin": 500,
                        "version": "4:17.12.3-1ubuntu1"
                    },
                    "install": {
                        "architecture": "amd64",
                        "id": 22713,
                        "pin": 500,
                        "version": "4:17.12.3-1ubuntu1"
                    }
                }
            }
        ],
        "search-terms": [
            "hello"
        ],
        "unknown-packages": []
    }
}`, "\n", "", -1)
	// all apt rpc ends with \n\n
	rpc = rpc + "\n\n"
	// this can be parsed without errors
	_, err := snap.ReadRpc(bufio.NewReader(bytes.NewBufferString(rpc)))
	c.Assert(err, IsNil)
}
