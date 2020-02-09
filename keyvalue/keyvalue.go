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

package keyvalue

import (
	"time"

	"github.com/go-redis/redis/v7"
)

// Store represents a key-value storage.
type Store interface {
	Get(key string) (string, error)
	Set(key, value string) error
	SetExpiring(key, value string, expires time.Duration) error
	Delete(key string) error
}

// Prefixed is a key-value store that prefixes each key.
type Prefixed struct {
	store  Store
	prefix string
}

func NewPrefixed(store Store, prefix string) *Prefixed {
	return &Prefixed{
		store:  store,
		prefix: prefix,
	}
}

func (s *Prefixed) Get(key string) (string, error) {
	return s.store.Get(s.prefix + key)
}

func (s *Prefixed) Set(key, value string) error {
	return s.store.Set(s.prefix+key, value)
}

func (s *Prefixed) SetExpiring(key, value string, expires time.Duration) error {
	return s.store.SetExpiring(s.prefix+key, value, expires)
}

func (s *Prefixed) Delete(key string) error {
	return s.store.Delete(s.prefix + key)
}

type Redis struct {
	client *redis.Client
}

func NewRedis(client *redis.Client) *Redis {
	return &Redis{
		client: client,
	}
}

func (s *Redis) Get(key string) (string, error) {
	val, err := s.client.Get(key).Result()
	if err == redis.Nil {
		return "", nil
	}

	return val, err
}

func (s *Redis) Set(key, value string) error {
	return s.SetExpiring(key, value, -1)
}

func (s *Redis) SetExpiring(key, value string, expires time.Duration) error {
	return s.client.Set(key, value, expires).Err()
}

func (s *Redis) Delete(key string) error {
	return s.client.Del(key).Err()
}
