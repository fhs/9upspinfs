// Copyright 2018 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"crypto/sha1"
	"io"
	"os"
	"path"
	"runtime"
	"sort"
	"strings"
	"sync"

	"upspin.io/client"
	"upspin.io/log"
	"upspin.io/upspin"

	plan9 "9fans.net/go/plan9/client"
	go9p "github.com/lionkov/go9p/p"
	"github.com/lionkov/go9p/p/srv"
)

type upspinFS struct {
	srv.Srv
	client    upspin.Client
	userDirs  map[upspin.UserName]bool
	fileCache *fileCache
}

var _ srv.FidOps = (*upspinFS)(nil)
var _ srv.ReqOps = (*upspinFS)(nil)

func newUpspinFS(cfg upspin.Config, debug int) *upspinFS {
	return &upspinFS{
		Srv:      srv.Srv{Debuglevel: debug},
		client:   client.New(cfg),
		userDirs: map[upspin.UserName]bool{cfg.UserName(): true},
		fileCache: &fileCache{
			m: make(map[upspin.PathName]*File),
		},
	}
}

func (f *upspinFS) Attach(req *srv.Req) {
	if req.Afid != nil {
		req.RespondError(srv.Enoauth)
		return
	}
	req.Fid.Aux = new(Fid)
	req.RespondRattach(&rootQid)
}

func (f *upspinFS) Walk(req *srv.Req) {
	fid := req.Fid.Aux.(*Fid)
	tc := req.Tc

	if req.Newfid.Aux == nil {
		req.Newfid.Aux = new(Fid)
	}
	nfid := req.Newfid.Aux.(*Fid)
	*nfid = *fid

	wqids := make([]go9p.Qid, len(tc.Wname))
	path := string(fid.path)
	entry := fid.entry
	i := 0
	for ; i < len(tc.Wname); i++ {
		var p string
		if path == "" {
			p = tc.Wname[i]
		} else {
			p = path + "/" + tc.Wname[i]
		}
		ent, err := f.client.Lookup(upspin.PathName(p), false)
		if err != nil {
			if i == 0 {
				req.RespondError(srv.Enoent)
				return
			}
			break
		}
		if path == "" {
			f.userDirs[upspin.UserName(tc.Wname[i])] = true
		}
		wqids[i] = *dir2Qid(ent)
		path = p
		entry = ent
	}
	nfid.path = upspin.PathName(path)
	nfid.entry = entry
	req.RespondRwalk(wqids[0:i])
}

func (f *upspinFS) Open(req *srv.Req) {
	fid := req.Fid.Aux.(*Fid)
	tc := req.Tc

	if fid.path == "" {
		count := 0
		for user := range f.userDirs {
			entry, err := f.client.Lookup(upspin.PathName(user), false)
			if err != nil {
				req.RespondError(err)
			}
			st := dir2Dir(string(user), entry)
			b := go9p.PackDir(st, req.Conn.Dotu)
			fid.dirents = append(fid.dirents, b...)
			count += len(b)
			fid.direntends = append(fid.direntends, count)
		}
		req.RespondRopen(&rootQid, 0)
		return
	}
	if fid.entry.IsDir() {
		dirContents, err := f.client.Glob(string(fid.path) + "/*")
		if err != nil {
			req.RespondError(err)
		}
		count := 0
		for _, entry := range dirContents {
			st := dir2Dir(string(entry.Name), entry)
			b := go9p.PackDir(st, req.Conn.Dotu)
			fid.dirents = append(fid.dirents, b...)
			count += len(b)
			fid.direntends = append(fid.direntends, count)
		}
	} else {
		var err error
		switch tc.Mode & 3 {
		case go9p.OWRITE, go9p.ORDWR:
			fid.file, err = f.fileCache.Writable(f.client, fid.path, tc.Mode&go9p.OTRUNC != 0)
		default:
			fid.file, err = f.client.Open(fid.path)
		}
		if err != nil {
			req.RespondError(err)
			return
		}
	}
	req.RespondRopen(dir2Qid(fid.entry), 0)
}

func (f *upspinFS) Create(req *srv.Req) {
	fid := req.Fid.Aux.(*Fid)
	tc := req.Tc

	path := upspin.PathName(string(fid.path) + "/" + tc.Name)
	if _, err := f.client.Lookup(path, false); err == nil {
		req.RespondError(srv.Eexist)
		return
	}
	const badPerms = go9p.DMSYMLINK | go9p.DMLINK | go9p.DMNAMEDPIPE | go9p.DMDEVICE
	var err error
	var entry *upspin.DirEntry
	var file upspin.File
	switch {
	case tc.Perm&go9p.DMDIR != 0:
		entry, err = f.client.MakeDirectory(path)
	case tc.Perm&badPerms != 0:
		req.RespondError(&go9p.Error{"not implemented", go9p.EIO})
		return
	default:
		// Write an empty file in case Walk happened before file is closed.
		entry, err = f.client.Put(path, []byte{})
		if err == nil {
			file, err = f.fileCache.Writable(f.client, path, true)
		}
	}
	if err != nil {
		req.RespondError(err)
		return
	}
	fid.path = path
	fid.entry = entry
	fid.file = file
	req.RespondRcreate(dir2Qid(fid.entry), 0)
}

func (f *upspinFS) Read(req *srv.Req) {
	fid := req.Fid.Aux.(*Fid)
	tc := req.Tc
	rc := req.Rc

	go9p.InitRread(rc, tc.Count)
	var count int
	if fid.path == "" || fid.entry.IsDir() {
		if tc.Count == 0 || len(fid.direntends) == 0 {
			goto done
		}
		i := 0
		if tc.Offset != 0 {
			i = sort.SearchInts(fid.direntends, int(tc.Offset))
			if i >= len(fid.direntends) || fid.direntends[i] != int(tc.Offset) {
				req.RespondError(&go9p.Error{"invalid offset", go9p.EINVAL})
			}
		}
		if int(tc.Offset) == fid.direntends[len(fid.direntends)-1] {
			goto done
		}
		count = int(tc.Count)
		j := sort.SearchInts(fid.direntends, int(tc.Offset)+count)
		if j >= len(fid.direntends) || fid.direntends[j] != int(tc.Offset)+count {
			if j == 0 {
				count = 0
			} else {
				count = fid.direntends[j-1] - int(tc.Offset)
			}
		}
		if count <= 0 {
			req.RespondError(&go9p.Error{"too small read size for dir entry", go9p.EINVAL})
			return
		}
		copy(rc.Data, fid.dirents[tc.Offset:int(tc.Offset)+count])
	} else {
		var err error
		count, err = fid.file.ReadAt(rc.Data, int64(tc.Offset))
		if err != nil && err != io.EOF {
			req.RespondError(err)
			return
		}
	}
done:
	go9p.SetRreadCount(rc, uint32(count))
	req.Respond()
}

func (f *upspinFS) Write(req *srv.Req) {
	fid := req.Fid.Aux.(*Fid)
	tc := req.Tc

	n, err := fid.file.WriteAt(tc.Data, int64(tc.Offset))
	if err != nil {
		req.RespondError(err)
		return
	}
	req.RespondRwrite(uint32(n))
}

func (f *upspinFS) Clunk(req *srv.Req) {
	req.RespondRclunk()
}

func (f *upspinFS) Remove(req *srv.Req) {
	fid := req.Fid.Aux.(*Fid)
	if err := f.client.Delete(fid.path); err != nil {
		req.RespondError(err)
		return
	}
	req.RespondRremove()
}

func (f *upspinFS) Stat(req *srv.Req) {
	fid := req.Fid.Aux.(*Fid)
	req.RespondRstat(dir2Dir(string(fid.path), fid.entry))
}

func (f *upspinFS) Wstat(req *srv.Req) {
	fid := req.Fid.Aux.(*Fid)
	dir := &req.Tc.Dir

	os.Stdout.Sync()
	if dir.Name != "" {
		fiddir, _ := path.Split(string(fid.path))
		destpath := upspin.PathName(dir.Name)
		if destdir, _ := path.Split(string(dir.Name)); destdir == "" {
			// filename is relative to source directory
			destpath = upspin.PathName(path.Join(fiddir, dir.Name))
		}
		if _, err := f.client.Lookup(destpath, false); err == nil {
			req.RespondError(srv.Eexist)
			return
		}
		entry, err := f.client.Rename(fid.path, destpath)
		if err != nil {
			req.RespondError(err)
			return
		}
		fid.path = destpath
		fid.entry = entry
		req.RespondRwstat()
		return
	}
	req.RespondError(srv.Enotimpl)
}

func (f *upspinFS) FidDestroy(sfid *srv.Fid) {
	if sfid.Aux == nil {
		return
	}
	fid := sfid.Aux.(*Fid)
	if fid.file != nil {
		f.fileCache.Close(fid.file)
	}
	// TODO: delete file if ORCLOSE create mode?
}

type Fid struct {
	path  upspin.PathName
	entry *upspin.DirEntry

	// Initialized in Open or Create
	file       upspin.File
	dirents    []byte
	direntends []int
}

func dir2Dir(path string, d *upspin.DirEntry) *go9p.Dir {
	dir := new(go9p.Dir)
	dir.Uid = "augie"
	dir.Gid = "augie"
	dir.Mode = 0700

	if path == "" {
		dir.Qid = rootQid
		dir.Mode |= go9p.DMDIR
		dir.Name = "/"
		return dir
	}
	dir.Qid = *dir2Qid(d)
	if d.IsDir() {
		dir.Mode |= go9p.DMDIR
	}
	dir.Uid = string(d.Writer)
	dir.Atime = uint32(d.Time)
	dir.Mtime = uint32(d.Time)
	sz, _ := d.Size()
	dir.Length = uint64(sz)
	dir.Name = path[strings.LastIndex(path, "/")+1:]
	return dir
}

func getuint64(v []byte) uint64 {
	n := uint64(0)
	for _, b := range v[:8] {
		n = (n << 8) | uint64(b)
	}
	return n
}

// Qidpath returns the QID path for a path name.
// Some (old) 9fans discussion on "Qid path generation" using hash functions:
// https://marc.info/?l=9fans&m=111558880320502&w=2
func qidpath(name upspin.PathName) uint64 {
	b := sha1.Sum([]byte(name))
	return getuint64(b[:8])
}

func dir2Qid(d *upspin.DirEntry) *go9p.Qid {
	typ := uint8(0)
	if d.IsDir() {
		typ |= go9p.QTDIR
	}
	return &go9p.Qid{
		Path:    qidpath(d.Name),
		Version: uint32(d.Sequence),
		Type:    typ,
	}
}

var rootQid = go9p.Qid{
	Path:    qidpath(upspin.PathName("/")),
	Version: 0,
	Type:    go9p.QTDIR,
}

func do(cfg upspin.Config, net, addr string, debug int) {
	srv := newUpspinFS(cfg, debug)
	if !srv.Start(srv) {
		log.Debug.Fatal("Srv start failed")
	}
	if net == "service" {
		switch runtime.GOOS {
		case "plan9":
			conn, err := NewServiceConn(addr)
			if err != nil {
				log.Debug.Fatalf("DialService failed: %v", err)
			}
			srv.NewConn(conn)
			// Wait for Go runtime to detect deadlock.
			// Go9p does not provide a way detect when the goroutines
			// started by srv.NewConn have terminated.
			select {}
		default:
			net = "unix"
			addr = plan9.Namespace() + "/" + addr
		}
	}
	if err := srv.StartNetListener(net, addr); err != nil {
		log.Debug.Fatal(err)
	}
}

// FileCache stores a mapping of path name to the open file used for writing.
// This is used to implement concurrent writes.
type fileCache struct {
	m map[upspin.PathName]*File
	sync.Mutex
}

func (fc *fileCache) Writable(client upspin.Client, name upspin.PathName, truncate bool) (*File, error) {
	fc.Lock()
	defer fc.Unlock()
	file, ok := fc.m[name]
	if ok {
		return file, nil
	}
	file, err := Writable(client, name, truncate)
	if err != nil {
		return nil, err
	}
	fc.m[name] = file
	return file, nil
}

func (fc *fileCache) Close(file upspin.File) error {
	fc.Lock()
	defer fc.Unlock()

	name := file.Name()
	ff, ok := fc.m[name]
	if !ok || ff != file {
		// Some possibilities:
		// (1) The file was not opened for writing.
		// (2) The file is already closed by a Tcluck of some other fid
		//	that pointed to the same file.
		return file.Close()
	}
	err := file.Close()
	delete(fc.m, name)
	return err
}
