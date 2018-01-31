// Copyright 2018 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Command 9upspinfs is a 9P file server for Upspin.

Usage:

	9upspinfs [flags]

	By default, 9upspinfs starts the 9P file server as the Plan 9
	service named "upspin".

The flags are:

	-addr string
		network listen address (default "upspin")
	-debug int
		9P debug level
	-net string
		network name for listen address (default "service")

Examples:

To listen on TCP:

	9upspinfs -net tcp -addr localhost:7777

If you have Plan9Port (https://9fans.github.io/plan9port/):

	9upspinfs &	# posts service to p9p namespace directory
	9pfuse $(namespace)/upspin /mnt/upspin

But you're better off using upspinfs
(https://godoc.org/upspin.io/cmd/upspinfs) instead.

On Plan 9:

	9upspinfs &
	mount /srv/upspin /mnt/upspin
*/
package main