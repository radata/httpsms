package repositories

import (
	"os"
	"strings"
	"time"
)

// CUSTOM FILE — not upstream. Keep local repository changes here so pulling a
// new httpSMS release touches gorm_user_repository.go in one place only: the
// Timezone field of the struct literal in LoadOrStore.
//
// WHY THIS EXISTS
//
// entities.User declares:
//
//	Timezone string `... gorm:"default:Africa/Accra"`
//
// and LoadOrStore never sets the field, so every new account inherits the
// upstream author's own timezone from the COLUMN DEFAULT. On a Dutch
// deployment that silently backdates or postdates everything a user reads —
// message timestamps, the send-schedule window, heartbeat times — by an hour or
// two depending on DST, with nothing anywhere reporting a problem.
//
// The struct tag cannot read the environment, so the value has to be supplied
// at row-creation time instead. Existing rows are NOT touched: this only fills
// in accounts created from here on. Change an existing one in Settings →
// Timezone, which posts to the same field.
//
// DEFAULT_TIMEZONE is an IANA name, e.g. Europe/Amsterdam. Resolution works
// because the api image sets ENV ZONEINFO=/zoneinfo.zip over the zoneinfo.zip
// copied out of the Go toolchain (see api/Dockerfile) — without that,
// LoadLocation would fail for every name but UTC.
func defaultTimezoneCustom() string {
	tz := strings.TrimSpace(os.Getenv("DEFAULT_TIMEZONE"))
	if tz == "" {
		return "UTC"
	}

	// Validate rather than trust. A typo stored here would be written to every
	// new row and only surface later as a LoadLocation error deep in an email
	// or scheduling path, far from the cause. UTC is a deliberate fallback: it
	// is obviously neutral, so a wrong-looking clock points at this setting
	// instead of looking like someone's plausible home timezone.
	if _, err := time.LoadLocation(tz); err != nil {
		return "UTC"
	}

	return tz
}
