// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package devicestate

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/snapcore/snapd/osutil"
)

func generateRSAKey(keyLength int) (*rsa.PrivateKey, error) {
	// The temporary directory is created with mode
	// 0700 by ioutil.TempDir, see:
	//   https://github.com/golang/go/blob/master/src/io/ioutil/tempfile.go#L84
	tempDir, err := ioutil.TempDir(os.TempDir(), "snapd")
	if err != nil {
		return nil, err
	}

	defer os.RemoveAll(tempDir)

	rsaKeyFile := filepath.Join(tempDir, "rsa.key")

	cmd := exec.Command("ssh-keygen", "-t", "rsa", "-b", strconv.Itoa(keyLength), "-N", "", "-f", rsaKeyFile, "-m", "PEM")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, osutil.OutputErr(out, err)
	}

	d, err := ioutil.ReadFile(rsaKeyFile)
	if err != nil {
		return nil, err
	}

	blk, _ := pem.Decode(d)
	if blk == nil {
		return nil, errors.New("cannot decode PEM block")
	}

	key, err := x509.ParsePKCS1PrivateKey(blk.Bytes)
	if err != nil {
		return nil, err
	}

	err = key.Validate()
	if err != nil {
		return nil, err
	}

	err = os.RemoveAll(tempDir)
	if err != nil {
		return nil, err
	}

	return key, err
}
