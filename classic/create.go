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
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ubuntu-core/snappy/arch"
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/release"
)

var (
	lxdBaseURL   = "https://images.linuxcontainers.org"
	lxdIndexPath = "/meta/1.0/index-system"
)

// will be overriden by tests
var getgrnam = osutil.Getgrnam

func findDownloadPathFromLxdIndex(r io.Reader) (string, error) {
	arch := arch.UbuntuArchitecture()
	lsb, err := release.ReadLsb()
	if err != nil {
		return "", err
	}
	release := lsb.Codename

	needle := fmt.Sprintf("ubuntu;%s;%s;default;", release, arch)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), needle) {
			l := strings.Split(scanner.Text(), ";")
			if len(l) < 6 {
				return "", fmt.Errorf("cannot find download path in %s", scanner.Text())
			}
			return l[5], nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error while reading the index-system data: %s", err)
	}

	return "", fmt.Errorf("needle %q not found", needle)
}

func findDownloadURL() (string, error) {
	resp, err := http.Get(lxdBaseURL + lxdIndexPath)
	if err != nil {
		return "", fmt.Errorf("failed to downlaod lxdIndexUrl: %v", err)
	}
	defer resp.Body.Close()

	dlPath, err := findDownloadPathFromLxdIndex(resp.Body)
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("%s%s", lxdBaseURL, filepath.Join(dlPath, "rootfs.tar.xz"))

	return url, nil
}

func downloadFile(url string, pbar progress.Meter) (fn string, err error) {
	name := "classic"

	w, err := ioutil.TempFile("", name)
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			os.Remove(w.Name())
		}
	}()
	defer w.Close()

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("failed to download %s: %v", url, resp.StatusCode)
	}

	if pbar != nil {
		pbar.Start(name, float64(resp.ContentLength))
		mw := io.MultiWriter(w, pbar)
		_, err = io.Copy(mw, resp.Body)
		pbar.Finished()
	} else {
		_, err = io.Copy(w, resp.Body)
	}

	return w.Name(), err
}

// policyRc contains a custom policy-rc.d script that we drop into the
// classic chroot. It will prevent all daemons installed via apt/dpkg
// from starting.
//
// The format is specified in
// https://people.debian.org/~hmh/invokerc.d-policyrc.d-specification.txt
var policyRc = []byte(`#!/bin/sh
while true; do
    case "$1" in
      -*) shift ;;
      makedev) exit 0;;
      x11-common) exit 0;;
      *) exit 101;;
    esac
done
`)

var runInChroot = func(chroot string, cmd ...string) error {
	cmdArgs := []string{"chroot", chroot}
	cmdArgs = append(cmdArgs, cmd...)

	if output, err := exec.Command(cmdArgs[0], cmdArgs[1:]...).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to run %q in chroot %q: %s (%q)", cmd, chroot, err, output)
	}

	return nil
}

func downloadLxdRootfs(pbar progress.Meter) (string, error) {
	url, err := findDownloadURL()
	if err != nil {
		return "", err
	}

	fname, err := downloadFile(url, pbar)
	if err != nil {
		return "", err
	}

	return fname, nil
}

func unpackLxdRootfs(fname string) error {
	if err := os.MkdirAll(dirs.ClassicDir, 0755); err != nil {
		return fmt.Errorf("failed to create classic mode dir: %s", err)
	}

	cmd := exec.Command("tar", "-C", dirs.ClassicDir, "-xpf", fname)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to unpack %s: %s (%s)", fname, err, output)
	}

	return nil
}

func customizeClassicChroot() error {
	// create the snappy mountpoint
	if err := os.MkdirAll(filepath.Join(dirs.ClassicDir, "snappy"), 0755); err != nil {
		return fmt.Errorf("failed to create snappy mount point: %s", err)
	}

	// copy configs
	for _, f := range []string{"hostname", "hosts", "timezone", "localtime"} {
		src := filepath.Join("/etc/", f)
		dst := filepath.Join(dirs.ClassicDir, "etc", f)
		if err := osutil.CopyFile(src, dst, osutil.CopyFlagPreserveAll); err != nil {
			return err
		}
	}

	// ensure daemons do not start
	if err := ioutil.WriteFile(filepath.Join(dirs.ClassicDir, "/usr/sbin/policy-rc.d"), policyRc, 0755); err != nil {
		return fmt.Errorf("failed to write policy-rc.d: %s", err)
	}

	// remove ubuntu user, will come from snappy OS
	if err := runInChroot(dirs.ClassicDir, "deluser", "ubuntu"); err != nil {
		return err
	}

	// install extra packages; make sure chroot can resolve DNS
	resolveDir := filepath.Join(dirs.ClassicDir, "/run/resolvconf/")
	if err := os.MkdirAll(resolveDir, 0755); err != nil {
		return fmt.Errorf("failed to create %s: %s", resolveDir, err)
	}
	src := filepath.Join(dirs.GlobalRootDir, "/run/resolvconf/resolv.conf")
	dst := filepath.Join(dirs.ClassicDir, "/run/resolvconf/")
	if err := osutil.CopyFile(src, dst, osutil.CopyFlagPreserveAll); err != nil {
		return err
	}

	// enable libnss-extrausers
	if err := runInChroot(dirs.ClassicDir, "apt-get", "install", "-y", "libnss-extrausers"); err != nil {
		return err
	}
	// this regexp adds "extrausers" after the passwd/group/shadow
	// lines in /etc/nsswitch.conf
	cmd := exec.Command("sed", "-i", "-r", "/^(passwd|group|shadow):/ s/$/ extrausers/", filepath.Join(dirs.ClassicDir, "/etc/nsswitch.conf"))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to enable libness-extrausers: %s", output)
	}

	// clean up cruft (bad lxd rootfs!)
	if output, err := exec.Command("sh", "-c", fmt.Sprintf("rm -rf %s/run/*", dirs.ClassicDir)).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to cleanup classic /run dir: %s (%s)", err, output)
	}

	// Add hosts "sudo" group into the classic env
	grp, err := getgrnam("sudo")
	if err != nil {
		return fmt.Errorf("failed to get group info for the 'sudo' group: %s", err)
	}
	for _, sudoUser := range grp.Mem {
		// We need to use "runInClassicEnv" so that we get the
		// bind mount of the /var/lib/extrausers directory.
		// Without that the "SUDO_USER" will not exist in the chroot
		if err := runInClassicEnv("usermod", "-a", "-G", "sudo", sudoUser); err != nil {
			if err := unmountBindMounts(); err != nil {
				// we can not return an error here if we
				// still have bind mounts in place, the
				// writable dir may still have /home mounted
				// so we can not remove /writable/classic
				// again
				panic("cannot undo bind mounts")
			}
			return fmt.Errorf("failed to add %s to the sudo users: %s", sudoUser, err)
		}
	}

	return nil
}

// Create creates a new classic shell envirionment
func Create(pbar progress.Meter) (err error) {
	if Enabled() {
		return fmt.Errorf("clasic mode already created in %s", dirs.ClassicDir)
	}

	// ensure we kill the classic env if there is any error
	defer func() {
		if err != nil {
			os.RemoveAll(dirs.ClassicDir)
		}
	}()

	lxdRootfsTar, err := downloadLxdRootfs(pbar)
	if err != nil {
		return err
	}
	defer os.Remove(lxdRootfsTar)

	if err := unpackLxdRootfs(lxdRootfsTar); err != nil {
		return err
	}

	if err := customizeClassicChroot(); err != nil {
		return err
	}

	return nil
}
