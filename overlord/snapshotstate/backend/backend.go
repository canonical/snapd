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
	"context"
	"crypto"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
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
)

// Flags encompasses extra flags for snapshots backend Save.
type Flags struct {
	Auto bool
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

	var names []string
	var readErr error
	for readErr == nil && err == nil {
		names, readErr = dirNames(dir, 100)
		// note os.Readdirnames can return a non-empty names and a non-nil err
		for _, name := range names {
			if err = ctx.Err(); err != nil {
				break
			}

			filename := filepath.Join(dirs.SnapshotsDir, name)
			reader, openError := backendOpen(filename)
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

type exportMetadata struct {
	Format int       `json:"format"`
	Date   time.Time `json:"date"`
	Files  []string  `json:"files"`
}

// Export allows exporting a given snapshot set
func Export(ctx context.Context, setID uint64) (*SnapshotExport, error) {
	return NewSnapshotExport(ctx, setID)
}

type SnapshotExport struct {
	// open snapshot files
	snapshotFiles []*os.File

	// size
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
			// dup() the file descriptor
			fd, err := syscall.Dup(int(reader.Fd()))
			if err != nil {
				return fmt.Errorf("cannot dup %v", reader.Name())
			}
			f := os.NewFile(uintptr(fd), reader.Name())
			if f == nil {
				return fmt.Errorf("invalid fd %v for %v", fd, reader.Name())
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

	// Export once into a dummy writer so that we can set the size
	// of the export. This is then used to set the Content-Length
	// in the response correctly.
	//
	// Note that the size of the generated tar could change if the
	// time switches between this export and the export we stream
	// to the client to a time after the year 2242. This is unlikely
	// but a known issue with this approach here.
	var sz osutil.Sizer
	se = &SnapshotExport{snapshotFiles: snapshotFiles}
	if err = se.StreamTo(&sz); err != nil {
		return nil, fmt.Errorf("cannot calculcate the size for %v: %s", setID, err)
	}
	se.size = sz.Size()

	return se, nil
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
	for _, snapshotFd := range se.snapshotFiles {
		stat, err := snapshotFd.Stat()
		if err != nil {
			return err
		}
		if !stat.Mode().IsRegular() {
			// should never happen
			return fmt.Errorf("unexported special file %q in snapshot: %s", stat.Name(), stat.Mode())
		}
		if _, err := snapshotFd.Seek(0, 0); err != nil {
			return fmt.Errorf("cannot seek on %v: %v", stat.Name(), err)
		}
		hdr, err := tar.FileInfoHeader(stat, "")
		if err != nil {
			return fmt.Errorf("symlink: %v", stat.Name())
		}
		if err = tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("cannot write header for %v: %v", stat.Name(), err)
		}
		if _, err := io.Copy(tw, snapshotFd); err != nil {
			return fmt.Errorf("cannot write data for %v: %v", stat.Name(), err)
		}

		files = append(files, path.Base(snapshotFd.Name()))
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
