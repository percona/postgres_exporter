// Copyright (C) 2023 Percona LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package distribution

import (
	"database/sql"
	"fmt"
	"sync"

	"github.com/lib/pq"
)

const (
	Standard = "standard"
	Aurora   = "aurora"
)

// cache stores detection results keyed by host:port. If a host:port is not found in
// the cache, we query the database to determine if it is an Aurora instance.
var (
	cache = make(map[string]string)
	mtx   sync.Mutex
)

func Get(dsn string, db *sql.DB) string {
	fingerprint, err := parseFingerprint(dsn)
	if err != nil {
		fingerprint = dsn
	}

	mtx.Lock()
	defer mtx.Unlock()

	if cached, ok := cache[fingerprint]; ok {
		return cached
	}

	// Detect Aurora by checking if aurora_version function exists.
	row := db.QueryRow("SELECT to_regproc('aurora_version') IS NOT NULL;")
	var detected bool
	if err := row.Scan(&detected); err == nil && detected {
		cache[fingerprint] = Aurora
		return Aurora
	}

	cache[fingerprint] = Standard
	return Standard
}

func IsAurora(dsn string, db *sql.DB) bool {
	return Get(dsn, db) == Aurora
}

func parseFingerprint(dsn string) (string, error) {
	cfg, err := pq.NewConfig(dsn)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s:%d", cfg.Host, cfg.Port), nil
}
