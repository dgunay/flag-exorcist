package flagexorcist_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dgunay/flag-exorcist/flagexorcist"
	"github.com/rs/zerolog"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAll(t *testing.T) {
	t.Parallel()

	workDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get wd: %s", err)
	}

	flagexorcist.Initialize(flagexorcist.Config{
		Cutoff:      0,
		FlagSymbols: []string{"MyFlag"},
		LogLevel:    flagexorcist.LogLevel(zerolog.DebugLevel),
		RepoPath:    "..",
	})

	testdata := filepath.Join(filepath.Dir(workDir), "testdata")
	analysistest.Run(t, testdata, flagexorcist.Analyzer, "./src/...")
}
