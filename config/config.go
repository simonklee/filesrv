// Copyright 2015 Simon Zimmermann. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package config

import "github.com/BurntSushi/toml"

type Config struct {
	Listen        string
	TmpDir        string
	Origin        string
	AllowOrigin   []string `toml:"allow-origin"`
	HTTPRateLimit int64
}

func (c *Config) HasTempDir() bool {
	return c.TmpDir != ""
}

func ReadFile(filename string) (*Config, error) {
	config := new(Config)
	_, err := toml.DecodeFile(filename, config)

	if err != nil {
		return nil, err
	}

	if config.HTTPRateLimit == 0 {
		config.HTTPRateLimit = 1000
	}

	return config, err
}
