// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	main "github.com/snapcore/snapd/cmd/snap-fde-keymgr"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/testutil"
)

type mainSuite struct{}

var _ = Suite(&mainSuite{})

func TestT(t *testing.T) {
	TestingT(t)
}

func (s *mainSuite) TestAddKey(c *C) {
	d := c.MkDir()
	dev := ""
	rkey := keys.RecoveryKey{}
	addCalls := 0
	restore := main.MockAddRecoveryKeyToLUKS(func(recoveryKey keys.RecoveryKey, luksDev string) error {
		addCalls++
		dev = luksDev
		rkey = recoveryKey
		// recovery key is already written to a file
		c.Assert(filepath.Join(d, "recovery.key"), testutil.FileEquals, rkey[:])
		return nil
	})
	defer restore()
	devUsingKey := ""
	addUsingKeyCalls := 0
	var authzKey keys.EncryptionKey
	restore = main.MockAddRecoveryKeyToLUKSUsingKey(func(recoveryKey keys.RecoveryKey, key keys.EncryptionKey, luksDev string) error {
		addUsingKeyCalls++
		devUsingKey = luksDev
		authzKey = key
		// recovery key is already written to a file
		c.Assert(filepath.Join(d, "recovery.key"), testutil.FileEquals, rkey[:])
		return nil
	})
	defer restore()
	c.Assert(os.WriteFile(filepath.Join(d, "authz.key"), []byte{1, 1, 1}, 0644), IsNil)
	mylog.Check(main.Run([]string{
		"add-recovery-key",
		"--devices", "/dev/vda4",
		"--authorizations", "keyring",
		"--devices", "/dev/vda5",
		"--authorizations", "file:" + filepath.Join(d, "authz.key"),
		"--key-file", filepath.Join(d, "recovery.key"),
	}))

	c.Check(addCalls, Equals, 1)
	c.Check(dev, Equals, "/dev/vda4")
	c.Check(addUsingKeyCalls, Equals, 1)
	c.Check(devUsingKey, Equals, "/dev/vda5")
	c.Check(rkey, Not(DeepEquals), keys.RecoveryKey{})
	c.Assert(filepath.Join(d, "recovery.key"), testutil.FileEquals, rkey[:])

	oldKey := rkey
	mylog.
		// add again, in which case already existing key is read back
		Check(main.Run([]string{
			"add-recovery-key",
			"--devices", "/dev/vda4",
			"--authorizations", "keyring",
			"--devices", "/dev/vda5",
			"--authorizations", "file:" + filepath.Join(d, "authz.key"),
			"--key-file", filepath.Join(d, "recovery.key"),
		}))

	c.Check(addCalls, Equals, 2)
	c.Check(dev, Equals, "/dev/vda4")
	c.Check(addUsingKeyCalls, Equals, 2)
	c.Check(devUsingKey, Equals, "/dev/vda5")
	c.Assert(authzKey, DeepEquals, keys.EncryptionKey([]byte{1, 1, 1}))
	c.Check(rkey, DeepEquals, oldKey)
	// file was overwritten
	c.Assert(filepath.Join(d, "recovery.key"), testutil.FileEquals, rkey[:])
}

func (s *mainSuite) TestAddKeyRequiresAuthz(c *C) {
	restore := main.MockAddRecoveryKeyToLUKS(func(recoveryKey keys.RecoveryKey, luksDev string) error {
		c.Fail()
		return fmt.Errorf("unexpected call")
	})
	defer restore()
	restore = main.MockAddRecoveryKeyToLUKSUsingKey(func(recoveryKey keys.RecoveryKey, key keys.EncryptionKey, luksDev string) error {
		c.Fail()
		return fmt.Errorf("unexpected call")
	})
	defer restore()
	d := c.MkDir()
	mylog.Check(main.Run([]string{
		"add-recovery-key",
		"--devices", "/dev/vda4",
		"--authorizations", "keyring",
		"--devices", "/dev/vda5",
		"--key-file", filepath.Join(d, "recovery.key"),
	}))
	c.Assert(err, ErrorMatches, "cannot add recovery keys: mismatch in the number of devices and authorizations")
	mylog.

		// --authorization=invalid
		Check(main.Run([]string{
			"add-recovery-key",
			"--devices", "/dev/vda4",
			"--authorizations", "invalid",
			"--devices", "/dev/vda5",
			"--authorizations", "file:" + filepath.Join(d, "authz.key"),
			"--key-file", filepath.Join(d, "recovery.key"),
		}))
	c.Assert(err, ErrorMatches, `cannot add recovery keys with invalid authorizations: unknown authorization method "invalid"`)
	mylog.

		// authorization key file does not exist
		Check(main.Run([]string{
			"add-recovery-key",
			"--devices", "/dev/vda4",
			"--authorizations", "keyring",
			"--devices", "/dev/vda5",
			"--authorizations", "file:" + filepath.Join(d, "authz.key"),
			"--key-file", filepath.Join(d, "recovery.key"),
		}))
	c.Assert(err, ErrorMatches, `cannot add recovery keys with invalid authorizations: authorization file .*/authz.key does not exist`)
}

type addKeyTestCase struct {
	errAddToLUKS         error
	addCalls             int
	errAddToLUKSUsingKey error
	addUsingKeyCalls     int
	expErr               string
}

func (s *mainSuite) testAddKeyIdempotent(c *C, tc addKeyTestCase) {
	d := c.MkDir()
	c.Assert(os.WriteFile(filepath.Join(d, "authz.key"), []byte{1, 1, 1}, 0644), IsNil)
	rkey := keys.RecoveryKey{'r', 'e', 'c', 'o', 'v', 'e', 'r', 'y'}
	c.Assert(os.WriteFile(filepath.Join(d, "recovery.key"), rkey[:], 0600), IsNil)

	addCalls := 0
	restore := main.MockAddRecoveryKeyToLUKS(func(recoveryKey keys.RecoveryKey, luksDev string) error {
		addCalls++
		c.Check(luksDev, Equals, "/dev/vda4")
		c.Check(recoveryKey, DeepEquals, rkey)
		return tc.errAddToLUKS
	})
	defer restore()
	addUsingKeyCalls := 0
	restore = main.MockAddRecoveryKeyToLUKSUsingKey(func(recoveryKey keys.RecoveryKey, key keys.EncryptionKey, luksDev string) error {
		addUsingKeyCalls++
		c.Check(luksDev, Equals, "/dev/vda5")
		c.Check(recoveryKey, DeepEquals, rkey)
		return tc.errAddToLUKSUsingKey
	})
	defer restore()
	mylog.Check(main.Run([]string{
		"add-recovery-key",
		"--devices", "/dev/vda4",
		"--authorizations", "keyring",
		"--devices", "/dev/vda5",
		"--authorizations", "file:" + filepath.Join(d, "authz.key"),
		"--key-file", filepath.Join(d, "recovery.key"),
	}))
	if tc.expErr != "" {
		c.Assert(err, ErrorMatches, tc.expErr)
	} else {

	}
	c.Check(addCalls, Equals, tc.addCalls)
	c.Check(addUsingKeyCalls, Equals, tc.addUsingKeyCalls)
	// file was not overwritten
	c.Assert(filepath.Join(d, "recovery.key"), testutil.FileEquals, rkey[:])
}

func (s *mainSuite) TestAddKeyIdempotentBothEmpty(c *C) {
	s.testAddKeyIdempotent(c, addKeyTestCase{
		addCalls:         1,
		addUsingKeyCalls: 1,
	})
}

func (s *mainSuite) TestAddKeyIdempotentOneErr(c *C) {
	s.testAddKeyIdempotent(c, addKeyTestCase{
		addCalls:     1,
		errAddToLUKS: errors.New("mock error"),
		expErr:       "cannot add recovery key to LUKS device: mock error",
	})
}

func (s *mainSuite) TestAddKeyIdempotentOtherErr(c *C) {
	s.testAddKeyIdempotent(c, addKeyTestCase{
		addCalls:             1,
		addUsingKeyCalls:     1,
		errAddToLUKSUsingKey: errors.New("mock error"),
		expErr:               "cannot add recovery key to LUKS device using authorization key: mock error",
	})
}

func (s *mainSuite) TestAddKeyIdempotentBothPresent(c *C) {
	s.testAddKeyIdempotent(c, addKeyTestCase{
		addCalls:             1,
		addUsingKeyCalls:     1,
		errAddToLUKS:         errors.New("mock error: cryptsetup failed with: Key slot 1 is full, please select another one."),
		errAddToLUKSUsingKey: errors.New("mock error: cryptsetup failed with: Key slot 1 is full, please select another one."),
	})
}

func (s *mainSuite) TestAddKeyIdempotentOnePresent(c *C) {
	s.testAddKeyIdempotent(c, addKeyTestCase{
		addCalls:         1,
		addUsingKeyCalls: 1,
		errAddToLUKS:     errors.New("mock error: cryptsetup failed with: Key slot 1 is full, please select another one."),
	})
}

func (s *mainSuite) TestRemoveKey(c *C) {
	dev := ""
	removeCalls := 0
	restore := main.MockRemoveRecoveryKeyFromLUKS(func(luksDev string) error {
		removeCalls++
		dev = luksDev
		return nil
	})
	defer restore()
	removeUsingKeyCalls := 0
	devUsingKey := ""
	var authzKey keys.EncryptionKey
	restore = main.MockRemoveRecoveryKeyFromLUKSUsingKey(func(key keys.EncryptionKey, luksDev string) error {
		authzKey = key
		removeUsingKeyCalls++
		devUsingKey = luksDev
		return nil
	})
	defer restore()
	d := c.MkDir()
	// key which will be removed
	c.Assert(os.WriteFile(filepath.Join(d, "recovery.key"), []byte{0, 0, 0}, 0644), IsNil)

	c.Assert(os.WriteFile(filepath.Join(d, "authz.key"), []byte{1, 1, 1}, 0644), IsNil)
	mylog.Check(main.Run([]string{
		"remove-recovery-key",
		"--devices", "/dev/vda4",
		"--authorizations", "keyring",
		"--devices", "/dev/vda5",
		"--authorizations", "file:" + filepath.Join(d, "authz.key"),
		"--key-files", filepath.Join(d, "recovery.key"),
	}))

	c.Check(removeCalls, Equals, 1)
	c.Check(dev, Equals, "/dev/vda4")
	c.Check(removeUsingKeyCalls, Equals, 1)
	c.Check(devUsingKey, Equals, "/dev/vda5")
	c.Assert(authzKey, DeepEquals, keys.EncryptionKey([]byte{1, 1, 1}))
	c.Assert(filepath.Join(d, "recovery.key"), testutil.FileAbsent)
	mylog.
		// again when the recover key file is gone already
		Check(main.Run([]string{
			"remove-recovery-key",
			"--devices", "/dev/vda4",
			"--authorizations", "keyring",
			"--devices", "/dev/vda5",
			"--authorizations", "file:" + filepath.Join(d, "authz.key"),
			"--key-files", filepath.Join(d, "recovery.key"),
		}))
	c.Check(removeCalls, Equals, 2)
	c.Check(removeUsingKeyCalls, Equals, 2)

}

func (s *mainSuite) TestRemoveKeyRequiresAuthz(c *C) {
	restore := main.MockRemoveRecoveryKeyFromLUKS(func(luksDev string) error {
		c.Fail()
		return fmt.Errorf("unexpected call")
	})
	defer restore()
	restore = main.MockRemoveRecoveryKeyFromLUKSUsingKey(func(key keys.EncryptionKey, luksDev string) error {
		c.Fail()
		return fmt.Errorf("unexpected call")
	})
	defer restore()
	d := c.MkDir()
	mylog.Check(main.Run([]string{
		"remove-recovery-key",
		"--devices", "/dev/vda4",
		"--authorizations", "keyring",
		"--devices", "/dev/vda5",
		"--key-files", filepath.Join(d, "recovery.key"),
	}))
	c.Assert(err, ErrorMatches, "cannot remove recovery keys: mismatch in the number of devices and authorizations")
	mylog.

		// --authorization=invalid
		Check(main.Run([]string{
			"remove-recovery-key",
			"--devices", "/dev/vda4",
			"--authorizations", "invalid",
			"--devices", "/dev/vda5",
			"--authorizations", "file:" + filepath.Join(d, "authz.key"),
			"--key-files", filepath.Join(d, "recovery.key"),
		}))
	c.Assert(err, ErrorMatches, `cannot remove recovery keys with invalid authorizations: unknown authorization method "invalid"`)
	mylog.

		// authorization key file does not exist
		Check(main.Run([]string{
			"remove-recovery-key",
			"--devices", "/dev/vda4",
			"--authorizations", "keyring",
			"--devices", "/dev/vda5",
			"--authorizations", "file:" + filepath.Join(d, "authz.key"),
			"--key-files", filepath.Join(d, "recovery.key"),
		}))
	c.Assert(err, ErrorMatches, `cannot remove recovery keys with invalid authorizations: authorization file .*/authz.key does not exist`)
}

// 1 in ASCII repeated 32 times
const all1sKey = `{"key":"MTExMTExMTExMTExMTExMTExMTExMTExMTExMTExMTE="}`

func (s *mainSuite) TestChangeEncryptionKey(c *C) {
	b := bytes.NewBufferString(all1sKey)
	restore := main.MockOsStdin(b)
	defer restore()
	unexpectedCall := func(newKey keys.EncryptionKey, luksDev string) error {
		c.Errorf("unexpected call")
		return fmt.Errorf("unexpected call")
	}
	defer main.MockStageLUKSEncryptionKeyChange(unexpectedCall)
	defer main.MockTransitionLUKSEncryptionKeyChange(unexpectedCall)
	mylog.Check(main.Run([]string{
		"change-encryption-key",
		"--device", "/dev/vda4",
	}))
	c.Assert(err, ErrorMatches, "cannot change encryption key without stage or transition request")
	mylog.Check(main.Run([]string{
		"change-encryption-key",
		"--device", "/dev/vda4",
		"--stage", "--transition",
	}))
	c.Assert(err, ErrorMatches, "cannot both stage and transition the encryption key change")
}

func (s *mainSuite) TestStageEncryptionKey(c *C) {
	b := bytes.NewBufferString(all1sKey)
	restore := main.MockOsStdin(b)
	defer restore()
	dev := ""
	stageCalls := 0
	var key []byte
	var stageErr error
	restore = main.MockStageLUKSEncryptionKeyChange(func(newKey keys.EncryptionKey, luksDev string) error {
		stageCalls++
		dev = luksDev
		key = newKey
		return stageErr
	})
	defer restore()
	restore = main.MockTransitionLUKSEncryptionKeyChange(func(newKey keys.EncryptionKey, luksDev string) error {
		c.Errorf("unexpected call")
		return fmt.Errorf("unexpected call")
	})
	defer restore()
	mylog.Check(main.Run([]string{
		"change-encryption-key",
		"--device", "/dev/vda4",
		"--stage",
	}))

	c.Check(stageCalls, Equals, 1)
	c.Check(dev, Equals, "/dev/vda4")
	// secboot encryption key size
	c.Check(key, DeepEquals, bytes.Repeat([]byte("1"), 32))

	restore = main.MockOsStdin(bytes.NewBufferString(all1sKey))
	defer restore()
	stageErr = fmt.Errorf("mock stage error")
	mylog.Check(main.Run([]string{
		"change-encryption-key",
		"--device", "/dev/vda4",
		"--stage",
	}))
	c.Assert(err, ErrorMatches, "cannot stage LUKS device encryption key change: mock stage error")
}

func (s *mainSuite) TestTransitionEncryptionKey(c *C) {
	b := bytes.NewBufferString(all1sKey)
	restore := main.MockOsStdin(b)
	defer restore()
	dev := ""
	transitionCalls := 0
	var key []byte
	var transitionErr error
	restore = main.MockStageLUKSEncryptionKeyChange(func(newKey keys.EncryptionKey, luksDev string) error {
		c.Errorf("unexpected call")
		return fmt.Errorf("unexpected call")
	})
	defer restore()
	restore = main.MockTransitionLUKSEncryptionKeyChange(func(newKey keys.EncryptionKey, luksDev string) error {
		transitionCalls++
		dev = luksDev
		key = newKey
		return transitionErr
	})
	defer restore()
	defer restore()
	mylog.Check(main.Run([]string{
		"change-encryption-key",
		"--device", "/dev/vda4",
		"--transition",
	}))

	c.Check(transitionCalls, Equals, 1)
	c.Check(dev, Equals, "/dev/vda4")
	// secboot encryption key size
	c.Check(key, DeepEquals, bytes.Repeat([]byte("1"), 32))

	restore = main.MockOsStdin(bytes.NewBufferString(all1sKey))
	defer restore()
	transitionErr = fmt.Errorf("mock transition error")
	mylog.Check(main.Run([]string{
		"change-encryption-key",
		"--device", "/dev/vda4",
		"--transition",
	}))
	c.Assert(err, ErrorMatches, "cannot transition LUKS device encryption key change: mock transition error")
}
