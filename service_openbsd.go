// Copyright 2018 Aaron Bieber
// Use of this source code is governed by a zlib-style
// license that can be found in the LICENSE file.

package service

import (
	"strings"
)

type openbsdRCService struct {
	name        string
	detect      func() bool
	interactive func() bool
	new         func(i Interface, c *Config) (Service, error)
}

func (sc openbsdRCService) String() string {
	return sc.name
}
func (sc openbsdRCService) Detect() bool {
	return sc.detect()
}
func (sc openbsdRCService) Interactive() bool {
	return sc.interactive()
}
func (sc openbsdRCService) New(i Interface, c *Config) (Service, error) {
	return sc.new(i, c)
}

func init() {
	ChooseSystem(openbsdRCService{
		name:        "openbsd-rc",
		detect:      func() bool { return true },
		interactive: func() bool { return false },
		new:         newRCService,
	},
	)
}

var tf = map[string]interface{}{
	"cmd": func(s string) string {
		return strings.Replace(s, `"`, `\"`, -1)
	},
	"cmdEscape": func(s string) string {
		return strings.Replace(s, " ", `\x20`, -1)
	},
}
