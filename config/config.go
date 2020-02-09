// A simple website in Go.
// Copyright (c) 2020. Tamás Demeter-Haludka
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package config

import (
	"os"
	"strings"

	"github.com/sirupsen/logrus"
)

// Storage represents a configuration storage.
type Storage interface {
	Get(key string) string
}

// LoggerStorage is a storage that can log its values.
type LoggerStorage interface {
	Storage
	LogAll(logger logrus.FieldLogger)
}

// PrefixerStorage is an extension of Storage that prefixes each key.
type PrefixerStorage struct {
	storage Storage
	prefix  string
}

// NewPrefixerStorage creates a PrefixerStorage.
func NewPrefixerStorage(storage Storage, prefix string) *PrefixerStorage {
	return &PrefixerStorage{
		storage: storage,
		prefix:  prefix,
	}
}

func (s *PrefixerStorage) Get(key string) string {
	return s.storage.Get(s.prefix + key)
}

func (s *PrefixerStorage) LogAll(logger logrus.FieldLogger) {
	if ls, ok := s.storage.(LoggerStorage); ok {
		ls.LogAll(logger)
	}
}

// EnvStorage loads the configuration from environment variables.
type EnvStorage struct{}

func (s EnvStorage) Get(key string) string {
	return os.Getenv(strings.ToUpper(key))
}

func (s EnvStorage) LogAll(logger logrus.FieldLogger) {
	fields := make(logrus.Fields)
	for _, line := range os.Environ() {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			fields[parts[0]] = parts[1]
		}
	}
	logger.WithFields(fields).Debugln("environment variables")
}

// MapStorage loads the configuration from a map.
type MapStorage map[string]string

func (m MapStorage) Get(key string) string {
	return m[key]
}

func (m MapStorage) LogAll(logger logrus.FieldLogger) {
	fields := make(logrus.Fields)
	for k, v := range m {
		fields[k] = v
	}
	logger.WithFields(fields).Debugln("configuration variables")
}
