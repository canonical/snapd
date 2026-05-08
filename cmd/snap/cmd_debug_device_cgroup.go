// -*- Mode: Go; indent-tabs-mode: t; tab-width: 4 -*-
//go:build !darwin

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

package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/jessevdk/go-flags"
	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/sandbox/ebpf"
)

var shortDeviceCgroupHelp = i18n.G("Show devices allowed in a snap's device cgroup")

var longDeviceCgroupHelp = i18n.G(`
The device-cgroup command shows the devices currently allowed in
the device cgroup of a snap. On cgroup v1, this reads the devices.list
file. On cgroup v2, this reads the BPF device hash map.

This command requires root privileges.
`)

type cmdDeviceCgroup struct {
	Log        bool `long:"log"`
	Discard    bool `long:"discard"`
	Positional struct {
		Snap string `positional-arg-name:"<snap>" required:"yes"`
	} `positional-args:"yes"`
}

func init() {
	addDebugCommand("device-cgroup", shortDeviceCgroupHelp, longDeviceCgroupHelp, func() flags.Commander {
		return &cmdDeviceCgroup{}
	}, nil, nil)
}

func (cmd *cmdDeviceCgroup) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	snapName := cmd.Positional.Snap

	if cmd.Log || cmd.Discard {
		cgroupVer, err := cgroup.Version()
		if err != nil {
			return fmt.Errorf("cannot determine cgroup version: %v", err)
		}
		if cgroupVer != cgroup.V2 {
			return fmt.Errorf("this action requires cgroup v2 support")
		}
	}

	tags, err := cgroup.FindActiveDeviceMediationForSnap(snapName)
	if err != nil {
		return err
	}
	if len(tags) == 0 {
		return fmt.Errorf("no device cgroup found for snap %q", snapName)
	}

	if cmd.Log && cmd.Discard {
		return fmt.Errorf("--log and --discard cannot be used together")
	}

	if cmd.Log {
		return monitor(snapName, tags)
	}

	if cmd.Discard {
		return discard(tags)
	}

	return showDevices(tags)
}

func showDevices(tags []string) error {
	w := tabWriter()
	defer w.Flush()

	for _, tag := range tags {
		fmt.Fprintf(w, "Security tag: %s\n", tag)
		entries, err := cgroup.ListMediatedDevicesForSecurityTag(tag)
		if err != nil {
			fmt.Fprintf(w, "  error: %v\n", err)
		} else {
			printDeviceEntries(w, entries)
		}
		fmt.Fprintln(w)
	}

	return nil
}

// devKey identifies a device by type and major:minor numbers.
type devKey struct {
	devType byte
	major   uint32
	minor   uint32
}

// wellKnownDevNodes maps well-known Linux device (major,minor) pairs to their
// /dev paths. Sourced from udev-support.c (devices always allowed for snaps).
var wellKnownDevNodes = map[devKey]string{
	{'c', 1, 3}: "/dev/null",
	{'c', 1, 5}: "/dev/zero",
	{'c', 1, 7}: "/dev/full",
	{'c', 1, 8}: "/dev/random",
	{'c', 1, 9}: "/dev/urandom",
	{'c', 5, 0}: "/dev/tty",
	{'c', 5, 1}: "/dev/console",
	{'c', 5, 2}: "/dev/ptmx",
}

// resolveDevNode tries to find the /dev path for a device with the given
// major:minor numbers. Returns empty string if not found.
func resolveDevNode(devType byte, major, minor uint32) string {
	if path, ok := wellKnownDevNodes[devKey{devType, major, minor}]; ok {
		return path
	}
	wantRdev := unix.Mkdev(major, minor)

	var result string

	err := filepath.WalkDir("/dev", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		// Filter by device type cheaply using DirEntry.Type()
		mode := d.Type()
		isChar := mode&os.ModeDevice != 0 && mode&os.ModeCharDevice != 0
		isBlock := mode&os.ModeDevice != 0 && mode&os.ModeCharDevice == 0
		if devType == 'c' && !isChar {
			return nil
		}
		if devType == 'b' && !isBlock {
			return nil
		}
		// Only stat device nodes that match the type to get Rdev
		info, err := d.Info()
		if err != nil {
			return nil
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return nil
		}
		if stat.Rdev == wantRdev {
			result = path
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil && err != filepath.SkipDir {
		return ""
	}
	return result
}

func printDeviceEntries(w *tabwriter.Writer, entries []cgroup.DeviceEntry) {
	// Sort by type, then major, then minor (lexicographic for wildcard compat)
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].DevType != entries[j].DevType {
			return entries[i].DevType < entries[j].DevType
		}
		if entries[i].Major != entries[j].Major {
			return entries[i].Major < entries[j].Major
		}
		return entries[i].Minor < entries[j].Minor
	})

	fmt.Fprintf(w, "TYPE\tMAJOR:MINOR\tACCESS\tDEVICE\n")

	formatNodeNumber := func(n uint32) string {
		if n == cgroup.AccessAny {
			return "*"
		}
		return fmt.Sprintf("%d", n)
	}

	for _, e := range entries {
		devNode := "-"
		if e.Major != cgroup.AccessAny && e.Minor != cgroup.AccessAny {
			// It only makes sense to find matching device if we have exact
			// major/minor values
			if n := resolveDevNode(e.DevType, e.Major, e.Minor); n != "" {
				devNode = n
			}
		}
		fmt.Fprintf(w, "%c\t%s:%s\t%s\t%s\n", e.DevType,
			formatNodeNumber(e.Major), formatNodeNumber(e.Minor),
			e.Access, devNode)
	}
}

func monitor(snapName string, tags []string) error {
	// Open ring buffers for all security tags
	type tagReader struct {
		tag    string
		reader *ebpf.DeviceDenyLogReader
	}

	var readers []tagReader

	for _, tag := range tags {
		reader, err := ebpf.OpenDeviceDenyLog(tag)
		if err != nil {
			fmt.Fprintf(Stderr, "warning: cannot open deny log for %s: %v\n", tag, err)
			continue
		}
		readers = append(readers, tagReader{tag: tag, reader: reader})
	}

	if len(readers) == 0 {
		return fmt.Errorf("no deny ring buffers available for snap %q (kernel may not support BPF ring buffers)", snapName)
	}

	defer func() {
		for _, r := range readers {
			r.reader.Close()
		}
	}()

	fmt.Fprintf(Stdout, "Monitoring device access denials for snap %q (Ctrl+C to stop)...\n", snapName)

	// Handle Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Read events from all ring buffers concurrently
	type eventMsg struct {
		tag   string
		event *ebpf.DeviceDenyEvent
		err   error
	}
	eventCh := make(chan eventMsg, 16)

	for _, r := range readers {
		go func(tag string, rdr *ebpf.DeviceDenyLogReader) {
			for {
				record, err := rdr.Read()
				if err != nil {
					eventCh <- eventMsg{tag: tag, err: err}
					return
				}
				event, err := ebpf.DecodeDeviceDenyEvent(record.RawSample)
				if err != nil {
					eventCh <- eventMsg{tag: tag, err: err}
					continue
				}
				eventCh <- eventMsg{tag: tag, event: event}
			}
		}(r.tag, r.reader)
	}

	// Boot time for converting ktime to wall clock
	bootTime := osutil.BootTime()

	for {
		select {
		case <-sigCh:
			return nil
		case msg := <-eventCh:
			if msg.err != nil {
				// Reader closed or error
				fmt.Fprintf(Stderr, "warning: %s: %v\n", msg.tag, msg.err)
				continue
			}
			ev := msg.event
			var timeStr string
			if bootTime.IsZero() {
				// Cannot determine boot time, show relative offset
				timeStr = fmt.Sprintf("+%fs", float64(ev.Timestamp)/1e9)
			} else {
				wallTime := bootTime.Add(time.Duration(ev.Timestamp))
				timeStr = wallTime.Format("2006-01-02T15:04:05.000Z07:00")
			}
			devTypeStr := "char"
			if ev.DevType == 'b' {
				devTypeStr = "block"
			}
			// TODO: cache resolveDevNode results to avoid repeated /dev
			// walks for the same device in long-running log sessions.
			devNode := resolveDevNode(ev.DevType, ev.Major, ev.Minor)
			devNodeStr := ""
			if devNode != "" {
				devNodeStr = fmt.Sprintf("  %s", devNode)
			}
			fmt.Fprintf(Stdout, "%s  PID=%-6d %-15s DENY  %s %d:%d (%s)%s\n",
				timeStr,
				ev.PID,
				ev.CommString(),
				devTypeStr,
				ev.Major,
				ev.Minor,
				ev.AccessString(),
				devNodeStr,
			)
		}
	}
}

func discard(tags []string) error {
	logf := func(format string, a ...any) {
		fmt.Fprintf(Stderr, format+"\n", a...)
	}

	var firstErr error
	for _, tag := range tags {
		fmt.Fprintf(Stdout, "Discarding BPF objects for %s\n", tag)
		if err := ebpf.DiscardPinnedMaps(tag, logf); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
