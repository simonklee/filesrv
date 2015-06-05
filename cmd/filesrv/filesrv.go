// Copyright 2015 Simon Zimmermann. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

// filesrv starts a HTTP server which serves files from sources.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"

	"github.com/simonz05/filesrv/config"
	"github.com/simonz05/filesrv/server"
	"github.com/simonz05/util/log"
)

var (
	help           = flag.Bool("h", false, "show help text")
	laddr          = flag.String("http", ":6069", "set bind address for the HTTP server")
	version        = flag.Bool("version", false, "show version number and exit")
	configFilename = flag.String("config", "config.toml", "config file path")
	cpuprofile     = flag.String("debug.cpuprofile", "", "write cpu profile to file")
)

var Version = "0.1.0"

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s [OPTIONS]\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "\nOptions:\n")
	flag.PrintDefaults()
}

func main() {
	flag.Usage = usage
	flag.Parse()
	log.Println("start filesrv …")

	if *version {
		fmt.Fprintln(os.Stdout, Version)
		return
	}

	if *help {
		flag.Usage()
		os.Exit(1)
	}

	conf, err := config.ReadFile(*configFilename)

	if err != nil {
		log.Fatal(err)
	}

	if conf.Listen == "" && *laddr == "" {
		log.Fatal("Listen address required")
	} else if conf.Listen == "" {
		conf.Listen = *laddr
	}

	runtime.GOMAXPROCS(runtime.NumCPU())

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	closer, err := server.Init(conf)

	if err != nil {
		log.Fatalf("error instantiating HTTP server: %v", err)
	}

	err = server.ListenAndServe(conf.Listen, closer)

	if err != nil {
		log.Errorln(err)
	}
}
