package backend

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

func nextBackup(fn string) (string, error) {
	// this is a TOCTOU problem
	for n := 1; n < 100; n++ {
		cand := fmt.Sprintf("%s.~%d~", fn, n)
		if exists, _, _ := osutil.DirExists(cand); !exists {
			return cand, nil
		}
	}
	return "", fmt.Errorf("cannot find a backup name for %q", fn)
}

var backupRx = regexp.MustCompile(`\.~\d+~$`)

func backup2orig(fn string) string {
	if idx := backupRx.FindStringIndex(fn); len(idx) > 0 {
		return fn[:idx[0]]
	}
	return ""
}

func member(f *os.File, member string) (r io.ReadCloser, sz int64, err error) {
	if f == nil {
		// maybe "not open"?
		return nil, -1, io.EOF
	}

	// rewind the file
	// (shouldn't be needed, but doesn't hurt too much)
	if _, err := f.Seek(0, 0); err != nil {
		return nil, -1, err
	}

	fi, err := f.Stat()

	arch, err := zip.NewReader(f, fi.Size())
	if err != nil {
		return nil, -1, err
	}

	for _, f := range arch.File {
		if f.Name == member {
			rc, err := f.Open()
			return rc, int64(f.UncompressedSize64), err
		}
	}

	return nil, -1, fmt.Errorf("missing archive member %q", member)
}

func userArchiveName(userHome string) string {
	userHome = dirs.StripRootDir(userHome)
	return filepath.Join(userArchivePrefix, userHome+userArchiveSuffix)
}

func isUserArchive(userArchive string) bool {
	return strings.HasPrefix(userArchive, userArchivePrefix) && strings.HasSuffix(userArchive, userArchiveSuffix)
}

func userHome(userArchive string) string {
	// this _will_ panic if !isUserArchive(userArchive)
	return filepath.Join(dirs.GlobalRootDir, userArchive[len(userArchivePrefix):len(userArchive)-len(userArchiveSuffix)])
}

type bySnap []*client.Snapshot

func (a bySnap) Len() int           { return len(a) }
func (a bySnap) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a bySnap) Less(i, j int) bool { return a[i].Snap < a[j].Snap }

type byID []client.SnapshotGroup

func (a byID) Len() int           { return len(a) }
func (a byID) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byID) Less(i, j int) bool { return a[i].ID < a[j].ID }
