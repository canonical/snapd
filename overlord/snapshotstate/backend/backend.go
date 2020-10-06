// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

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

	osOpen      = os.Open
	dirNames    = (*os.File).Readdirnames
	backendOpen = Open
	timeNow     = time.Now

	usersForUsernames = usersForUsernamesImpl
)

// Flags encompasses extra flags for snapshots backend Save.
type Flags struct {
	Auto bool
}

// LastSnapshotSetID returns the highest set id number for the snapshots stored
// in snapshots directory; set ids are inferred from the filenames.
func LastSnapshotSetID() (uint64, error) {
	dir, err := osOpen(dirs.SnapshotsDir)
	if err != nil {
		if osutil.IsDirNotExist(err) {
			// no snapshots
			return 0, nil
		}
		return 0, fmt.Errorf("cannot open snapshots directory: %v", err)
	}
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
	if err := ctx.Err(); err != nil {
		return err
	}

	dir, err := osOpen(dirs.SnapshotsDir)
	if err != nil {
		if osutil.IsDirNotExist(err) {
			// no dir -> no snapshots
			return nil
		}
		return fmt.Errorf("cannot open snapshots directory: %v", err)
	}
	defer dir.Close()

	importsInProgress := map[uint64]bool{}
	var names []string
	var readErr error
	for readErr == nil && err == nil {
		names, readErr = dirNames(dir, 100)
		// note os.Readdirnames can return a non-empty names and a non-nil err
		for _, name := range names {
			if err = ctx.Err(); err != nil {
				break
			}

			// filter out non-snapshot directory entries
			ok, setID := isSnapshotFilename(name)
			if !ok {
				continue
			}
			// keep track of in-progress in a struct as well
			// to avoid races from the fact that we read only
			// 100 dir entries at a time
			if importsInProgress[setID] {
				continue
			}
			if newImportTransaction(setID).InProgress() {
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
				err = f(reader)
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
			if err != nil {
				break
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
	err := Iter(ctx, func(reader *Reader) error {
		if setID == 0 || reader.SetID == setID {
			if len(snapNames) == 0 || strutil.ListContains(snapNames, reader.Snap) {
				setshots[reader.SetID] = append(setshots[reader.SetID], &reader.Snapshot)
			}
		}
		return nil
	})

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
	id, err := strconv.Atoi(parts[0])
	if err != nil {
		return false, 0
	}
	return true, uint64(id)
}

// EstimateSnapshotSize calculates estimated size of the snapshot.
func EstimateSnapshotSize(si *snap.Info, usernames []string) (uint64, error) {
	var total uint64
	calculateSize := func(path string, finfo os.FileInfo, err error) error {
		if finfo.Mode().IsRegular() {
			total += uint64(finfo.Size())
		}
		return err
	}

	visitDir := func(dir string) error {
		exists, isDir, err := osutil.DirExists(dir)
		if err != nil {
			return err
		}
		if !(exists && isDir) {
			return nil
		}
		return filepath.Walk(dir, calculateSize)
	}

	for _, dir := range []string{si.DataDir(), si.CommonDataDir()} {
		if err := visitDir(dir); err != nil {
			return 0, err
		}
	}

	users, err := usersForUsernames(usernames)
	if err != nil {
		return 0, err
	}
	for _, usr := range users {
		if err := visitDir(si.UserDataDir(usr.HomeDir)); err != nil {
			return 0, err
		}
		if err := visitDir(si.UserCommonDataDir(usr.HomeDir)); err != nil {
			return 0, err
		}
	}

	// XXX: we could use a typical compression factor here
	return total, nil
}

// Save a snapshot
func Save(ctx context.Context, id uint64, si *snap.Info, cfg map[string]interface{}, usernames []string, flags *Flags) (*client.Snapshot, error) {
	if err := os.MkdirAll(dirs.SnapshotsDir, 0700); err != nil {
		return nil, err
	}

	var auto bool
	if flags != nil {
		auto = flags.Auto
	}

	snapshot := &client.Snapshot{
		SetID:    id,
		Snap:     si.InstanceName(),
		SnapID:   si.SnapID,
		Revision: si.Revision,
		Version:  si.Version,
		Epoch:    si.Epoch,
		Time:     timeNow(),
		SHA3_384: make(map[string]string),
		Size:     0,
		Conf:     cfg,
		Auto:     auto,
	}

	aw, err := osutil.NewAtomicFile(Filename(snapshot), 0600, 0, osutil.NoChown, osutil.NoChown)
	if err != nil {
		return nil, err
	}
	// if things worked, we'll commit (and Cancel becomes a NOP)
	defer aw.Cancel()

	w := zip.NewWriter(aw)
	defer w.Close() // note this does not close the file descriptor (that's done by hand on the atomic writer, above)
	if err := addDirToZip(ctx, snapshot, w, "root", archiveName, si.DataDir()); err != nil {
		return nil, err
	}

	users, err := usersForUsernames(usernames)
	if err != nil {
		return nil, err
	}

	for _, usr := range users {
		if err := addDirToZip(ctx, snapshot, w, usr.Username, userArchiveName(usr), si.UserDataDir(usr.HomeDir)); err != nil {
			return nil, err
		}
	}

	metaWriter, err := w.Create(metadataName)
	if err != nil {
		return nil, err
	}

	hasher := crypto.SHA3_384.New()
	enc := json.NewEncoder(io.MultiWriter(metaWriter, hasher))
	if err := enc.Encode(snapshot); err != nil {
		return nil, err
	}

	hashWriter, err := w.Create(metaHashName)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(hashWriter, "%x\n", hasher.Sum(nil))
	if err := w.Close(); err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := aw.Commit(); err != nil {
		return nil, err
	}

	return snapshot, nil
}

var isTesting = snapdenv.Testing()

func addDirToZip(ctx context.Context, snapshot *client.Snapshot, w *zip.Writer, username string, entry, dir string) error {
	parent, revdir := filepath.Split(dir)
	exists, isDir, err := osutil.DirExists(parent)
	if err != nil {
		return err
	}
	if exists && !isDir {
		logger.Noticef("Not saving directories under %q in snapshot #%d of %q as it is not a directory.", parent, snapshot.SetID, snapshot.Snap)
		return nil
	}
	if !exists {
		logger.Debugf("Not saving directories under %q in snapshot #%d of %q as it is does not exist.", parent, snapshot.SetID, snapshot.Snap)
		return nil
	}
	tarArgs := []string{
		"--create",
		"--sparse", "--gzip",
		"--directory", parent,
	}

	noRev, noCommon := true, true

	exists, isDir, err = osutil.DirExists(dir)
	if err != nil {
		return err
	}
	switch {
	case exists && isDir:
		tarArgs = append(tarArgs, revdir)
		noRev = false
	case exists && !isDir:
		logger.Noticef("Not saving %q in snapshot #%d of %q as it is not a directory.", dir, snapshot.SetID, snapshot.Snap)
	case !exists:
		logger.Debugf("Not saving %q in snapshot #%d of %q as it is does not exist.", dir, snapshot.SetID, snapshot.Snap)
	}

	common := filepath.Join(parent, "common")
	exists, isDir, err = osutil.DirExists(common)
	if err != nil {
		return err
	}
	switch {
	case exists && isDir:
		tarArgs = append(tarArgs, "common")
		noCommon = false
	case exists && !isDir:
		logger.Noticef("Not saving %q in snapshot #%d of %q as it is not a directory.", common, snapshot.SetID, snapshot.Snap)
	case !exists:
		logger.Debugf("Not saving %q in snapshot #%d of %q as it is does not exist.", common, snapshot.SetID, snapshot.Snap)
	}

	if noCommon && noRev {
		return nil
	}

	archiveWriter, err := w.CreateHeader(&zip.FileHeader{Name: entry})
	if err != nil {
		return err
	}

	var sz osutil.Sizer
	hasher := crypto.SHA3_384.New()

	cmd := tarAsUser(username, tarArgs...)
	cmd.Stdout = io.MultiWriter(archiveWriter, hasher, &sz)
	matchCounter := &strutil.MatchCounter{N: 1}
	cmd.Stderr = matchCounter
	if isTesting {
		matchCounter.N = -1
		cmd.Stderr = io.MultiWriter(os.Stderr, matchCounter)
	}
	if err := osutil.RunWithContext(ctx, cmd); err != nil {
		matches, count := matchCounter.Matches()
		if count > 0 {
			return fmt.Errorf("cannot create archive: %s (and %d more)", matches[0], count-1)
		}
		return fmt.Errorf("tar failed: %v", err)
	}

	snapshot.SHA3_384[entry] = fmt.Sprintf("%x", hasher.Sum(nil))
	snapshot.Size += sz.Size()

	return nil
}

var ErrCannotCancel = errors.New("cannot cancel: import already finished")

// importTransaction keeps track of the given snapshot ID import and
// ensures it can be commited/canceld in an atomic way.
//
// Start() must be called before the first data is imported. When the
// import is successful Commit() should be called.
//
// Cancel() will cancel the given import and cleanup. It's always save
// to defer a Cancel() it will just return a "ErrCannotCanel" after
// a commit.
type importTransaction struct {
	id       uint64
	commited bool
}

func newImportTransaction(setID uint64) *importTransaction {
	return &importTransaction{id: setID}
}

func (t *importTransaction) importInProgressFilepath() string {
	return filepath.Join(dirs.SnapshotsDir, fmt.Sprintf("%d_importing", t.id))
}
func (t *importTransaction) importInProgressFilesGlob() string {
	return filepath.Join(dirs.SnapshotsDir, fmt.Sprintf("%d_*.zip", t.id))
}

// Start marks the start of a snapshot import
func (t *importTransaction) Start() error {
	return ioutil.WriteFile(t.importInProgressFilepath(), nil, 0644)
}

func (t *importTransaction) InProgress() bool {
	return osutil.FileExists(t.importInProgressFilepath())
}

// Cancel cancels a snapshot import and cleanups any files on disk belonging
// to this snapshot ID.
func (t *importTransaction) Cancel() error {
	if t.commited {
		return ErrCannotCancel
	}
	inProgressImports, err := filepath.Glob(t.importInProgressFilesGlob())
	if err != nil {
		return err
	}
	var errs []error
	for _, p := range inProgressImports {
		if err := os.Remove(p); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		buf := bytes.NewBuffer(nil)
		for _, err := range errs {
			fmt.Fprintf(buf, " - %v\n", err)
		}
		return fmt.Errorf("cannot cancel import of id %d:\n%s", t.id, buf.String())
	}
	return nil
}

// Commit will commit a given transaction
func (t *importTransaction) Commit() error {
	if err := os.Remove(t.importInProgressFilepath()); err != nil {
		return err
	}
	t.commited = true
	return nil
}

// Import a snapshot from the export file format
func Import(ctx context.Context, id uint64, r io.Reader) (snapNames []string, err error) {
	errPrefix := fmt.Sprintf("cannot import snapshot %d", id)

	tr := newImportTransaction(id)
	if tr.InProgress() {
		return nil, fmt.Errorf("%s: already in progress for this id", errPrefix)
	}
	if err := tr.Start(); err != nil {
		return nil, err
	}
	defer tr.Cancel()

	// Unpack the streamed tar
	snapNames, err = unpackVerifySnapshotImport(r, id)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", errPrefix, err)
	}
	if err := tr.Commit(); err != nil {
		return nil, err
	}

	return snapNames, nil
}

func unpackVerifySnapshotImport(r io.Reader, realSetID uint64) (snapNames []string, err error) {
	var exportFound bool

	targetDir := dirs.SnapshotsDir

	tr := tar.NewReader(r)
	var tarErr error
	var header *tar.Header

	for tarErr == nil {
		header, tarErr = tr.Next()
		if tarErr == io.EOF {
			break
		}
		switch {
		case tarErr != nil:
			return nil, fmt.Errorf("failed reading snapshot import: %v", tarErr)
		case header == nil:
			// should not happen
			return nil, fmt.Errorf("tar header not found")
		case header.Typeflag == tar.TypeDir:
			return nil, errors.New("unexpected directory in import file")
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
			return nil, fmt.Errorf("unexpected filename in stream: %v", header.Name)
		}
		targetPath := path.Join(targetDir, fmt.Sprintf("%d_%s", realSetID, l[1]))

		t, err := os.OpenFile(targetPath, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return snapNames, fmt.Errorf("cannot create file %q: %v", targetPath, err)
		}
		defer t.Close()

		if _, err := io.Copy(t, tr); err != nil {
			return snapNames, fmt.Errorf("cannot copy file %q: %v", targetPath, err)
		}

		r, err := backendOpen(targetPath, realSetID)
		if err != nil {
			return snapNames, fmt.Errorf("cannot open snapshot: %v", err)
		}
		err = r.Check(context.TODO(), nil)
		r.Close()
		snapNames = append(snapNames, r.Snap)
		if err != nil {
			return snapNames, fmt.Errorf("validation failed for %q: %v", targetPath, err)
		}
	}
	if !exportFound {
		return nil, fmt.Errorf("no export.json file in uploaded data")
	}

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

	// remember setID mostly for nicer errors
	setID uint64

	// cached size, needs to be calculated with CalculateSize
	size int64
}

// NewSnapshotExport will return a SnapshotExport structure. It must be
// Close()ed after use to avoid leaking file descriptors.
func NewSnapshotExport(ctx context.Context, setID uint64) (se *SnapshotExport, err error) {
	var snapshotFiles []*os.File

	defer func() {
		// cleanup any open FDs if anything goes wrong
		if err != nil {
			for _, f := range snapshotFiles {
				f.Close()
			}
		}
	}()

	// Open all files first and keep the file descriptors
	// open. The caller should have locked the state so that no
	// delete/change snapshot operations can happen while the
	// files are getting opened.
	err = Iter(ctx, func(reader *Reader) error {
		if reader.SetID == setID {
			// Duplicate the file descriptor of the reader we were handed as
			// Iter() closes those as soon as this unnamed returns. We
			// re-package the file descriptor into snapshotFiles below.
			fd, err := syscall.Dup(int(reader.Fd()))
			if err != nil {
				return fmt.Errorf("cannot duplicate descriptor: %v", err)
			}
			f := os.NewFile(uintptr(fd), reader.Name())
			if f == nil {
				return fmt.Errorf("cannot open file from descriptor %d", fd)
			}
			snapshotFiles = append(snapshotFiles, f)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("cannot export snapshot %v: %v", setID, err)
	}
	if len(snapshotFiles) == 0 {
		return nil, fmt.Errorf("no snapshot data found for %v", setID)
	}

	se = &SnapshotExport{snapshotFiles: snapshotFiles, setID: setID}

	// ensure we never leak FDs even if the user does not call close
	runtime.SetFinalizer(se, (*SnapshotExport).Close)

	return se, nil
}

// Init will calculate the snapshot size. This can take some time
// so it should be called without any locks. The SnapshotExport
// keeps the FDs open so even files moved/deleted will be found.
func (se *SnapshotExport) Init() error {
	// Export once into a dummy writer so that we can set the size
	// of the export. This is then used to set the Content-Length
	// in the response correctly.
	//
	// Note that the size of the generated tar could change if the
	// time switches between this export and the export we stream
	// to the client to a time after the year 2242. This is unlikely
	// but a known issue with this approach here.
	var sz osutil.Sizer
	if err := se.StreamTo(&sz); err != nil {
		return fmt.Errorf("cannot calculcate the size for %v: %s", se.setID, err)
	}
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

func (se *SnapshotExport) StreamTo(w io.Writer) error {
	// write out a tar
	var files []string
	tw := tar.NewWriter(w)
	defer tw.Close()
	for _, snapshotFile := range se.snapshotFiles {
		stat, err := snapshotFile.Stat()
		if err != nil {
			return err
		}
		if !stat.Mode().IsRegular() {
			// should never happen
			return fmt.Errorf("unexported special file %q in snapshot: %s", stat.Name(), stat.Mode())
		}
		if _, err := snapshotFile.Seek(0, 0); err != nil {
			return fmt.Errorf("cannot seek on %v: %v", stat.Name(), err)
		}
		hdr, err := tar.FileInfoHeader(stat, "")
		if err != nil {
			return fmt.Errorf("symlink: %v", stat.Name())
		}
		if err = tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("cannot write header for %v: %v", stat.Name(), err)
		}
		if _, err := io.Copy(tw, snapshotFile); err != nil {
			return fmt.Errorf("cannot write data for %v: %v", stat.Name(), err)
		}

		files = append(files, path.Base(snapshotFile.Name()))
	}

	// write the metadata last, then the client can use that to
	// validate the archive is complete
	meta := exportMetadata{
		Format: 1,
		Date:   timeNow(),
		Files:  files,
	}
	metaDataBuf, err := json.Marshal(&meta)
	if err != nil {
		return fmt.Errorf("cannot marshal meta-data: %v", err)
	}
	hdr := &tar.Header{
		Typeflag: tar.TypeReg,
		Name:     "export.json",
		Size:     int64(len(metaDataBuf)),
		Mode:     0640,
		ModTime:  timeNow(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := tw.Write(metaDataBuf); err != nil {
		return err
	}

	return nil
}
