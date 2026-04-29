package versioncache

import (
	"database/sql"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/blang/semver/v4"
	"github.com/stretchr/testify/require"
)

func clearVersionCache() {
	Clear()
}

func resetVersionHooks() {
	CurrentTime = time.Now
}

const testServer = "127.0.0.1:5432"

func TestGetVersionUsesCache(t *testing.T) {
	clearVersionCache()
	resetVersionHooks()
	t.Cleanup(func() {
		clearVersionCache()
		resetVersionHooks()
	})

	now := time.Unix(1_000, 0)
	CurrentTime = func() time.Time { return now }

	versionCache[testServer] = versionInfo{
		semantic: semver.MustParse("14.22.0"),
		full:     "PostgreSQL 14.22",
		cachedAt: now,
	}

	version, versionString, err := GetVersion((*sql.DB)(nil), testServer, func(_ *sql.DB, _ string) (semver.Version, string, error) {
		t.Fatal("loadVersion should not be called for a fresh cache entry")
		return semver.Version{}, "", nil
	})
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
	CurrentTime = func() time.Time { return now }

	versionCache[testServer] = versionInfo{
		semantic: semver.MustParse("14.22.0"),
		full:     "PostgreSQL 14.22",
		cachedAt: now.Add(-TTL - time.Second),
	}

	version, versionString, err := GetVersion((*sql.DB)(nil), testServer, func(_ *sql.DB, _ string) (semver.Version, string, error) {
		return semver.MustParse("15.0.0"), "PostgreSQL 15.0", nil
	})
	require.NoError(t, err)
	require.Equal(t, semver.MustParse("15.0.0"), version)
	require.Equal(t, "PostgreSQL 15.0", versionString)

	cached, ok := versionCache[testServer]
	require.True(t, ok)
	require.Equal(t, versionInfo{
		semantic: semver.MustParse("15.0.0"),
		full:     "PostgreSQL 15.0",
		cachedAt: now,
	}, cached)
}

func TestGetVersionLoadsOnceForConcurrentMisses(t *testing.T) {
	clearVersionCache()
	resetVersionHooks()
	t.Cleanup(func() {
		clearVersionCache()
		resetVersionHooks()
	})

	var calls atomic.Int32
	loadVersion := func(_ *sql.DB, _ string) (semver.Version, string, error) {
		calls.Add(1)
		time.Sleep(10 * time.Millisecond)
		return semver.MustParse("15.0.0"), "PostgreSQL 15.0", nil
	}

	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			version, versionString, err := GetVersion((*sql.DB)(nil), testServer, loadVersion)
			require.NoError(t, err)
			require.Equal(t, semver.MustParse("15.0.0"), version)
			require.Equal(t, "PostgreSQL 15.0", versionString)
		}()
	}
	wg.Wait()

	require.Equal(t, int32(1), calls.Load())
}
