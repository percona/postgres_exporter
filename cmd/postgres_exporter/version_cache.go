package main

import (
	"database/sql"
	"sync"

	"github.com/blang/semver/v4"
	"github.com/go-kit/log/level"
)

type versionInfo struct {
	semantic semver.Version
	full     string
}

var versionCache sync.Map

func getVersion(db *sql.DB, server string) (semver.Version, string, error) {
	if version, ok := versionCache.Load(server); ok {
		cachedVersion := version.(versionInfo)
		level.Debug(logger).Log("msg", "Using cached PostgreSQL version", "server", server, "version", cachedVersion.semantic)
		return cachedVersion.semantic, cachedVersion.full, nil
	}

	semanticVersion, versionString, err := checkPostgresVersion(db, server)
	if err != nil {
		return semver.Version{}, "", err
	}

	versionCache.Store(server, versionInfo{
		semantic: semanticVersion,
		full:     versionString,
	})

	return semanticVersion, versionString, nil
}
