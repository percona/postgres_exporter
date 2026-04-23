package main

import (
	"database/sql"
	"sync"
	"testing"

	"github.com/blang/semver/v4"
	"github.com/stretchr/testify/require"
)

func clearVersionCache() {
	versionCache = sync.Map{}
}

func TestGetVersionUsesCache(t *testing.T) {
	clearVersionCache()
	t.Cleanup(clearVersionCache)

	versionCache.Store("127.0.0.1:5432", versionInfo{
		semantic: semver.MustParse("14.22.0"),
		full:     "PostgreSQL 14.22",
	})

	version, versionString, err := getVersion((*sql.DB)(nil), "127.0.0.1:5432")
	require.NoError(t, err)
	require.Equal(t, semver.MustParse("14.22.0"), version)
	require.Equal(t, "PostgreSQL 14.22", versionString)
}
