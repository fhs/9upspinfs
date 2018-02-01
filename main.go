// Copyright 2018 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"os"

	"upspin.io/cmd/cacheserver/cacheutil"
	"upspin.io/config"
	"upspin.io/flags"
	"upspin.io/log"
	"upspin.io/transports"
	"upspin.io/version"
)

const cmdName = "9upspinfs"

var _9pnet = flag.String("9pnet", "service", "network name for listen address")
var _9paddr = flag.String("9paddr", "upspin", "network listen address")
var debug = flag.Int("debug", 0, "9P debug level")

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s\n", os.Args[0])
	flag.PrintDefaults()
}

func main() {
	flag.Usage = usage
	flags.Parse(flags.Server, "cachedir", "cachesize", "prudent", "version")

	if flags.Version {
		fmt.Print(version.Version())
		return
	}

	// Normal setup, get configuration from file and push user cache onto config.
	cfg, err := config.FromFile(flags.Config)
	if err != nil {
		log.Debug.Fatal(err)
	}

	// Set any flags contained in the config.
	if err := config.SetFlagValues(cfg, cmdName); err != nil {
		log.Fatalf("%s: %s", cmdName, err)
	}

	transports.Init(cfg)

	// Start the cacheserver if needed.
	if cacheutil.Start(cfg) {
		// Using a cacheserver, adjust cache size for upspinfs down.
		flags.CacheSize = flags.CacheSize / 10
	}

	if flag.NArg() != 0 {
		usage()
		os.Exit(2)
	}
	do(cfg, *_9pnet, *_9paddr, *debug)
}
