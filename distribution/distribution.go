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
	"sync"
)

const (
	Standard = "standard"
	Aurora   = "aurora"
)

// cache stores detection results keyed by DSN. If a DSN is not found in
// the cache, we query the database to determine if it is an Aurora instance.
var cache sync.Map // map[string]string

func Get(dsn string, db *sql.DB) string {
	if v, ok := cache.Load(dsn); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}

	// Detect Aurora by checking if aurora_version function exists.
	row := db.QueryRow("SELECT to_regproc('aurora_version') IS NOT NULL;")
	var detected bool
	if err := row.Scan(&detected); err == nil && detected {
		cache.Store(dsn, Aurora)
		return Aurora
	}

	cache.Store(dsn, Standard)
	return Standard
}

func IsAurora(dsn string, db *sql.DB) bool {
	return Get(dsn, db) == Aurora
}
