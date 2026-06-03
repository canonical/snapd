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
	"errors"
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

// FdName uniquely identifies file descriptors passed from systemd to snapd.
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
	osGetpid        = os.Getpid
	osFileClose     = (*os.File).Close
	unixCloseOnExec = unix.CloseOnExec
	unixDup         = unix.Dup
	sdNotify        = systemd.SdNotify
	sdNotifyWithFds = systemd.SdNotifyWithFds
	netFileListener = net.FileListener
)

// Note: os.File is used to wrap raw fds so that the
// underlying fds are implicitly closed by finalizer
// for os.File, so no need for extra tracking.
var fdstore map[FdName][]*os.File
var mu sync.RWMutex

// sd_LISTEN_FDS_START is the starting file descriptor number for file descriptors
// passed from systemd.
const sd_LISTEN_FDS_START = 3

func initFdstore() {
	mu.Lock()
	defer mu.Unlock()

	if fdstore != nil {
		// fdstore map is lazily loaded once
		return
	}

	// Make sure initialization only happens once, only here.
	defer func() {
		os.Unsetenv("LISTEN_PID")
		os.Unsetenv("LISTEN_FDS")
		os.Unsetenv("LISTEN_FDNAMES")
	}()

	// Initialize fdstore before any processing so
	// it is only done once.
	fdstore = make(map[FdName][]*os.File)

	pid, err := strconv.Atoi(os.Getenv("LISTEN_PID"))
	if err != nil || pid != osGetpid() {
		return
	}

	nfds, err := strconv.Atoi(os.Getenv("LISTEN_FDS"))
	if err != nil || nfds == 0 {
		return
	}

	var names []string
	namesEnv, namesEnvExists := os.LookupEnv("LISTEN_FDNAMES")
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
		// We only allow activation sockets to be associated with multiple fds.
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
}

var ErrUnsupportedSystemdVersion = errors.New("unsupported systemd version")
var ErrNotFound = errors.New("file descriptor not found")

func checkSystemdVersion() error {
	// FDNAME=... was added in systemd v233, but for the sake
	// of being consistent with removal (FDSTOREREMOVE=1 was
	// added in systemd v236), require at least systemd v236.
	//
	// https://www.freedesktop.org/software/systemd/man/latest/sd_pid_notify_with_fds.html#FDNAME=%E2%80%A6
	if err := systemd.EnsureAtLeast(236); err != nil {
		return fmt.Errorf("%w: %v", ErrUnsupportedSystemdVersion, err)
	}
	return nil
}

// Remove removes file descriptors from systemd given their name.
// Remove cannot remove activation sockets.
func Remove(name FdName) error {
	initFdstore()

	if err := checkSystemdVersion(); err != nil {
		return fmt.Errorf("cannot remove file descriptor from fdstore: %w", err)
	}

	if name.isSocket() {
		// Activation sockets can only be passed down from systemd
		// i.e. file descriptors whose name has a ".socket" suffix
		return fmt.Errorf("cannot remove file descriptor from fdstore: sockets cannot be removed")
	}

	mu.Lock()
	defer mu.Unlock()

	if fdstore[name] == nil {
		return fmt.Errorf("cannot remove file descriptor from fdstore: %w", ErrNotFound)
	}

	return remove(name)
}

// remove file descriptors from systemd given their name.
//
// Caller must hold the fdstore lock.
func remove(name FdName) error {
	state := fmt.Sprintf("FDSTOREREMOVE=1\nFDNAME=%s", name)
	if err := sdNotify(state); err != nil {
		return err
	}

	for _, f := range fdstore[name] {
		osFileClose(f)
	}
	delete(fdstore, name)
	return nil
}

func duplicateFile(name FdName, f *os.File) (*os.File, error) {
	duplicatedFd, err := unixDup(int(f.Fd()))
	if err != nil {
		return nil, err
	}
	// TODO: Use raw fcntl and check for errors.
	unixCloseOnExec(duplicatedFd)

	// Wrapping fd with os.File is a safety measure so that the finalizer
	// would close the duplicated fd implicitly if it goes out of scope.
	return os.NewFile(uintptr(duplicatedFd), string(name)), nil
}

// Get retrieves a duplicate of the file descriptor passed from systemd by
// its name. close-on-exec is set on the returned file descriptor. An error
// matching ErrNotFound is returned if no matching file descriptor is found.
// Passed name cannot be a socket (i.e. cannot end in ".socket"), for
// activation sockets use ActivationListeners() instead.
//
// The fdstore holds a copy of the file descriptor, the caller needs to
// call Remove() on top of closing all privately held references in order
// to release all resources associated with a given fd.
func Get(name FdName) (*os.File, error) {
	initFdstore()

	if err := checkSystemdVersion(); err != nil {
		return nil, fmt.Errorf("cannot get file descriptor from fdstore: %w", err)
	}

	errPrefix := fmt.Sprintf("cannot get file descriptor named %q", name)

	if name.isSocket() {
		// Activation socket file descriptors should be accessed
		// through ActivationListeners.
		return nil, fmt.Errorf("internal error: %s: socket found, use ActivationListeners instead", errPrefix)
	}

	mu.RLock()
	defer mu.RUnlock()

	fds := fdstore[name]
	if len(fds) == 0 {
		return nil, fmt.Errorf("%s: %w", errPrefix, ErrNotFound)
	}

	return duplicateFile(name, fds[0])
}

// Add passes a file descriptor to systemd associated with a name
// to reuse it across snapd restarts.
//
//   - The file descriptors can be retrieved by calling Get().
//   - Only a single file descriptor can associated with a FdName.
//
// Maintains a copy of the underlying file descriptor internally. It
// is the caller's responsibility to close f when finished.
func Add(name FdName, f *os.File) error {
	initFdstore()

	if err := checkSystemdVersion(); err != nil {
		return fmt.Errorf("cannot add file descriptor to fdstore: %w", err)
	}

	if err := name.validate(); err != nil {
		return fmt.Errorf("cannot add file descriptor to fdstore: %v", err)
	}
	if name.isSocket() {
		// Activation sockets can only be passed down from systemd
		// i.e. file descriptors whose name has a ".socket" suffix
		return fmt.Errorf("cannot add file descriptor to fdstore: sockets are not allowed")
	}

	mu.Lock()
	defer mu.Unlock()

	if len(fdstore[name]) != 0 {
		return fmt.Errorf("cannot add file descriptor to fdstore: %q already exists", name)
	}

	duplicatedFile, err := duplicateFile(name, f)
	if err != nil {
		return err
	}

	state := fmt.Sprintf("FDSTORE=1\nFDNAME=%s", name)
	if err := sdNotifyWithFds(state, duplicatedFile); err != nil {
		osFileClose(duplicatedFile) // clean up the duplicated fd
		return fmt.Errorf("cannot add file descriptor to fdstore: %v", err)
	}

	fdstore[name] = []*os.File{duplicatedFile}
	return nil
}

// ActivationListeners returns activation listeners that were passed
// from systemd. Only sockets whose name has a ".socket" suffix are
// returned. Order of returned listeners is not deterministic.
//
// It is the caller's responsibility to close returned listeners when finished.
func ActivationListeners() (listeners []net.Listener, retErr error) {
	initFdstore()

	mu.RLock()
	defer mu.RUnlock()

	defer func() {
		// Clean up collected listeners on error since net.FileListener
		// duplicates the underlying fd.
		if retErr != nil && len(listeners) > 0 {
			for _, l := range listeners {
				l.Close()
			}
		}
	}()

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
