package distribution

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func clearCache() {
	mtx.Lock()
	defer mtx.Unlock()

	cache = make(map[string]string)
}

func TestParseFingerprint(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{
			name: "URL with port",
			dsn:  "postgres://user:pass@127.0.0.1:5433/postgres?sslmode=disable",
			want: "127.0.0.1:5433",
		},
		{
			name: "URL without port",
			dsn:  "postgres://user:pass@db.example.com/postgres?sslmode=disable",
			want: "db.example.com:5432",
		},
		{
			name: "Keyword DSN with host and port",
			dsn:  "host=db.example.com port=55432 user=pmm-agent dbname=postgres",
			want: "db.example.com:55432",
		},
		{
			name: "Keyword DSN defaults",
			dsn:  "user=pmm-agent dbname=postgres",
			want: "localhost:5432",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseFingerprint(tt.dsn)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestGetCachesByFingerprint(t *testing.T) {
	clearCache()
	t.Cleanup(clearCache)

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`SELECT to_regproc\('aurora_version'\) IS NOT NULL;`).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	first := Get("postgres://user:pass@127.0.0.1:5432/postgres?sslmode=disable", db)
	second := Get("postgres://user:pass@127.0.0.1:5432/app_1?sslmode=disable", db)

	require.Equal(t, Standard, first)
	require.Equal(t, Standard, second)
	require.NoError(t, mock.ExpectationsWereMet())
}
