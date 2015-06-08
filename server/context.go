// Copyright 2015 Simon Zimmermann. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package server

import (
	"net/http"

	"github.com/simonz05/filesrv"
	"github.com/simonz05/filesrv/config"
)

type context struct {
	filesystem http.FileSystem
}

func newContextFromConfig(conf *config.Config) (*context, error) {
	c := &context{}
	c.filesystem = filesrv.NewCache(filesrv.New(conf.Origin), 50, 1024*1024*512)
	return c, nil
}

func (c *context) Close() error {
	return nil
}
