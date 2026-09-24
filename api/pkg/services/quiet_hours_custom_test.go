package services

import (
	"context"
	"testing"
	"time"

	"github.com/NdoleStudio/httpsms/pkg/entities"
	"github.com/stretchr/testify/assert"
)

// CUSTOM FILE — not upstream. Tests for quiet_hours_custom.go.

func quietTestScheduleCustom() *entities.MessageSendSchedule {
	// Open 07:00–23:00 Amsterdam time on Wednesday (weekday 3).
	return &entities.MessageSendSchedule{
		Timezone: "Europe/Amsterdam",
		Windows: []entities.MessageSendScheduleWindow{
			{DayOfWeek: 3, StartMinute: 7 * 60, EndMinute: 23 * 60},
		},
	}
}

func TestQuietCurrentWindowStartCustom(t *testing.T) {
	schedule := quietTestScheduleCustom()
	location, _ := time.LoadLocation("Europe/Amsterdam")

	// Wednesday 2026-09-23 10:00 local is inside the window, which began at 07:00.
	start, ok := quietCurrentWindowStartCustom(schedule, time.Date(2026, 9, 23, 10, 0, 0, 0, location))
	assert.True(t, ok)
	assert.Equal(t, time.Date(2026, 9, 23, 7, 0, 0, 0, location).UTC(), start)

	// 23:30 is after the window closed.
	_, ok = quietCurrentWindowStartCustom(schedule, time.Date(2026, 9, 23, 23, 30, 0, 0, location))
	assert.False(t, ok)
}

func TestQuietResolveCustomMovesSendAtOutOfQuietHours(t *testing.T) {
	schedule := quietTestScheduleCustom()
	location, _ := time.LoadLocation("Europe/Amsterdam")

	// A 03:00 Wednesday send_at waits for the 07:00 window.
	resolved := schedule.ResolveScheduledAt(time.Date(2026, 9, 23, 3, 0, 0, 0, location))
	assert.Equal(t, time.Date(2026, 9, 23, 7, 0, 0, 0, location).UTC(), resolved)
}

func TestQuietHoursCustomUnwiredIsNeverQuiet(t *testing.T) {
	previous := quietHoursCustom
	quietHoursCustom = nil
	defer func() { quietHoursCustom = previous }()

	now := time.Now().UTC()
	assert.False(t, quietNowCustom(context.Background(), &entities.Phone{}))
	assert.Equal(t, now, quietResolveCustom(context.Background(), &entities.Phone{}, now))

	heartbeat := &entities.Heartbeat{Timestamp: now}
	got, quiet := quietMonitorCustom(context.Background(), heartbeat)
	assert.False(t, quiet)
	assert.Same(t, heartbeat, got)
}
