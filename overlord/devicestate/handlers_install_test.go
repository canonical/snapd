// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package devicestate_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	fdeBackend "github.com/snapcore/snapd/overlord/fdestate/backend"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/testutil"
)

type handlersInstallSuite struct {
	deviceMgrBaseSuite
}

var _ = Suite(&handlersInstallSuite{})

func (s *handlersInstallSuite) SetUpTest(c *C) {
	s.setupBaseTest(c, true)
}

func (s *handlersInstallSuite) TestDoInstallPreseed(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	chroot := filepath.Join(c.MkDir(), "chroot")
	err := os.MkdirAll(chroot, 0755)
	c.Assert(err, IsNil)

	chg, err := devicestate.InstallPreseed(st, "system-label", chroot)
	c.Assert(err, IsNil)

	toolPath, err := snapdtool.InternalToolPath("snap-preseed")
	c.Assert(err, IsNil)
	mock := testutil.MockCommand(c, toolPath, "")
	defer mock.Restore()

	st.Unlock()
	s.se.Ensure()
	s.se.Wait()
	st.Lock()

	c.Check(chg.Err(), IsNil)
	c.Check(chg.Status(), Equals, state.DoneStatus)

	c.Check(mock.Calls(), DeepEquals, [][]string{
		{"snap-preseed", "--hybrid", "--system-label", "system-label", chroot},
	})
}

func (s *handlersInstallSuite) TestDoInstallPreseedFromSnap(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	tmpDir := c.MkDir()
	chroot := filepath.Join(tmpDir, "chroot")
	err := os.MkdirAll(chroot, 0755)
	c.Assert(err, IsNil)

	// mock that we are running from a "snap" located in tmpDir
	snapPath := filepath.Join(tmpDir, "snap/snapd/1234")
	fakeExe := filepath.Join(snapPath, "usr/lib/snapd/snapd")

	err = os.MkdirAll(filepath.Dir(fakeExe), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(fakeExe, nil, 0755)
	c.Assert(err, IsNil)

	restore := snapdtool.MockOsReadlink(func(path string) (string, error) {
		if path != "/proc/self/exe" {
			return "", errors.New("unexpected usage")
		}
		return fakeExe, nil
	})
	defer restore()

	expectedToolPath := filepath.Join(snapPath, "usr/lib/snapd/snap-preseed")
	mock := testutil.MockCommand(c, expectedToolPath, "")
	defer mock.Restore()

	toolPath, err := snapdtool.InternalToolPath("snap-preseed")
	c.Assert(err, IsNil)
	c.Check(toolPath, Equals, expectedToolPath)

	chg, err := devicestate.InstallPreseed(st, "system-label", chroot)
	c.Assert(err, IsNil)

	st.Unlock()
	s.se.Ensure()
	s.se.Wait()
	st.Lock()

	c.Check(chg.Err(), IsNil)
	c.Check(chg.Status(), Equals, state.DoneStatus)

	c.Check(mock.Calls(), DeepEquals, [][]string{
		{"snap-preseed", "--hybrid", "--system-label", "system-label", chroot},
	})
}

type handlersInstallReprovisionSuite struct {
	deviceMgrBaseSuite

	dataKeys *mockContainer
	saveKeys *mockContainer
}

var _ = Suite(&handlersInstallReprovisionSuite{})

func (s *handlersInstallReprovisionSuite) SetUpTest(c *C) {
	const classic = true
	s.setupBaseTest(c, classic)

	s.dataKeys = &mockContainer{
		keys: map[string]*mockKey{
			"default":          {isRecovery: false, key: []byte("old-data")},
			"default-fallback": {isRecovery: false, key: []byte("old-data-fallback")},
			"default-recovery": {isRecovery: true, key: []byte("old-data-recovery")},
		},
	}

	s.saveKeys = &mockContainer{
		keys: map[string]*mockKey{
			"default":          {isRecovery: false, key: []byte("old-save")},
			"default-fallback": {isRecovery: false, key: []byte("old-save-fallback")},
			"default-recovery": {isRecovery: true, key: []byte("old-save-recovery")},
		},
	}

	dataContainer := &mockEncryptedContainer{devPath: "/dev/data", containerRole: "system-data"}
	saveContainer := &mockEncryptedContainer{devPath: "/dev/save", containerRole: "system-save"}
	s.AddCleanup(devicestate.MockFdestateGetEncryptedContainers(func(st *state.State) (containers []fdeBackend.EncryptedContainer, err error) {
		return []fdeBackend.EncryptedContainer{dataContainer, saveContainer}, nil
	}))

	s.AddCleanup(devicestate.MockSecbootListContainerRecoveryKeyNames(func(disk string) ([]string, error) {
		var ret []string
		switch disk {
		case "/dev/data":
			for name, key := range s.dataKeys.keys {
				if key.isRecovery {
					ret = append(ret, name)
				}
			}
		case "/dev/save":
			for name, key := range s.saveKeys.keys {
				if key.isRecovery {
					ret = append(ret, name)
				}
			}
		default:
			c.Errorf("unexpected disk")
			return nil, fmt.Errorf("unexpected disk")
		}
		return ret, nil
	}))

	s.AddCleanup(devicestate.MockSecbootDeleteContainerKey(func(disk string, name string) error {
		switch disk {
		case "/dev/data":
			delete(s.dataKeys.keys, name)
		case "/dev/save":
			delete(s.saveKeys.keys, name)
		default:
			c.Errorf("unexpected disk")
			return fmt.Errorf("unexpected disk")
		}
		return nil
	}))

	s.AddCleanup(devicestate.MockSecbootListContainerUnlockKeyNames(func(disk string) ([]string, error) {
		var ret []string
		switch disk {
		case "/dev/data":
			for name, key := range s.dataKeys.keys {
				if !key.isRecovery {
					ret = append(ret, name)
				}
			}
		case "/dev/save":
			for name, key := range s.saveKeys.keys {
				if !key.isRecovery {
					ret = append(ret, name)
				}
			}
		default:
			c.Errorf("unexpected disk")
			return nil, fmt.Errorf("unexpected disk")
		}
		return ret, nil
	}))

	s.AddCleanup(devicestate.MockSecbootRenameContainerKey(func(disk string, from string, to string) error {
		switch disk {
		case "/dev/data":
			key, hasKey := s.dataKeys.keys[from]
			if !hasKey {
				return fmt.Errorf("key did not exist")
			}
			s.dataKeys.keys[to] = key
			delete(s.dataKeys.keys, from)
		case "/dev/save":
			key, hasKey := s.saveKeys.keys[from]
			if !hasKey {
				return fmt.Errorf("key did not exist")
			}
			s.saveKeys.keys[to] = key
			delete(s.saveKeys.keys, from)
		default:
			c.Errorf("unexpected disk")
			return fmt.Errorf("unexpected disk")
		}
		return nil
	}))

	s.AddCleanup(devicestate.MockSecbootAddBootstrapKeyOnExistingDisk(func(node string, newKey keys.EncryptionKey) error {
		switch node {
		case "/dev/data":
			s.dataKeys.keys["bootstrap-key"] = &mockKey{isRecovery: false, key: []byte("temporary-data-key")}
		case "/dev/save":
			s.saveKeys.keys["bootstrap-key"] = &mockKey{isRecovery: false, key: []byte("temporary-save-key")}
		default:
			c.Errorf("unexpected disk")
			return fmt.Errorf("unexpected disk")
		}
		return nil
	}))

	s.AddCleanup(devicestate.MockSecbootCreateBootstrappedContainer(func(key secboot.DiskUnlockKey, devicePath string) secboot.BootstrappedContainer {
		switch devicePath {
		case "/dev/data":
			return &mockBootstrappedContainer{container: s.dataKeys}
		case "/dev/save":
			return &mockBootstrappedContainer{container: s.saveKeys}
		default:
			c.Errorf("unexpected disk")
			return nil
		}
	}))

	c.Assert(os.MkdirAll(dirs.SnapFDEDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapFDEDir, "ubuntu-save.key"), []byte("save-key"), 0644), IsNil)

}

func (s *handlersInstallReprovisionSuite) setupModel(c *C) {
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "hybrid",
		Serial: "serialserialserial",
	})
	s.makeModelAssertionInState(c, "canonical", "hybrid",
		map[string]any{
			"architecture": "amd64",
			"classic":      "true",
			"distribution": "ubuntu",
			"base":         "core26",
			"snaps": []any{
				map[string]any{
					"name": "pc-kernel",
					"id":   "pckernelidididididididididididid",
					"type": "kernel",
				},
				map[string]any{
					"name": "pc",
					"id":   "pcididididididididididididididid",
					"type": "gadget",
				},
				map[string]any{
					"name": "core26",
					"id":   "core26ididididididididididididid",
					"type": "base",
				},
			},
		})
}

type mockEncryptedContainer struct {
	devPath       string
	containerRole string
}

func (m *mockEncryptedContainer) DevPath() string {
	return m.devPath
}

func (m *mockEncryptedContainer) ContainerRole() string {
	return m.containerRole
}

func (m *mockEncryptedContainer) LegacyKeys() map[string]string {
	return nil
}

type mockKey struct {
	isRecovery bool
	key        []byte
	token      []byte
}

type mockContainer struct {
	keys map[string]*mockKey
}

type mockKeyDataWriter struct {
	container *mockContainer
	slotName  string
	buf       bytes.Buffer
}

func (m *mockKeyDataWriter) Write(p []byte) (n int, err error) {
	return m.buf.Write(p)
}

func (m *mockKeyDataWriter) Commit() error {
	m.container.keys[m.slotName].token = m.buf.Bytes()
	return nil
}

type mockBootstrappedContainer struct {
	container *mockContainer
}

func (m *mockBootstrappedContainer) AddKey(slotName string, newKey []byte) error {
	if _, has := m.container.keys[slotName]; has {
		return fmt.Errorf("already has key")
	}
	m.container.keys[slotName] = &mockKey{isRecovery: false, key: newKey}
	return nil
}

func (m *mockBootstrappedContainer) AddRecoveryKey(slotName string, rkey keys.RecoveryKey) error {
	if _, has := m.container.keys[slotName]; has {
		return fmt.Errorf("already has key")
	}
	m.container.keys[slotName] = &mockKey{isRecovery: true, key: rkey[:]}
	return nil
}

func (m *mockBootstrappedContainer) GetTokenWriter(slotName string) (secboot.KeyDataWriter, error) {
	return &mockKeyDataWriter{container: m.container, slotName: slotName}, nil
}

func (m *mockBootstrappedContainer) RemoveBootstrapKey() error {
	if _, has := m.container.keys["bootstrap-key"]; !has {
		return fmt.Errorf("missing bootstrap key")
	}
	delete(m.container.keys, "bootstrap-key")
	return nil
}

func (m *mockBootstrappedContainer) RegisterKeyAsUsed(primaryKey []byte, unlockKey []byte) {
}

func (s *handlersInstallReprovisionSuite) TestDoReprovisionHappy(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	rkey := keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData(&rkey, preinstallCheckContext))

	chg := st.NewChange("reprovision", "...")
	t := st.NewTask("do-reprovision", "reprovision test")
	chg.AddTask(t)

	defer devicestate.MockSecbootGetPCRHandleFromToken(func(disk string, name string) (uint32, error) {
		switch disk {
		case "/dev/data":
			switch name {
			case "snapd-reprovision-default":
				return 0, nil
			case "snapd-reprovision-default-fallback":
				return 42, nil
			default:
				c.Errorf("unexpected key %s:%s", disk, name)
				return 0, fmt.Errorf("unexpected key")
			}
		case "/dev/save":
			switch name {
			case "snapd-reprovision-default":
				return 42, nil
			case "snapd-reprovision-default-fallback":
				return 42, nil
			default:
				c.Errorf("unexpected key %s:%s", disk, name)
				return 0, fmt.Errorf("unexpected key")
			}
		default:
			c.Errorf("unexpected disk")
			return 0, fmt.Errorf("unexpected disk")
		}
	})()

	defer devicestate.MockSecbootReleasePCRResourceHandle(func(nv uint32) error {
		c.Check(nv, Equals, uint32(42))
		return nil
	})()

	defer devicestate.MockSecbootTestProtectorKey(func(ctx context.Context, disk string, keyName string, key []byte) (bool, error) {
		c.Errorf("unexpected")
		return false, fmt.Errorf("unexpected")
	})()

	preinstallCheckActionsCalls := 0
	defer devicestate.MockSecbootPreinstallCheckAction(func(pcc *secboot.PreinstallCheckContext, ctx context.Context, action *secboot.PreinstallAction) ([]secboot.PreinstallErrorDetails, error) {
		preinstallCheckActionsCalls++
		return []secboot.PreinstallErrorDetails{}, nil
	})()

	saveCheckResultsCalls := 0
	defer devicestate.MockSecbootSaveCheckResult(func(pcc *secboot.PreinstallCheckContext, filename string) error {
		saveCheckResultsCalls++
		return nil
	})()

	defer devicestate.MockSecbootCheckResult(func(pcc *secboot.PreinstallCheckContext) (*secboot.PreinstallCheckResult, error) {
		return &secboot.PreinstallCheckResult{}, nil
	})()

	defer devicestate.MockSnapstateKernelInfo(func(st *state.State, deviceCtx snapstate.DeviceContext) (*snap.Info, error) {
		sideInfo := &snap.SideInfo{RealName: "pc-kernel", Revision: snap.R(1), SnapID: "pc-kernel-id"}
		snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
			Active:   true,
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
			Current:  sideInfo.Revision,
			SnapType: "kernel",
		})
		yml := `
name: pc-kernel
version: 1.0
`
		return snaptest.MockSnap(c, yml, sideInfo), nil
	})()

	defer devicestate.MockKeysNewProtectorKey(func() (keys.ProtectorKey, error) {
		return keys.ProtectorKey([]byte("new-protector")), nil
	})()

	plainKey := &keys.PlainKey{}
	defer devicestate.MockKeysCreateProtectedKey(func(k keys.ProtectorKey, primaryKey []byte) (*keys.PlainKey, []byte, []byte, error) {
		c.Check(primaryKey, IsNil)
		c.Check(k, DeepEquals, keys.ProtectorKey([]byte("new-protector")))
		return plainKey, []byte("new-primary-key"), []byte("new-save-default"), nil
	})()

	defer devicestate.MockKeysPlainKeyWrite(func(key *keys.PlainKey, writer keys.KeyDataWriter) error {
		c.Check(key, Equals, plainKey)
		_, err := writer.Write([]byte("new-save-default-token"))
		c.Assert(err, IsNil)
		err = writer.Commit()
		c.Assert(err, IsNil)
		return nil
	})()

	defer devicestate.MockHookKeyProtectorFactory(func(m *devicestate.DeviceManager, s *snap.Info) (secboot.KeyProtectorFactory, error) {
		return nil, secboot.ErrNoKeyProtector
	})()

	bootMakeRunnableReprovisionCalls := 0
	defer devicestate.MockBootMakeRunnableReprovision(func(model *asserts.Model, protector secboot.KeyProtectorFactory, encryption *boot.EncryptionSetup) error {
		bootMakeRunnableReprovisionCalls++

		// TODO: check primary key

		c.Check(protector, IsNil)

		// Simulate what is expected
		s.dataKeys.keys["default"] = &mockKey{
			isRecovery: false,
			key:        []byte("new-data-default"),
			token:      []byte("new-data-default-token"),
		}
		s.dataKeys.keys["default-fallback"] = &mockKey{
			isRecovery: false,
			key:        []byte("new-data-fallback"),
			token:      []byte("new-data-fallback-token"),
		}
		s.saveKeys.keys["default-fallback"] = &mockKey{
			isRecovery: false,
			key:        []byte("new-save-fallback"),
			token:      []byte("new-save-fallback-token"),
		}
		delete(s.dataKeys.keys, "bootstrap-key")
		delete(s.saveKeys.keys, "bootstrap-key")

		return nil
	})()

	modeenv := &boot.Modeenv{
		Mode: "run",
	}
	c.Assert(modeenv.WriteTo(""), IsNil)

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, IsNil)

	c.Check(preinstallCheckActionsCalls, Equals, 1)
	c.Check(saveCheckResultsCalls, Equals, 1)
	c.Check(bootMakeRunnableReprovisionCalls, Equals, 1)

	c.Check(s.dataKeys.keys, DeepEquals, map[string]*mockKey{
		"default": {
			isRecovery: false,
			key:        []byte("new-data-default"),
			token:      []byte("new-data-default-token"),
		},
		"default-fallback": {
			isRecovery: false,
			key:        []byte("new-data-fallback"),
			token:      []byte("new-data-fallback-token"),
		},
		"default-recovery": {
			isRecovery: true,
			key:        []byte{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4},
		},
	})
	c.Check(s.saveKeys.keys, DeepEquals, map[string]*mockKey{
		"default": {
			isRecovery: false,
			key:        []byte("new-save-default"),
			token:      []byte("new-save-default-token"),
		},
		"default-fallback": {
			isRecovery: false,
			key:        []byte("new-save-fallback"),
			token:      []byte("new-save-fallback-token"),
		},
		"default-recovery": {
			isRecovery: true,
			key:        []byte{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4},
		},
	})

	newSaveKey, err := os.ReadFile(filepath.Join(dirs.SnapFDEDir, "ubuntu-save.key"))
	c.Assert(err, IsNil)
	c.Assert(newSaveKey, DeepEquals, []byte("new-protector"))
}

func (s *handlersInstallReprovisionSuite) TestDoReprovisionMissingCache(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("reprovision", "...")
	t := st.NewTask("do-reprovision", "reprovision test")
	chg.AddTask(t)

	// Don't set up any cache data - this simulates missing reprovision context
	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "missing reprovision context")
}

func (s *handlersInstallReprovisionSuite) TestDoReprovisionWrongCacheType(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("reprovision", "...")
	t := st.NewTask("do-reprovision", "reprovision test")
	chg.AddTask(t)

	// Set up wrong cache type
	st.Cache(devicestate.ReprovisionSetupDataKey{}, "wrong-type")

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, `internal error: wrong data type for reprovisionSetupDataKey`)
}

func (s *handlersInstallReprovisionSuite) TestDoReprovisionMissingCheckContext(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("reprovision", "...")
	t := st.NewTask("do-reprovision", "reprovision test")
	chg.AddTask(t)

	rkey := keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}
	// Create setupData with recovery key but nil checkContext
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData(&rkey, nil))

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "missing post install check context")
}

func (s *handlersInstallReprovisionSuite) TestDoReprovisionMissingRecoveryKey(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("reprovision", "...")
	t := st.NewTask("do-reprovision", "reprovision test")
	chg.AddTask(t)

	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	// Create setupData with check context but nil recovery key
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData(nil, preinstallCheckContext))

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "missing recovery key")
}

func (s *handlersInstallReprovisionSuite) TestDoReprovisionNewProtectorKeyError(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("reprovision", "...")
	t := st.NewTask("do-reprovision", "reprovision test")
	chg.AddTask(t)

	rkey := keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData(&rkey, preinstallCheckContext))

	defer devicestate.MockSecbootGetPCRHandleFromToken(func(disk string, name string) (uint32, error) {
		return 0, nil
	})()

	defer devicestate.MockSecbootReleasePCRResourceHandle(func(nv uint32) error {
		return nil
	})()

	defer devicestate.MockSecbootTestProtectorKey(func(ctx context.Context, disk string, keyName string, key []byte) (bool, error) {
		return false, nil
	})()

	defer devicestate.MockKeysNewProtectorKey(func() (keys.ProtectorKey, error) {
		return nil, fmt.Errorf("protector key failed")
	})()

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "protector key failed")
}

func (s *handlersInstallReprovisionSuite) TestDoReprovisionCreateProtectedKeyError(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("reprovision", "...")
	t := st.NewTask("do-reprovision", "reprovision test")
	chg.AddTask(t)

	rkey := keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData(&rkey, preinstallCheckContext))

	defer devicestate.MockSecbootGetPCRHandleFromToken(func(disk string, name string) (uint32, error) {
		return 0, nil
	})()

	defer devicestate.MockSecbootReleasePCRResourceHandle(func(nv uint32) error {
		return nil
	})()

	defer devicestate.MockSecbootTestProtectorKey(func(ctx context.Context, disk string, keyName string, key []byte) (bool, error) {
		return false, nil
	})()

	defer devicestate.MockKeysNewProtectorKey(func() (keys.ProtectorKey, error) {
		return keys.ProtectorKey([]byte("new-protector")), nil
	})()

	defer devicestate.MockKeysCreateProtectedKey(func(k keys.ProtectorKey, primaryKey []byte) (*keys.PlainKey, []byte, []byte, error) {
		return nil, nil, nil, fmt.Errorf("protected key creation failed")
	})()

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "protected key creation failed")
}

func (s *handlersInstallReprovisionSuite) TestDoReprovisionKernelInfoError(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("reprovision", "...")
	t := st.NewTask("do-reprovision", "reprovision test")
	chg.AddTask(t)

	rkey := keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData(&rkey, preinstallCheckContext))

	plainKey := &keys.PlainKey{}
	defer devicestate.MockKeysCreateProtectedKey(func(k keys.ProtectorKey, primaryKey []byte) (*keys.PlainKey, []byte, []byte, error) {
		return plainKey, []byte("new-primary-key"), []byte("new-save-default"), nil
	})()

	defer devicestate.MockKeysPlainKeyWrite(func(key *keys.PlainKey, writer keys.KeyDataWriter) error {
		_, err := writer.Write([]byte("new-save-default-token"))
		c.Assert(err, IsNil)
		err = writer.Commit()
		c.Assert(err, IsNil)
		return nil
	})()

	defer devicestate.MockSnapstateKernelInfo(func(st *state.State, deviceCtx snapstate.DeviceContext) (*snap.Info, error) {
		return nil, fmt.Errorf("kernel info failed")
	})()

	c.Assert(os.MkdirAll(dirs.SnapFDEDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapFDEDir, "ubuntu-save.key"), []byte("save-key"), 0644), IsNil)

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "cannot get kernel info: kernel info failed")
}

func (s *handlersInstallReprovisionSuite) TestDoReprovisionKeyProtectorError(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("reprovision", "...")
	t := st.NewTask("do-reprovision", "reprovision test")
	chg.AddTask(t)

	rkey := keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData(&rkey, preinstallCheckContext))

	plainKey := &keys.PlainKey{}
	defer devicestate.MockKeysCreateProtectedKey(func(k keys.ProtectorKey, primaryKey []byte) (*keys.PlainKey, []byte, []byte, error) {
		return plainKey, []byte("new-primary-key"), []byte("new-save-default"), nil
	})()

	defer devicestate.MockKeysPlainKeyWrite(func(key *keys.PlainKey, writer keys.KeyDataWriter) error {
		_, err := writer.Write([]byte("new-save-default-token"))
		c.Assert(err, IsNil)
		err = writer.Commit()
		c.Assert(err, IsNil)
		return nil
	})()

	defer devicestate.MockSnapstateKernelInfo(func(st *state.State, deviceCtx snapstate.DeviceContext) (*snap.Info, error) {
		sideInfo := &snap.SideInfo{RealName: "pc-kernel", Revision: snap.R(1), SnapID: "pc-kernel-id"}
		snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
			Active:   true,
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
			Current:  sideInfo.Revision,
			SnapType: "kernel",
		})
		yml := `
name: pc-kernel
version: 1.0
`
		return snaptest.MockSnap(c, yml, sideInfo), nil
	})()

	defer devicestate.MockHookKeyProtectorFactory(func(m *devicestate.DeviceManager, s *snap.Info) (secboot.KeyProtectorFactory, error) {
		return nil, fmt.Errorf("key protector failed")
	})()

	defer devicestate.MockSecbootSaveCheckResult(func(pcc *secboot.PreinstallCheckContext, filename string) error {
		return nil
	})()

	defer devicestate.MockSecbootPreinstallCheckAction(func(pcc *secboot.PreinstallCheckContext, ctx context.Context, action *secboot.PreinstallAction) ([]secboot.PreinstallErrorDetails, error) {
		return []secboot.PreinstallErrorDetails{}, nil
	})()

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "key protector failed")
}

func (s *handlersInstallReprovisionSuite) TestDoReprovisionPreinstallCheckError(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("reprovision", "...")
	t := st.NewTask("do-reprovision", "reprovision test")
	chg.AddTask(t)

	rkey := keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData(&rkey, preinstallCheckContext))

	plainKey := &keys.PlainKey{}
	defer devicestate.MockKeysCreateProtectedKey(func(k keys.ProtectorKey, primaryKey []byte) (*keys.PlainKey, []byte, []byte, error) {
		return plainKey, []byte("new-primary-key"), []byte("new-save-default"), nil
	})()

	defer devicestate.MockKeysPlainKeyWrite(func(key *keys.PlainKey, writer keys.KeyDataWriter) error {
		_, err := writer.Write([]byte("new-save-default-token"))
		c.Assert(err, IsNil)
		err = writer.Commit()
		c.Assert(err, IsNil)
		return nil
	})()

	defer devicestate.MockSnapstateKernelInfo(func(st *state.State, deviceCtx snapstate.DeviceContext) (*snap.Info, error) {
		sideInfo := &snap.SideInfo{RealName: "pc-kernel", Revision: snap.R(1), SnapID: "pc-kernel-id"}
		snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
			Active:   true,
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
			Current:  sideInfo.Revision,
			SnapType: "kernel",
		})
		yml := `
name: pc-kernel
version: 1.0
`
		return snaptest.MockSnap(c, yml, sideInfo), nil
	})()

	defer devicestate.MockSecbootPreinstallCheckAction(func(pcc *secboot.PreinstallCheckContext, ctx context.Context, action *secboot.PreinstallAction) ([]secboot.PreinstallErrorDetails, error) {
		return nil, fmt.Errorf("preinstall check failed")
	})()

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "preinstall check failed")
}

func (s *handlersInstallReprovisionSuite) TestDoReprovisionPreinstallCheckErrorDetails(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("reprovision", "...")
	t := st.NewTask("do-reprovision", "reprovision test")
	chg.AddTask(t)

	rkey := keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData(&rkey, preinstallCheckContext))

	plainKey := &keys.PlainKey{}
	defer devicestate.MockKeysCreateProtectedKey(func(k keys.ProtectorKey, primaryKey []byte) (*keys.PlainKey, []byte, []byte, error) {
		return plainKey, []byte("new-primary-key"), []byte("new-save-default"), nil
	})()

	defer devicestate.MockKeysPlainKeyWrite(func(key *keys.PlainKey, writer keys.KeyDataWriter) error {
		_, err := writer.Write([]byte("new-save-default-token"))
		c.Assert(err, IsNil)
		err = writer.Commit()
		c.Assert(err, IsNil)
		return nil
	})()

	defer devicestate.MockSnapstateKernelInfo(func(st *state.State, deviceCtx snapstate.DeviceContext) (*snap.Info, error) {
		sideInfo := &snap.SideInfo{RealName: "pc-kernel", Revision: snap.R(1), SnapID: "pc-kernel-id"}
		snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
			Active:   true,
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
			Current:  sideInfo.Revision,
			SnapType: "kernel",
		})
		yml := `
name: pc-kernel
version: 1.0
`
		return snaptest.MockSnap(c, yml, sideInfo), nil
	})()

	defer devicestate.MockSecbootPreinstallCheckAction(func(pcc *secboot.PreinstallCheckContext, ctx context.Context, action *secboot.PreinstallAction) ([]secboot.PreinstallErrorDetails, error) {
		return []secboot.PreinstallErrorDetails{{}}, nil
	})()

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "postinstall check found some issues")
}

func (s *handlersInstallReprovisionSuite) TestDoReprovisionMakeRunnableError(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("reprovision", "...")
	t := st.NewTask("do-reprovision", "reprovision test")
	chg.AddTask(t)

	rkey := keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData(&rkey, preinstallCheckContext))

	plainKey := &keys.PlainKey{}
	defer devicestate.MockKeysCreateProtectedKey(func(k keys.ProtectorKey, primaryKey []byte) (*keys.PlainKey, []byte, []byte, error) {
		return plainKey, []byte("new-primary-key"), []byte("new-save-default"), nil
	})()

	defer devicestate.MockKeysPlainKeyWrite(func(key *keys.PlainKey, writer keys.KeyDataWriter) error {
		_, err := writer.Write([]byte("new-save-default-token"))
		c.Assert(err, IsNil)
		err = writer.Commit()
		c.Assert(err, IsNil)
		return nil
	})()

	defer devicestate.MockSnapstateKernelInfo(func(st *state.State, deviceCtx snapstate.DeviceContext) (*snap.Info, error) {
		sideInfo := &snap.SideInfo{RealName: "pc-kernel", Revision: snap.R(1), SnapID: "pc-kernel-id"}
		snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
			Active:   true,
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
			Current:  sideInfo.Revision,
			SnapType: "kernel",
		})
		yml := `
name: pc-kernel
version: 1.0
`
		return snaptest.MockSnap(c, yml, sideInfo), nil
	})()

	defer devicestate.MockSecbootPreinstallCheckAction(func(pcc *secboot.PreinstallCheckContext, ctx context.Context, action *secboot.PreinstallAction) ([]secboot.PreinstallErrorDetails, error) {
		return []secboot.PreinstallErrorDetails{}, nil
	})()

	defer devicestate.MockSecbootSaveCheckResult(func(pcc *secboot.PreinstallCheckContext, filename string) error {
		return nil
	})()

	defer devicestate.MockSecbootCheckResult(func(pcc *secboot.PreinstallCheckContext) (*secboot.PreinstallCheckResult, error) {
		return &secboot.PreinstallCheckResult{}, nil
	})()

	defer devicestate.MockBootMakeRunnableReprovision(func(model *asserts.Model, protector secboot.KeyProtectorFactory, encryption *boot.EncryptionSetup) error {
		return fmt.Errorf("make runnable failed")
	})()

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "cannot make system runnable: make runnable failed")
}

func (s *handlersInstallReprovisionSuite) TestDoReprovisionFdestateGetError(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("reprovision", "...")
	t := st.NewTask("do-reprovision", "reprovision test")
	chg.AddTask(t)

	rkey := keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData(&rkey, preinstallCheckContext))

	// Mock fdestateGetEncryptedContainers to return an error (e.g., disk gone)
	defer devicestate.MockFdestateGetEncryptedContainers(func(st *state.State) ([]fdeBackend.EncryptedContainer, error) {
		return nil, fmt.Errorf("storage backend failure")
	})()

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "storage backend failure")
}

func (s *handlersInstallReprovisionSuite) TestDoReprovisionMultipleDataDisks(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("reprovision", "...")
	t := st.NewTask("do-reprovision", "reprovision test")
	chg.AddTask(t)

	rkey := keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData(&rkey, preinstallCheckContext))

	// Mock fdestateGetEncryptedContainers to return multiple system-data disks
	defer devicestate.MockFdestateGetEncryptedContainers(func(st *state.State) ([]fdeBackend.EncryptedContainer, error) {
		return []fdeBackend.EncryptedContainer{
			&mockEncryptedContainer{devPath: "/dev/data1", containerRole: "system-data"},
			&mockEncryptedContainer{devPath: "/dev/data2", containerRole: "system-data"},
			&mockEncryptedContainer{devPath: "/dev/save", containerRole: "system-save"},
		}, nil
	})()

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "multiple containers found with role system-data")
}

func (s *handlersInstallReprovisionSuite) TestDoReprovisionMultipleSaveDisks(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("reprovision", "...")
	t := st.NewTask("do-reprovision", "reprovision test")
	chg.AddTask(t)

	rkey := keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData(&rkey, preinstallCheckContext))

	// Mock fdestateGetEncryptedContainers to return multiple system-save disks
	defer devicestate.MockFdestateGetEncryptedContainers(func(st *state.State) ([]fdeBackend.EncryptedContainer, error) {
		return []fdeBackend.EncryptedContainer{
			&mockEncryptedContainer{devPath: "/dev/data", containerRole: "system-data"},
			&mockEncryptedContainer{devPath: "/dev/save1", containerRole: "system-save"},
			&mockEncryptedContainer{devPath: "/dev/save2", containerRole: "system-save"},
		}, nil
	})()

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "multiple containers found with role system-save")
}

func (s *handlersInstallReprovisionSuite) TestDoReprovisionNoSaveDisk(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("reprovision", "...")
	t := st.NewTask("do-reprovision", "reprovision test")
	chg.AddTask(t)

	rkey := keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData(&rkey, preinstallCheckContext))

	// Mock fdestateGetEncryptedContainers to return only data disk (no save)
	defer devicestate.MockFdestateGetEncryptedContainers(func(st *state.State) ([]fdeBackend.EncryptedContainer, error) {
		return []fdeBackend.EncryptedContainer{
			&mockEncryptedContainer{devPath: "/dev/data", containerRole: "system-data"},
		}, nil
	})()

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "no save container found")
}

func (s *handlersInstallReprovisionSuite) TestDoReprovisionNoDataDisk(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("reprovision", "...")
	t := st.NewTask("do-reprovision", "reprovision test")
	chg.AddTask(t)

	rkey := keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData(&rkey, preinstallCheckContext))

	// Mock fdestateGetEncryptedContainers to return only save disk (no data)
	defer devicestate.MockFdestateGetEncryptedContainers(func(st *state.State) ([]fdeBackend.EncryptedContainer, error) {
		return []fdeBackend.EncryptedContainer{
			&mockEncryptedContainer{devPath: "/dev/save", containerRole: "system-save"},
		}, nil
	})()

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "no data container found")
}
