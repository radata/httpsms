package telemetry

import (
	"os"
	"sync"

	"github.com/rs/zerolog"
)

// CUSTOM FILE — not upstream. Holds the log-level policy so zerolog_logger.go
// keeps a single-line change: the argument to SetGlobalLevel in
// NewZerologLogger.
//
// WHY THIS EXISTS
//
// NewZerologLogger called
//
//	zerolog.SetGlobalLevel(zerolog.TraceLevel)
//
// unconditionally, on EVERY construction — and WithService builds a new logger
// each time it is called, so the level was re-forced to Trace repeatedly during
// startup. Setting a level anywhere else (for example in the di package's log
// driver) was silently overwritten a moment later, which is exactly what
// happened: LOG_LEVEL=info was honoured, then immediately undone, and the
// container still emitted every container.go Debug line.
//
// On the server that is hundreds of lines per boot into a json-file capped at
// 10m x 3, which rotates away the startup errors worth keeping — the same
// argument as USE_HTTP_LOGGER=false.
//
// Resolved ONCE via sync.Once. The value cannot change during a process
// lifetime, and NewZerologLogger is called many times, so re-reading the
// environment on every call would be pure waste.
var (
	globalLevelOnce  sync.Once
	globalLevelValue zerolog.Level
)

// GlobalLevelCustom is the zerolog level this process should run at.
//
//   - local (ENV=local): Trace, unchanged from upstream. A dev box is where you
//     want every line.
//   - anywhere else: LOG_LEVEL if it parses, otherwise Info.
//
// An unparseable LOG_LEVEL falls back to Info rather than failing: a typo in an
// observability setting must never stop the API from booting.
func GlobalLevelCustom() zerolog.Level {
	globalLevelOnce.Do(func() {
		if os.Getenv("ENV") == "local" {
			globalLevelValue = zerolog.TraceLevel
			return
		}

		globalLevelValue = zerolog.InfoLevel
		if raw := os.Getenv("LOG_LEVEL"); raw != "" {
			if parsed, err := zerolog.ParseLevel(raw); err == nil {
				globalLevelValue = parsed
			}
		}
	})

	return globalLevelValue
}
