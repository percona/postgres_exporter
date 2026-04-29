package versioncache

import (
	"database/sql"
	"sync"
	"time"

	"github.com/blang/semver/v4"
)

type LoadVersion func(*sql.DB, string) (semver.Version, string, error)

type versionInfo struct {
	semantic semver.Version
	full     string
	cachedAt time.Time
}

var (
	versionCache = make(map[string]versionInfo)
	versionMtx   sync.Mutex
	CurrentTime  = time.Now
)

const TTL = 1 * time.Minute

func GetVersion(db *sql.DB, server string, loadVersion LoadVersion) (semver.Version, string, error) {
	versionMtx.Lock()
	defer versionMtx.Unlock()

	if cachedVersion, ok := versionCache[server]; ok {
		if CurrentTime().Sub(cachedVersion.cachedAt) < TTL {
			return cachedVersion.semantic, cachedVersion.full, nil
		}
	}

	semanticVersion, versionString, err := loadVersion(db, server)
	if err != nil {
		return semver.Version{}, "", err
	}

	versionCache[server] = versionInfo{
		semantic: semanticVersion,
		full:     versionString,
		cachedAt: CurrentTime(),
	}

	return semanticVersion, versionString, nil
}

func Clear() {
	versionMtx.Lock()
	defer versionMtx.Unlock()

	versionCache = make(map[string]versionInfo)
}
