package unzip

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrExceededMaxEntrySize is returned when a single zip entry exceeds MaxEntrySize.
var ErrExceededMaxEntrySize = errors.New("zip entry exceeds maximum allowed decompressed size")

// ErrExceededMaxTotalSize is returned when the total decompressed output exceeds MaxTotalSize.
var ErrExceededMaxTotalSize = errors.New("zip archive exceeds maximum allowed total decompressed size")

type Unzip struct {
	// MaxEntrySize is the maximum decompressed size allowed per zip entry, in bytes.
	// Zero means no limit.
	MaxEntrySize int64
	// MaxTotalSize is the maximum total decompressed size allowed across all entries, in bytes.
	// Zero means no limit.
	MaxTotalSize int64
}

func New() *Unzip {
	return &Unzip{}
}

func (uz Unzip) Extract(source, destination string) (filenames []string, retErr error) {
	r, err := zip.OpenReader(source)
	if err != nil {
		return nil, err
	}

	defer func() {
		if cerr := r.Close(); cerr != nil && retErr == nil {
			retErr = cerr
		}
	}()

	err = os.MkdirAll(destination, 0o755)
	if err != nil {
		return nil, err
	}

	var totalWritten int64
	var extractedFiles []string
	for _, f := range r.File {
		n, err := uz.extractAndWriteFile(destination, f)
		if err != nil {
			return nil, err
		}
		totalWritten += n
		if uz.MaxTotalSize > 0 && totalWritten > uz.MaxTotalSize {
			return nil, fmt.Errorf("%w: %d bytes exceeds limit of %d", ErrExceededMaxTotalSize, totalWritten, uz.MaxTotalSize)
		}

		extractedFiles = append(extractedFiles, f.Name)
	}

	return extractedFiles, nil
}

func (uz Unzip) extractAndWriteFile(destination string, f *zip.File) (written int64, retErr error) {
	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer func() {
		if cerr := rc.Close(); cerr != nil && retErr == nil {
			retErr = cerr
		}
	}()

	path := filepath.Join(destination, f.Name)
	if !strings.HasPrefix(path, filepath.Clean(destination)+string(os.PathSeparator)) {
		return 0, fmt.Errorf("%s: illegal file path", path)
	}

	if f.FileInfo().IsDir() {
		err = os.MkdirAll(path, 0o755)
		if err != nil {
			return 0, err
		}
		return 0, nil
	}

	err = os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		return 0, err
	}
	mode := f.Mode()
	if runtime.GOOS != "windows" {
		if mode.Perm() == fs.FileMode(0o444) {
			log.Println(mode.Perm(), fs.FileMode(0o444))
			mode = fs.FileMode(0o644)
		}
	}
	outFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return 0, err
	}
	defer func() {
		if cerr := outFile.Close(); cerr != nil && retErr == nil {
			retErr = cerr
		}
	}()

	var reader io.Reader = rc
	if uz.MaxEntrySize > 0 {
		reader = io.LimitReader(rc, uz.MaxEntrySize+1)
	}

	n, err := io.Copy(outFile, reader)
	if err != nil {
		return n, err
	}

	if uz.MaxEntrySize > 0 && n > uz.MaxEntrySize {
		return n, fmt.Errorf("%w: entry %q decompressed to %d bytes, limit is %d", ErrExceededMaxEntrySize, f.Name, n, uz.MaxEntrySize)
	}

	return n, nil
}
