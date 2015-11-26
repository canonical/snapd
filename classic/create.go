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

	"github.com/ubuntu-core/snappy/helpers"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/release"
)

var (
	lxdBaseURL   = "https://images.linuxcontainers.org"
	lxdIndexPath = "/meta/1.0/index-system"
)

func findDownloadPathFromLxdIndex(r io.Reader) (string, error) {
	arch := arch.UbuntuArchitecture()
	lsb, err := release.ReadLsb()
	if err != nil {
		return "", err
	}
	release := lsb.Codename

	needle := fmt.Sprintf("ubuntu;%s;%s;default", release, arch)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), needle) {
			l := strings.Split(scanner.Text(), ";")
			if len(l) < 6 {
				return "", fmt.Errorf("can not find download path in %s", scanner.Text())
			}
			return l[5], nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	return "", fmt.Errorf("needle %s not found", needle)
}

func findDownloadURL() (string, error) {
	resp, err := http.Get(lxdBaseURL + lxdIndexPath)
	if err != nil {
		return "", fmt.Errorf("failed to downlaod lxdIndexUrl: %s", err)
	}
	defer resp.Body.Close()

	dlPath, err := findDownloadPathFromLxdIndex(resp.Body)
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("%s%s%s", lxdBaseURL, dlPath, "rootfs.tar.xz")

	return url, nil
}

func downloadFile(url string, pbar progress.Meter) (string, error) {
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

var policyRc = []byte(`
#!/bin/sh
while true; do
    case "\$1" in
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
		return fmt.Errorf("failed to run %s in chroot %s: %s (%s)", cmd, chroot, err, output)
	}

	return nil
}

var newProgress = func() progress.Meter {
	return progress.NewTextProgress()
}

func downloadLxdRootfs() (string, error) {
	url, err := findDownloadURL()
	if err != nil {
		return "", err
	}

	pbar := newProgress()
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
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to unpack %s: %s", fname, err)
	}

	return nil
}

func customizeClassicChroot() error {
	// copy configs
	for _, f := range []string{"hostname", "hosts", "timezone", "localtime"} {
		src := filepath.Join("/etc/", f)
		dst := filepath.Join(dirs.ClassicDir, "etc", f)
		if err := helpers.CopyFile(src, dst, helpers.CopyFlagPreserveAll); err != nil {
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
	src := "/run/resolvconf/resolv.conf"
	dst := filepath.Join(dirs.ClassicDir, "/run/resolvconf/")
	if err := helpers.CopyFile(src, dst, helpers.CopyFlagPreserveAll); err != nil {
		return fmt.Errorf("failed to copy %s to %s", src, dst)
	}

	// enable libnss-extrausers
	if err := runInChroot(dirs.ClassicDir, "apt-get", "install", "-y", "libnss-extrausers"); err != nil {
		return err
	}
	cmd := exec.Command("sed", "-i", "-r", "/^(passwd|group|shadow):/ s/$/ extrausers/", filepath.Join(dirs.ClassicDir, "/etc/nsswitch.conf"))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to enable libness-extrausers: %s", output)
	}

	// clean up cruft (bad lxd rootfs!)
	if output, err := exec.Command("sh", "-c", fmt.Sprintf("rm -rf %s/run/*", dirs.ClassicDir)).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to cleanup classic /run dir: %s (%s)", err, output)
	}

	return nil
}

// Create creates a new classic shell envirionment
func Create() error {
	if Enabled() {
		return fmt.Errorf("clasic mode already created in %s", dirs.ClassicDir)
	}

	lxdRootfsTar, err := downloadLxdRootfs()
	if err != nil {
		return err
	}

	if err := unpackLxdRootfs(lxdRootfsTar); err != nil {
		return err
	}

	if err := customizeClassicChroot(); err != nil {
		return err
	}

	return nil
}
