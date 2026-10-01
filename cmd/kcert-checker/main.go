package main

import (
	"flag"

	"github.com/sirajudheenam/kcert-checker/app"
)

func main() {
	opts := app.Options{}
	flag.StringVar(&opts.ConfigPath, "config", "config.yaml", "Path to configuration file")
	flag.StringVar(&opts.OutputFormat, "output", "table", "Output format: table or json")
	flag.StringVar(&opts.ListenAddr, "listen", "", "Override metrics listen address (e.g. :9090)")
	flag.StringVar(&opts.FailOn, "fail-on", "", "Exit non-zero if any cert has this status or worse: warning|critical|expired")
	flag.BoolVar(&opts.Once, "once", false, "Run a single scan, print results, and exit")
	flag.Parse()

	app.Run(opts)
}
