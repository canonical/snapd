// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build linux

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
 */

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/snapcore/snapd/osutil/user"
	"github.com/snapcore/snapd/usersession/userbus"
)

type property struct {
	Name  string
	Value dbus.Variant
}

type command struct {
	Path          string
	Args          []string
	IgnoreFailure bool
}

type auxiliaryUnit struct {
	Name       string
	Properties []property
}

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("expected uid")
	}
	uid, err := strconv.ParseUint(os.Args[1], 10, 32)
	if err != nil {
		return err
	}
	account, err := user.LookupId(os.Args[1])
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := userbus.Connect(ctx, uint32(uid))
	if err != nil {
		return err
	}
	defer conn.Close()
	var actualUID, helperPID uint32
	if err := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetConnectionUnixUser", 0, conn.Names()[0]).Store(&actualUID); err != nil {
		return err
	}
	if actualUID != uint32(uid) {
		return fmt.Errorf("connected as uid %d instead of %d", actualUID, uid)
	}
	if err := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetConnectionUnixProcessID", 0, conn.Names()[0]).Store(&helperPID); err != nil {
		return err
	}
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", helperPID))
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(status), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "Uid:", "Gid:":
			want := os.Args[1]
			if fields[0] == "Gid:" {
				want = account.Gid
			}
			for _, value := range fields[1:] {
				if value != want {
					return fmt.Errorf("incorrect helper credentials: %s", line)
				}
			}
		case "Groups:":
			if len(fields) != 1 {
				return fmt.Errorf("helper inherited supplementary groups: %s", line)
			}
		}
	}
	fmt.Printf("connected as uid %d through helper %d\n", actualUID, helperPID)
	signals := make(chan *dbus.Signal, 128)
	conn.Signal(signals)
	if err := conn.AddMatchSignalContext(ctx, dbus.WithMatchSender("org.freedesktop.systemd1"), dbus.WithMatchInterface("org.freedesktop.systemd1.Manager"), dbus.WithMatchMember("JobRemoved")); err != nil {
		return err
	}
	manager := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
	if err := manager.CallWithContext(ctx, "org.freedesktop.systemd1.Manager.Subscribe", 0).Err; err != nil {
		return err
	}

	dir, err := os.MkdirTemp("", "userbus-driver-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	var files []*os.File
	for _, name := range []string{"stdin", "stdout", "stderr"} {
		file, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		defer file.Close()
		files = append(files, file)
	}
	if _, err := files[0].WriteString("hello\n"); err != nil {
		return err
	}
	if _, err := files[0].Seek(0, io.SeekStart); err != nil {
		return err
	}
	name := fmt.Sprintf("snapd-userbus-test-%d.service", os.Getpid())
	properties := []property{
		{"Type", dbus.MakeVariant("oneshot")},
		{"CollectMode", dbus.MakeVariant("inactive-or-failed")},
		{"WorkingDirectory", dbus.MakeVariant("/tmp")},
		{"ExecStart", dbus.MakeVariant([]command{{"/bin/sh", []string{"sh", "-c", `read -r line; printf 'uid=%s gid=%s stdin=%s\n' "$(id -u)" "$(id -g)" "$line"; echo 'separate stderr' >&2; cat /proc/self/cgroup`}, false}})},
		{"StandardInputFileDescriptor", dbus.MakeVariant(dbus.UnixFD(files[0].Fd()))},
		{"StandardOutputFileDescriptor", dbus.MakeVariant(dbus.UnixFD(files[1].Fd()))},
		{"StandardErrorFileDescriptor", dbus.MakeVariant(dbus.UnixFD(files[2].Fd()))},
	}
	var job dbus.ObjectPath
	if err := manager.CallWithContext(ctx, "org.freedesktop.systemd1.Manager.StartTransientUnit", 0, name, "fail", properties, []auxiliaryUnit{}).Store(&job); err != nil {
		return err
	}
	for {
		select {
		case signal, ok := <-signals:
			if !ok {
				return fmt.Errorf("bus disconnected while waiting for job")
			}
			if signal.Name != "org.freedesktop.systemd1.Manager.JobRemoved" || signal.Body[1] != job {
				continue
			}
			if signal.Body[3] != "done" {
				return fmt.Errorf("job failed: %v", signal.Body)
			}
			fmt.Printf("JobRemoved: %v\n", signal.Body)
			return checkOutput(dir, os.Args[1], account.Gid)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func checkOutput(dir, uid, gid string) error {
	out, err := os.ReadFile(filepath.Join(dir, "stdout"))
	if err != nil {
		return err
	}
	diagnostic, err := os.ReadFile(filepath.Join(dir, "stderr"))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(string(out), fmt.Sprintf("uid=%s gid=%s stdin=hello\n", uid, gid)) || !strings.Contains(string(out), "/user@"+uid+".service/") || string(diagnostic) != "separate stderr\n" {
		return fmt.Errorf("unexpected service output: stdout=%q stderr=%q", out, diagnostic)
	}
	fmt.Printf("stdout: %sstderr: %s", out, diagnostic)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
