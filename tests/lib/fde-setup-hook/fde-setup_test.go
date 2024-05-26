// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"bytes"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	fdeHook "github.com/snapcore/snapd/tests/lib/fde-setup-hook"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type fdeSetupSuite struct{}

var _ = Suite(&fdeSetupSuite{})

var (
	// there is a static handle used in the test fde-hook
	b64testKeyHandle = base64.StdEncoding.EncodeToString(fdeHook.TestKeyHandle)

	// the test hook uses simple xor13 encryption
	mockKey          = []byte("encrypted-payload")
	b64Key           = base64.StdEncoding.EncodeToString(mockKey)
	mockEncryptedKey = fdeHook.Xor13(mockKey)
	b64EncryptedKey  = base64.StdEncoding.EncodeToString(mockEncryptedKey)
)

func (r *fdeSetupSuite) TestRunFdeSetup(c *C) {
	fdeSetupResultStdin := filepath.Join(c.MkDir(), "stdin")
	mockedSnapctl := testutil.MockCommand(c, "snapctl", fmt.Sprintf(`
if [ "$1" = "fde-setup-request" ]; then
    echo '{"op":"initial-setup","key":"%s","key-name":"key-name"}'
elif [ "$1" = "fde-setup-result" ]; then
    cat - > "%s"
else
    echo "Unexpected argument $1"
    exit 1
fi
`, b64Key, fdeSetupResultStdin))
	defer mockedSnapctl.Restore()
	mylog.Check(fdeHook.RunFdeSetup())

	c.Check(fdeSetupResultStdin, testutil.FileEquals, fmt.Sprintf(`{"sealed-key":"%s","handle":"%s"}`, b64EncryptedKey, b64testKeyHandle))
}

func (r *fdeSetupSuite) TestRunFdeRevealKey(c *C) {
	// strings are base64 encoded
	mockedStdin := bytes.NewBufferString(fmt.Sprintf(`{"op":"reveal","handle":"%s","sealed-key":"%s"}`, b64testKeyHandle, b64EncryptedKey))
	mockedStdout := bytes.NewBuffer(nil)
	restore := fdeHook.MockStdinStdout(mockedStdin, mockedStdout)
	defer restore()
	mylog.Check(fdeHook.RunFdeRevealKey())

	c.Check(mockedStdout.String(), Equals, fmt.Sprintf(`{"key":"%s"}`, b64Key)+"\n")
}
