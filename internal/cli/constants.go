// Package cli is the smstunnel command-line tool: the same commands, options, output and exit codes as the
// Node.js version (npm package "smstunnel"), built on github.com/narcisbodea/smstunnel-go.
package cli

// Version is the CLI's version. Release builds set it with -ldflags "-X <module>/internal/cli.Version=<x.y.z>";
// it is kept equal to the Node.js version's package.json.
var Version = "1.0.0"

const (
	defaultAPIBase = "https://smstunnel.io/api/v1"
	dashboardURL   = "https://dashboard.smstunnel.io"
	playURL        = "https://play.google.com/store/apps/details?id=io.smstunnel.app"

	maxTextLength           = 1600
	maxCodeTextLength       = 320
	maxIdempotencyKeyLength = 255
)

// Exit codes (README "Exit codes").
const (
	exitOK           = 0
	exitUnexpected   = 1
	exitUsage        = 2
	exitAuth         = 3
	exitForbidden    = 4
	exitNotFound     = 5
	exitRateLimited  = 6
	exitUnavailable  = 7
	exitTextFailed   = 8
	exitStillWaiting = 9
	exitInterrupted  = 130
)
