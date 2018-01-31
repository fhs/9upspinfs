// Copyright 2018 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// 9upspinfs is a 9P file server for Upspin.
package main

import (
	"flag"
	"fmt"
	"os"

	"upspin.io/config"
	"upspin.io/flags"
	"upspin.io/log"
)

var network = flag.String("net", "service", "network name for listen address")
var addr = flag.String("addr", "upspin", "network listen address")
var debug = flag.Int("debug", 0, "9P debug level")

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s\n", os.Args[0])
	flag.PrintDefaults()
}

func main() {
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() != 0 {
		usage()
		os.Exit(2)
	}
	cfg, err := config.FromFile(flags.Config)
	if err != nil {
		log.Debug.Fatal(err)
	}
	do(cfg, *network, *addr, *debug)
}
