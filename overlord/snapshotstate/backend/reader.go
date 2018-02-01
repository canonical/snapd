package backend

import (
	"bytes"
	"crypto"
	"fmt"
	"hash"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"syscall"

	"golang.org/x/net/context"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/jsonutil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

// A Reader is a snapshot that's been opened for reading.
type Reader struct {
	*os.File
	client.Snapshot
}

// Open a Snapshot given its full filename
func Open(fn string) (rsh *Reader, e error) {
	f, err := os.Open(fn)
	if err != nil {
		return nil, err
	}
	defer func() {
		if e != nil {
			f.Close()
		}
	}()

	rsh = &Reader{
		File: f,
	}

	// First, grab the metadata hash
	var sz sizer
	metaHashReader, metaHashSize, err := member(f, metaHashName)
	if err != nil {
		return nil, err
	}
	metaHashBuf, err := ioutil.ReadAll(io.TeeReader(metaHashReader, &sz))
	if err != nil {
		return nil, err
	}
	if sz.size != metaHashSize {
		return nil, client.ErrSnapshotSizeMismatch
	}
	metaHash := string(bytes.TrimSpace(metaHashBuf))

	// Next, grab the metadata itself
	sz.Reset()

	hasher := crypto.SHA3_384.New()
	metaReader, metaSize, err := member(f, metadataName)
	if err != nil {
		return nil, err
	}

	if err := jsonutil.DecodeWithNumber(io.TeeReader(metaReader, io.MultiWriter(hasher, &sz)), &rsh.Snapshot); err != nil {
		return nil, client.ErrSnapshotUnsupported
	}
	if sz.size != metaSize {
		return nil, client.ErrSnapshotSizeMismatch
	}

	actualMetaHash := fmt.Sprintf("%x", hasher.Sum(nil))
	if actualMetaHash != metaHash {
		return nil, client.ErrSnapshotHashMismatch
	}

	if rsh.Uninitialised() {
		return nil, client.ErrSnapshotUnsupported
	}

	return rsh, nil
}

// Filename that the Reader reads from.
func (r *Reader) Filename() string {
	return Filename(&r.Snapshot)
}

func (r *Reader) checkOne(ctx context.Context, entry string, hasher hash.Hash) error {
	body, reportedSize, err := member(r.File, entry)
	if err != nil {
		return err
	}
	defer body.Close()

	hashsum := r.Hashsums[entry]
	readSize, err := io.Copy(io.MultiWriter(osutil.ContextWriter(ctx), hasher), body)
	if err != nil {
		return err
	}

	if readSize != reportedSize {
		return client.ErrSnapshotSizeMismatch
	}

	sha3 := fmt.Sprintf("%x", hasher.Sum(nil))
	if sha3 != hashsum {
		return client.ErrSnapshotHashMismatch
	}
	return nil
}

// Check that the data contained in the snapshot matches its hashsums.
func (r *Reader) Check(ctx context.Context, homes []string) error {
	sort.Strings(homes)

	hasher := crypto.SHA3_384.New()
	for entry := range r.Hashsums {
		if len(homes) > 0 && isUserArchive(entry) {
			home := userHome(entry)
			if !strutil.SortedListContains(homes, home) {
				logger.Debugf("skipping check of %q", home)
				continue
			}
		}

		if err := r.checkOne(ctx, entry, hasher); err != nil {
			return err
		}
		hasher.Reset()
	}

	return nil
}

// A Logf writes somewhere the user will see it
//
// (typically this is task.Logf when running via the overlord, or fmt.Printf
// when running direct)
type Logf func(format string, args ...interface{})

// Restore the data from the snapshot.
//
// If successful this will replace the existing data (for the revision
// in the snapshot) with that contained in the snapshot.
func (r *Reader) Restore(ctx context.Context, homes []string, logf Logf) error {
	_, err := r.restore(ctx, false, homes, logf)
	return err
}

// RestoreLeavingBackup is like Restore, but it leaves the old data in
// a backup directory.
func (r *Reader) RestoreLeavingBackup(ctx context.Context, homes []string, logf Logf) (*Backup, error) {
	return r.restore(ctx, true, homes, logf)
}

func (r *Reader) restore(ctx context.Context, leaveBackup bool, homes []string, logf Logf) (b *Backup, e error) {
	b = &Backup{}
	defer func() {
		if e != nil {
			logger.Debugf("restore failed; undoing")
			b.Revert()
			b = nil
		} else if !leaveBackup {
			logger.Debugf("restore succeeded; cleaning up")
			b.Cleanup()
			b = nil
		}
	}()

	sort.Strings(homes)
	isRoot := sys.Geteuid() == 0
	si := snap.MinimalPlaceInfo(r.Snap, r.Revision)
	hasher := crypto.SHA3_384.New()
	var sz sizer

	for entry := range r.Hashsums {
		if err := ctx.Err(); err != nil {
			return b, err
		}

		var dest string
		isUser := isUserArchive(entry)
		uid := sys.UserID(osutil.NoChown)
		gid := sys.GroupID(osutil.NoChown)

		if isUser {
			home := userHome(entry)
			if len(homes) > 0 && !strutil.SortedListContains(homes, home) {
				logger.Debugf("skipping restore of %q", home)
				continue
			}
			dest = si.UserDataDir(home)
			fi, err := os.Stat(home)
			if err != nil {
				if osutil.IsDirNotExist(err) {
					logf("skipping restore of %q as %q doesn't exist", dest, home)
				} else {
					logf("skipping restore of %q: %v", dest, err)
				}
				continue
			}

			if !fi.IsDir() {
				logf("skipping restore of %q as %q is not a directory", dest, home)
				continue
			}

			if st, ok := fi.Sys().(*syscall.Stat_t); ok && isRoot {
				// the mkdir below will use the uid/gid of home
				if st.Uid > 0 {
					uid = sys.UserID(st.Uid)
				}
				if st.Gid > 0 {
					gid = sys.GroupID(st.Gid)
				}
			}
		} else {
			if entry != archiveName {
				// hmmm
				logf("skipping restore of unknown entry %q", entry)
				continue
			}
			dest = si.DataDir()
		}
		parent, revdir := filepath.Split(dest)

		exists, isDir, err := osutil.DirExists(parent)
		if err != nil {
			return b, err
		}
		if !exists {
			// NOTE that the chown won't happen (it'll be NoChown)
			// for the system path, and we won't be creating the
			// user's home (as we skip restore in that case).
			// Also no chown happens for root/root.
			if err := osutil.MkdirAllChown(parent, 0755, uid, gid); err != nil {
				return b, err
			}
			b.Created = append(b.Created, parent)
		} else if !isDir {
			return b, fmt.Errorf("cannot restore snapshot into %q: not a directory", parent)
		}

		tempdir, err := ioutil.TempDir(parent, ".snapshot")
		if err != nil {
			return b, err
		}
		// one way or another we want tempdir gone
		defer func() {
			if err := os.RemoveAll(tempdir); err != nil {
				logf("cannot clean up temporary directory %q: %v", tempdir, err)
			}
		}()

		logger.Debugf("restoring %q into %q", entry, tempdir)

		body, reportedSize, err := member(r.File, entry)
		if err != nil {
			return b, err
		}

		hashsum := r.Hashsums[entry]

		tr := io.TeeReader(body, io.MultiWriter(hasher, &sz))

		// resist the temptation of using archive/tar unless it's proven
		// that calling out to tar has issues -- there are a lot of
		// special cases we'd need to consider otherwise
		cmd := exec.Command("tar",
			"--extract", // "--verbose", "--verbose",
			"--preserve-permissions", "--preserve-order", "--gunzip",
			"--directory", tempdir)
		cmd.Env = []string{}
		cmd.Stdin = tr
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stderr

		if err = osutil.RunWithContext(ctx, cmd); err != nil {
			return b, err
		}

		sha3 := fmt.Sprintf("%x", hasher.Sum(nil))
		if sha3 != hashsum {
			return b, client.ErrSnapshotHashMismatch
		}

		if sz.size != reportedSize {
			return b, client.ErrSnapshotSizeMismatch
		}

		// TODO: something with Config

		for _, dir := range []string{"common", revdir} {
			source := filepath.Join(tempdir, dir)
			if exists, _, err := osutil.DirExists(source); err != nil {
				return b, err
			} else if !exists {
				continue
			}
			target := filepath.Join(parent, dir)
			exists, _, err := osutil.DirExists(target)
			if err != nil {
				return b, err
			}
			if exists {
				backup, err := nextBackup(target)
				if err != nil {
					return b, err
				}
				if err := os.Rename(target, backup); err != nil {
					return b, err
				}
				b.Moved = append(b.Moved, backup)
			}

			if err := os.Rename(source, target); err != nil {
				return b, err
			}
			b.Created = append(b.Created, target)
		}

		sz.Reset()
		hasher.Reset()
	}

	return b, nil
}
