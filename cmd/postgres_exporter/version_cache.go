package main

import (
	"database/sql"
	"sync"
	"time"

	"github.com/blang/semver/v4"
	"github.com/go-kit/log/level"
)

type versionInfo struct {
	semantic semver.Version
	full     string
	cachedAt time.Time
}

var (
	versionCache sync.Map
	loadVersion  = checkPostgresVersion
	currentTime  = time.Now
)

const versionCacheTTL = 5 * time.Minute

func getVersion(db *sql.DB, server string) (semver.Version, string, error) {
	if version, ok := versionCache.Load(server); ok {
		cachedVersion := version.(versionInfo)
		if currentTime().Sub(cachedVersion.cachedAt) < versionCacheTTL {
			level.Debug(logger).Log("msg", "Using cached PostgreSQL version", "server", server, "version", cachedVersion.semantic)
			return cachedVersion.semantic, cachedVersion.full, nil
		}

		level.Debug(logger).Log("msg", "Cached PostgreSQL version expired", "server", server, "version", cachedVersion.semantic)
	}

	semanticVersion, versionString, err := loadVersion(db, server)
	if err != nil {
		return semver.Version{}, "", err
	}

	versionCache.Store(server, versionInfo{
		semantic: semanticVersion,
		full:     versionString,
		cachedAt: currentTime(),
	})

	return semanticVersion, versionString, nil
}
