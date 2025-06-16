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

package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	_ "github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"
)

const (
	// Query file constants
	QueriesHRFile     = "../../queries-hr.yml"
	QueriesMRFile     = "../../queries-mr.yaml"
	QueriesLRFile     = "../../queries-lr.yaml"
	QueriesUptimeFile = "../../queries-postgres-uptime.yml"
)

type CustomQuerySuite struct {
	queryFiles []string
}

var _ = Suite(&CustomQuerySuite{})

// SetUpSuite initializes the test suite
func (s *CustomQuerySuite) SetUpSuite(c *C) {
	s.queryFiles = []string{
		QueriesHRFile,
		QueriesMRFile,
		QueriesLRFile,
		QueriesUptimeFile,
	}
}

// Test for SQL injection prevention and query safety
func (s *CustomQuerySuite) TestCustomQuerysSafety(c *C) {
	// Define patterns for potentially dangerous SQL operations
	dangerousPatterns := []struct {
		pattern string
		message string
	}{
		{`(?i)\bDROP\s+TABLE\b`, "DROP TABLE"},
		{`(?i)\bDROP\s+DATABASE\b`, "DROP DATABASE"},
		{`(?i)\bDELETE\s+FROM\b`, "DELETE FROM"},
		{`(?i)\bINSERT\s+INTO\b`, "INSERT INTO"},
		{`(?i)\bUPDATE\s+\w+\s+SET\b`, "UPDATE SET"},
		{`(?i)\bALTER\s+TABLE\b`, "ALTER TABLE"},
		{`(?i)\bCREATE\s+TABLE\b`, "CREATE TABLE"},
		{`(?i)\bTRUNCATE\s+TABLE\b`, "TRUNCATE TABLE"},
		{`--.*`, "SQL comment --"},
		{`/\*.*\*/`, "SQL comment /* */"},
		{`\bxp_\w+`, "Extended stored procedure"},
		{`\bsp_\w+`, "System stored procedure"},
	}

	for _, filePath := range s.queryFiles {
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			continue
		}

		data, err := os.ReadFile(filePath)
		c.Assert(err, IsNil)

		var userQueries UserQueries
		err = yaml.Unmarshal(data, &userQueries)
		c.Assert(err, IsNil)

		for queryName, query := range userQueries {
			// Check for dangerous SQL patterns using regex
			for _, dangerousPattern := range dangerousPatterns {
				matched, regexErr := regexp.MatchString(dangerousPattern.pattern, query.Query)
				c.Assert(regexErr, IsNil, Commentf("Regex error for pattern %s", dangerousPattern.pattern))
				c.Assert(matched, Equals, false,
					Commentf("Query '%s' in file '%s' contains potentially dangerous pattern: '%s'",
						queryName, filePath, dangerousPattern.message))
			}
		}
	}
}

// createTempQueryDir creates a temporary directory with query files for testing.
// It copies the specified query files into the temporary directory and returns the directory path.
// Returns empty string if no files were successfully copied.
func (s *CustomQuerySuite) createTempQueryDir(c *C, queryFiles []string) string {
	tempDir, err := os.MkdirTemp("", "postgres_exporter_test_queries_*")
	c.Assert(err, IsNil, Commentf("Failed to create temp directory"))

	copiedFiles := 0
	for _, queryFile := range queryFiles {
		if _, err := os.Stat(queryFile); os.IsNotExist(err) {
			c.Logf("Query file %s does not exist, skipping", queryFile)
			continue
		}

		// Read source file
		data, err := os.ReadFile(queryFile)
		c.Assert(err, IsNil, Commentf("Failed to read query file: %s", queryFile))

		// Write to temp directory
		fileName := filepath.Base(queryFile)
		destPath := filepath.Join(tempDir, fileName)
		err = os.WriteFile(destPath, data, 0644)
		c.Assert(err, IsNil, Commentf("Failed to write query file to temp dir: %s", destPath))

		copiedFiles++
	}

	if copiedFiles == 0 {
		os.RemoveAll(tempDir)
		return ""
	}

	return tempDir
}

// createTempQueryDirWithContent creates a temporary directory with custom query content.
// This is useful for testing specific query configurations without needing external files.
func (s *CustomQuerySuite) createTempQueryDirWithContent(c *C, content string, filename string) string {
	tempDir, err := os.MkdirTemp("", "postgres_exporter_test_queries_*")
	c.Assert(err, IsNil, Commentf("Failed to create temp directory"))

	destPath := filepath.Join(tempDir, filename)
	err = os.WriteFile(destPath, []byte(content), 0644)
	c.Assert(err, IsNil, Commentf("Failed to write query file to temp dir: %s", destPath))

	return tempDir
}

// MetricAnalysisResult holds the results of analyzing collected metrics
type MetricAnalysisResult struct {
	HasUserQueriesError   bool
	HasExecutedMetric     bool
	UserQueriesLoadError  float64
	ExecutedCount         float64
	FoundExpectedMetrics  map[string]bool
	TotalMetricsCollected int
}

// extractMetricName extracts the metric name from a Prometheus metric descriptor
func (s *CustomQuerySuite) extractMetricName(desc *prometheus.Desc) string {
	metricDescString := desc.String()
	if strings.Contains(metricDescString, `fqName: "`) {
		start := strings.Index(metricDescString, `fqName: "`) + 9
		end := strings.Index(metricDescString[start:], `"`)
		if end > 0 {
			return metricDescString[start : start+end]
		}
	}
	return ""
}

// analyzeMetrics collects and analyzes metrics from a channel
func (s *CustomQuerySuite) analyzeMetrics(metricsChan <-chan prometheus.Metric, expectedMetrics []string) MetricAnalysisResult {
	result := MetricAnalysisResult{
		FoundExpectedMetrics: make(map[string]bool),
	}

	for metric := range metricsChan {
		result.TotalMetricsCollected++

		metricName := s.extractMetricName(metric.Desc())
		if metricName == "" {
			continue
		}

		// Check for user queries load error metric
		if metricName == "pg_exporter_user_queries_load_error" {
			result.HasUserQueriesError = true
			dto := &dto.Metric{}
			if err := metric.Write(dto); err == nil && dto.Gauge != nil {
				result.UserQueriesLoadError = dto.Gauge.GetValue()
			}
		}

		// Check for user queries executed metric
		if metricName == "pg_exporter_user_queries_executed_total" {
			result.HasExecutedMetric = true
			dto := &dto.Metric{}
			if err := metric.Write(dto); err == nil && dto.Counter != nil {
				result.ExecutedCount = dto.Counter.GetValue()
			}
		}

		// Check for expected custom metrics
		for _, expectedMetric := range expectedMetrics {
			if metricName == expectedMetric {
				result.FoundExpectedMetrics[expectedMetric] = true
			}
		}
	}

	return result
}

// validateMetricAnalysisResult validates the results of metric analysis
func (s *CustomQuerySuite) validateMetricAnalysisResult(c *C, result MetricAnalysisResult, expectedMetrics []string, testName string) {
	// Verify metrics were collected
	c.Assert(result.TotalMetricsCollected, Not(Equals), 0,
		Commentf("Should collect at least some metrics for %s", testName))

	// Verify user queries load error metric exists and shows success
	c.Assert(result.HasUserQueriesError, Equals, true,
		Commentf("Should have user_queries_load_error metric for %s", testName))
	c.Assert(result.UserQueriesLoadError, Equals, 0.0,
		Commentf("user_queries_load_error should be 0 for %s, got %f", testName, result.UserQueriesLoadError))

	// Verify custom query execution tracking exists
	c.Assert(result.HasExecutedMetric, Equals, true,
		Commentf("Should have user_queries_executed_total metric for %s", testName))
	c.Assert(result.ExecutedCount >= 0.0, Equals, true,
		Commentf("user_queries_executed_total should be >= 0 for %s, got %f", testName, result.ExecutedCount))

	// Verify expected custom metrics are present (if queries executed successfully and we have expectations)
	if result.ExecutedCount > 0 && len(expectedMetrics) > 0 {
		for _, expectedMetric := range expectedMetrics {
			c.Assert(result.FoundExpectedMetrics[expectedMetric], Equals, true,
				Commentf("Expected custom metric '%s' should be present for %s when queries execute successfully", expectedMetric, testName))
		}
	}
}

// TestExporterScrapeWithCustomQueries tests the complete Exporter.scrape flow with custom queries
func (s *CustomQuerySuite) TestExporterScrapeWithCustomQueries(c *C) {
	dsn := os.Getenv("DATA_SOURCE_NAME")
	if dsn == "" {
		c.Skip("DATA_SOURCE_NAME not set, skipping exporter scrape tests")
		return
	}

	// Connect to PostgreSQL to create test data in the default database
	db, err := sql.Open("postgres", dsn)
	c.Assert(err, IsNil)
	defer db.Close()

	// Create test tables in the default database for testing
	testQueries := []string{
		`DROP TABLE IF EXISTS test_custom_query_table`,
		`CREATE TABLE test_custom_query_table (
			id SERIAL PRIMARY KEY,
			name VARCHAR(100),
			value NUMERIC,
			created_at TIMESTAMP DEFAULT NOW()
		)`,
		`INSERT INTO test_custom_query_table (name, value) 
		 SELECT 'test_' || i, random() * 100 
		 FROM generate_series(1, 10) i`,
	}

	for _, query := range testQueries {
		_, err := db.Exec(query)
		c.Assert(err, IsNil, Commentf("Failed to execute setup query: %s", query))
	}

	// Clean up test table at the end
	defer func() {
		db.Exec(`DROP TABLE IF EXISTS test_custom_query_table`)
	}()

	// Test different query resolutions
	testCases := []struct {
		name          string
		resolution    MetricResolution
		queryContent  string
		queryFile     string
		expectMetrics []string
	}{
		{
			name:       "Simple Test Query",
			resolution: HR,
			queryContent: `
pg_test_simple:
  query: "SELECT 1 as test_value, 'test_label' as test_name"
  metrics:
    - test_name:
        usage: "LABEL"
        description: "Test label"
    - test_value:
        usage: "GAUGE"
        description: "Test value"
`,
			queryFile: "test-simple.yml",
			expectMetrics: []string{
				"pg_test_simple_test_value",
			},
		},
		{
			name:       "Test Table Query",
			resolution: HR,
			queryContent: `
pg_test_table_stats:
  query: "SELECT name, value, count(*) as row_count FROM test_custom_query_table GROUP BY name, value"
  metrics:
    - name:
        usage: "LABEL"
        description: "Test name"
    - value:
        usage: "GAUGE"
        description: "Test value"
    - row_count:
        usage: "GAUGE"
        description: "Row count"
`,
			queryFile: "test-table.yml",
			expectMetrics: []string{
				"pg_test_table_stats_value",
				"pg_test_table_stats_row_count",
			},
		},
	}

	for _, testCase := range testCases {
		// Create temporary directory with query content
		tempDir := s.createTempQueryDirWithContent(c, testCase.queryContent, testCase.queryFile)
		defer os.RemoveAll(tempDir)

		// Create exporter with custom queries directory
		queryPaths := map[MetricResolution]string{
			testCase.resolution: tempDir,
		}

		exporter := NewExporter(
			[]string{dsn},
			WithUserQueriesEnabled(testCase.resolution),
			WithUserQueriesPath(queryPaths),
			DisableDefaultMetrics(true), // Focus on custom queries only
			DisableSettingsMetrics(true),
		)
		c.Assert(exporter, NotNil, Commentf("Failed to create exporter for %s", testCase.name))

		// Create a channel to collect metrics
		metricsChan := make(chan prometheus.Metric, 1000)

		// Use the proper Prometheus Collect interface
		go func() {
			defer close(metricsChan)
			exporter.Collect(metricsChan)
		}()

		// Collect and analyze metrics
		result := s.analyzeMetrics(metricsChan, testCase.expectMetrics)

		// Validate the results
		s.validateMetricAnalysisResult(c, result, testCase.expectMetrics, testCase.name)
	}
}

// TestExporterScrapeWithMultipleQueryFiles tests scraping with multiple query files
func (s *CustomQuerySuite) TestExporterScrapeWithMultipleQueryFiles(c *C) {
	dsn := os.Getenv("DATA_SOURCE_NAME")
	if dsn == "" {
		c.Skip("DATA_SOURCE_NAME not set, skipping multiple files test")
		return
	}

	// Connect to PostgreSQL to create test data in the default database
	db, err := sql.Open("postgres", dsn)
	c.Assert(err, IsNil)
	defer db.Close()

	// Create test tables in the default database for testing
	testQueries := []string{
		`DROP TABLE IF EXISTS test_multi_query_table`,
		`CREATE TABLE test_multi_query_table (
			id SERIAL PRIMARY KEY,
			category VARCHAR(50),
			amount NUMERIC,
			created_at TIMESTAMP DEFAULT NOW()
		)`,
		`INSERT INTO test_multi_query_table (category, amount) 
		 SELECT 'category_' || (i % 5), random() * 1000 
		 FROM generate_series(1, 20) i`,
		// Add another table to generate user table statistics for LR queries
		`DROP TABLE IF EXISTS test_lr_table`,
		`CREATE TABLE test_lr_table (
			id SERIAL PRIMARY KEY,
			name VARCHAR(100),
			value INTEGER,
			updated_at TIMESTAMP DEFAULT NOW()
		)`,
		`INSERT INTO test_lr_table (name, value) 
		 SELECT 'item_' || i, i * 10 
		 FROM generate_series(1, 50) i`,
		// Generate some table activity for statistics
		`SELECT COUNT(*) FROM test_multi_query_table`,
		`SELECT COUNT(*) FROM test_lr_table WHERE value > 100`,
		`UPDATE test_lr_table SET updated_at = NOW() WHERE id <= 10`,
	}

	for _, query := range testQueries {
		_, err := db.Exec(query)
		c.Assert(err, IsNil, Commentf("Failed to execute setup query: %s", query))
	}

	// Clean up test tables at the end
	defer func() {
		db.Exec(`DROP TABLE IF EXISTS test_multi_query_table`)
		db.Exec(`DROP TABLE IF EXISTS test_lr_table`)
	}()

	// Create temporary directories for different resolutions using specific query files
	var hrDir, mrDir, lrDir string

	// Create HR directory with HR-specific query file
	hrDir = s.createTempQueryDir(c, []string{QueriesHRFile, QueriesUptimeFile})
	if hrDir != "" {
		defer os.RemoveAll(hrDir)
	}

	// Create MR directory with MR-specific query file
	mrDir = s.createTempQueryDir(c, []string{QueriesMRFile})
	if mrDir != "" {
		defer os.RemoveAll(mrDir)
	}

	// Create LR directory with LR-specific query file
	lrDir = s.createTempQueryDir(c, []string{QueriesLRFile})
	if lrDir != "" {
		defer os.RemoveAll(lrDir)
	}

	// Create separate exporters for each resolution
	resolutions := []struct {
		name          string
		res           MetricResolution
		dir           string
		expectMetrics []string
	}{
		{
			name: "HR",
			res:  HR,
			dir:  hrDir,
			expectMetrics: []string{
				// Common metrics that should be present in HR queries
				"pg_postmaster_uptime_seconds", // from queries-postgres-uptime.yml
			},
		},
		{
			name: "MR",
			res:  MR,
			dir:  mrDir,
			expectMetrics: []string{
				// Common metrics that should be present in MR queries
				"pg_replication_lag",               // from queries-mr.yaml
				"pg_postmaster_start_time_seconds", // from queries-mr.yaml
				"pg_database_size_bytes",           // from queries-mr.yaml
			},
		},
		{
			name: "LR",
			res:  LR,
			dir:  lrDir,
			expectMetrics: []string{
				// Common metrics that should be present in LR queries
				"pg_stat_user_tables_seq_scan",         // from pg_stat_user_tables
				"pg_stat_user_tables_n_live_tup",       // from pg_stat_user_tables
				"pg_stat_user_tables_n_dead_tup",       // from pg_stat_user_tables
				"pg_statio_user_tables_heap_blks_read", // from pg_statio_user_tables
				"pg_statio_user_tables_heap_blks_hit",  // from pg_statio_user_tables
			},
		},
	}

	// Test each resolution with its query files
	for _, resolution := range resolutions {
		// Create exporter for this specific resolution
		exporter := NewExporter(
			[]string{dsn},
			WithUserQueriesEnabled(resolution.res),
			WithUserQueriesPath(map[MetricResolution]string{
				resolution.res: resolution.dir,
			}),
			DisableDefaultMetrics(true),
			DisableSettingsMetrics(true),
			AutoDiscoverDatabases(true),
		)
		c.Assert(exporter, NotNil, Commentf("Failed to create exporter for %s", resolution.name))

		// Collect metrics from this exporter
		metricsChan := make(chan prometheus.Metric, 1000)
		go func() {
			defer close(metricsChan)
			exporter.Collect(metricsChan)
		}()

		// Collect and analyze metrics
		result := s.analyzeMetrics(metricsChan, resolution.expectMetrics)

		// Validate the results
		s.validateMetricAnalysisResult(c, result, resolution.expectMetrics, resolution.name)
	}
}
