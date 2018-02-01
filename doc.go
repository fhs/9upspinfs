// Copyright 2018 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Command 9upspinfs is a 9P file server for Upspin.

If the config or flags specify a cache server endpoint and cacheserver
is not running, upspinfs will attempt to start one. All the flags listed
below are also passed to the cacheserver should one be started.

Usage:

	9upspinfs [flags]

	By default, 9upspinfs starts the 9P file server as the Plan 9
	service named "upspin".

The flags are:

  -9paddr string
    	network listen address (default "upspin")
  -9pnet string
    	network name for listen address (default "service")
  -addr host:port
    	publicly accessible network address (host:port)
  -cachedir directory
    	directory containing all file caches (default "$HOME/upspin")
  -cachesize int
    	maximum bytes for file caches (default 5000000000)
  -config file
    	user's configuration file (default "$HOME/upspin/config")
  -debug int
    	9P debug level
  -http address
    	address for incoming insecure network connections (default ":80")
  -https address
    	address for incoming secure network connections (default ":443")
  -insecure
    	whether to serve insecure HTTP instead of HTTPS
  -letscache directory
    	Let's Encrypt cache directory (default "$HOME/upspin/letsencrypt")
  -log level
    	level of logging: debug, info, error, disabled (default info)
  -prudent
    	protect against malicious directory server
  -tls_cert file
    	TLS Certificate file in PEM format
  -tls_key file
    	TLS Key file in PEM format
  -version
    	print build version and exit
  -writethrough
    	make storage cache writethrough

Examples:

To listen on TCP:

	9upspinfs -9pnet tcp -9paddr localhost:7777

If you have Plan9Port (https://9fans.github.io/plan9port/):

	9upspinfs &	# posts service to p9p namespace directory
	# mount using v9fs
	mount -t 9p $(namespace)/acme /mnt/upspin -o trans=unix,uname=$USER
	# or mount using fuse
	9pfuse $(namespace)/upspin /mnt/upspin

But you're better off using upspinfs
(https://godoc.org/upspin.io/cmd/upspinfs) instead.

On Plan 9:

	9upspinfs &
	mount /srv/upspin /n/upspin
*/
package main
