// Copyright 2018 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build darwin dragonfly freebsd linux nacl netbsd openbsd solaris windows

package main

import (
	"net"
)

func NewServiceConn(name string) (net.Conn, error) {
	panic("unimplemented")
}
