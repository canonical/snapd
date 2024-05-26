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
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil"
)

func generateRSAKey(keyLength int) (*rsa.PrivateKey, error) {
	// The temporary directory is created with mode
	// 0700 by os.MkdirTemp, see:
	//   https://github.com/golang/go/blob/3b29222ffdcaea70842ed167632468f54a1783ae/src/os/tempfile.go#L98
	tempDir := mylog.Check2(os.MkdirTemp(os.TempDir(), "snapd"))

	defer os.RemoveAll(tempDir)

	rsaKeyFile := filepath.Join(tempDir, "rsa.key")

	cmd := exec.Command("ssh-keygen", "-t", "rsa", "-b", strconv.Itoa(keyLength), "-N", "", "-f", rsaKeyFile, "-m", "PEM")
	out := mylog.Check2(cmd.CombinedOutput())

	d := mylog.Check2(os.ReadFile(rsaKeyFile))

	blk, _ := pem.Decode(d)
	if blk == nil {
		return nil, errors.New("cannot decode PEM block")
	}

	key := mylog.Check2(x509.ParsePKCS1PrivateKey(blk.Bytes))
	mylog.Check(key.Validate())
	mylog.Check(os.RemoveAll(tempDir))

	return key, err
}
