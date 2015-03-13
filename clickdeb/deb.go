package clickdeb

import (
	"archive/tar"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"launchpad.net/snappy/helpers"

	"github.com/blakesmith/ar"
)

var (
	// ErrSnapInvalidContent is returned if a snap package contains
	// invalid content
	ErrSnapInvalidContent = errors.New("snap contains invalid content")
)

func xzPipeReader(r io.Reader) io.Reader {
	pr, pw := io.Pipe()
	cmd := exec.Command("xz", "--decompress", "--stdout")
	cmd.Stdin = r
	cmd.Stdout = pw

	// run xz in its own go-routine
	go func() {
		pw.CloseWithError(cmd.Run())
	}()

	return pr
}

func clickVerifyContentFn(path string) (string, error) {
	path = filepath.Clean(path)
	if strings.Contains(path, "..") {
		return "", ErrSnapInvalidContent
	}

	return path, nil
}

// ClickDeb provides support for the "click" containers (a special kind of
// deb package)
type ClickDeb struct {
	Path string

	file *os.File
}

// ControlContent returns the content of the given control data member
// (e.g. the content of the "manifest" file in the control.tar.gz ar member)
func (d *ClickDeb) ControlContent(controlMember string) ([]byte, error) {
	var err error

	d.file, err = os.Open(d.Path)
	if err != nil {
		return nil, err
	}
	defer d.file.Close()

	dataReader, err := d.skipToArMember("control.tar")
	if err != nil {
		return nil, err
	}

	var content []byte
	err = helpers.TarIterate(dataReader, func(tr *tar.Reader, hdr *tar.Header) error {
		if filepath.Clean(hdr.Name) == controlMember {
			content, err = ioutil.ReadAll(tr)
			if err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return content, nil
}

// Unpack unpacks the data.tar.{gz,bz2,xz} into the given target directory
// with click specific verification, i.e. no files will be extracted outside
// of the targetdir (no ".." inside the data.tar is allowed)
func (d *ClickDeb) Unpack(targetDir string) error {
	var err error

	d.file, err = os.Open(d.Path)
	if err != nil {
		return err
	}
	defer d.file.Close()

	dataReader, err := d.skipToArMember("data.tar")
	if err != nil {
		return err
	}

	// and unpack
	return helpers.UnpackTar(dataReader, targetDir, clickVerifyContentFn)
}

func addFileToAr(arWriter *ar.Writer, filename string) error {
	stat, err := os.Stat(filename)
	if err != nil {
		return err
	}
	dataF, err := os.Open(filename)
	if err != nil {
		return nil
	}
	defer dataF.Close()
	size := stat.Size()
	if size%2 == 1 {
		size++
	}
	hdr := &ar.Header{
		Name: filepath.Base(filename),
		Mode: int64(stat.Mode()),
		Size: size,
	}
	arWriter.WriteHeader(hdr)
	_, err = io.Copy(arWriter, dataF)
	// io.Copy() is confused by the fact that a ar file must be even
	// aligned, so it may return a bigger amounts of bytes written
	// than requested. This is not a error so we ignore it
	if err != io.ErrShortWrite {
		return err
	}

	return nil
}

func addDataToAr(arWriter *ar.Writer, filename string, data []byte) error {
	size := int64(len(data))
	if size%2 == 1 {
		size++
	}
	hdr := &ar.Header{
		Name: filename,
		Mode: 0644,
		Size: size,
	}
	arWriter.WriteHeader(hdr)
	_, err := arWriter.Write([]byte(data))
	if err != nil {
		return err
	}

	return nil
}

func IsSymlink(mode os.FileMode) bool {
	return (mode & os.ModeSymlink) == 1
}

type TarIterFunc func(path string) bool

func TarCreate(tarname string, sourceDir string, fn TarIterFunc) error {
	w, err := os.Create(tarname)
	if err != nil {
		return err
	}
	defer w.Close()

	// FIXME: hardcoded gzip
	gzipWriter := gzip.NewWriter(w)
	defer gzipWriter.Close()
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	helpers.ChDir(sourceDir, func() {
		err = filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
			st, err := os.Stat(path)
			if err != nil {
				return err
			}
			if !st.Mode().IsRegular() && !IsSymlink(st.Mode()) {
				return nil
			}

			add := true
			if fn != nil {
				add = fn(path)
			}
			if !add {
				return nil
			}

			// huh? golang, come on!
			target, _ := os.Readlink(path)
			hdr, err := tar.FileInfoHeader(info, target)
			if err != nil {
				return err
			}
			hdr.Name = "./" + path
			hdr.Uid = 0
			hdr.Gid = 0
			hdr.Uname = "root"
			hdr.Gname = "root"

			if err := tarWriter.WriteHeader(hdr); err != nil {
				return err
			}

			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.Copy(tarWriter, f)
			if err != nil {
				return err
			}

			return nil
		})
	})

	return err
}

// Pack takes a build debian directory with DEBIAN/ dir and creates a
// deb from it
func (d *ClickDeb) Pack(sourceDir string) error {
	var err error

	// create file
	d.file, err = os.Create(d.Path)
	if err != nil {
		return err
	}
	defer d.file.Close()

	// control
	tempdir, err := ioutil.TempDir("", "data")
	defer os.RemoveAll(tempdir)
	if err != nil {
		return err
	}

	// control
	controlName := filepath.Join(tempdir, "control.tar.gz")
	if err := TarCreate(controlName, filepath.Join(sourceDir, "DEBIAN"), nil); err != nil {
		return err
	}

	// data
	dataName := filepath.Join(tempdir, "data.tar.gz")
	err = TarCreate(dataName, sourceDir, func(path string) bool {
		if strings.HasPrefix(path, "DEBIAN/") {
			return false
		}

		return true
	})

	if err != nil {
		return err
	}

	// create ar
	arWriter := ar.NewWriter(d.file)
	arWriter.WriteGlobalHeader()

	// debian magic
	if err := addDataToAr(arWriter, "debian-binary", []byte("2.0\n")); err != nil {
		return err
	}

	// control file
	if err := addFileToAr(arWriter, controlName); err != nil {
		return err
	}

	// data file
	if err := addFileToAr(arWriter, dataName); err != nil {
		return err
	}

	return nil
}

func (d *ClickDeb) skipToArMember(memberPrefix string) (io.Reader, error) {
	var err error

	// find the right ar member
	arReader := ar.NewReader(d.file)
	var header *ar.Header
	for {
		header, err = arReader.Next()
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(header.Name, memberPrefix) {
			break
		}
	}

	// find out what compression
	var dataReader io.Reader
	switch {
	case strings.HasSuffix(header.Name, ".gz"):
		dataReader, err = gzip.NewReader(d.file)
		if err != nil {
			return nil, err
		}
	case strings.HasSuffix(header.Name, ".bz2"):
		dataReader = bzip2.NewReader(d.file)
	case strings.HasSuffix(header.Name, ".xz"):
		dataReader = xzPipeReader(d.file)
	default:
		return nil, fmt.Errorf("Can not handle %s", header.Name)
	}

	return dataReader, nil
}
