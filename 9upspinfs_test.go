// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//

// +build !windows

package main

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	rtdebug "runtime/debug"
	"testing"

	go9p "github.com/lionkov/go9p/p"
	"github.com/lionkov/go9p/p/clnt"
	"upspin.io/bind"
	"upspin.io/config"
	"upspin.io/errors"
	"upspin.io/factotum"
	"upspin.io/test/testutil"
	"upspin.io/upspin"

	dirserver "upspin.io/dir/inprocess"
	keyserver "upspin.io/key/inprocess"
	storeserver "upspin.io/store/inprocess"
)

type TestUser upspin.UserName

var _ go9p.User = TestUser("")

func (tu TestUser) Name() string               { return string(tu) }
func (tu TestUser) Id() int                    { return -1 }
func (tu TestUser) Groups() []go9p.Group       { return nil }
func (tu TestUser) IsMember(g go9p.Group) bool { return false }

var testConfig struct {
	root string
	cfg  upspin.Config
	clnt *clnt.Clnt
}

const (
	perm             = 0777
	maxBytes   int64 = 1e8
	serverAddr       = "127.0.0.1:7777"
)

func checkTransport(s upspin.Service) {
	if s == nil {
		panic(fmt.Sprintf("nil service"))
	}
	if t := s.Endpoint().Transport; t != upspin.InProcess {
		panic(fmt.Sprintf("bad transport %v, want inprocess", t))
	}
}

func testSetup(userName upspin.UserName) upspin.Config {
	inProcess := upspin.Endpoint{
		Transport: upspin.InProcess,
		NetAddr:   "", // ignored
	}

	// Create baseCfg with user1's keys.
	f, err := factotum.NewFromDir(testutil.Repo("key", "testdata", "user1")) // Always use user1's keys.
	if err != nil {
		panic("cannot initialize factotum: " + err.Error())
	}

	cfg := config.New()
	cfg = config.SetPacking(cfg, upspin.EEPack)
	cfg = config.SetKeyEndpoint(cfg, inProcess)
	cfg = config.SetStoreEndpoint(cfg, inProcess)
	cfg = config.SetDirEndpoint(cfg, inProcess)
	cfg = config.SetFactotum(cfg, f)

	bind.RegisterKeyServer(upspin.InProcess, keyserver.New())
	bind.RegisterStoreServer(upspin.InProcess, storeserver.New())
	bind.RegisterDirServer(upspin.InProcess, dirserver.New(cfg))

	cfg = config.SetUserName(cfg, userName)
	key, _ := bind.KeyServer(cfg, cfg.KeyEndpoint())
	checkTransport(key)
	dir, _ := bind.DirServer(cfg, cfg.DirEndpoint())
	checkTransport(dir)
	if cfg.Factotum().PublicKey() == "" {
		panic("empty public key")
	}
	user := &upspin.User{
		Name:      upspin.UserName(userName),
		Dirs:      []upspin.Endpoint{cfg.DirEndpoint()},
		Stores:    []upspin.Endpoint{cfg.StoreEndpoint()},
		PublicKey: cfg.Factotum().PublicKey(),
	}
	if err := key.Put(user); err != nil {
		panic(err)
	}
	name := upspin.PathName(userName) + "/"
	entry := &upspin.DirEntry{
		Name:       name,
		SignedName: name,
		Attr:       upspin.AttrDirectory,
		Writer:     userName,
	}
	_, err = dir.Put(entry)
	if err != nil && !errors.Is(errors.Exist, err) {
		panic(err)
	}
	return cfg
}

func mount() error {
	// Set up a user config.
	uname := upspin.UserName("user1@google.com")
	cfg := testSetup(uname)
	testConfig.cfg = cfg

	// start server
	go do(cfg, "tcp", serverAddr, *debug)

	// The server may take some time to start up
	var client *clnt.Clnt
	var err error
	cur, err := user.Current()
	if err != nil {
		return err
	}
	user9p := TestUser(cur.Username)
	for i := 0; i < 16; i++ {
		if client, err = clnt.Mount("tcp", serverAddr, "", 8192, user9p); err == nil {
			break
		}
	}
	if err != nil {
		return fmt.Errorf("Connect failed after many tries: %v", err)
	}
	testConfig.clnt = client
	testConfig.root = string(uname) + "/"
	return nil
}

func cleanup() {
	testConfig.clnt.Unmount()
}

func TestMain(m *testing.M) {
	if err := mount(); err != nil {
		fmt.Fprintf(os.Stderr, "startServer failed: %s\n", err)
		os.Exit(1)
	}
	rv := m.Run()
	cleanup()
	os.Exit(rv)
}

func mkTestDir(t *testing.T, name string) string {
	testDir := filepath.Join(testConfig.root, name)
	if _, err := testConfig.clnt.FCreate(testDir, perm|go9p.DMDIR, 0); err != nil {
		fatal(t, err)
	}
	return testDir
}

func randomBytes(t *testing.T, len int) []byte {
	buf := make([]byte, len)
	if _, err := rand.Read(buf); err != nil {
		fatal(t, err)
	}
	return buf
}

func writeFile(t *testing.T, fn string, buf []byte) *clnt.File {
	f, err := testConfig.clnt.FCreate(fn, 0600, go9p.OWRITE)
	if err != nil {
		// file already exists, so try to open it
		f, err = testConfig.clnt.FOpen(fn, go9p.OWRITE)
	}
	if err != nil {
		fatal(t, err)
	}
	n, err := f.Writen(buf, 0)
	if err != nil {
		f.Close()
		fatal(t, err)
	}
	if n != len(buf) {
		f.Close()
		fatalf(t, "%s: wrote %d bytes, expected %d", fn, n, len(buf))
	}
	return f
}

func readAndCheckContentsOrDie(t *testing.T, fn string, buf []byte) {
	err := readAndCheckContents(t, fn, buf)
	if err != nil {
		fatal(t, err)
	}
}

func readAndCheckContents(t *testing.T, fn string, buf []byte) error {
	f, err := testConfig.clnt.FOpen(fn, go9p.OREAD)
	if err != nil {
		return err
	}
	defer f.Close()
	rbuf := make([]byte, len(buf)+10)
	n, err := io.ReadFull(f, rbuf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return err
	}
	if n != len(buf) {
		return fmt.Errorf("%s: read %d bytes, expected %d", fn, n, len(buf))
	}
	for i := range buf {
		if buf[i] != rbuf[i] {
			return fmt.Errorf("%s: error at byte %d: %.2x should be %.2x", fn, i, rbuf[i], buf[i])
		}
	}
	return nil
}

func mkFile(t *testing.T, fn string, buf []byte) {
	f := writeFile(t, fn, buf)
	if err := f.Close(); err != nil {
		fatal(t, err)
	}
}

func mkDir(t *testing.T, fn string) {
	if _, err := testConfig.clnt.FCreate(fn, perm|go9p.DMDIR, 0); err != nil {
		fatal(t, err)
	}
}

func remove(t *testing.T, fn string) {
	if err := testConfig.clnt.FRemove(fn); err != nil {
		fatal(t, err)
	}
	notExist(t, fn, "removal")
}

func notExist(t *testing.T, fn, event string) {
	if _, err := testConfig.clnt.FStat(fn); err == nil {
		fatalf(t, "%s: should not exist after %s", fn, event)
	}
}

// TestFile tests creating, writing, reading, and removing a file.
func TestFile(t *testing.T) {
	testDir := mkTestDir(t, "testfile")
	buf := randomBytes(t, 16*1024)

	// Create and write a file.
	fn := filepath.Join(testDir, "file")
	wf := writeFile(t, fn, buf)

	// Read before close.
	// TODO: uncomment after caching is implemented
	//readAndCheckContentsOrDie(t, fn, buf)

	// Read after close.
	if err := wf.Close(); err != nil {
		t.Fatal(err)
	}
	readAndCheckContentsOrDie(t, fn, buf)

	// Test Rewriting part of the file.
	for i := 0; i < len(buf)/2; i++ {
		buf[i] = buf[i] ^ 0xff
	}
	wf = writeFile(t, fn, buf[:len(buf)/2])
	if err := wf.Close(); err != nil {
		t.Fatal(err)
	}
	readAndCheckContentsOrDie(t, fn, buf)
	remove(t, fn)
	remove(t, testDir)
}

func rename(src, dst string) error {
	d := go9p.NewWstatDir()
	fid, err := testConfig.clnt.FWalk(src)
	if err != nil {
		return err
	}
	d.Name = dst
	return testConfig.clnt.Wstat(fid, d)
}

// TestRename tests renaming a file.
func TestRename(t *testing.T) {
	testDir := mkTestDir(t, "testrename")

	// Check that file is renamed and old name is no longer valid.
	original := filepath.Join(testDir, "original")
	newname := filepath.Join(testDir, "newname")
	mkFile(t, original, []byte(original))
	if err := rename(original, newname); err != nil {
		t.Fatal(err)
	}
	readAndCheckContentsOrDie(t, newname, []byte(original))
	notExist(t, original, "rename")
	remove(t, newname)

	remove(t, testDir)
}

func fatal(t *testing.T, args ...interface{}) {
	t.Log(fmt.Sprintln(args...))
	t.Log(string(rtdebug.Stack()))
	t.FailNow()
}

func fatalf(t *testing.T, format string, args ...interface{}) {
	t.Log(fmt.Sprintf(format, args...))
	t.Log(string(rtdebug.Stack()))
	t.FailNow()
}
