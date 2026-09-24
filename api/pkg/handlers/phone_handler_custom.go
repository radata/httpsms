package handlers

import (
	"fmt"
	"strings"

	"github.com/NdoleStudio/httpsms/pkg/entities"
	"github.com/NdoleStudio/httpsms/pkg/repositories"
	"github.com/NdoleStudio/httpsms/pkg/services"
	"github.com/NdoleStudio/httpsms/pkg/telemetry"
	"github.com/NdoleStudio/stacktrace"
	"github.com/gofiber/fiber/v3"
)

// CUSTOM FILE — not upstream. GET /v1/phones/quiet-hours tells the Android app
// when its phone is quiet, so the app can stop its own heartbeats and uploads
// for the same hours the server holds messages. See
// services/quiet_hours_custom.go for what quiet hours are and why.

// QuietHoursHandlerCustom serves a phone's quiet-hours schedule.
type QuietHoursHandlerCustom struct {
	handler
	logger     telemetry.Logger
	tracer     telemetry.Tracer
	quietHours *services.QuietHoursCustom
}

// QuietHoursResponseCustom is the send schedule as the app needs it. No
// windows means the phone is never quiet.
type QuietHoursResponseCustom struct {
	Owner    string                               `json:"owner"`
	Timezone string                               `json:"timezone"`
	Windows  []entities.MessageSendScheduleWindow `json:"windows"`
}

// NewQuietHoursHandlerCustom creates a new QuietHoursHandlerCustom
func NewQuietHoursHandlerCustom(logger telemetry.Logger, tracer telemetry.Tracer, quietHours *services.QuietHoursCustom) (h *QuietHoursHandlerCustom) {
	return &QuietHoursHandlerCustom{
		logger:     logger.WithService(fmt.Sprintf("%T", h)),
		tracer:     tracer,
		quietHours: quietHours,
	}
}

// RegisterPhoneAPIKeyRoutes registers the route with the same middleware chain
// as the app's other calls (heartbeats, fcm-token), so both phone API keys and
// account API keys work.
func (h *QuietHoursHandlerCustom) RegisterPhoneAPIKeyRoutes(router fiber.Router, middlewares ...fiber.Handler) {
	h.register(router, fiber.MethodGet, "/v1/phones/quiet-hours", middlewares, h.Show)
}

// Show returns the quiet-hours schedule for ?owner=<phone number>.
func (h *QuietHoursHandlerCustom) Show(c fiber.Ctx) error {
	ctx, span := h.tracer.StartFromFiberCtx(c)
	defer span.End()

	ctxLogger := h.tracer.CtxLogger(h.logger, span)

	owner := strings.TrimSpace(c.Query("owner"))
	if owner == "" {
		return h.responseBadRequest(c, stacktrace.NewError("the owner query parameter is required"))
	}

	if !h.authorizePhoneAPIKey(c, owner) {
		return h.responsePhoneAPIKeyUnauthorized(c, owner, h.userFromContext(c))
	}

	schedule, err := h.quietHours.ScheduleForOwner(ctx, h.userIDFomContext(c), owner)
	if stacktrace.GetCode(err) == repositories.ErrCodeNotFound {
		return h.responseNotFound(c, fmt.Sprintf("cannot find phone with number [%s]", owner))
	}
	if err != nil {
		ctxLogger.Error(stacktrace.Propagatef(err, "cannot load quiet hours for phone [%s]", owner))
		return h.responseInternalServerError(c)
	}

	response := QuietHoursResponseCustom{Owner: owner, Windows: []entities.MessageSendScheduleWindow{}}
	if schedule != nil {
		response.Timezone = schedule.Timezone
		response.Windows = schedule.Windows
	}
	return h.responseOK(c, "fetched quiet hours", response)
}
