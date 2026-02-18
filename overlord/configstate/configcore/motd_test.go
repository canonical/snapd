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

package configcore_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/testutil"
)

const (
	motdOptionKey               = "system.motd"
	defaultMotdFilePathReadonly = "/usr/lib/motd.d/50-default"
	defaultMotdFilePathWritable = "/etc/motd.d/50-default"
)

type motdSuite struct {
	configcoreSuite

	readonlyDirPath  string
	readonlyFilePath string
	writableDirPath  string
	writableFilePath string
}

var _ = Suite(&motdSuite{})

func (s *motdSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)

	// Create the default readonly motd file to simulate UC24+ system
	s.readonlyFilePath = filepath.Join(dirs.GlobalRootDir, defaultMotdFilePathReadonly)
	s.readonlyDirPath = filepath.Dir(s.readonlyFilePath)
	err := os.MkdirAll(s.readonlyDirPath, 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(s.readonlyFilePath, []byte("Default MOTD\n"), 0644)
	c.Assert(err, IsNil)

	s.writableFilePath = filepath.Join(dirs.GlobalRootDir, defaultMotdFilePathWritable)
	s.writableDirPath = filepath.Dir(s.writableFilePath)
}

func (s *motdSuite) TearDownTest(c *C) {
	s.configcoreSuite.TearDownTest(c)
}

func (s *motdSuite) TestValidateMotdConfigurationValid(c *C) {
	// Valid short MOTD
	conf := &mockConf{
		state: s.state,
		conf: map[string]any{
			motdOptionKey: "Test MOTD",
		},
	}
	err := configcore.FilesystemOnlyRun(coreDev, conf)
	c.Assert(err, IsNil)

	// Valid multi-line MOTD
	conf = &mockConf{
		state: s.state,
		conf: map[string]any{
			motdOptionKey: "Welcome to Ubuntu Core!\nThis is a test message of the day.",
		},
	}
	err = configcore.FilesystemOnlyRun(coreDev, conf)
	c.Assert(err, IsNil)

	// Empty MOTD (unset)
	conf = &mockConf{
		state: s.state,
		conf: map[string]any{
			motdOptionKey: "",
		},
	}
	err = configcore.FilesystemOnlyRun(coreDev, conf)
	c.Assert(err, IsNil)

	// MOTD that is exactly 64 KiB (minus 1 for newline)
	conf = &mockConf{
		state: s.state,
		conf: map[string]any{
			motdOptionKey: strings.Repeat("a", 64*1024-1),
		},
	}
	err = configcore.FilesystemOnlyRun(coreDev, conf)
	c.Assert(err, IsNil)
}

func (s *motdSuite) TestValidateMotdConfigurationInvalid(c *C) {
	// MOTD that exceeds 64 KiB
	conf := &mockConf{
		state: s.state,
		conf: map[string]any{
			motdOptionKey: strings.Repeat("a", 64*1024+1),
		},
	}
	err := configcore.FilesystemOnlyRun(coreDev, conf)
	c.Assert(err, ErrorMatches, `cannot set message of the day: size .* bytes exceeds limit of 65536 bytes`)
}

func (s *motdSuite) TestIsMotdConfigurationSupportedTrue(c *C) {
	// SetUpTest creates defaultMotdFilepathReadonly, so it should be supported
	conf := &mockConf{
		state: s.state,
		conf: map[string]any{
			motdOptionKey: "Test MOTD",
		},
	}
	err := configcore.FilesystemOnlyRun(coreDev, conf)
	c.Assert(err, IsNil)
}

func (s *motdSuite) TestIsMotdConfigurationSupportedFalse(c *C) {
	// As SetUpTest creates defaultMotdFilepathReadonly,
	// remove it to simulate older than UC24 systems
	err := os.Remove(s.readonlyFilePath)
	c.Assert(err, IsNil)

	conf := &mockConf{
		state: s.state,
		conf: map[string]any{
			motdOptionKey: "Test MOTD",
		},
	}
	err = configcore.FilesystemOnlyRun(coreDev, conf)
	c.Assert(err, ErrorMatches, "cannot set message of the day: unsupported on this system, requires UC24\\+")
}

func (s *motdSuite) TestHandleMotdConfigurationSetFileExists(c *C) {
	// First add the writable file
	err := os.MkdirAll(s.writableDirPath, 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(s.writableFilePath, []byte("Old MOTD\n"), 0644)
	c.Assert(err, IsNil)

	// Now set motd to a new value
	motd := "Welcome to Ubuntu Core!\nThis is a test message of the day."
	conf := &mockConf{
		state: s.state,
		conf: map[string]any{
			motdOptionKey: motd,
		},
	}
	err = configcore.FilesystemOnlyRun(coreDev, conf)
	c.Assert(err, IsNil)

	// Verify the file content was written correctly
	c.Check(s.writableFilePath, testutil.FileEquals, motd+"\n")
}

func (s *motdSuite) TestHandleMotdConfigurationSetFileDoesNotExist(c *C) {
	// Ensure the writable file does not exist
	_, err := os.Lstat(s.writableFilePath)
	c.Assert(errors.Is(err, fs.ErrNotExist), Equals, true)

	// Now set motd
	motd := "Welcome to Ubuntu Core!\nThis is a test message of the day."
	conf := &mockConf{
		state: s.state,
		conf: map[string]any{
			motdOptionKey: motd,
		},
	}
	err = configcore.FilesystemOnlyRun(coreDev, conf)
	c.Assert(err, IsNil)

	// Verify directory was created with correct permissions
	dirInfo, err := os.Lstat(s.writableDirPath)
	c.Assert(err, IsNil)
	c.Check(dirInfo.IsDir(), Equals, true)
	c.Check(dirInfo.Mode().Perm(), Equals, os.FileMode(0755))

	// Verify file was created with correct permissions
	fileInfo, err := os.Lstat(s.writableFilePath)
	c.Assert(err, IsNil)
	c.Check(fileInfo.Mode().IsRegular(), Equals, true)
	c.Check(fileInfo.Mode().Perm(), Equals, os.FileMode(0644))

	// Verify the file content was written correctly
	c.Check(s.writableFilePath, testutil.FileEquals, motd+"\n")
}

func (s *motdSuite) TestHandleMotdConfigurationSetMkdirAllError(c *C) {
	// Mock os.MkdirAll to return an error
	restore := configcore.MockOsMkdirAll(func(string, os.FileMode) error {
		return errors.New("permission denied")
	})
	defer restore()

	conf := &mockConf{
		state: s.state,
		conf: map[string]any{
			motdOptionKey: "Test MOTD",
		},
	}
	err := configcore.FilesystemOnlyRun(coreDev, conf)
	c.Assert(err, ErrorMatches, "cannot set message of the day: permission denied")
}

func (s *motdSuite) TestHandleMotdConfigurationSetAtomicWriteFileError(c *C) {
	// Mock osutil.AtomicWriteFile to return an error
	restore := configcore.MockOsutilAtomicWriteFile(func(string, []byte, os.FileMode, osutil.AtomicWriteFlags) error {
		return errors.New("permission denied")
	})
	defer restore()

	// Now set motd
	conf := &mockConf{
		state: s.state,
		conf: map[string]any{
			motdOptionKey: "Test MOTD",
		},
	}
	err := configcore.FilesystemOnlyRun(coreDev, conf)
	c.Assert(err, ErrorMatches, "cannot set message of the day: permission denied")
}

func (s *motdSuite) TestHandleMotdConfigurationUnsetFileExists(c *C) {
	// Unsetting when the writable file exists should remove it
	// First add the writable file
	err := os.MkdirAll(s.writableDirPath, 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(s.writableFilePath, []byte("Test MOTD\n"), 0644)
	c.Assert(err, IsNil)

	// Now unset motd
	conf := &mockConf{
		state: s.state,
		conf: map[string]any{
			motdOptionKey: "",
		},
	}
	err = configcore.FilesystemOnlyRun(coreDev, conf)
	c.Assert(err, IsNil)

	// Verify the writable file was removed
	_, err = os.Lstat(s.writableFilePath)
	c.Assert(errors.Is(err, fs.ErrNotExist), Equals, true)
}

func (s *motdSuite) TestHandleMotdConfigurationUnsetRemoveError(c *C) {
	// First add the writable file
	err := os.MkdirAll(s.writableDirPath, 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(s.writableFilePath, []byte("Test MOTD\n"), 0644)
	c.Assert(err, IsNil)

	// Mock os.Remove to return an error
	restore := configcore.MockOsRemove(func(string) error {
		return errors.New("permission denied")
	})
	defer restore()

	// Now unset motd
	conf := &mockConf{
		state: s.state,
		conf: map[string]any{
			motdOptionKey: "",
		},
	}
	err = configcore.FilesystemOnlyRun(coreDev, conf)
	c.Assert(err, ErrorMatches, "cannot unset message of the day: permission denied")

	// Verify the writable file still exists
	_, err = os.Lstat(s.writableFilePath)
	c.Assert(err, IsNil)
}

func (s *motdSuite) TestHandleMotdConfigurationUnsetFileDoesNotExist(c *C) {
	// Unsetting when the writable file doesn't exist should not error
	// Ensure the writable file does not exist
	_, err := os.Lstat(s.writableFilePath)
	c.Assert(errors.Is(err, fs.ErrNotExist), Equals, true)

	// Now unset motd
	conf := &mockConf{
		state: s.state,
		conf: map[string]any{
			motdOptionKey: "",
		},
	}
	err = configcore.FilesystemOnlyRun(coreDev, conf)
	c.Assert(err, IsNil)
}

func (s *motdSuite) TestHandleMotdConfigurationGetMotdFromSystemReadError(c *C) {
	// Mock osReadFile to return an error
	restore := configcore.MockOsReadFile(func(string) ([]byte, error) {
		return nil, errors.New("permission denied")
	})
	defer restore()

	conf := &mockConf{
		state: s.state,
		conf: map[string]any{
			motdOptionKey: "Test MOTD",
		},
	}
	err := configcore.FilesystemOnlyRun(coreDev, conf)
	c.Assert(err, ErrorMatches, `cannot get message of the day: .*permission denied`)
}

func (s *motdSuite) TestHandleMotdConfigurationSameMotdAsCurrent(c *C) {
	// Setting motd to same as current value should not create the writable file
	// Ensure the writable file does not exist
	_, err := os.Lstat(s.writableFilePath)
	c.Assert(errors.Is(err, fs.ErrNotExist), Equals, true)

	// "\n" needs to be added here as mockConf doesn't mock the calling of externalConfig getters
	conf := &mockConf{
		state: s.state,
		conf: map[string]any{
			motdOptionKey: "Default MOTD\n",
		},
	}
	err = configcore.FilesystemOnlyRun(coreDev, conf)
	c.Assert(err, IsNil)

	// Verify that the writable file was not created
	_, err = os.Lstat(s.writableFilePath)
	c.Assert(errors.Is(err, fs.ErrNotExist), Equals, true)
}

func (s *motdSuite) TestFilesystemOnlyApplySuccess(c *C) {
	// Create the readonly motd file in tmpDir
	tmpDir := c.MkDir()
	readonlyFilePath := filepath.Join(tmpDir, defaultMotdFilePathReadonly)
	err := os.MkdirAll(filepath.Dir(readonlyFilePath), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(readonlyFilePath, []byte("Default MOTD\n"), 0644)
	c.Assert(err, IsNil)

	motd := "Welcome to Ubuntu Core!\nThis is a test message of the day."
	conf := configcore.PlainCoreConfig(map[string]any{
		motdOptionKey: motd,
	})
	err = configcore.FilesystemOnlyApply(coreDev, tmpDir, conf)
	c.Assert(err, IsNil)

	writableFilePath := filepath.Join(tmpDir, defaultMotdFilePathWritable)
	c.Check(writableFilePath, testutil.FileEquals, motd+"\n")
}

func (s *motdSuite) TestFilesystemOnlyApplyNoMotdChange(c *C) {
	// Create the readonly and writable motd files in tmpDir
	tmpDir := c.MkDir()
	readonlyFilePath := filepath.Join(tmpDir, defaultMotdFilePathReadonly)
	err := os.MkdirAll(filepath.Dir(readonlyFilePath), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(readonlyFilePath, []byte("Default MOTD\n"), 0644)
	c.Assert(err, IsNil)
	writableFilePath := filepath.Join(tmpDir, defaultMotdFilePathWritable)
	err = os.MkdirAll(filepath.Dir(writableFilePath), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(writableFilePath, []byte("Test MOTD\n"), 0644)
	c.Assert(err, IsNil)

	conf := configcore.PlainCoreConfig(map[string]any{
		"non.motd.key": "value",
	})
	err = configcore.FilesystemOnlyApply(coreDev, tmpDir, conf)
	c.Assert(err, IsNil)

	// Verify that the writable file was not modified
	c.Check(writableFilePath, testutil.FileEquals, "Test MOTD\n")
}

func (s *motdSuite) TestFilesystemOnlyApplyValidationFails(c *C) {
	// Don't create the readonly motd file - simulates unsupported system
	tmpDir := c.MkDir()

	conf := configcore.PlainCoreConfig(map[string]any{
		motdOptionKey: "Test MOTD",
	})
	err := configcore.FilesystemOnlyApply(coreDev, tmpDir, conf)
	c.Assert(err, ErrorMatches, "cannot set message of the day: unsupported on this system, requires UC24\\+")
}

func (s *motdSuite) TestGetMotdFromSystemHelperInvalidKey(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)

	var motd string
	err := tr.Get("core", "system.motd.subkey", &motd)
	c.Assert(err, ErrorMatches, `cannot get message of the day: invalid key "system.motd.subkey", expected system.motd`)
}

func (s *motdSuite) TestGetMotdFromSystemUnsupported(c *C) {
	// When system is unsupported, Get should return an empty string with no error
	s.state.Lock()
	defer s.state.Unlock()

	// As SetUpTest creates defaultMotdFilepathReadonly,
	// remove it to simulate older than UC24 systems
	err := os.Remove(s.readonlyFilePath)
	c.Assert(err, IsNil)

	tr := config.NewTransaction(s.state)
	var motd string
	err = tr.Get("core", motdOptionKey, &motd)
	c.Assert(err, IsNil)
	c.Check(motd, Equals, "")
}

func (s *motdSuite) TestGetMotdFromSystemReadonly(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// Ensure writable file does not exist, only readonly
	_, err := os.Lstat(s.writableFilePath)
	c.Assert(errors.Is(err, fs.ErrNotExist), Equals, true)
	_, err = os.Lstat(s.readonlyFilePath)
	c.Assert(err, IsNil)

	tr := config.NewTransaction(s.state)

	var motd string
	err = tr.Get("core", motdOptionKey, &motd)
	c.Assert(err, IsNil)
	c.Check(motd, Equals, "Default MOTD\n")
}

func (s *motdSuite) TestGetMotdFromSystemWritable(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// Create writable file with custom content
	err := os.MkdirAll(s.writableDirPath, 0755)
	c.Assert(err, IsNil)
	customMotd := "Welcome to Ubuntu Core!\nThis is a test message of the day."
	err = os.WriteFile(s.writableFilePath, []byte(customMotd+"\n"), 0644)
	c.Assert(err, IsNil)

	tr := config.NewTransaction(s.state)

	// Should read from writable file, not readonly
	var motd string
	err = tr.Get("core", motdOptionKey, &motd)
	c.Assert(err, IsNil)
	c.Check(motd, Equals, customMotd+"\n")
}

func (s *motdSuite) TestGetMotdFromSystemReadError(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// Mock osReadFile to return an error
	restore := configcore.MockOsReadFile(func(string) ([]byte, error) {
		return nil, errors.New("permission denied")
	})
	defer restore()

	tr := config.NewTransaction(s.state)

	var motd string
	err := tr.Get("core", motdOptionKey, &motd)
	c.Assert(err, ErrorMatches, `cannot get message of the day: .*permission denied`)
}
