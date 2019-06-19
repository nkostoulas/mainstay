// Copyright (c) 2018 CommerceBlock Team
// Use of this source code is governed by an MIT
// license that can be found in the LICENSE file.

package config

import (
	"bytes"
	"encoding/json"
	"errors"
)

// Handle reading conf files and parsing configuration options

const (
	ErroConfigNameNotFound   = "config name not found"
	ErrorConfigValueNotFound = "config value not found"
)

type ClientCfg map[string]interface{}

// Get config for a specific base name from conf file
func getCfg(name string, conf []byte) (ClientCfg, error) {
	file := bytes.NewReader(conf)
	dec := json.NewDecoder(file)
	var j map[string]map[string]interface{}
	err := dec.Decode(&j)
	if err != nil {
		return ClientCfg{}, errors.New(ErroConfigNameNotFound)
	}
	val, ok := j[name]
	if !ok {
		return ClientCfg{}, errors.New(ErroConfigNameNotFound)
	}
	return val, nil
}

// Get string values of config options for a base category
func (conf ClientCfg) getValue(key string) (string, error) {
	val, ok := conf[key]
	if !ok {
		return "", errors.New(ErrorConfigValueNotFound)
	}
	str, ok := val.(string)
	if !ok {
		return "", errors.New(ErrorConfigValueNotFound)
	}
	return str, nil
}

// Try get string values of config options for a base category
func (conf ClientCfg) tryGetValue(key string) string {
	val, ok := conf[key]
	if !ok {
		return ""
	}
	str, ok := val.(string)
	if !ok {
		return ""
	}
	return str
}
