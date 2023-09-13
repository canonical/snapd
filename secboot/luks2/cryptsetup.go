// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package luks2

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"github.com/snapcore/snapd/osutil"

	"golang.org/x/xerrors"
)

const (
	// AnySlot tells a command to automatically choose an appropriate slot
	// as opposed to hard coding one.
	AnySlot = -1
)

// cryptsetupCmd is a helper for running the cryptsetup command. If stdin is supplied, data read
// from it is supplied to cryptsetup via its stdin. If callback is supplied, it will be invoked
// after cryptsetup has started.
func cryptsetupCmd(stdin io.Reader, args ...string) error {
	cmd := exec.Command("cryptsetup", args...)
	cmd.Stdin = stdin

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cryptsetup failed with: %v", osutil.OutputErr(output, err))
	}

	return nil
}

// KDFOptions specifies parameters for the Argon2 KDF.
type KDFOptions struct {
	// TargetDuration specifies the target time for benchmarking of the
	// time and memory cost parameters. If it is zero then the cryptsetup
	// default is used. If ForceIterations is not zero then this is ignored.
	TargetDuration time.Duration

	// MemoryKiB specifies the maximum memory cost in KiB when ForceIterations
	// is zero, or the actual memory cost in KiB when ForceIterations is not zero.
	// If this is set to zero, then the cryptsetup default is used.
	MemoryKiB int

	// ForceIterations specifies the time cost. If set to zero, the time
	// and memory cost are determined by benchmarking the algorithm based on
	// the specified TargetDuration. Set to a non-zero number to force the
	// time cost to the value of this field, and the memory cost to the value
	// of MemoryKiB, disabling benchmarking.
	ForceIterations int

	// Parallel sets the maximum number of parallel threads. Cryptsetup may
	// choose a lower value based on its own maximum and the number of available
	// CPU cores.
	Parallel int
}

func (options *KDFOptions) appendArguments(args []string) []string {
	// use argon2i as the KDF
	args = append(args, "--pbkdf", "argon2i")

	switch {
	case options.ForceIterations != 0:
		// Disable benchmarking by forcing the time cost
		args = append(args,
			"--pbkdf-force-iterations", strconv.Itoa(options.ForceIterations))
	case options.TargetDuration != 0:
		args = append(args,
			"--iter-time", strconv.FormatInt(int64(options.TargetDuration/time.Millisecond), 10))
	}

	if options.MemoryKiB != 0 {
		args = append(args, "--pbkdf-memory", strconv.Itoa(options.MemoryKiB))
	}

	if options.Parallel != 0 {
		args = append(args, "--pbkdf-parallel", strconv.Itoa(options.Parallel))
	}

	return args
}

// AddKeyOptions provides the options for adding a key to a LUKS2 volume
type AddKeyOptions struct {
	// KDFOptions describes the KDF options for the new key slot.
	KDFOptions KDFOptions

	// Slot is the keyslot to use. Note that the default value is slot 0. In
	// order to automatically choose a slot, use AnySlot.
	Slot int
}

var writeExistingKeyToFifo = func(fifoPath string, existingKey []byte) error {
	f, err := os.OpenFile(fifoPath, os.O_WRONLY, 0)
	if err != nil {
		return xerrors.Errorf("cannot open FIFO for passing existing key to cryptsetup: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(existingKey); err != nil {
		return xerrors.Errorf("cannot pass existing key to cryptsetup: %w", err)
	}
	if err := f.Close(); err != nil {
		return xerrors.Errorf("cannot close write end of FIFO: %w", err)
	}
	return nil
}

// AddKey adds the supplied key in to a new keyslot for specified LUKS2 container. In order to do this,
// an existing key must be provided. The KDF for the new keyslot will be configured to use argon2i with
// the supplied benchmark time. The key will be added to the supplied slot.
//
// If options is not supplied, the default KDF benchmark time is used and the command will
// automatically choose an appropriate slot.
func AddKey(devicePath string, existingKey, key []byte, options *AddKeyOptions) error {
	if options == nil {
		options = &AddKeyOptions{Slot: AnySlot}
	}

	fifoPath, cleanupFifo, err := mkFifo()
	if err != nil {
		return xerrors.Errorf("cannot create FIFO for passing existing key to cryptsetup: %w", err)
	}
	defer cleanupFifo()

	args := []string{
		// add a new key
		"luksAddKey",
		// LUKS2 only
		"--type", "luks2",
		// read existing key from named pipe
		"--key-file", fifoPath}

	// apply KDF options
	args = options.KDFOptions.appendArguments(args)

	if options.Slot != AnySlot {
		args = append(args, "--key-slot", strconv.Itoa(options.Slot))
	}

	args = append(args,
		// container to add key to
		devicePath,
		// read new key from stdin.
		// Note that we can't supply the new key and existing key via the same channel
		// because pipes and FIFOs aren't seekable - we would need to use an actual file
		// in order to be able to do this.
		"-")

	cmd := exec.Command("cryptsetup", args...)
	cmd.Stdin = bytes.NewReader(key)

	// Writing to the fifo must happen in a go-routine as it may block
	// if the other side is not connected. Special care must be taken
	// about the cleanup.
	fifoErrCh := make(chan error)
	go func() {
		fifoErr := writeExistingKeyToFifo(fifoPath, existingKey)
		if fifoErr != nil {
			// kill to ensure cmd is not lingering
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
			// also ensure fifo is closed
			cleanupFifo()
		}
		fifoErrCh <- fifoErr
	}()

	output, cmdErr := cmd.CombinedOutput()
	if cmdErr != nil {
		// cleanupFifo will open/close the fifo to ensure the
		// writeExistingKeyToFifo() does not leak while waiting
		// for the other side of the fifo to connect (it may never
		// do)
		cleanupFifo()
	}
	fifoErr := <-fifoErrCh

	switch {
	case cmdErr != nil && (fifoErr == nil || errors.Is(fifoErr, syscall.EPIPE)):
		// cmdErr and EPIPE means the problem is with cmd, no
		// need to display the EPIPE error
		return fmt.Errorf("cryptsetup failed with: %v", osutil.OutputErr(output, cmdErr))
	case cmdErr != nil || fifoErr != nil:
		// For all other cases show a generic error message
		return fmt.Errorf("cryptsetup failed with: %v (fifo failed with: %v)", osutil.OutputErr(output, err), fifoErr)
	}

	return nil
}

// KillSlot erases the keyslot with the supplied slot number from the specified LUKS2 container.
// Note that a valid key for a remaining keyslot must be supplied, in order to prevent the last
// keyslot from being erased.
func KillSlot(devicePath string, slot int, key []byte) error {
	return cryptsetupCmd(bytes.NewReader(key), "luksKillSlot", "--type", "luks2", "--key-file", "-", devicePath, strconv.Itoa(slot))
}

// SetSlotPriority sets the priority of the keyslot with the supplied slot number on
// the specified LUKS2 container.
func SetSlotPriority(devicePath string, slot int, priority SlotPriority) error {
	return cryptsetupCmd(nil, "config", "--priority", priority.String(), "--key-slot", strconv.Itoa(slot), devicePath)
}
