// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package errtracker

import (
	"bytes"
	"crypto/md5"
	"crypto/sha512"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/snapcore/bolt"
	"gopkg.in/mgo.v2/bson"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
)

var (
	CrashDbURLBase string
	SnapdVersion   string

	// The machine-id file is at different locations depending on how the system
	// is setup. On Fedora for example /var/lib/dbus/machine-id doesn't exist
	// but we have /etc/machine-id. See
	// https://www.freedesktop.org/software/systemd/man/machine-id.html for a
	// few more details.
	machineIDs = []string{"/etc/machine-id", "/var/lib/dbus/machine-id"}

	mockedHostSnapd = ""
	mockedCoreSnapd = ""

	snapConfineProfile = "/etc/apparmor.d/usr.lib.snapd.snap-confine"

	procCpuinfo     = "/proc/cpuinfo"
	procSelfExe     = "/proc/self/exe"
	procSelfCwd     = "/proc/self/cwd"
	procSelfCmdline = "/proc/self/cmdline"

	osGetenv = os.Getenv
	timeNow  = time.Now
)

type reportsDB struct {
	db *bolt.DB

	// map of hash(dupsig) -> time-of-report
	reportedBucket *bolt.Bucket

	// time until an error report is cleaned from the database,
	// usually 7 days
	cleanupTime time.Duration
}

func hashString(s string) string {
	h := sha512.New()
	io.WriteString(h, s)
	return fmt.Sprintf("%x", h.Sum(nil))
}

func newReportsDB(fname string) (*reportsDB, error) {
	if err := os.MkdirAll(filepath.Dir(fname), 0755); err != nil {
		return nil, err
	}
	bdb, err := bolt.Open(fname, 0600, &bolt.Options{
		Timeout: 10 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	bdb.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("reported"))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		return nil
	})

	db := &reportsDB{
		db:          bdb,
		cleanupTime: time.Duration(7 * 24 * time.Hour),
	}

	return db, nil
}

func (db *reportsDB) Close() error {
	return db.db.Close()
}

// AlreadyReported returns true if an identical report has been sent recently
func (db *reportsDB) AlreadyReported(dupSig string) bool {
	// robustness
	if db == nil {
		return false
	}
	var reported []byte
	db.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("reported"))
		reported = b.Get([]byte(hashString(dupSig)))
		return nil
	})
	return len(reported) > 0
}

func (db *reportsDB) cleanupOldRecords() {
	db.db.Update(func(tx *bolt.Tx) error {
		now := time.Now()

		b := tx.Bucket([]byte("reported"))
		b.ForEach(func(dupSigHash, reportTime []byte) error {
			var t time.Time
			t.UnmarshalBinary(reportTime)

			if now.After(t.Add(db.cleanupTime)) {
				if err := b.Delete(dupSigHash); err != nil {
					return err
				}
			}
			return nil
		})
		return nil
	})
}

// MarkReported marks an error report as reported to the error tracker
func (db *reportsDB) MarkReported(dupSig string) error {
	// robustness
	if db == nil {
		return fmt.Errorf("cannot mark error report as reported with an uninitialized reports database")
	}
	db.cleanupOldRecords()

	return db.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("reported"))
		tb, err := time.Now().MarshalBinary()
		if err != nil {
			return err
		}
		return b.Put([]byte(hashString(dupSig)), tb)
	})
}

func whoopsieEnabled() bool {
	cmd := exec.Command("systemctl", "is-enabled", "whoopsie.service")
	output, _ := cmd.CombinedOutput()
	switch string(output) {
	case "enabled\n":
		return true
	case "disabled\n":
		return false
	default:
		logger.Debugf("unexpected output when checking for whoopsie.service (not installed?): %s", output)
		return true
	}
}

// distroRelease returns a distro release as it is expected by daisy.ubuntu.com
func distroRelease() string {
	ID := release.ReleaseInfo.ID
	if ID == "ubuntu" {
		ID = "Ubuntu"
	}

	return fmt.Sprintf("%s %s", ID, release.ReleaseInfo.VersionID)
}

func readMachineID() ([]byte, error) {
	for _, id := range machineIDs {
		machineID, err := ioutil.ReadFile(id)
		if err == nil {
			return bytes.TrimSpace(machineID), nil
		} else if !os.IsNotExist(err) {
			logger.Noticef("cannot read %s: %s", id, err)
		}
	}

	return nil, fmt.Errorf("cannot report: no suitable machine id file found")
}

func snapConfineProfileDigest(suffix string) string {
	profileText, err := ioutil.ReadFile(filepath.Join(dirs.GlobalRootDir, snapConfineProfile+suffix))
	if err != nil {
		return ""
	}
	// NOTE: uses md5sum for easier comparison against dpkg meta-data
	return fmt.Sprintf("%x", md5.Sum(profileText))
}

var didSnapdReExec = func() string {
	// TODO: move this into osutil.Reexeced() ?
	exe, err := os.Readlink(procSelfExe)
	if err != nil {
		return "unknown"
	}
	if strings.HasPrefix(exe, dirs.SnapMountDir) {
		return "yes"
	}
	return "no"
}

// Report reports an error with the given snap to the error tracker
func Report(snap, errMsg, dupSig string, extra map[string]string) (string, error) {
	if extra == nil {
		extra = make(map[string]string)
	}
	extra["ProblemType"] = "Snap"
	extra["Snap"] = snap

	// check if we haven't already reported this error
	db, err := newReportsDB(dirs.ErrtrackerDbDir)
	if err != nil {
		logger.Noticef("cannot open error reports database: %v", err)
	}
	defer db.Close()

	if db.AlreadyReported(dupSig) {
		return "already-reported", nil
	}

	// do the actual report
	oopsID, err := report(errMsg, dupSig, extra)
	if err != nil {
		return "", err
	}
	if err := db.MarkReported(dupSig); err != nil {
		logger.Noticef("cannot mark %s as reported", oopsID)
	}

	return oopsID, nil
}

// ReportRepair reports an error with the given repair assertion script
// to the error tracker
func ReportRepair(repair, errMsg, dupSig string, extra map[string]string) (string, error) {
	if extra == nil {
		extra = make(map[string]string)
	}
	extra["ProblemType"] = "Repair"
	extra["Repair"] = repair

	return report(errMsg, dupSig, extra)
}

func detectVirt() string {
	cmd := exec.Command("systemd-detect-virt")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func journalError() string {
	// TODO: look into using systemd package (needs refactor)

	// Before changing this line to be more consistent or nicer or anything
	// else, remember it needs to run a lot of different systemd's: today,
	// anything from 238 (on arch) to 204 (on ubuntu 14.04); this is why
	// doing the refactor to the systemd package to only worry about this in
	// there might be worth it.
	output, err := exec.Command("journalctl", "-b", "--priority=warning..err", "--lines=1000").CombinedOutput()
	if err != nil {
		if len(output) == 0 {
			return fmt.Sprintf("error: %v", err)
		}
		output = append(output, fmt.Sprintf("\nerror: %v", err)...)
	}
	return string(output)
}

func procCpuinfoMinimal() string {
	buf, err := ioutil.ReadFile(procCpuinfo)
	if err != nil {
		// if we can't read cpuinfo, we want to know _why_
		return fmt.Sprintf("error: %v", err)
	}
	idx := bytes.LastIndex(buf, []byte("\nprocessor\t:"))

	// if not found (which will happen on non-x86 architectures, which is ok
	// because they'd typically not have the same info over and over again),
	// return whole buffer; otherwise, return from just after the \n
	return string(buf[idx+1:])
}

func procExe() string {
	out, err := os.Readlink(procSelfExe)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	return out
}

func procCwd() string {
	out, err := os.Readlink(procSelfCwd)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	return out
}

func procCmdline() string {
	out, err := ioutil.ReadFile(procSelfCmdline)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	return string(out)
}

func environ() string {
	safeVars := []string{
		"SHELL", "TERM", "LANGUAGE", "LANG", "LC_CTYPE",
		"LC_COLLATE", "LC_TIME", "LC_NUMERIC",
		"LC_MONETARY", "LC_MESSAGES", "LC_PAPER",
		"LC_NAME", "LC_ADDRESS", "LC_TELEPHONE",
		"LC_MEASUREMENT", "LC_IDENTIFICATION", "LOCPATH",
	}
	unsafeVars := []string{"XDG_RUNTIME_DIR", "LD_PRELOAD", "LD_LIBRARY_PATH"}
	knownPaths := map[string]bool{
		"/snap/bin":               true,
		"/var/lib/snapd/snap/bin": true,
		"/sbin":                   true,
		"/bin":                    true,
		"/usr/sbin":               true,
		"/usr/bin":                true,
		"/usr/local/sbin":         true,
		"/usr/local/bin":          true,
		"/usr/local/games":        true,
		"/usr/games":              true,
	}

	// + 1 for PATH
	out := make([]string, 0, len(safeVars)+len(unsafeVars)+1)

	for _, k := range safeVars {
		if v := osGetenv(k); v != "" {
			out = append(out, fmt.Sprintf("%s=%s", k, v))
		}
	}

	for _, k := range unsafeVars {
		if v := osGetenv(k); v != "" {
			out = append(out, k+"=<set>")
		}
	}

	if paths := filepath.SplitList(osGetenv("PATH")); len(paths) > 0 {
		for i, p := range paths {
			p = filepath.Clean(p)
			if !knownPaths[p] {
				if strings.Contains(p, "/home") || strings.Contains(p, "/tmp") {
					p = "(user)"
				} else {
					p = "(custom)"
				}
			}
			paths[i] = p
		}
		out = append(out, fmt.Sprintf("PATH=%s", strings.Join(paths, string(filepath.ListSeparator))))
	}

	return strings.Join(out, "\n")
}

func report(errMsg, dupSig string, extra map[string]string) (string, error) {
	if CrashDbURLBase == "" {
		return "", nil
	}
	if extra == nil || extra["ProblemType"] == "" {
		return "", fmt.Errorf(`key "ProblemType" not set in %v`, extra)
	}

	if !whoopsieEnabled() {
		return "", nil
	}

	machineID, err := readMachineID()
	if err != nil {
		return "", err
	}

	identifier := fmt.Sprintf("%x", sha512.Sum512(machineID))

	crashDbUrl := fmt.Sprintf("%s/%s", CrashDbURLBase, identifier)

	hostSnapdPath := filepath.Join(dirs.DistroLibExecDir, "snapd")
	coreSnapdPath := filepath.Join(dirs.SnapMountDir, "core/current/usr/lib/snapd/snapd")
	if mockedHostSnapd != "" {
		hostSnapdPath = mockedHostSnapd
	}
	if mockedCoreSnapd != "" {
		coreSnapdPath = mockedCoreSnapd
	}
	hostBuildID, _ := osutil.ReadBuildID(hostSnapdPath)
	coreBuildID, _ := osutil.ReadBuildID(coreSnapdPath)
	if hostBuildID == "" {
		hostBuildID = "unknown"
	}
	if coreBuildID == "" {
		coreBuildID = "unknown"
	}

	report := map[string]string{
		"Architecture":       arch.UbuntuArchitecture(),
		"SnapdVersion":       SnapdVersion,
		"DistroRelease":      distroRelease(),
		"HostSnapdBuildID":   hostBuildID,
		"CoreSnapdBuildID":   coreBuildID,
		"Date":               timeNow().Format(time.ANSIC),
		"KernelVersion":      osutil.KernelVersion(),
		"ErrorMessage":       errMsg,
		"DuplicateSignature": dupSig,

		"JournalError":       journalError(),
		"ExecutablePath":     procExe(),
		"ProcCmdline":        procCmdline(),
		"ProcCpuinfoMinimal": procCpuinfoMinimal(),
		"ProcCwd":            procCwd(),
		"ProcEnviron":        environ(),
		"DetectedVirt":       detectVirt(),
		"SourcePackage":      "snapd",

		"DidSnapdReExec": didSnapdReExec(),
	}

	if desktop := osGetenv("XDG_CURRENT_DESKTOP"); desktop != "" {
		report["CurrentDesktop"] = desktop
	}

	for k, v := range extra {
		// only set if empty
		if _, ok := report[k]; !ok {
			report[k] = v
		}
	}

	// include md5 hashes of the apparmor conffile for easier debbuging
	// of not-updated snap-confine apparmor profiles
	for _, sp := range []struct {
		suffix string
		key    string
	}{
		{"", "MD5SumSnapConfineAppArmorProfile"},
		{".dpkg-new", "MD5SumSnapConfineAppArmorProfileDpkgNew"},
		{".real", "MD5SumSnapConfineAppArmorProfileReal"},
		{".real.dpkg-new", "MD5SumSnapConfineAppArmorProfileRealDpkgNew"},
	} {
		digest := snapConfineProfileDigest(sp.suffix)
		if digest != "" {
			report[sp.key] = digest
		}

	}

	// see if we run in testing mode
	if osutil.GetenvBool("SNAPPY_TESTING") {
		logger.Noticef("errtracker.Report is *not* sent because SNAPPY_TESTING is set")
		logger.Noticef("report: %v", report)
		return "oops-not-sent", nil
	}

	// send it for real
	reportBson, err := bson.Marshal(report)
	if err != nil {
		return "", err
	}
	client := &http.Client{}
	req, err := http.NewRequest("POST", crashDbUrl, bytes.NewBuffer(reportBson))
	if err != nil {
		return "", err
	}
	req.Header.Add("Content-Type", "application/octet-stream")
	req.Header.Add("X-Whoopsie-Version", httputil.UserAgent())
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("cannot upload error report, return code: %d", resp.StatusCode)
	}
	oopsID, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(oopsID), nil
}
