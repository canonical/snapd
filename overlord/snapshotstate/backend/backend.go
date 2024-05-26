// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2020 Canonical Ltd
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

package backend

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"crypto"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/strutil"
)

const (
	archiveName  = "archive.tgz"
	metadataName = "meta.json"
	metaHashName = "meta.sha3_384"

	userArchivePrefix = "user/"
	userArchiveSuffix = ".tgz"
)

var (
	// Stop is used to ask Iter to stop iteration, without it being an error.
	Stop = errors.New("stop iteration")

	osOpen               = os.Open
	dirNames             = (*os.File).Readdirnames
	backendOpen          = Open
	timeNow              = time.Now
	snapReadSnapshotYaml = snap.ReadSnapshotYaml

	usersForUsernames = usersForUsernamesImpl
)

// LastSnapshotSetID returns the highest set id number for the snapshots stored
// in snapshots directory; set ids are inferred from the filenames.
func LastSnapshotSetID() (uint64, error) {
	dir := mylog.Check2(osOpen(dirs.SnapshotsDir))

	// no snapshots

	defer dir.Close()

	var maxSetID uint64

	var readErr error
	for readErr == nil {
		var names []string
		// note os.Readdirnames can return a non-empty names and a non-nil err
		names, readErr = dirNames(dir, 100)
		for _, name := range names {
			if ok, setID := isSnapshotFilename(name); ok {
				if setID > maxSetID {
					maxSetID = setID
				}
			}
		}
	}
	if readErr != nil && readErr != io.EOF {
		return 0, readErr
	}
	return maxSetID, nil
}

// Iter loops over all snapshots in the snapshots directory, applying the given
// function to each. The snapshot will be closed after the function returns. If
// the function returns an error, iteration is stopped (and if the error isn't
// Stop, it's returned as the error of the iterator).
func Iter(ctx context.Context, f func(*Reader) error) error {
	mylog.Check(ctx.Err())

	dir := mylog.Check2(osOpen(dirs.SnapshotsDir))

	// no dir -> no snapshots

	defer dir.Close()

	importsInProgress := map[uint64]bool{}
	var names []string
	var readErr error
	for readErr == nil && err == nil {
		names, readErr = dirNames(dir, 100)
		// note os.Readdirnames can return a non-empty names and a non-nil err
		for _, name := range names {
			mylog.Check(ctx.Err())

			// filter out non-snapshot directory entries
			ok, setID := isSnapshotFilename(name)
			if !ok {
				continue
			}
			// keep track of in-progress in a map as well
			// to avoid races. E.g.:
			// 1. The dirNnames() are read
			// 2. 99_some-snap_1.0_x1.zip is returned
			// 3. the code checks if 99_importing is there,
			//    it is so 99_some-snap is skipped
			// 4. other snapshots are examined
			// 5. in-parallel 99_importing finishes
			// 7. 99_other-snap_1.0_x1.zip is now examined
			// 8. code checks if 99_importing is there, but it
			//    is no longer there because import
			//    finished in the meantime. We still
			//    want to not call the callback with
			//    99_other-snap or the callback would get
			//    an incomplete view about 99_snapshot.
			if importsInProgress[setID] {
				continue
			}
			if importInProgressFor(setID) {
				importsInProgress[setID] = true
				continue
			}

			filename := filepath.Join(dirs.SnapshotsDir, name)
			reader, openError := backendOpen(filename, setID)
			// reader can be non-nil even when openError is not nil (in
			// which case reader.Broken will have a reason). f can
			// check and either ignore or return an error when
			// finding a broken snapshot.
			if reader != nil {
				mylog.Check(f(reader))
			} else {
				// TODO: use warnings instead
				logger.Noticef("Cannot open snapshot %q: %v.", name, openError)
			}
			if openError == nil {
				// if openError was nil the snapshot was opened and needs closing
				if closeError := reader.Close(); err == nil {
					err = closeError
				}
			}

		}
	}

	if readErr != nil && readErr != io.EOF {
		return readErr
	}

	if err == Stop {
		err = nil
	}

	return err
}

// List valid snapshots sets.
func List(ctx context.Context, setID uint64, snapNames []string) ([]client.SnapshotSet, error) {
	setshots := map[uint64][]*client.Snapshot{}
	mylog.Check(Iter(ctx, func(reader *Reader) error {
		if setID == 0 || reader.SetID == setID {
			if len(snapNames) == 0 || strutil.ListContains(snapNames, reader.Snap) {
				setshots[reader.SetID] = append(setshots[reader.SetID], &reader.Snapshot)
			}
		}
		return nil
	}))

	sets := make([]client.SnapshotSet, 0, len(setshots))
	for id, shots := range setshots {
		sort.Sort(bySnap(shots))
		sets = append(sets, client.SnapshotSet{ID: id, Snapshots: shots})
	}

	sort.Sort(byID(sets))

	return sets, err
}

// Filename of the given client.Snapshot in this backend.
func Filename(snapshot *client.Snapshot) string {
	// this _needs_ the snap name and version to be valid
	return filepath.Join(dirs.SnapshotsDir, fmt.Sprintf("%d_%s_%s_%s.zip", snapshot.SetID, snapshot.Snap, snapshot.Version, snapshot.Revision))
}

// isSnapshotFilename checks if the given filePath is a snapshot file name, i.e.
// if it starts with a numeric set id and ends with .zip extension;
// filePath can be just a file name, or a full path.
func isSnapshotFilename(filePath string) (ok bool, setID uint64) {
	fname := filepath.Base(filePath)
	// XXX: we could use a regexp here to match very precisely all the elements
	// of the filename following Filename() above, but perhaps it's better no to
	// go overboard with it in case the format evolves in the future. Only check
	// if the name starts with a set-id and ends with .zip.
	//
	// Filename is "<sid>_<snapName>_version_revision.zip", e.g. "16_snapcraft_4.2_5407.zip"
	ext := filepath.Ext(fname)
	if ext != ".zip" {
		return false, 0
	}
	parts := strings.SplitN(fname, "_", 2)
	if len(parts) != 2 {
		return false, 0
	}
	// invalid: no parts following <sid>_
	if parts[1] == ext {
		return false, 0
	}
	id := mylog.Check2(strconv.Atoi(parts[0]))

	return true, uint64(id)
}

// EstimateSnapshotSize calculates estimated size of the snapshot.
func EstimateSnapshotSize(si *snap.Info, usernames []string, dirOpts *dirs.SnapDirOptions) (uint64, error) {
	var total uint64
	calculateSize := func(path string, finfo os.FileInfo, err error) error {
		if finfo.Mode().IsRegular() {
			total += uint64(finfo.Size())
		}
		return err
	}

	visitDir := func(dir string) error {
		exists, isDir := mylog.Check3(osutil.DirExists(dir))

		if !(exists && isDir) {
			return nil
		}
		return filepath.Walk(dir, calculateSize)
	}

	for _, dir := range []string{si.DataDir(), si.CommonDataDir()} {
		mylog.Check(visitDir(dir))
	}

	users := mylog.Check2(usersForUsernames(usernames, dirOpts))

	for _, usr := range users {
		mylog.Check(visitDir(si.UserDataDir(usr.HomeDir, dirOpts)))
		mylog.Check(visitDir(si.UserCommonDataDir(usr.HomeDir, dirOpts)))

	}

	// XXX: we could use a typical compression factor here
	return total, nil
}

// Save a snapshot
func Save(ctx context.Context, id uint64, si *snap.Info, cfg map[string]interface{}, usernames []string, dynSnapshotOpts *snap.SnapshotOptions, dirOpts *dirs.SnapDirOptions) (*client.Snapshot, error) {
	mylog.Check(os.MkdirAll(dirs.SnapshotsDir, 0700))

	snapshot := &client.Snapshot{
		SetID:    id,
		Snap:     si.InstanceName(),
		SnapID:   si.SnapID,
		Revision: si.Revision,
		Version:  si.Version,
		Epoch:    si.Epoch,
		Time:     timeNow(),
		// Pass only dynamic snapshot options here. Static options are tied to the snap version
		// and should not be repeated in snapshot metadata on every save.
		Options:  dynSnapshotOpts,
		SHA3_384: make(map[string]string),
		Size:     0,
		Conf:     cfg,
		// Note: Auto is no longer set in the Snapshot.
	}

	snapshotOptions := mylog.Check2(snapReadSnapshotYaml(si))

	if dynSnapshotOpts != nil {
		mylog.Check(snapshotOptions.MergeDynamicExcludes(dynSnapshotOpts.Exclude))
	}

	aw := mylog.Check2(osutil.NewAtomicFile(Filename(snapshot), 0600, 0, osutil.NoChown, osutil.NoChown))

	// if things worked, we'll commit (and Cancel becomes a NOP)
	defer aw.Cancel()

	w := zip.NewWriter(aw)
	defer w.Close() // note this does not close the file descriptor (that's done by hand on the atomic writer, above)
	savingUserData := false
	baseDataDir := snap.BaseDataDir(si.InstanceName())
	mylog.Check(addSnapDirToZip(ctx, snapshot, w, "root", archiveName, baseDataDir, savingUserData, snapshotOptions.Exclude))

	users := mylog.Check2(usersForUsernames(usernames, dirOpts))

	savingUserData = true
	for _, usr := range users {
		snapDataDir := filepath.Dir(si.UserDataDir(usr.HomeDir, dirOpts))
		mylog.Check(addSnapDirToZip(ctx, snapshot, w, usr.Username, userArchiveName(usr), snapDataDir, savingUserData, snapshotOptions.Exclude))

	}

	metaWriter := mylog.Check2(w.Create(metadataName))

	hasher := crypto.SHA3_384.New()
	enc := json.NewEncoder(io.MultiWriter(metaWriter, hasher))
	mylog.Check(enc.Encode(snapshot))

	hashWriter := mylog.Check2(w.Create(metaHashName))

	fmt.Fprintf(hashWriter, "%x\n", hasher.Sum(nil))
	mylog.Check(w.Close())
	mylog.Check(ctx.Err())
	mylog.Check(aw.Commit())

	return snapshot, nil
}

var isTesting = snapdenv.Testing()

// addSnapDirToZip adds the 'common' and the 'rev' revisioned dir under 'snapDir'
// to the snapshot. If one doesn't exist, it's ignored. If none exists, the
// operation is skipped.
func addSnapDirToZip(ctx context.Context, snapshot *client.Snapshot, w *zip.Writer, username, entry, snapDir string, savingUserData bool, excludePaths []string) error {
	paths := mylog.Check2(pathsForSnapshot(snapDir, snapshot))

	if len(paths) == 0 {
		return nil
	}

	expandSnapDataDirs := func(varName string) string {
		// Validation of the environment variables has already been performed.
		// We just need to make sure that we consider the right variables
		// according to whether we are saving user or system data.
		switch {
		case varName == "SNAP_COMMON" && !savingUserData:
			fallthrough
		case varName == "SNAP_USER_COMMON" && savingUserData:
			return "common"
		case varName == "SNAP_DATA" && !savingUserData:
			fallthrough
		case varName == "SNAP_USER_DATA" && savingUserData:
			return snapshot.Revision.String()
		}
		// The variable specified does not match the current operating mode
		// (for example, the variable is SNAP_COMMON but we are saving user
		// data); in this case, we need to inform our caller that the returned
		// string should be ignored and not added to the "tar" parameters. In
		// order to do this, we return a "-" as a sentinel.
		return "-"
	}

	var expExcludePaths []string
	for _, excludePath := range excludePaths {
		expandedPath := os.Expand(excludePath, expandSnapDataDirs)
		// "-" is the sentinel returned by expandSnapDataDirs() if the
		// exclusion path is not relevant for the type of data being considered
		if expandedPath[0] == '-' {
			continue
		}
		expExcludePaths = append(expExcludePaths, expandedPath)
	}

	return addToZip(ctx, snapshot, w, username, entry, paths, expExcludePaths)
}

// addToZip adds 'paths' to the snapshot. tar will change into the paths' parent
// directory before creating the archive so that parent dirs are not added.
func addToZip(ctx context.Context, snapshot *client.Snapshot, w *zip.Writer, username, entry string, paths []string, excludePaths []string) error {
	archiveWriter := mylog.Check2(w.CreateHeader(&zip.FileHeader{Name: entry}))

	tarArgs := []string{
		"--create",
		"--sparse", "--gzip",
		"--format", "gnu",
		"--anchored",
		"--no-wildcards-match-slash",
	}

	for _, path := range excludePaths {
		tarArgs = append(tarArgs, fmt.Sprintf("--exclude=%s", path))
	}

	// use --directory so that the directory is added without its parent dirs
	for _, path := range paths {
		parent, dir := filepath.Split(path)
		tarArgs = append(tarArgs, "--directory", parent, dir)
	}

	var sz osutil.Sizer
	hasher := crypto.SHA3_384.New()

	cmd := tarAsUser(username, tarArgs...)
	cmd.Stdout = io.MultiWriter(archiveWriter, hasher, &sz)

	// keep (at most) the last 5 non-empty lines of what 'tar' writes to stderr
	// (those are the most likely contain the reason for fatal errors)
	matchCounter := &strutil.MatchCounter{
		N:     5,
		LastN: true,
	}
	cmd.Stderr = matchCounter

	if isTesting {
		matchCounter.N = -1
		cmd.Stderr = io.MultiWriter(os.Stderr, matchCounter)
	}
	mylog.Check(osutil.RunWithContext(ctx, cmd))

	// we have at most 5 matches here

	snapshot.SHA3_384[entry] = fmt.Sprintf("%x", hasher.Sum(nil))
	snapshot.Size += sz.Size()

	return nil
}

// pathsForSnapshot returns a list of absolute paths under 'snapDir' that should
// be included in the snapshot (based on what directories exist).
func pathsForSnapshot(snapDir string, snapshot *client.Snapshot) ([]string, error) {
	dirExists := func(path string) (bool, error) {
		exists, isDir := mylog.Check3(osutil.DirExists(path))

		if exists && isDir {
			return true, nil
		}

		if !exists {
			logger.Debugf("Not saving %q in snapshot #%d of %q as it is does not exist.", path, snapshot.SetID, snapshot.Snap)
		} else if !isDir {
			logger.Noticef("Not saving %q in snapshot #%d of %q as it is not a directory.", path, snapshot.SetID, snapshot.Snap)
		}

		return false, nil
	}

	var snapshotPaths []string
	for _, subDir := range []string{snapshot.Revision.String(), "common"} {
		subPath := filepath.Join(snapDir, subDir)
		if ok := mylog.Check2(dirExists(subPath)); err != nil {
			return nil, err
		} else if ok {
			snapshotPaths = append(snapshotPaths, subPath)
		}
	}

	return snapshotPaths, nil
}

var ErrCannotCancel = errors.New("cannot cancel: import already finished")

// multiError collects multiple errors that affected an operation.
type multiError struct {
	header string
	errs   []error
}

// newMultiError returns a new multiError struct initialized with
// the given format string that explains what operation potentially
// went wrong. multiError can be nested and will render correctly
// in these cases.
func newMultiError(header string, errs []error) error {
	return &multiError{header: header, errs: errs}
}

// Error formats the error string.
func (me *multiError) Error() string {
	return me.nestedError(0)
}

// helper to ensure formatting of nested multiErrors works.
func (me *multiError) nestedError(level int) string {
	indent := strings.Repeat(" ", level)
	buf := bytes.NewBufferString(fmt.Sprintf("%s:\n", me.header))
	if level > 8 {
		return "circular or too deep error nesting (max 8)?!"
	}
	for i, err := range me.errs {
		switch v := err.(type) {
		case *multiError:
			fmt.Fprintf(buf, "%s- %v", indent, v.nestedError(level+1))
		default:
			fmt.Fprintf(buf, "%s- %v", indent, err)
		}
		if i < len(me.errs)-1 {
			fmt.Fprintf(buf, "\n")
		}
	}
	return buf.String()
}

var (
	importingFnRegexp = regexp.MustCompile("^([0-9]+)_importing$")
	importingFnGlob   = "[0-9]*_importing"
	importingFnFmt    = "%d_importing"
	importingForIDFmt = "%d_*.zip"
)

// importInProgressFor return true if the given snapshot id has an import
// that is in progress.
func importInProgressFor(setID uint64) bool {
	return newImportTransaction(setID).InProgress()
}

// importTransaction keeps track of the given snapshot ID import and
// ensures it can be committed/cancelled in an atomic way.
//
// Start() must be called before the first data is imported. When the
// import is successful Commit() should be called.
//
// Cancel() will cancel the given import and cleanup. It's always safe
// to defer a Cancel() it will just return a "ErrCannotCancel" after
// a commit.
type importTransaction struct {
	id        uint64
	lockPath  string
	committed bool
}

// newImportTransaction creates a new importTransaction for the given
// snapshot id.
func newImportTransaction(setID uint64) *importTransaction {
	return &importTransaction{
		id:       setID,
		lockPath: filepath.Join(dirs.SnapshotsDir, fmt.Sprintf(importingFnFmt, setID)),
	}
}

// newImportTransactionFromImportFile creates a new importTransaction
// for the given import file path. It may return an error if an
// invalid file was specified.
func newImportTransactionFromImportFile(p string) (*importTransaction, error) {
	parts := importingFnRegexp.FindStringSubmatch(path.Base(p))
	if len(parts) != 2 {
		return nil, fmt.Errorf("cannot determine snapshot id from %q", p)
	}
	setID := mylog.Check2(strconv.ParseUint(parts[1], 10, 64))

	return newImportTransaction(setID), nil
}

// Start marks the start of a snapshot import
func (t *importTransaction) Start() error {
	return t.lock()
}

// InProgress returns true if there is an import for this transactions
// snapshot ID already.
func (t *importTransaction) InProgress() bool {
	return osutil.FileExists(t.lockPath)
}

// Cancel cancels a snapshot import and cleanups any files on disk belonging
// to this snapshot ID.
func (t *importTransaction) Cancel() error {
	if t.committed {
		return ErrCannotCancel
	}
	inProgressImports := mylog.Check2(filepath.Glob(filepath.Join(dirs.SnapshotsDir, fmt.Sprintf(importingForIDFmt, t.id))))

	var errs []error
	for _, p := range inProgressImports {
		mylog.Check(os.Remove(p))
	}
	mylog.Check(t.unlock())

	if len(errs) > 0 {
		return newMultiError(fmt.Sprintf("cannot cancel import for set id %d", t.id), errs)
	}
	return nil
}

// Commit will commit a given transaction
func (t *importTransaction) Commit() error {
	mylog.Check(t.unlock())

	t.committed = true
	return nil
}

func (t *importTransaction) lock() error {
	return os.WriteFile(t.lockPath, nil, 0644)
}

func (t *importTransaction) unlock() error {
	return os.Remove(t.lockPath)
}

var filepathGlob = filepath.Glob

// CleanupAbandonedImports will clean any import that is in progress.
// This is meant to be called at startup of snapd before any real imports
// happen. It is not safe to run this concurrently with any other snapshot
// operation.
//
// The amount of snapshots cleaned is returned and an error if one or
// more cleanups did not succeed.
func CleanupAbandonedImports() (cleaned int, err error) {
	inProgressSnapshots := mylog.Check2(filepathGlob(filepath.Join(dirs.SnapshotsDir, importingFnGlob)))

	var errs []error
	for _, p := range inProgressSnapshots {
		tr := mylog.Check2(newImportTransactionFromImportFile(p))
		mylog.Check(tr.Cancel())

	}
	if len(errs) > 0 {
		return cleaned, newMultiError("cannot cleanup imports", errs)
	}
	return cleaned, nil
}

// ImportFlags carries extra flags to drive import behavior.
type ImportFlags struct {
	// noDuplicatedImportCheck tells import not to check for existing snapshot
	// with same content hash (and not report DuplicatedSnapshotImportError).
	NoDuplicatedImportCheck bool
}

// Import a snapshot from the export file format
func Import(ctx context.Context, id uint64, r io.Reader, flags *ImportFlags) (snapNames []string, err error) {
	mylog.Check(os.MkdirAll(dirs.SnapshotsDir, 0700))

	errPrefix := fmt.Sprintf("cannot import snapshot %d", id)

	tr := newImportTransaction(id)
	if tr.InProgress() {
		return nil, fmt.Errorf("%s: already in progress for this set id", errPrefix)
	}
	mylog.Check(tr.Start())

	// Cancel once Committed is a NOP
	defer tr.Cancel()

	// Unpack and validate the streamed data
	//
	// XXX: this will leak snapshot IDs, i.e. we allocate a new
	// snapshot ID before but then we error here because of e.g.
	// duplicated import attempts
	snapNames = mylog.Check2(unpackVerifySnapshotImport(ctx, r, id, flags))
	mylog.Check(tr.Commit())

	return snapNames, nil
}

func writeOneSnapshotFile(targetPath string, tr io.Reader) error {
	t := mylog.Check2(os.OpenFile(targetPath, os.O_CREATE|os.O_RDWR, 0600))

	defer t.Close()
	mylog.Check2(io.Copy(t, tr))

	return nil
}

type DuplicatedSnapshotImportError struct {
	SetID     uint64
	SnapNames []string
}

func (e DuplicatedSnapshotImportError) Error() string {
	return fmt.Sprintf("cannot import snapshot, already available as snapshot id %v", e.SetID)
}

func checkDuplicatedSnapshotSetWithContentHash(ctx context.Context, contentHash []byte) error {
	snapshotSetMap := map[uint64]client.SnapshotSet{}
	mylog.

		// XXX: deal with import in progress here
		Check(

			// get all current snapshotSets
			Iter(ctx, func(reader *Reader) error {
				ss := snapshotSetMap[reader.SetID]
				ss.Snapshots = append(ss.Snapshots, &reader.Snapshot)
				snapshotSetMap[reader.SetID] = ss
				return nil
			}))

	for setID, ss := range snapshotSetMap {
		h := mylog.Check2(ss.ContentHash())

		if bytes.Equal(h, contentHash) {
			var snapNames []string
			for _, snapshot := range ss.Snapshots {
				snapNames = append(snapNames, snapshot.Snap)
			}
			return DuplicatedSnapshotImportError{SetID: setID, SnapNames: snapNames}
		}
	}
	return nil
}

func unpackVerifySnapshotImport(ctx context.Context, r io.Reader, realSetID uint64, flags *ImportFlags) (snapNames []string, err error) {
	var exportFound bool

	tr := tar.NewReader(r)
	var tarErr error
	var header *tar.Header

	if flags == nil {
		flags = &ImportFlags{}
	}

	for tarErr == nil {
		header, tarErr = tr.Next()
		if tarErr == io.EOF {
			break
		}
		switch {
		case tarErr != nil:
			return nil, fmt.Errorf("cannot read snapshot import: %v", tarErr)
		case header == nil:
			// should not happen
			return nil, fmt.Errorf("tar header not found")
		case header.Typeflag == tar.TypeDir:
			return nil, errors.New("unexpected directory in import file")
		}

		// files within the snapshot should never use parent elements
		if strings.Contains(header.Name, "../") {
			return nil, fmt.Errorf("invalid filename in import file")
		}

		if header.Name == "content.json" {
			var ej contentJSON
			dec := json.NewDecoder(tr)
			mylog.Check(dec.Decode(&ej))

			if !flags.NoDuplicatedImportCheck {
				mylog.Check(
					// XXX: this is potentially slow as it needs
					//      to open all snapshots files and read a
					//      small amount of data from them
					checkDuplicatedSnapshotSetWithContentHash(ctx, ej.ContentHash))
			}
			continue
		}

		if header.Name == "export.json" {
			// XXX: read into memory and validate once we
			// hashes in export.json
			exportFound = true
			continue
		}

		// Format of the snapshot import is:
		//     $setID_.....
		// But because the setID is local this will not be correct
		// for our system and we need to discard this setID.
		//
		// So chop off the incorrect (old) setID and just use
		// the rest that is still valid.
		l := strings.SplitN(header.Name, "_", 2)
		if len(l) != 2 {
			return nil, fmt.Errorf("unexpected filename in import stream: %v", header.Name)
		}
		targetPath := path.Join(dirs.SnapshotsDir, fmt.Sprintf("%d_%s", realSetID, l[1]))
		mylog.Check(writeOneSnapshotFile(targetPath, tr))

		r := mylog.Check2(backendOpen(targetPath, realSetID))
		mylog.Check(r.Check(context.TODO(), nil))
		r.Close()
		snapNames = append(snapNames, r.Snap)

	}

	if !exportFound {
		return nil, fmt.Errorf("no export.json file in uploaded data")
	}
	// XXX: validate using the unmarshalled export.json hashes here

	return snapNames, nil
}

type exportMetadata struct {
	Format int       `json:"format"`
	Date   time.Time `json:"date"`
	Files  []string  `json:"files"`
}

type SnapshotExport struct {
	// open snapshot files
	snapshotFiles []*os.File

	// contentHash of the full snapshot
	contentHash []byte

	// remember setID mostly for nicer errors
	setID uint64

	// cached size, needs to be calculated with CalculateSize
	size int64
}

// NewSnapshotExport will return a SnapshotExport structure. It must be
// Close()ed after use to avoid leaking file descriptors.
func NewSnapshotExport(ctx context.Context, setID uint64) (se *SnapshotExport, err error) {
	var snapshotFiles []*os.File
	var snapshotSet client.SnapshotSet

	defer func() {
		// cleanup any open FDs if anything goes wrong
	}()
	mylog.

		// Open all files first and keep the file descriptors
		// open. The caller should have locked the state so that no
		// delete/change snapshot operations can happen while the
		// files are getting opened.
		Check(Iter(ctx, func(reader *Reader) error {
			if reader.SetID == setID {
				snapshotSet.Snapshots = append(snapshotSet.Snapshots, &reader.Snapshot)

				// Duplicate the file descriptor of the reader
				// we were handed as Iter() closes those as
				// soon as this unnamed returns. We re-package
				// the file descriptor into snapshotFiles
				// below.
				fd := mylog.Check2(syscall.Dup(int(reader.Fd())))

				f := os.NewFile(uintptr(fd), reader.Name())
				if f == nil {
					return fmt.Errorf("cannot open file from descriptor %d", fd)
				}
				snapshotFiles = append(snapshotFiles, f)
			}
			return nil
		}))

	if len(snapshotFiles) == 0 {
		return nil, fmt.Errorf("no snapshot data found for %v", setID)
	}

	h := mylog.Check2(snapshotSet.ContentHash())

	se = &SnapshotExport{snapshotFiles: snapshotFiles, setID: setID, contentHash: h}

	// ensure we never leak FDs even if the user does not call close
	runtime.SetFinalizer(se, (*SnapshotExport).Close)

	return se, nil
}

// Init will calculate the snapshot size. This can take some time
// so it should be called without any locks. The SnapshotExport
// keeps the FDs open so even files moved/deleted will be found.
func (se *SnapshotExport) Init() error {
	// Export once into a fake writer so that we can set the size
	// of the export. This is then used to set the Content-Length
	// in the response correctly.
	//
	// Note that the size of the generated tar could change if the
	// time switches between this export and the export we stream
	// to the client to a time after the year 2242. This is unlikely
	// but a known issue with this approach here.
	var sz osutil.Sizer
	mylog.Check(se.StreamTo(&sz))

	se.size = sz.Size()
	return nil
}

func (se *SnapshotExport) Size() int64 {
	return se.size
}

func (se *SnapshotExport) Close() {
	for _, f := range se.snapshotFiles {
		f.Close()
	}
	se.snapshotFiles = nil
}

type contentJSON struct {
	ContentHash []byte `json:"content-hash"`
}

func (se *SnapshotExport) StreamTo(w io.Writer) error {
	// write out a tar
	var files []string
	tw := tar.NewWriter(w)
	defer tw.Close()

	// export contentHash as content.json
	h := mylog.Check2(json.Marshal(contentJSON{se.contentHash}))

	hdr := &tar.Header{
		Typeflag: tar.TypeReg,
		Name:     "content.json",
		Size:     int64(len(h)),
		Mode:     0640,
		ModTime:  timeNow(),
	}
	mylog.Check(tw.WriteHeader(hdr))
	mylog.Check2(tw.Write(h))

	// write out the individual snapshots
	for _, snapshotFile := range se.snapshotFiles {
		stat := mylog.Check2(snapshotFile.Stat())

		if !stat.Mode().IsRegular() {
			// should never happen
			return fmt.Errorf("unexported special file %q in snapshot: %s", stat.Name(), stat.Mode())
		}
		mylog.Check2(snapshotFile.Seek(0, 0))

		hdr := mylog.Check2(tar.FileInfoHeader(stat, ""))
		mylog.Check(tw.WriteHeader(hdr))
		mylog.Check2(io.Copy(tw, snapshotFile))

		files = append(files, path.Base(snapshotFile.Name()))
	}

	// SnapshotExporter has se.Close() set as a finalizer, thus when the object
	// is no longer referenced, se.Close() (which closes all files) will be
	// called automatically after/during a GC pass. We don't know if the caller
	// retains a reference to the object (eg. for any outstanding calls to some
	// of its functions), and the last explicit reference in the code above was
	// kept for the purpose of accessing the snapshot files list, which is done
	// before the final file is read, so we need to mark object as alive until
	// after every file has been read.
	runtime.KeepAlive(se)

	// write the metadata last, then the client can use that to
	// validate the archive is complete
	meta := exportMetadata{
		Format: 1,
		Date:   timeNow(),
		Files:  files,
	}
	metaDataBuf := mylog.Check2(json.Marshal(&meta))

	hdr = &tar.Header{
		Typeflag: tar.TypeReg,
		Name:     "export.json",
		Size:     int64(len(metaDataBuf)),
		Mode:     0640,
		ModTime:  timeNow(),
	}
	mylog.Check(tw.WriteHeader(hdr))
	mylog.Check2(tw.Write(metaDataBuf))

	return nil
}
