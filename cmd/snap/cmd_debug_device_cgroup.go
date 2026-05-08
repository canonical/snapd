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
	"bufio"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/jessevdk/go-flags"
	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/sandbox/ebpf"
)

var shortDeviceCgroupHelp = i18n.G("Inspect device cgroup state for a snap")

var errSkip = errors.New("skip walk")

var longDeviceCgroupHelp = i18n.G(`
The device-cgroup command allows inspection of device cgroup filtering
state for a snap. This includes viewing the list of allowed devices and
monitoring device access denials logged by the BPF device filter.

This command requires root privileges.
`)

type cmdDeviceCgroup struct{}

type cmdDeviceCgroupDevices struct {
	Positional struct {
		Snap string `positional-arg-name:"<snap>" required:"yes"`
	} `positional-args:"yes"`
}

type cmdDeviceCgroupLog struct {
	Positional struct {
		Snap string `positional-arg-name:"<snap>" required:"yes"`
	} `positional-args:"yes"`
}

type cmdDeviceCgroupDiscard struct {
	Positional struct {
		Snap string `positional-arg-name:"<snap>" required:"yes"`
	} `positional-args:"yes"`
}

func init() {
	cmd := addDebugCommand("device-cgroup", shortDeviceCgroupHelp, longDeviceCgroupHelp, func() flags.Commander {
		return &cmdDeviceCgroup{}
	}, nil, nil)
	cmd.hidden = true
	cmd.extra = func(c *flags.Command) {
		c.AddCommand("devices", i18n.G("Show allowed devices in a snap's device cgroup"), i18n.G(`
The devices sub-command shows the devices currently allowed in
the device cgroup for a given snap. On cgroup v1, this reads the
devices.list file. On cgroup v2, this reads the BPF device hash map.

Requires root.
`), &cmdDeviceCgroupDevices{})
		c.AddCommand("log", i18n.G("Show device access denial log for a snap"), i18n.G(`
The log sub-command monitors device access denials logged by the
BPF device cgroup filter. Events include the process name, PID, device
type/major/minor, and access type.

This is only available on cgroup v2 systems with kernel 5.8+ ring buffer
support. Requires root.
`), &cmdDeviceCgroupLog{})
		c.AddCommand("discard", i18n.G("Remove pinned BPF objects for a snap"), i18n.G(`
The discard sub-command removes the pinned BPF device hash map and deny
ring buffer for a given snap. This is useful for cleaning up after a snap
has been removed or when debugging BPF state issues.

Only available on cgroup v2. Requires root.
`), &cmdDeviceCgroupDiscard{})
	}
}

func (cmd *cmdDeviceCgroup) Execute(args []string) error {
	return flag.ErrHelp
}

// findSecurityTags finds all security tags for a given snap name by looking
// at pinned BPF maps or cgroup v1 directories.
func findSecurityTags(snapName string, cgroupVer int) ([]string, error) {
	if cgroupVer == cgroup.V2 {
		return findSecurityTagsV2(snapName)
	}
	return findSecurityTagsV1(snapName)
}

func findSecurityTagsV2(snapName string) ([]string, error) {
	// Security tags in bpffs have dots replaced with underscores.
	// Pattern: snap_<name>_<app> (ring buffers have @denylog suffix)
	prefix := fmt.Sprintf("snap_%s_", snapName)
	entries, err := os.ReadDir(dirs.SnapBPFFSDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read %s: %v", dirs.SnapBPFFSDir, err)
	}
	var tags []string
	for _, e := range entries {
		name := e.Name()
		// Skip ring buffer files (@denylog suffix)
		if strings.HasSuffix(name, "@denylog") {
			continue
		}
		if strings.HasPrefix(name, prefix) {
			tags = append(tags, bpfNameToSecurityTag(name))
		}
	}
	sort.Strings(tags)
	return tags, nil
}

// bpfNameToSecurityTag converts a bpffs entry name back to a security
// tag. The bpffs name has the format snap_<name>_<app> where dots in the
// original tag were replaced with underscores. Since snap and app names
// cannot contain underscores but instance keys use a single underscore
// as separator (e.g. snapname_instancekey), we split on the first and
// last underscore to reconstruct the tag correctly.
func bpfNameToSecurityTag(name string) string {
	first := strings.Index(name, "_")
	last := strings.LastIndex(name, "_")
	if first < 0 || first == last {
		return strings.ReplaceAll(name, "_", ".")
	}
	return name[:first] + "." + name[first+1:last] + "." + name[last+1:]
}

func findSecurityTagsV1(snapName string) ([]string, error) {
	cgroupDevicesPath := "/sys/fs/cgroup/devices"
	prefix := fmt.Sprintf("snap.%s.", snapName)
	entries, err := os.ReadDir(cgroupDevicesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read %s: %v", cgroupDevicesPath, err)
	}
	var tags []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), prefix) {
			tags = append(tags, e.Name())
		}
	}
	sort.Strings(tags)
	return tags, nil
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
	_ = filepath.Walk("/dev", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return nil
		}
		mode := info.Mode()
		isChar := mode&os.ModeCharDevice != 0 && mode&os.ModeDevice != 0
		isBlock := mode&os.ModeDevice != 0 && !isChar
		if devType == 'c' && !isChar {
			return nil
		}
		if devType == 'b' && !isBlock {
			return nil
		}
		if stat.Rdev == wantRdev {
			result = path
			return errSkip
		}
		return nil
	})
	return result
}

func (cmd *cmdDeviceCgroupDevices) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	snapName := cmd.Positional.Snap

	cgroupVer, err := cgroup.Version()
	if err != nil {
		return fmt.Errorf("cannot determine cgroup version: %v", err)
	}

	tags, err := findSecurityTags(snapName, cgroupVer)
	if err != nil {
		return err
	}
	if len(tags) == 0 {
		return fmt.Errorf("no device cgroup found for snap %q", snapName)
	}

	w := tabWriter()
	defer w.Flush()

	for _, tag := range tags {
		fmt.Fprintf(w, "Security tag: %s\n", tag)
		var entries []deviceEntry
		var err error
		if cgroupVer == cgroup.V2 {
			entries, err = collectDevicesV2(tag)
		} else {
			entries, err = collectDevicesV1(tag)
		}
		if err != nil {
			fmt.Fprintf(w, "  error: %v\n", err)
		} else {
			printDeviceEntries(w, entries)
		}
		fmt.Fprintln(w)
	}

	return nil
}

// deviceEntry represents a single device entry for display.
type deviceEntry struct {
	devType byte   // 'c', 'b', or 'a'
	major   string // numeric or "*"
	minor   string // numeric or "*"
	access  string // e.g. "rwm"
	devNode string // resolved path or "-"
}

func printDeviceEntries(w *tabwriter.Writer, entries []deviceEntry) {
	// Sort by type, then major, then minor (lexicographic for wildcard compat)
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].devType != entries[j].devType {
			return entries[i].devType < entries[j].devType
		}
		if entries[i].major != entries[j].major {
			return entries[i].major < entries[j].major
		}
		return entries[i].minor < entries[j].minor
	})

	fmt.Fprintf(w, "TYPE\tMAJOR:MINOR\tACCESS\tDEVICE\n")
	for _, e := range entries {
		fmt.Fprintf(w, "%c\t%s:%s\t%s\t%s\n", e.devType, e.major, e.minor, e.access, e.devNode)
	}
}

func collectDevicesV2(securityTag string) ([]deviceEntry, error) {
	m, err := ebpf.LoadDeviceMap(securityTag)
	if err != nil {
		return nil, err
	}
	defer m.Close()

	var entries []deviceEntry

	err = ebpf.IterateDeviceMap(m, func(key ebpf.DeviceKey) error {
		minor := fmt.Sprintf("%d", key.Minor)
		if key.Minor == math.MaxUint32 {
			minor = "*"
		}
		devNode := "-"
		if key.Minor != math.MaxUint32 {
			if n := resolveDevNode(key.Type, key.Major, key.Minor); n != "" {
				devNode = n
			}
		}
		entries = append(entries, deviceEntry{
			devType: key.Type,
			major:   fmt.Sprintf("%d", key.Major),
			minor:   minor,
			access:  "rwm",
			devNode: devNode,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("cannot iterate device map: %v", err)
	}

	return entries, nil
}

func collectDevicesV1(securityTag string) ([]deviceEntry, error) {
	path := filepath.Join("/sys/fs/cgroup/devices", securityTag, "devices.list")
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %v", path, err)
	}
	defer f.Close()

	var entries []deviceEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Format: "c 1:3 rwm" or "a *:* rwm"
		parts := strings.Fields(line)
		if len(parts) != 3 {
			continue
		}
		devTypeStr := parts[0]
		majMin := parts[1]
		access := parts[2]

		majMinParts := strings.SplitN(majMin, ":", 2)
		if len(majMinParts) != 2 {
			continue
		}
		major := majMinParts[0]
		minor := majMinParts[1]

		devNode := "-"
		if devTypeStr != "a" && major != "*" && minor != "*" {
			var maj, min uint32
			if _, err := fmt.Sscanf(majMin, "%d:%d", &maj, &min); err == nil {
				var devType byte
				if devTypeStr == "c" {
					devType = 'c'
				} else {
					devType = 'b'
				}
				if n := resolveDevNode(devType, maj, min); n != "" {
					devNode = n
				}
			}
		}
		entries = append(entries, deviceEntry{
			devType: devTypeStr[0],
			major:   major,
			minor:   minor,
			access:  access,
			devNode: devNode,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

func (cmd *cmdDeviceCgroupLog) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	snapName := cmd.Positional.Snap

	cgroupVer, err := cgroup.Version()
	if err != nil {
		return fmt.Errorf("cannot determine cgroup version: %v", err)
	}
	if cgroupVer != cgroup.V2 {
		return fmt.Errorf("device cgroup deny logging is not available on cgroup v1")
	}

	tags, err := findSecurityTagsV2(snapName)
	if err != nil {
		return err
	}
	if len(tags) == 0 {
		return fmt.Errorf("no device cgroup found for snap %q", snapName)
	}

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

func (cmd *cmdDeviceCgroupDiscard) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	snapName := cmd.Positional.Snap

	cgroupVer, err := cgroup.Version()
	if err != nil {
		return fmt.Errorf("cannot determine cgroup version: %v", err)
	}
	if cgroupVer != cgroup.V2 {
		return fmt.Errorf("discard is only supported on cgroup v2")
	}

	tags, err := findSecurityTagsV2(snapName)
	if err != nil {
		return err
	}
	if len(tags) == 0 {
		fmt.Fprintf(Stderr, "no pinned BPF objects found for snap %q\n", snapName)
		return nil
	}

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

