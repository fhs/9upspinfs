// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The code in this file is derived from upspin.io/client/file package
// to workaround an issue with WriteAt.
// See https://github.com/upspin/upspin/issues/573

package main

import (
	"upspin.io/errors"
	"upspin.io/upspin"
)

// maxInt is the int64 representation of the maximum value of an int.
// It allows us to verify that an int64 value never exceeds the length of a slice.
// In the tests, we cut it down to manageable size for overflow checking.
var maxInt = int64(^uint(0) >> 1)

// File is a simple implementation of upspin.File.
// It always keeps the whole file in memory under the assumption
// that it is encrypted and must be read and written atomically.
type File struct {
	name     upspin.PathName // Full path name.
	offset   int64           // File location for next read or write operation. Constrained to <= maxInt.
	writable bool            // File is writable (made with Create, not Open).
	closed   bool            // Whether the file has been closed, preventing further operations.

	// Used only by readers.
	config upspin.Config
	entry  *upspin.DirEntry
	size   int64
	bu     upspin.BlockUnpacker
	// Keep the most recently unpacked block around
	// in case a subsequent readAt starts at the same place.
	lastBlockIndex int
	lastBlockBytes []byte

	// Used only by writers.
	client upspin.Client // Client the File belongs to.
	data   []byte        // Contents of file.
}

var _ upspin.File = (*File)(nil)

// Writable creates a new file with a given name, belonging to a given
// client for write. Once closed, the file will overwrite any existing
// file with the same name.
func Writable(client upspin.Client, name upspin.PathName, truncate bool) (*File, error) {
	var data []byte
	if !truncate {
		var err error
		data, err = client.Get(name)
		if err != nil {
			return nil, err
		}
	}
	return &File{
		client:   client,
		name:     name,
		writable: true,
		data:     data,
	}, nil
}

// Name implements upspin.File.
func (f *File) Name() upspin.PathName {
	return f.name
}

// Read implements upspin.File.
func (f *File) Read(b []byte) (n int, err error) {
	panic("not implemented")
}

// ReadAt implements upspin.File.
func (f *File) ReadAt(b []byte, off int64) (n int, err error) {
	panic("not implemented")
}

// Seek implements upspin.File.
func (f *File) Seek(offset int64, whence int) (ret int64, err error) {
	panic("not implemented")
}

// Write implements upspin.File.
func (f *File) Write(b []byte) (n int, err error) {
	panic("not implemented")
}

// WriteAt implements upspin.File.
func (f *File) WriteAt(b []byte, off int64) (n int, err error) {
	const op errors.Op = "file.WriteAt"
	return f.writeAt(op, b, off)
}

func (f *File) writeAt(op errors.Op, b []byte, off int64) (n int, err error) {
	if f.closed {
		return 0, f.errClosed(op)
	}
	if !f.writable {
		return 0, errors.E(op, errors.Invalid, f.name, "not open for write")
	}
	if off < 0 {
		return 0, errors.E(op, errors.Invalid, f.name, "negative offset")
	}
	end := off + int64(len(b))
	if end > maxInt {
		return 0, errors.E(op, errors.Invalid, f.name, "file too long")
	}
	if end > int64(cap(f.data)) {
		// Grow the capacity of f.data but keep length the same.
		// Be careful not to ask for more than an int's worth of length.
		nLen := end * 3 / 2
		if nLen > maxInt {
			nLen = maxInt
		}
		ndata := make([]byte, len(f.data), nLen)
		copy(ndata, f.data)
		f.data = ndata
	}
	// Capacity is OK now. Fix the length if necessary.
	if end > int64(len(f.data)) {
		f.data = f.data[:end]
	}
	copy(f.data[off:], b)
	return len(b), nil
}

// Close implements upspin.File.
func (f *File) Close() error {
	const op errors.Op = "file.Close"
	if f.closed {
		return f.errClosed(op)
	}
	f.closed = true
	if !f.writable {
		f.lastBlockIndex = -1
		f.lastBlockBytes = nil
		if err := f.bu.Close(); err != nil {
			return errors.E(op, err)
		}
		return nil
	}
	_, err := f.client.Put(f.name, f.data)
	f.data = nil // Might as well release it early.
	return err
}

func (f *File) errClosed(op errors.Op) error {
	return errors.E(op, errors.Invalid, f.name, "is closed")
}
