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

package osutil

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ddkwork/golibrary/mylog"
)

// CopyFlag is used to tweak the behaviour of CopyFile
type CopyFlag uint8

const (
	// CopyFlagDefault is the default behaviour
	CopyFlagDefault CopyFlag = 0
	// CopyFlagSync does a sync after copying the files
	CopyFlagSync CopyFlag = 1 << iota
	// CopyFlagOverwrite overwrites the target if it exists
	CopyFlagOverwrite
	// CopyFlagPreserveAll preserves mode,owner,time attributes
	CopyFlagPreserveAll
)

var (
	openfile = doOpenFile
	copyfile = doCopyFile
)

type fileish interface {
	Close() error
	Sync() error
	Fd() uintptr
	Stat() (os.FileInfo, error)
	Read([]byte) (int, error)
	Write([]byte) (int, error)
}

func doOpenFile(name string, flag int, perm os.FileMode) (fileish, error) {
	return os.OpenFile(name, flag, perm)
}

// CopyFile copies src to dst
func CopyFile(src, dst string, flags CopyFlag) (err error) {
	if flags&CopyFlagPreserveAll != 0 {
		mylog.Check(
			// Our native copy code does not preserve all attributes
			// (yet). If the user needs this functionality we just
			// fallback to use the system's "cp" binary to do the copy.
			runCpPreserveAll(src, dst, "copy all"))

		if flags&CopyFlagSync != 0 {
			return runSync()
		}
		return nil
	}

	fin := mylog.Check2(openfile(src, os.O_RDONLY, 0))

	defer func() {
		if cerr := fin.Close(); cerr != nil && err == nil {
			mylog.Check(fmt.Errorf("when closing %s: %w", src, cerr))
		}
	}()

	fi := mylog.Check2(fin.Stat())

	outflags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if flags&CopyFlagOverwrite == 0 {
		outflags |= os.O_EXCL
	}

	fout := mylog.Check2(openfile(dst, outflags, fi.Mode()))

	defer func() {
		if cerr := fout.Close(); cerr != nil && err == nil {
			mylog.Check(fmt.Errorf("when closing %s: %w", dst, cerr))
		}
	}()
	mylog.Check(copyfile(fin, fout, fi))

	if flags&CopyFlagSync != 0 {
		mylog.Check(fout.Sync())
	}

	return nil
}

// AtomicWriteFileCopy writes to dst a copy of src using AtomicFile
// internally to create the destination.
// The destination path is always overwritten. The destination and
// the owning directory are synced after copy completes. Pass additional flags
// for AtomicFile wrapping the destination.
func AtomicWriteFileCopy(dst, src string, flags AtomicWriteFlags) (err error) {
	fin := mylog.Check2(openfile(src, os.O_RDONLY, 0))

	defer func() {
		if cerr := fin.Close(); cerr != nil && err == nil {
			mylog.Check(fmt.Errorf("when closing %s: %v", src, cerr))
		}
	}()

	fi := mylog.Check2(fin.Stat())

	fout := mylog.Check2(NewAtomicFile(dst, fi.Mode(), flags, NoChown, NoChown))

	fout.SetModTime(fi.ModTime())
	defer func() {
		if cerr := fout.Cancel(); cerr != ErrCannotCancel && err == nil {
			mylog.Check(fmt.Errorf("cannot cancel temporary file copy %s: %v", fout.Name(), cerr))
		}
	}()
	mylog.Check(copyfile(fin, fout, fi))
	mylog.Check(fout.Commit())

	return nil
}

func runCmd(cmd *exec.Cmd, errdesc string) error {
	if output := mylog.Check2(cmd.CombinedOutput()); err != nil {
		output = bytes.TrimSpace(output)
		if exitCode := mylog.Check2(ExitCode(err)); err == nil {
			return &CopySpecialFileError{
				desc:     errdesc,
				exitCode: exitCode,
				output:   output,
			}
		}
		return &CopySpecialFileError{
			desc:   errdesc,
			err:    err,
			output: output,
		}
	}

	return nil
}

func runSync(args ...string) error {
	return runCmd(exec.Command("sync", args...), "sync")
}

func runCpPreserveAll(path, dest, errdesc string) error {
	return runCmd(exec.Command("cp", "-av", path, dest), errdesc)
}

// CopySpecialFile is used to copy all the things that are not files
// (like device nodes, named pipes etc)
func CopySpecialFile(path, dest string) error {
	mylog.Check(runCpPreserveAll(path, dest, "copy device node"))

	return runSync(filepath.Dir(dest))
}

// CopySpecialFileError is returned if a special file copy fails
type CopySpecialFileError struct {
	desc     string
	exitCode int
	output   []byte
	err      error
}

func (e CopySpecialFileError) Error() string {
	if e.err == nil {
		return fmt.Sprintf("failed to %s: %q (%v)", e.desc, e.output, e.exitCode)
	}

	return fmt.Sprintf("failed to %s: %q (%v)", e.desc, e.output, e.err)
}
