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

package fdstore

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd"
	"golang.org/x/sys/unix"
)

const sd_LISTEN_FDS_START = 3

type FdName string

const (
	FdNameMemfdSecretState FdName = "memfd-secret-state"
)

var knownFdNames = map[FdName]bool{
	FdNameMemfdSecretState: true,
}

func (name FdName) validate() error {
	if !name.isSocket() && !knownFdNames[name] {
		return fmt.Errorf(`unknown file descriptor name %q`, name)
	}
	return nil
}

func (name FdName) isSocket() bool {
	return strings.HasSuffix(string(name), ".socket")
}

var (
	osGetenv        = os.Getenv
	osUnsetenv      = os.Unsetenv
	osLookupEnv     = os.LookupEnv
	osGetpid        = os.Getpid
	unixClose       = unix.Close
	unixCloseOnExec = unix.CloseOnExec
	sdNotify        = systemd.SdNotify
	sdNotifyWithFds = systemd.SdNotifyWithFds
)

var fdstore map[FdName][]int
var mu sync.RWMutex

func initFdstore() {
	mu.Lock()
	defer mu.Unlock()

	if fdstore != nil {
		// fdstore map is lazily loaded once
		return
	}

	// Make sure initialization only happens once, only here.
	defer func() {
		osUnsetenv("LISTEN_PID")
		osUnsetenv("LISTEN_FDS")
		osUnsetenv("LISTEN_FDNAMES")
	}()

	// Initialize fdstore before any processing so
	// it is only done once.
	fdstore = make(map[FdName][]int)

	pid, err := strconv.Atoi(osGetenv("LISTEN_PID"))
	if err != nil || pid != osGetpid() {
		return
	}

	nfds, err := strconv.Atoi(osGetenv("LISTEN_FDS"))
	if err != nil || nfds == 0 {
		return
	}

	var names []string
	namesEnv, namesEnvExists := osLookupEnv("LISTEN_FDNAMES")
	if namesEnvExists {
		names = strings.Split(namesEnv, ":")
	} else {
		// Likely old systemd <227 (e.g. amazon-linux-2), Assume all passed
		// fds are activation sockets as a fallback.
		names = make([]string, nfds)
		for i := 0; i < nfds; i++ {
			// A generic name with .socket suffix is enough
			// to be picked up by ActivationSocketFds.
			names[i] = "activation.socket"
		}
	}

	if len(names) != nfds {
		logger.Noticef("internal error: cannot initialize fdstore: $LISTEN_FDNAMES does not match $LISTEN_FDS")
		return
	}

	for i := 0; i < nfds; i++ {
		fd := sd_LISTEN_FDS_START + i
		name := FdName(names[i])
		fdstore[name] = append(fdstore[name], fd)

		// TODO: Use raw fcntl and check for errors.
		unixCloseOnExec(fd)
	}

	// Prune unexpected file descriptors
	for name, fds := range fdstore {
		shouldRemove := false
		if err := name.validate(); err != nil {
			logger.Noticef("unexpected fdstore entry %q found: %v", name, err)
			shouldRemove = true
		}
		// Only activation sockets can be associated with multiple fds.
		if !name.isSocket() && len(fds) != 1 {
			logger.Noticef("unexpected fdstore entry %[1]q found: %[1]q has more than one fd", name)
			shouldRemove = true
		}
		if shouldRemove {
			logger.Noticef("removing unexpected fdstore entry %q", name)
			if err := removeUnlocked(name); err != nil {
				logger.Noticef("internal error: cannot remove fdstore entry %q: %v\n", name, err)
				continue
			}
		}
	}

	return
}

// Remove removes file descriptors from systemd given their name.
// Remove cannot remove activation sockets.
func Remove(name FdName) (err error) {
	initFdstore()

	if name.isSocket() {
		// Activation sockets can only be passed down from systemd
		// i.e. file descriptors whose name has a ".socket" suffix
		return fmt.Errorf("cannot remove file descriptor from fdstore: sockets cannot be removed")
	}

	mu.Lock()
	defer mu.Unlock()
	return removeUnlocked(name)
}

func removeUnlocked(name FdName) (err error) {
	// FDSTOREREMOVE=1 was added in systemd v236
	//
	// https://www.freedesktop.org/software/systemd/man/latest/sd_pid_notify_with_fds.html#FDSTOREREMOVE=1
	if err := systemd.EnsureAtLeast(236); err != nil {
		return fmt.Errorf("cannot remove file descriptor from fdstore: %v", err)
	}

	state := fmt.Sprintf("FDSTOREREMOVE=1\nFDNAME=%s", name)
	if err := sdNotify(state); err != nil {
		return err
	}

	var closeErrs []error
	for _, fd := range fdstore[name] {
		if err := unixClose(fd); err != nil {
			// record error and keep going
			closeErrs = append(closeErrs, err)
		}
	}

	delete(fdstore, name)
	return strutil.JoinErrors(closeErrs...)
}

// Get retrieves file descriptors passed from systemd by their name.
// close-on-exec is set on the returned file descriptor. -1 is returned
// if no matching file descriptor is found or if the passed name
// corresponds to a socket (i.e. ends in ".socket"). To get activation
// sockets use fdstore.ActivationSocketFds() instead.
func Get(name FdName) (fd int) {
	initFdstore()

	mu.RLock()
	defer mu.RUnlock()

	if name.isSocket() {
		// Activation socket file descriptors should be accessed
		// through ActivationSocketFds.
		return -1
	}

	fds := fdstore[name]
	if len(fds) != 1 {
		return -1
	}
	return fds[0]
}

// Add passes a file descriptor to systemd associated with a name
// to reuse it across snapd restarts.
//
//   - The file descriptors can be retrieved by calling Get().
//   - Only a single file descriptor can associated with a FdName.
func Add(name FdName, fd int) error {
	initFdstore()

	// FDNAME=... was added in systemd v233, but for the sake
	// of being consistent with removal (FDSTOREREMOVE=1 was
	// added in systemd v236), require at least systemd v236.
	//
	// https://www.freedesktop.org/software/systemd/man/latest/sd_pid_notify_with_fds.html#FDNAME=%E2%80%A6
	if err := systemd.EnsureAtLeast(236); err != nil {
		return fmt.Errorf("cannot add file descriptor to fdstore: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if err := name.validate(); err != nil {
		return fmt.Errorf("cannot add file descriptor to fdstore: %v", err)
	}
	if name.isSocket() {
		// Activation sockets can only be passed down from systemd
		// i.e. file descriptors whose name has a ".socket" suffix
		return fmt.Errorf("cannot add file descriptor to fdstore: sockets are not allowed")
	}
	if len(fdstore[name]) != 0 {
		return fmt.Errorf("cannot add file descriptor to fdstore: %q already exists", name)
	}

	state := fmt.Sprintf("FDSTORE=1\nFDNAME=%s", name)
	if err := sdNotifyWithFds(state, fd); err != nil {
		return fmt.Errorf("cannot add file descriptor to fdstore: %v", err)
	}

	fdstore[name] = []int{fd}
	return nil
}

// ActivationSocketFds returns activation socket file descriptors
// that were passed from systemd. Only sockets whose name has a
// ".socket" suffix are returned.
func ActivationSocketFds() (socketFds map[string][]int) {
	initFdstore()

	mu.RLock()
	defer mu.RUnlock()

	socketFds = make(map[string][]int)
	// The file descriptor name defaults to the name of the socket
	// unit (including its .socket suffix), unless it was explicitly
	// assigned by setting `FileDescriptorName=` on the socket unit.
	//
	// `FileDescriptorName=` was added in systemd version 227.
	for name, fds := range fdstore {
		if name.isSocket() {
			socketFds[string(name)] = append(socketFds[string(name)], fds...)
		}
	}

	return socketFds
}
