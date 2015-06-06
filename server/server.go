// Copyright 2015 Simon Zimmermann. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

// Package server implements a HTTP interface for the session service.

package server

import (
	"io"
	"net"
	"net/http"

	"github.com/simonz05/filesrv/config"
	"github.com/simonz05/util/handler"
	"github.com/simonz05/util/ioutil"
	"github.com/simonz05/util/log"
	"github.com/simonz05/util/sig"
)
import _ "expvar"

func Init(conf *config.Config) (io.Closer, error) {
	c, err := newContextFromConfig(conf)
	if err != nil {
		return nil, err
	}
	err = installHandlers(c)
	return io.Closer(c), err
}

func installHandlers(c *context) error {
	// global middleware
	var middleware []func(http.Handler) http.Handler

	switch log.Severity {
	case log.LevelDebug:
		middleware = append(middleware, handler.LogHandler, handler.MeasureHandler, handler.DebugHandle, handler.RecoveryHandler)
	case log.LevelInfo:
		middleware = append(middleware, handler.LogHandler, handler.RecoveryHandler)
	default:
		middleware = append(middleware, handler.RecoveryHandler)
	}

	http.Handle("/", handler.Use(http.FileServer(c.filesystem), middleware...))
	return nil
}

func ListenAndServe(laddr string, shutdown io.Closer) error {
	l, err := net.Listen("tcp", laddr)

	if err != nil {
		return err
	}

	log.Printf("server: Listen on %s", l.Addr())

	closer := ioutil.MultiCloser([]io.Closer{l, shutdown})
	sig.TrapCloser(closer)
	err = http.Serve(l, nil)
	log.Printf("server: Shutting down ..")
	return err
}
