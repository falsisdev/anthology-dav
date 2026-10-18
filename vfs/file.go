package vfs

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"
)

// VirtualFileInfo implements os.FileInfo for virtual directories and files.
type VirtualFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	isDir   bool
}

func NewDirFileInfo(name string, modTime time.Time) *VirtualFileInfo {
	if modTime.IsZero() {
		modTime = time.Now()
	}
	return &VirtualFileInfo{
		name:    name,
		size:    0,
		mode:    os.ModeDir | 0755,
		modTime: modTime,
		isDir:   true,
	}
}

func NewContentFileInfo(name string, size int64, modTime time.Time) *VirtualFileInfo {
	if modTime.IsZero() {
		modTime = time.Now()
	}
	return &VirtualFileInfo{
		name:    name,
		size:    size,
		mode:    0644,
		modTime: modTime,
		isDir:   false,
	}
}

func (fi *VirtualFileInfo) Name() string       { return fi.name }
func (fi *VirtualFileInfo) Size() int64        { return fi.size }
func (fi *VirtualFileInfo) Mode() os.FileMode  { return fi.mode }
func (fi *VirtualFileInfo) ModTime() time.Time { return fi.modTime }
func (fi *VirtualFileInfo) IsDir() bool        { return fi.isDir }
func (fi *VirtualFileInfo) Sys() interface{}   { return nil }

// VirtualFile implements webdav.File, io.Reader, io.ReaderAt, io.Seeker, and io.Writer.
type VirtualFile struct {
	info     *VirtualFileInfo
	reader   *bytes.Reader
	children []fs.FileInfo
	dirPos   int
	onFetch  func() ([]byte, error)
	loaded   bool
}

func NewVirtualDir(name string, children []fs.FileInfo, modTime time.Time) *VirtualFile {
	return &VirtualFile{
		info:     NewDirFileInfo(name, modTime),
		children: children,
	}
}

func NewVirtualFile(name string, content []byte, modTime time.Time) *VirtualFile {
	return &VirtualFile{
		info:   NewContentFileInfo(name, int64(len(content)), modTime),
		reader: bytes.NewReader(content),
		loaded: true,
	}
}

func NewLazyVirtualFile(name string, estimatedSize int64, modTime time.Time, onFetch func() ([]byte, error)) *VirtualFile {
	return &VirtualFile{
		info:    NewContentFileInfo(name, estimatedSize, modTime),
		onFetch: onFetch,
		loaded:  false,
	}
}

func (f *VirtualFile) ensureLoaded() error {
	if f.loaded {
		return nil
	}
	if f.onFetch != nil {
		data, err := f.onFetch()
		if err != nil {
			return err
		}
		f.info.size = int64(len(data))
		f.reader = bytes.NewReader(data)
	} else {
		f.reader = bytes.NewReader(nil)
	}
	f.loaded = true
	return nil
}

func (f *VirtualFile) Close() error {
	return nil
}

func (f *VirtualFile) Read(p []byte) (n int, err error) {
	if f.info.isDir {
		return 0, os.ErrInvalid
	}
	if err := f.ensureLoaded(); err != nil {
		return 0, err
	}
	return f.reader.Read(p)
}

func (f *VirtualFile) ReadAt(p []byte, off int64) (n int, err error) {
	if f.info.isDir {
		return 0, os.ErrInvalid
	}
	if err := f.ensureLoaded(); err != nil {
		return 0, err
	}
	return f.reader.ReadAt(p, off)
}

func (f *VirtualFile) Seek(offset int64, whence int) (int64, error) {
	if f.info.isDir {
		return 0, os.ErrInvalid
	}
	if err := f.ensureLoaded(); err != nil {
		return 0, err
	}
	return f.reader.Seek(offset, whence)
}

func (f *VirtualFile) Readdir(count int) ([]fs.FileInfo, error) {
	if !f.info.isDir {
		return nil, os.ErrInvalid
	}
	if count <= 0 {
		res := f.children[f.dirPos:]
		f.dirPos = len(f.children)
		return res, nil
	}
	if f.dirPos >= len(f.children) {
		return nil, io.EOF
	}
	end := f.dirPos + count
	if end > len(f.children) {
		end = len(f.children)
	}
	res := f.children[f.dirPos:end]
	f.dirPos = end
	if f.dirPos >= len(f.children) {
		return res, io.EOF
	}
	return res, nil
}

func (f *VirtualFile) Stat() (fs.FileInfo, error) {
	return f.info, nil
}

func (f *VirtualFile) Write(p []byte) (n int, err error) {
	return 0, os.ErrPermission
}

// SanitizeFileName cleans characters that are invalid or problematic on macOS Finder, Infuse, and WebDAV.
func SanitizeFileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Untitled"
	}
	replacer := strings.NewReplacer(
		"/", "-",
		"\\", "-",
		":", " -",
		"?", "",
		"*", "",
		"\"", "'",
		"<", "(",
		">", ")",
		"|", "-",
		"\x00", "",
		"\r", "",
		"\n", " ",
	)
	cleaned := replacer.Replace(name)
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	cleaned = strings.Trim(cleaned, ". ")
	if cleaned == "" {
		return "Untitled"
	}
	return cleaned
}
