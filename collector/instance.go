// Copyright 2023 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package collector

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"github.com/blang/semver/v4"
	"github.com/lib/pq"
)

type instance struct {
	dsn     string
	name    string
	db      *sql.DB
	version semver.Version
}

func newInstance(dsn string) (*instance, error) {
	i := &instance{
		dsn: dsn,
	}

	// "Create" a database handle to verify the DSN provided is valid.
	// Open is not guaranteed to create a connection.
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.Close()

	i.name, err = parseServerName(dsn)
	if err != nil {
		return nil, err
	}
	return i, nil
}

// copy returns a copy of the instance.
func (i *instance) copy() *instance {
	return &instance{
		dsn:  i.dsn,
		name: i.name,
	}
}

func (i *instance) setup() error {
	db, err := sql.Open("postgres", i.dsn)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	i.db = db

	version, err := queryVersion(i.db)
	if err != nil {
		return fmt.Errorf("error querying postgresql version: %w", err)
	} else {
		i.version = version
	}
	return nil
}

func (i *instance) getDB() *sql.DB {
	return i.db
}

func (i *instance) Close() error {
	return i.db.Close()
}

// Regex used to get the "short-version" from the postgres version field.
// The result of SELECT version() is something like "PostgreSQL 9.6.2 on x86_64-pc-linux-gnu, compiled by gcc (GCC) 6.2.1 20160830, 64-bit"
var versionRegex = regexp.MustCompile(`^\w+ ((\d+)(\.\d+)?(\.\d+)?)`)
var serverVersionRegex = regexp.MustCompile(`^((\d+)(\.\d+)?(\.\d+)?)`)

func queryVersion(db *sql.DB) (semver.Version, error) {
	var version string
	err := db.QueryRow("SELECT version();").Scan(&version)
	if err != nil {
		return semver.Version{}, err
	}
	submatches := versionRegex.FindStringSubmatch(version)
	if len(submatches) > 1 {
		return semver.ParseTolerant(submatches[1])
	}

	// We could also try to parse the version from the server_version field.
	// This is of the format 13.3 (Debian 13.3-1.pgdg100+1)
	err = db.QueryRow("SHOW server_version;").Scan(&version)
	if err != nil {
		return semver.Version{}, err
	}
	submatches = serverVersionRegex.FindStringSubmatch(version)
	if len(submatches) > 1 {
		return semver.ParseTolerant(submatches[1])
	}
	return semver.Version{}, fmt.Errorf("could not parse version from %q", version)
}

func parseServerName(url string) (string, error) {
	dsn, err := pq.ParseURL(url)
	if err != nil {
		dsn = url
	}

	pairs := strings.Split(dsn, " ")
	kv := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		splitted := strings.SplitN(pair, "=", 2)
		if len(splitted) != 2 {
			return "", fmt.Errorf("malformed dsn %q", dsn)
		}
		// Newer versions of pq.ParseURL quote values so trim them off if they exist
		key := strings.Trim(splitted[0], "'\"")
		value := strings.Trim(splitted[1], "'\"")
		kv[key] = value
	}

	var fingerprint string

	if host, ok := kv["host"]; ok {
		fingerprint += host
	} else {
		fingerprint += "localhost"
	}

	if port, ok := kv["port"]; ok {
		fingerprint += ":" + port
	} else {
		fingerprint += ":5432"
	}

	return fingerprint, nil
}
