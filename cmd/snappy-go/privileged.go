package main

import (
	"errors"
	"os"
	"syscall"
)

var (
	// ErrAlreadyLocked is returned when an attempts is made to lock an
	// already-locked FileLock.
	ErrAlreadyLocked = errors.New("already locked")

	// ErrNotLocked is returned when an attempts is made to unlock an
	// unlocked FileLock.
	ErrNotLocked = errors.New("not locked")
)

// Returns name of lockfile created to serialise privileged operations
var lockfileName = func() string {
	return "/run/snappy.lock"
}

// FileLock is a Lock file object used to serialise access for
// privileged operations.
type FileLock struct {
	Filename string
	realFile *os.File
}

// Privileged type that encapsulates everything needed to run a
// privileged operation.
type Privileged struct {
	lock *FileLock
}

// Determine if caller is running as the superuser
var isRoot = func() bool {
	return syscall.Getuid() == 0
}

// NewFileLock creates a new lock object (but does not lock it).
func NewFileLock(path string) *FileLock {
	return &FileLock{Filename: path}
}

// NewPrivileged should be called when starting a privileged operation.
func NewPrivileged() (*Privileged, error) {

	if !isRoot() {
		// FIXME: return ErrNeedRoot
		return nil, errors.New("command requires sudo (root)")
	}

	p := new(Privileged)
	p.lock = NewFileLock(lockfileName())

	return p, p.lock.Lock()
}

// Stop should be called to signifiy that all privileged operations have
// completed.
func (p *Privileged) Stop() error {
	if !isRoot() {
		// FIXME: return ErrNeedRoot
		return errors.New("command requires sudo (root)")
	}
	return p.lock.Unlock()
}

// Lock the FileLock object.
// Returns ErrAlreadyLocked if an existing lock is in place.
func (l *FileLock) Lock() error {

	var err error

	// XXX: don't try to create exclusively - we care if the file failed to
	// be created, but we don't care if it already existed as the lock _on_ the
	// file is the most important thing.
	flags := (os.O_CREATE | os.O_WRONLY)

	f, err := os.OpenFile(l.Filename, flags, 0600)
	if err != nil {
		return err
	}
	l.realFile = f

	// Note: we don't want to block if the lock is already held.
	how := (syscall.LOCK_EX | syscall.LOCK_NB)

	if err = syscall.Flock(int(l.realFile.Fd()), how); err != nil {
		return ErrAlreadyLocked
	}

	return nil
}

// Unlock the FileLock object.
// Returns ErrNotLocked if no existing lock is in place.
func (l *FileLock) Unlock() error {
	if err := syscall.Flock(int(l.realFile.Fd()), syscall.LOCK_UN); err != nil {
		return ErrNotLocked
	}

	// unlink first
	if err := os.Remove(l.Filename); err != nil {
		return err
	}

	if err := l.realFile.Close(); err != nil {
		return err
	}

	return nil
}
