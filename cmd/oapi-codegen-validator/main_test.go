package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestGenerateRules(t *testing.T) {
	runDir(t, "testdata/generate_rules")
}

func TestEnrichSpec(t *testing.T) {
	runDir(t, "testdata/enrich_spec")
}

func runDir(t *testing.T, dir string) {
	t.Helper()
	inputs, err := filepath.Glob(filepath.Join(dir, "*.input.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, inputs, "no test input files found in %s", dir)

	for _, inputPath := range inputs {
		base := strings.TrimSuffix(filepath.Base(inputPath), ".input.yaml")
		t.Run(base, func(t *testing.T) {
			doc := loadFile(t, inputPath)
			errorPath := filepath.Join(dir, base+".error")

			if _, err := os.Stat(errorPath); err == nil {
				err := enrichSpec(doc)
				require.Error(t, err)
				if msg, _ := os.ReadFile(errorPath); len(strings.TrimSpace(string(msg))) > 0 {
					assert.ErrorContains(t, err, strings.TrimSpace(string(msg)))
				}
				return
			}

			require.NoError(t, enrichSpec(doc))

			expectedPath := filepath.Join(dir, base+".expected.yaml")
			assertMatchesFile(t, doc, expectedPath)
		})
	}
}

func loadFile(t *testing.T, path string) *openapi3.T {
	t.Helper()
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(path)
	require.NoError(t, err, "failed to load %s", path)
	return doc
}

func assertMatchesFile(t *testing.T, actual *openapi3.T, expectedPath string) {
	t.Helper()
	expected := loadFile(t, expectedPath)
	// yaml.Marshal sorts map keys alphabetically, giving stable output for equal docs.
	expectedYAML, err := yaml.Marshal(expected)
	require.NoError(t, err)
	actualYAML, err := yaml.Marshal(actual)
	require.NoError(t, err)
	assert.Equal(t, string(expectedYAML), string(actualYAML))
}
