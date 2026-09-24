package services

import (
	"context"
	"time"

	"github.com/NdoleStudio/httpsms/pkg/entities"
	"github.com/NdoleStudio/httpsms/pkg/repositories"
	"github.com/NdoleStudio/stacktrace"
)

// CUSTOM FILE — not upstream. Quiet hours for the gateway phone.
//
// WHAT "QUIET" MEANS
//
// A phone is quiet whenever it is OUTSIDE the send windows of the message send
// schedule attached to it (Settings → phone → Message Send Schedule). No
// schedule, or a schedule with no windows, means never quiet — upstream
// behaviour, unchanged.
//
// WHY
//
// Every push the server sends is high priority (see Send in
// phone_notification_service.go), and every high-priority push wakes the data
// radio. On this phone a radio wake is not free: it brings the VPN up and fires
// the "connectivity changed" event that a pile of other apps listen for. During
// quiet hours httpSMS must cause NO network traffic at all, from either end.
//
// Server side that is three things, each a one-line hook in upstream code:
//
//  1. Outgoing messages already wait for the next window — upstream's
//     PhoneNotificationRepository.Schedule does that. Explicit send_at requests
//     skipped it; scheduleExact now clamps them too (quietResolveCustom).
//  2. The missed-heartbeat probe is a high-priority push. SendHeartbeatFCM
//     does not send it while quiet.
//  3. The phone goes silent on purpose, so HeartbeatService.Monitor must not
//     read that silence as "phone offline" and email about it.
//
// Expiry needs nothing: the expiry clock starts when the push is SENT
// (ScheduleExpirationCheck uses NotificationSentAt), and a held message has not
// been pushed yet.
//
// The app learns the same windows from GET /v1/phones/quiet-hours
// (handlers/phone_handler_custom.go) and stops its own heartbeats and uploads to
// match — android/.../QuietHoursCustom.kt.

// QuietHoursCustom resolves a phone's quiet period from its send schedule.
type QuietHoursCustom struct {
	phoneRepository    repositories.PhoneRepository
	scheduleRepository repositories.MessageSendScheduleRepository
}

// quietHoursCustom is package state rather than a field on each service so that
// no upstream constructor changes shape. Nil means the feature is not wired up,
// and every check below then answers "not quiet".
var quietHoursCustom *QuietHoursCustom

// SetQuietHoursCustom wires quiet hours in. Called once from the DI container.
func SetQuietHoursCustom(phoneRepository repositories.PhoneRepository, scheduleRepository repositories.MessageSendScheduleRepository) *QuietHoursCustom {
	quietHoursCustom = &QuietHoursCustom{
		phoneRepository:    phoneRepository,
		scheduleRepository: scheduleRepository,
	}
	return quietHoursCustom
}

// ScheduleForOwner returns the send schedule attached to the phone with this
// number, or nil when it has none.
func (q *QuietHoursCustom) ScheduleForOwner(ctx context.Context, userID entities.UserID, owner string) (*entities.MessageSendSchedule, error) {
	phone, err := q.phoneRepository.Load(ctx, userID, owner)
	if err != nil {
		return nil, stacktrace.Propagatef(err, "cannot load phone [%s] for user [%s]", owner, userID)
	}
	return q.ScheduleForPhone(ctx, phone)
}

// ScheduleForPhone returns the send schedule attached to a phone, or nil when
// it has none.
func (q *QuietHoursCustom) ScheduleForPhone(ctx context.Context, phone *entities.Phone) (*entities.MessageSendSchedule, error) {
	if phone == nil || phone.MessageSendScheduleID == nil {
		return nil, nil
	}
	schedule, err := q.scheduleRepository.Load(ctx, phone.UserID, *phone.MessageSendScheduleID)
	if stacktrace.GetCode(err) == repositories.ErrCodeNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, stacktrace.Propagatef(err, "cannot load send schedule [%s] for phone [%s]", *phone.MessageSendScheduleID, phone.ID)
	}
	return schedule, nil
}

// quietNowCustom reports whether the phone is outside its send windows right
// now. Any failure to decide answers false: the fallback is upstream behaviour.
func quietNowCustom(ctx context.Context, phone *entities.Phone) bool {
	if quietHoursCustom == nil {
		return false
	}
	schedule, err := quietHoursCustom.ScheduleForPhone(ctx, phone)
	if err != nil || schedule == nil {
		return false
	}
	now := time.Now().UTC()
	return schedule.ResolveScheduledAt(now).After(now)
}

// quietNowByOwnerCustom is quietNowCustom for callers that only hold the
// phone number.
func quietNowByOwnerCustom(ctx context.Context, userID entities.UserID, owner string) bool {
	if quietHoursCustom == nil {
		return false
	}
	phone, err := quietHoursCustom.phoneRepository.Load(ctx, userID, owner)
	if err != nil {
		return false
	}
	return quietNowCustom(ctx, phone)
}

// quietMonitorCustom is the heartbeat monitor's view of quiet hours. It returns
// quiet=true while the phone is inside quiet hours: the caller then skips both
// the missed-heartbeat push and the offline alarm.
//
// Outside quiet hours it returns a heartbeat whose Timestamp is never earlier
// than the start of the current window. Upstream only reacts while the last
// heartbeat is 16–80 minutes old, so after a quiet night — last heartbeat hours
// ago — it would never react again, and a phone that failed to check in when
// the window opened would go unnoticed. Measuring from the window start gives
// the phone the normal grace period after quiet hours end, then the normal probe
// and alarm. The returned value is a copy; the caller's heartbeat is untouched.
func quietMonitorCustom(ctx context.Context, heartbeat *entities.Heartbeat) (*entities.Heartbeat, bool) {
	if quietHoursCustom == nil || heartbeat == nil {
		return heartbeat, false
	}
	phone, err := quietHoursCustom.phoneRepository.Load(ctx, heartbeat.UserID, heartbeat.Owner)
	if err != nil {
		return heartbeat, false
	}
	schedule, err := quietHoursCustom.ScheduleForPhone(ctx, phone)
	if err != nil || schedule == nil || len(schedule.Windows) == 0 {
		return heartbeat, false
	}

	now := time.Now().UTC()
	if schedule.ResolveScheduledAt(now).After(now) {
		return heartbeat, true
	}

	windowStart, ok := quietCurrentWindowStartCustom(schedule, now)
	if !ok || !heartbeat.Timestamp.Before(windowStart) {
		return heartbeat, false
	}
	adjusted := *heartbeat
	adjusted.Timestamp = windowStart
	return &adjusted, false
}

// quietCurrentWindowStartCustom returns when the send window containing now
// began. Same window arithmetic as MessageSendSchedule.ResolveScheduledAt.
func quietCurrentWindowStartCustom(schedule *entities.MessageSendSchedule, now time.Time) (time.Time, bool) {
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return time.Time{}, false
	}
	local := now.In(location)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	for _, window := range schedule.Windows {
		if window.DayOfWeek != int(local.Weekday()) {
			continue
		}
		start := midnight.Add(time.Duration(window.StartMinute) * time.Minute)
		end := midnight.Add(time.Duration(window.EndMinute) * time.Minute)
		if !local.Before(start) && local.Before(end) {
			return start.UTC(), true
		}
	}
	return time.Time{}, false
}

// quietResolveCustom moves an explicit send time out of quiet hours to the
// start of the next window. Upstream lets send_at bypass the schedule; here a
// 03:00 send_at would wake the radio at 03:00, which is exactly what quiet
// hours exist to prevent.
func quietResolveCustom(ctx context.Context, phone *entities.Phone, scheduledAt time.Time) time.Time {
	if quietHoursCustom == nil {
		return scheduledAt
	}
	schedule, err := quietHoursCustom.ScheduleForPhone(ctx, phone)
	if err != nil || schedule == nil {
		return scheduledAt
	}
	return schedule.ResolveScheduledAt(scheduledAt)
}
