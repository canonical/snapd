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

package osutil

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"syscall"

	"github.com/snapcore/snapd/dirs"
)

func exitStatus(err error) (bool, int) {
	e, ok := err.(*exec.ExitError)
	if !ok {
		return false, -1
	}
	ws, ok := e.Sys().(syscall.WaitStatus)
	if !ok {
		return false, -1
	}

	return true, ws.ExitStatus()
}

func unwrapErr(errbuf *bytes.Buffer, err error) error {
	if isStatus, status := exitStatus(err); isStatus && status == 2 {
		return os.ErrNotExist
	}

	if errbuf.Len() > 0 {
		return errors.New(errbuf.String())
	}

	return fmt.Errorf("exec: %v", err)
}

func privHelperCmd(u *user.User, verb, filename string) *exec.Cmd {
	return exec.Command(dirs.PrivHelper, u.Uid, u.Gid, verb, filename)
}

func PrivRead(u *user.User, filename string) ([]byte, error) {
	var buf, errbuf bytes.Buffer

	cmd := privHelperCmd(u, "read", filename)
	cmd.Stdout = &buf
	cmd.Stderr = &errbuf
	if err := cmd.Run(); err != nil {
		return nil, unwrapErr(&errbuf, err)
	}

	return buf.Bytes(), nil
}

func PrivWrite(u *user.User, filename string, data []byte) error {
	var errbuf bytes.Buffer

	cmd := privHelperCmd(u, "write", filename)
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stderr = &errbuf
	if err := cmd.Run(); err != nil {
		return unwrapErr(&errbuf, err)
	}

	return nil
}

func PrivRemove(u *user.User, filename string) error {
	var errbuf bytes.Buffer

	cmd := privHelperCmd(u, "remove", filename)
	cmd.Stderr = &errbuf
	if err := cmd.Run(); err != nil {
		return unwrapErr(&errbuf, err)
	}

	return nil
}
