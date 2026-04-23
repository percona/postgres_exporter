package main

import (
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/blang/semver/v4"
	"github.com/stretchr/testify/require"
)

func clearVersionCache() {
	versionCache = sync.Map{}
}

func resetVersionHooks() {
	loadVersion = checkPostgresVersion
	currentTime = time.Now
}

func TestGetVersionUsesCache(t *testing.T) {
	clearVersionCache()
	resetVersionHooks()
	t.Cleanup(func() {
		clearVersionCache()
		resetVersionHooks()
	})

	now := time.Unix(1_000, 0)
	currentTime = func() time.Time { return now }

	versionCache.Store("127.0.0.1:5432", versionInfo{
		semantic: semver.MustParse("14.22.0"),
		full:     "PostgreSQL 14.22",
		cachedAt: now,
	})

	version, versionString, err := getVersion((*sql.DB)(nil), "127.0.0.1:5432")
	require.NoError(t, err)
	require.Equal(t, semver.MustParse("14.22.0"), version)
	require.Equal(t, "PostgreSQL 14.22", versionString)
}

func TestGetVersionRefreshesExpiredCache(t *testing.T) {
	clearVersionCache()
	resetVersionHooks()
	t.Cleanup(func() {
		clearVersionCache()
		resetVersionHooks()
	})

	now := time.Unix(1_000, 0)
	currentTime = func() time.Time { return now }

	versionCache.Store("127.0.0.1:5432", versionInfo{
		semantic: semver.MustParse("14.22.0"),
		full:     "PostgreSQL 14.22",
		cachedAt: now.Add(-versionCacheTTL - time.Second),
	})

	loadVersion = func(_ *sql.DB, _ string) (semver.Version, string, error) {
		return semver.MustParse("15.0.0"), "PostgreSQL 15.0", nil
	}

	version, versionString, err := getVersion((*sql.DB)(nil), "127.0.0.1:5432")
	require.NoError(t, err)
	require.Equal(t, semver.MustParse("15.0.0"), version)
	require.Equal(t, "PostgreSQL 15.0", versionString)

	cached, ok := versionCache.Load("127.0.0.1:5432")
	require.True(t, ok)
	require.Equal(t, versionInfo{
		semantic: semver.MustParse("15.0.0"),
		full:     "PostgreSQL 15.0",
		cachedAt: now,
	}, cached)
}
