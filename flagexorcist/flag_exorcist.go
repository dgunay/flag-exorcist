package flagexorcist

import (
	"go/ast"
	"go/token"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/samber/mo"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

type Config struct {
	// The symbols that the user wants to look at
	FlagSymbols []string `env:"FLAG_SYMBOLS" env-required:"true"`

	// Cutoff duration for how old a flag can be before we complain about it
	Cutoff time.Duration `env:"CUTOFF" env-required:"true"`

	// Log level to log at
	LogLevel loglevel `env:"LOG_LEVEL" env-default:"info"`

	// Path to the git repo. Defaults to the current directory.
	RepoPath string `env:"REPO_PATH" env-default:"."`
}

type loglevel zerolog.Level

func (l *loglevel) SetValue(s string) error {
	lvl, err := zerolog.ParseLevel(s)
	if err != nil {
		return err
	}
	*l = loglevel(lvl)
	return nil
}

type runner struct {
	cfg Config
	l   zerolog.Logger
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
	// Get the full path to the repo
	repoPath, err := filepath.Abs(cfg.RepoPath)
	if err != nil {
		panic(err)
	}
	cfg.RepoPath = repoPath
	r.cfg = cfg

	r.l = log.Logger.Level(zerolog.Level(cfg.LogLevel))
}

func (r *runner) run(pass *analysis.Pass) (any, error) {
	r.l.Debug().Str("package", pass.Pkg.Name()).Msg("Running flagexorcist on package")

	// Get the git repo
	r.l.Debug().Str("path", r.cfg.RepoPath).Msg("Opening git repo")
	repo, err := git.PlainOpen(r.cfg.RepoPath)
	if err != nil {
		return nil, errors.Wrap(err, "open git repo")
	}

	identifiers := r.findFlagIdents(pass)

	// sort these into declarations and usages
	declarationCommitTimes := map[string]time.Time{}
	usagesByFlag := map[string][]*ast.Ident{}
	for _, id := range identifiers {
		if isDeclaration(id) && !hasKey(declarationCommitTimes, id.Name) {
			timeCommitted := r.timeCommitted(repo, id.Name, pass.Fset.Position(id.NamePos))
			if t := timeCommitted.OrEmpty(); !t.IsZero() {
				declarationCommitTimes[id.Name] = t
			}
		} else {
			usagesByFlag[id.Name] = append(usagesByFlag[id.Name], id)
		}
	}

	// We complain if any used symbol is very old
	for symbol, committedAt := range declarationCommitTimes {
		usages, ok := usagesByFlag[symbol]
		if !ok {
			continue
		}

		r.l.Debug().
			Time("committedAt", committedAt).
			Dur("cutoff", r.cfg.Cutoff).
			Str("symbol", symbol).
			Msg("Checking if flag is old")
		if committedAt.Before(time.Now().Add(-r.cfg.Cutoff)) {
			for _, usage := range usages {
				pass.Reportf(
					usage.Pos(),
					"Flag '%v', added on %v, is more than %v days old",
					symbol, committedAt.Format("2006-01-02"),
					r.cfg.Cutoff.Hours()/24,
				)
			}
		}

	}

	return nil, nil
}

func (r *runner) findFlagIdents(pass *analysis.Pass) []*ast.Ident {
	idents := []*ast.Ident{}

	inspect := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	nodeFilter := []ast.Node{
		(*ast.Ident)(nil),
	}
	inspect.Preorder(nodeFilter, func(node ast.Node) {
		id := node.(*ast.Ident)
		for _, symbol := range r.cfg.FlagSymbols {
			// TODO: disambiguate between packages

			if id.Name == symbol {
				r.l.Debug().
					Str("symbol", symbol).
					Any("pos", pass.Fset.Position(id.NamePos)).
					Msg("Found usage or declaration of flag symbol")

				idents = append(idents, id)
			}
		}
	})

	return idents
}

func isDeclaration(ident *ast.Ident) bool {
	if ident.Obj == nil {
		// The identifier doesn't have an object, it is not a declaration.
		return false
	}

	switch parent := ident.Obj.Decl.(type) {
	case *ast.ValueSpec:
		// The identifier is part of a const/var declaration.
		// Check if it's the first name in the list.
		for i, name := range parent.Names {
			if ident == name {
				return i == 0
			}
		}
	case *ast.Field:
		// The identifier is part of a struct field declaration.
		return ident == parent.Names[0]
	}

	return false
}

// Given some symbol, find the commit where it was added and return the Time of
// the commit.
func (r *runner) timeCommitted(
	repo *git.Repository, symbol string, pos token.Position,
) mo.Option[time.Time] {
	iter, err := repo.Log(&git.LogOptions{
		// Until: &r.cfg.Cutoff, // TODO: reinstate this
	})
	if err != nil {
		panic(err) // TODO:
		return mo.None[time.Time]()
	}

	timestamp := mo.None[time.Time]()

	iter.ForEach(func(commit *object.Commit) error {
		var file *object.File
		iter, err := commit.Files()
		if err != nil {
			return err
		}

		// Chop off everything before the base of the repo path to compare just
		// the relative path.
		searchFileName := strings.TrimPrefix(pos.Filename, r.cfg.RepoPath+"/")
		err = iter.ForEach(func(f *object.File) error {
			if f.Name == searchFileName {
				file = f
				return io.EOF
			}
			return nil
		})

		// If the file is found, search for the symbol within the file
		if file != nil {
			// TODO: we should only check changes to the file, not the whole file
			contents, err := file.Contents()
			if err != nil {
				return err
			}
			if strings.Contains(contents, symbol) {
				// The symbol was found in this commit, so return the commit timestamp
				r.l.Debug().
					Str("symbol", symbol).
					Str("file", pos.Filename).
					Str("commit", commit.Hash.String()).
					Str("when", commit.Author.When.String()).
					Msg("Symbol found in commit")
				timestamp = mo.Some[time.Time](commit.Author.When)
				return nil
			}
		}

		return nil
	})

	return timestamp
}

func hasKey[K comparable, V any](m map[K]V, k K) bool {
	_, ok := m[k]
	return ok
}
