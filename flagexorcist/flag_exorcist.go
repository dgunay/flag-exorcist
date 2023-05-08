package flagexorcist

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/rs/zerolog"
	"github.com/samber/mo"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
)

type Config struct {
	// The symbols that the user wants to look at
	FlagSymbols []string `env:"FLAG_SYMBOLS" env-required:true`

	// Cutoff date for when a flag is considered old
	Cutoff time.Time `env:"CUTOFF" env-default:"2019-01-01"`

	// Log level to log at
	LogLevel zerolog.Level `env:"LOG_LEVEL" env-default:"info"`
}

type runner struct {
	cfg Config
}

var r runner

var Analyzer *analysis.Analyzer = &analysis.Analyzer{
	Name: "flagexorcist",
	Doc:  "Finds old flags",
	Run:  r.run,
	Requires: []*analysis.Analyzer{
		inspect.Analyzer,
	},
}

func Initialize(cfg Config) {
	r.cfg = cfg
}

func (r *runner) run(pass *analysis.Pass) (any, error) {
	identifiers := r.findFlagIdents(pass)

	timesCommmitted := map[*ast.Ident]time.Time{}

	// We find all usages of those symbols
	for _, id := range identifiers {
		use := pass.TypesInfo.Uses[id]
		pos := pass.Fset.Position(use.Pos())
		if timeCommitted(id.Name, pos).Before(r.cfg.Cutoff) {
		}
	}

	// We complain if any used symbol is very old

	return nil, nil
}

func (r *runner) findFlagIdents(pass *analysis.Pass) map[string]*ast.Ident {
	idents := map[string]*ast.Ident{}

	// Find the identifiers for the symbols we care about
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			// We only care about flag declarations
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}

			for _, symbol := range r.cfg.FlagSymbols {
				// TODO: disambiguate between packages

				if _, exists := idents[symbol]; id.Name == symbol && !exists {
					idents[symbol] = id
				}
			}

			return true
		})
	}

	return idents
}

// Given some symbol, find the commit where it was added and return the Time of
// the commit.
func (r *runner) timeCommitted(repo *git.Repository, symbol string, pos token.Position) mo.Option[time.Time] {
	iter, err := repo.Log(&git.LogOptions{Until: &r.cfg.Cutoff})
	if err != nil {
		panic(err) // TODO:
		return mo.None[time.Time]()
	}

	iter.ForEach(func(commit *object.Commit) error {
		// Get the list of files changed in this commit
		patch, err := commit.PatchContext(context.Background(), commit)
		if err != nil {
			return err
		}

		// Search for the given file in the list of changed files
		var file *object.File
		for _, f := range patch.Stats() {
			if f.To.Name == "/path/to/myfile.txt" {
				file, err = commit.File(f.To.Name)
				if err != nil {
					return err
				}
				break
			}
		}

		// If the file is found, search for the symbol within the file
		if file != nil {
			contents, err := file.Contents()
			if err != nil {
				return err
			}
			if symbolRegex.MatchString(contents) {
				// The symbol was found in this commit, so return the commit timestamp
				fmt.Printf("Symbol found in commit %v\n", parent.Hash)
				fmt.Printf("Symbol was added on %v\n", parent.Author.When)
				return nil
			}
		}

		return nil
	})
}
