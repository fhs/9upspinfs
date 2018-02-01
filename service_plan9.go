// Copyright 2018 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build plan9

package main

import (
	"fmt"
	"net"
	"os"
	"syscall"

	go9p "github.com/lionkov/go9p/p"
)

func PostFD(name string, pfd int) (*os.File, error) {
	p := fmt.Sprintf("/srv/%s", name)
	fd, err := syscall.Create(p, go9p.OWRITE|go9p.ORCLOSE|go9p.OCEXEC, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "|0")
	_, err = fmt.Fprintf(f, "%d", pfd)
	return f, err
}

type ServiceConn struct {
	*os.File
	name string
	srvf *os.File
}

// NewServiceConn returns a connection that has been posted
// to a Plan 9 service file (in /srv).
func NewServiceConn(name string) (net.Conn, error) {
	var fd [2]int
	if err := syscall.Pipe(fd[:]); err != nil {
		return nil, err
	}
	srvf, err := PostFD(name, fd[0])
	if err != nil {
		syscall.Close(fd[0])
		syscall.Close(fd[1])
		return nil, err
	}
	syscall.Close(fd[0])

	return &ServiceConn{
		File: os.NewFile(uintptr(fd[1]), "|1"),
		name: name,
		srvf: srvf,
	}, nil
}

func (c *ServiceConn) LocalAddr() net.Addr {
	return ServiceAddr(c.name)
}

func (c *ServiceConn) RemoteAddr() net.Addr {
	return ServiceAddr(c.name)
}

type ServiceAddr string

func (sa ServiceAddr) Network() string {
	return "service"
}

func (sa ServiceAddr) String() string {
	return string(sa)
}
