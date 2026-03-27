// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025-2026 Canonical Ltd
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
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/systemd"
	"golang.org/x/sys/unix"
)

const sd_LISTEN_FDS_START = 3

type FdName string

const (
	FdNameMemfdSecretState FdName = "memfd-secret-state"
)

// All names that are not sockets that are maintained within snapd.
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
	unixCloseOnExec = unix.CloseOnExec
	unixDup         = unix.Dup
	sdNotify        = systemd.SdNotify
	sdNotifyWithFds = systemd.SdNotifyWithFds
	netFileListener = net.FileListener
)

// Note: os.File is used to wrap raw fds so that the
// underlying fds are impicitly closed by finalizer
// for os.File, so no need for extra tracking.
var fdstore map[FdName][]*os.File
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
	fdstore = make(map[FdName][]*os.File)

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
			// to be picked up by ActivationListeners.
			names[i] = fmt.Sprintf("activation-fd-%d.socket", i)
		}
	}

	if len(names) != nfds {
		logger.Noticef("internal error: cannot initialize fdstore: $LISTEN_FDNAMES does not match $LISTEN_FDS")
		return
	}

	for i := 0; i < nfds; i++ {
		fd := sd_LISTEN_FDS_START + i
		name := FdName(names[i])
		fdstore[name] = append(fdstore[name], os.NewFile(uintptr(fd), string(name)))

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
			if err := remove(name); err != nil {
				logger.Noticef("internal error: cannot remove fdstore entry %q: %v", name, err)
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
	return remove(name)
}

// remove file descriptors from systemd given their name.
//
// Caller must hold the fdstore lock.
func remove(name FdName) (err error) {
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

	// Note: Removing the all references of os.File will impicitly
	// close opened fds by finalizer for os.File so no need to
	// explicitly call close.
	delete(fdstore, name)
	return nil
}

// Get retrieves file descriptor passed from systemd by its name.
// close-on-exec is set on the returned file descriptor. An error is
// returned if no matching file descriptor is found, if more than one
// matching file descriptors are found or if the passed name corresponds
// to a socket (i.e. ends in ".socket"). To get activation sockets use
// fdstore.ActivationListeners() instead.
//
// It is the caller's responsibility to close f when finished.
func Get(name FdName) (f *os.File, retErr error) {
	initFdstore()

	mu.RLock()
	defer mu.RUnlock()

	errPrefix := fmt.Sprintf("cannot get file descriptor named %q", name)

	if name.isSocket() {
		// Activation socket file descriptors should be accessed
		// through ActivationListeners.
		return nil, fmt.Errorf("internal error: %s: socket found, use ActivationListeners instead", errPrefix)
	}

	fds := fdstore[name]
	if len(fds) != 1 {
		return nil, fmt.Errorf("%s: no matching file descriptor found", errPrefix)
	} else if len(fds) > 1 {
		return nil, fmt.Errorf("%s: found more than one matching file descriptors", errPrefix)
	}

	duplicatedFd, err := unixDup(int(fds[0].Fd()))
	if err != nil {
		return nil, err
	}
	unixCloseOnExec(duplicatedFd)
	// Currently no errors are returned below, but wrapping fd
	// with os.File is a safety measure in case some error is
	// returned below in the future so the finalizer would
	// close the duplicated fd implicitly.
	f = os.NewFile(uintptr(duplicatedFd), string(name))

	return f, nil
}

// Add passes a file descriptor to systemd associated with a name
// to reuse it across snapd restarts.
//
//   - The file descriptors can be retrieved by calling Get().
//   - Only a single file descriptor can associated with a FdName.
//
// It is the caller's responsibility to close f when finished.
func Add(name FdName, f *os.File) (retErr error) {
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

	duplicatedFd, err := unixDup(int(f.Fd()))
	if err != nil {
		return err
	}
	unixCloseOnExec(duplicatedFd)
	// Wrapping fd with os.File so that if some error is
	// returned below, the finalizer for os.File would
	// close the duplicated fd implicitly.
	duplicatedFile := os.NewFile(uintptr(duplicatedFd), string(name))

	state := fmt.Sprintf("FDSTORE=1\nFDNAME=%s", name)
	if err := sdNotifyWithFds(state, duplicatedFd); err != nil {
		return fmt.Errorf("cannot add file descriptor to fdstore: %v", err)
	}

	fdstore[name] = []*os.File{duplicatedFile}
	return nil
}

// ActivationListeners returns activation listeners that were passed
// from systemd. Only sockets whose name has a ".socket" suffix are
// returned.
//
// It is the caller's responsibility to close returned listeners when finished.
func ActivationListeners() (listeners []net.Listener, retErr error) {
	initFdstore()

	mu.RLock()
	defer mu.RUnlock()

	// The file descriptor name defaults to the name of the socket
	// unit (including its .socket suffix), unless it was explicitly
	// assigned by setting `FileDescriptorName=` on the socket unit.
	//
	// `FileDescriptorName=` was added in systemd version 227.
	for name, fds := range fdstore {
		if name.isSocket() {
			for _, fd := range fds {
				// net.FileListener duplicates the underlying fd, so the
				// internally tracked fd is safe even if caller closed
				// the listener.
				listener, err := netFileListener(fd)
				if err != nil {
					return nil, err
				}
				listeners = append(listeners, listener)
			}
		}
	}

	return listeners, nil
}
