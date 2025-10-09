// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package systemd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/snapcore/snapd/logger"
)

const sd_LISTEN_FDS_START = 3

type FdName string

const (
	FdNameRemoved          FdName = "removed"
	FdNameRecoveryKeyStore FdName = "rkey"
)

var knownFdNames = map[FdName]bool{
	FdNameRecoveryKeyStore: true,
}

func (name FdName) Validate() error {
	if !knownFdNames[name] {
		return fmt.Errorf(`unknown file descriptor name %q`, name)
	}
	return nil
}

var osGetenv = os.Getenv
var osSetenv = os.Setenv

// GetFds retrieves file descriptors passed from systemd by their name.
func GetFds(name FdName) (fds []int) {
	for _, entry := range allFds() {
		if entry.name == string(name) {
			fds = append(fds, entry.fd)
		}
	}

	return fds
}

// AddFds passes file descriptors to systemd associated with a name
// to keep them opened across snapd restarts.
//
// The file descriptors can be retrieved by using GetFds.
func AddFds(name FdName, fds ...int) error {
	if err := name.Validate(); err != nil {
		return fmt.Errorf("cannot add file descriptor: %v", err)
	}
	state := fmt.Sprintf("FDSTORE=1\nFDNAME=%s", name)
	return SdNotifyWithFds(state, fds...)
}

var syscallClose = syscall.Close

// PruneFds removes all unexpected file descriptors passed from
// systemd.
//
// This should be called once during early initialization. This is
// needed so that if an old version of snapd uploaded an fd that the
// new version doesn’t recognize anymore it’s good idea to close it
// both in snapd and in the fdstore.
func PruneFds() {
	for _, entry := range allFds() {
		if !strings.HasSuffix(string(entry.name), ".socket") && !knownFdNames[FdName(entry.name)] {
			// cleanup unknown fds for graceful transitions between snapd versions
			if err := removeFds(FdName(entry.name)); err != nil {
				// best-effort cleanup, keep going
				logger.Noticef("internal error: cannot remove unknown fdName %q: %v", entry.name, err)
				continue
			}
			if err := syscallClose(entry.fd); err != nil {
				// best-effort cleanup, keep going
				logger.Noticef("internal error: cannot close fd %d: %v", entry.fd, err)
				continue
			}
		}
	}
}

func removeFds(name FdName) (err error) {
	// mark as "removed" in $LISTEN_FDNAMES so they are ignored
	// on subsequent allFds calls.
	names := strings.Split(osGetenv("LISTEN_FDNAMES"), ":")
	for i := range names {
		if names[i] == string(name) {
			names[i] = string(FdNameRemoved)
		}
	}
	if err := osSetenv("LISTEN_FDNAMES", strings.Join(names, ":")); err != nil {
		return fmt.Errorf("cannot mark %q fds as removed: %v", name, err)
	}

	state := fmt.Sprintf("FDSTOREREMOVE=1\nFDNAME=%s", name)
	return SdNotify(state)
}

// ActivationSocketFds returns activation socket file descriptors
// that were passed from systemd. Only sockets whose name has a
// .socket suffix are returned.
func ActivationSocketFds() (socketFds map[string][]int) {
	socketFds = make(map[string][]int, 0)
	// The file descriptor name defaults to the name of the socket
	// unit (including its .socket suffix), unless it was explicitly
	// assigned by setting `FileDescriptorName=` on the socket unit.
	//
	// `FileDescriptorName=` was added in systemd version 227.
	for _, entry := range allFds() {
		if strings.HasSuffix(string(entry.name), ".socket") {
			socketFds[entry.name] = append(socketFds[entry.name], entry.fd)
		}
	}

	return socketFds
}

type fdWithName struct {
	fd   int
	name string
}

func allFds() []fdWithName {
	nfds, err := strconv.Atoi(osGetenv("LISTEN_FDS"))
	if err != nil || nfds == 0 {
		return nil
	}

	names := strings.Split(osGetenv("LISTEN_FDNAMES"), ":")

	fds := make([]fdWithName, 0, nfds)
	for i := 0; i < nfds; i++ {
		fdEntry := fdWithName{}
		fdEntry.fd = sd_LISTEN_FDS_START + i
		if i < len(names) {
			fdEntry.name = names[i]
		}
		if fdEntry.name != string(FdNameRemoved) {
			fds = append(fds, fdEntry)
		}
	}

	return fds
}
