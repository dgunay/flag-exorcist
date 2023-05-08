package main

import (
	"github.com/dgunay/flag-exorcist/flagexorcist"
	"github.com/ilyakaznacheev/cleanenv"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() {
	cfg := flagexorcist.Config{}
	if err := cleanenv.ReadEnv(&cfg); err != nil {
		panic(err)
	}

	flagexorcist.Initialize(cfg)
	analyzer := flagexorcist.Analyzer

	singlechecker.Main(analyzer)
}
