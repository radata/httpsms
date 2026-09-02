package validators

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/NdoleStudio/httpsms/pkg/entities"
	"github.com/NdoleStudio/httpsms/pkg/requests"
	"github.com/NdoleStudio/httpsms/pkg/services"
	"github.com/NdoleStudio/httpsms/pkg/telemetry"
	"github.com/google/uuid"
	"github.com/thedevsaddam/govalidator"
)

// PhoneHandlerValidator validates models used in handlers.PhoneHandler
type PhoneHandlerValidator struct {
	validator
	logger          telemetry.Logger
	tracer          telemetry.Tracer
	scheduleService *services.MessageSendScheduleService
}

// NewPhoneHandlerValidator creates a new handlers.PhoneHandler validator
func NewPhoneHandlerValidator(
	logger telemetry.Logger,
	tracer telemetry.Tracer,
	scheduleService *services.MessageSendScheduleService,
) (v *PhoneHandlerValidator) {
	return &PhoneHandlerValidator{
		logger:          logger.WithService(fmt.Sprintf("%T", v)),
		tracer:          tracer,
		scheduleService: scheduleService,
	}
}

// ValidateIndex validates the requests.HeartbeatIndex request
func (validator *PhoneHandlerValidator) ValidateIndex(_ context.Context, request requests.PhoneIndex) url.Values {
	v := govalidator.New(govalidator.Options{
		Data: &request,
		Rules: govalidator.MapData{
			"limit": []string{
				"required",
				"numeric",
				"min:1",
				"max:20",
			},
			"skip": []string{
				"required",
				"numeric",
				"min:0",
			},
			"query": []string{
				"max:100",
			},
		},
	})
	return v.ValidateStruct()
}

// ValidateUpsert validates requests.PhoneUpsert
func (validator *PhoneHandlerValidator) ValidateUpsert(ctx context.Context, userID entities.UserID, request requests.PhoneUpsert) url.Values {
	v := govalidator.New(govalidator.Options{
		Data: &request,
		Rules: govalidator.MapData{
			"phone_number": []string{
				"required",
				phoneNumberRule,
			},
			"fcm_token": []string{
				"min:0",
				"max:1000",
			},
			"messages_per_minute": []string{
				"min:0",
				"max:60",
			},
			"max_send_attempts": []string{
				"min:0",
				"max:5",
			},
			"sim": []string{
				"required",
				"in:" + strings.Join([]string{entities.SIM1.String(), entities.SIM2.String()}, ","),
			},
			"message_expiration_seconds": []string{
				"min:60",
				"max:7200",
			},
			"message_send_schedule_id": []string{
				"uuid",
			},
		},
		// govalidator's defaults name the raw JSON key and phrase a numeric bound
		// as "must be maximum 7200 in size", which says neither what the unit is
		// nor what a sane value looks like. Each message below states the limit and
		// what it means, because the limit is the one thing the person needs.
		//
		// Written as bare PREDICATES, no field name: the client prefixes the field
		// (see getApiErrorDetails in web/app/utils/api-error.ts), so a message that
		// names itself gets read twice.
		Messages: govalidator.MapData{
			"messages_per_minute": []string{
				"min:cannot be negative.",
				"max:must be 60 or less — the gateway phone sends at most one SMS per second.",
			},
			"max_send_attempts": []string{
				"min:cannot be negative.",
				"max:must be 5 or less.",
			},
			"message_expiration_seconds": []string{
				"min:must be at least 60 (1 minute).",
				"max:must be at most 7200 (2 hours).",
			},
		},
	})

	result := v.ValidateStruct()
	if request.MaxSendAttempts > 0 && request.MessageExpirationSeconds == 0 {
		result.Add("message_expiration_seconds", "cannot be 0 when max send attempts is greater than 0 — a retry needs a window to retry inside.")
	}

	if len(result) > 0 {
		return result
	}

	if strings.TrimSpace(request.MessageSendScheduleID) != "" {
		scheduleID, _ := uuid.Parse(strings.TrimSpace(request.MessageSendScheduleID))
		if _, err := validator.scheduleService.Load(ctx, userID, scheduleID); err != nil {
			result.Add("message_send_schedule_id", "The message_send_schedule_id does not belong to the authenticated user or does not exist")
		}
	}

	return result
}

// ValidateFCMToken validates requests.PhoneFCMToken
func (validator *PhoneHandlerValidator) ValidateFCMToken(_ context.Context, request requests.PhoneFCMToken) url.Values {
	v := govalidator.New(govalidator.Options{
		Data: &request,
		Rules: govalidator.MapData{
			"phone_number": []string{
				"required",
				phoneNumberRule,
			},
			"fcm_token": []string{
				"min:0",
				"max:1000",
			},
			"sim": []string{
				"required",
				"in:" + strings.Join([]string{entities.SIM1.String(), entities.SIM2.String()}, ","),
			},
		},
	})

	return v.ValidateStruct()
}

// ValidateDelete ValidateUpsert validates requests.PhoneDelete
func (validator *PhoneHandlerValidator) ValidateDelete(_ context.Context, request requests.PhoneDelete) url.Values {
	v := govalidator.New(govalidator.Options{
		Data: &request,
		Rules: govalidator.MapData{
			"phoneID": []string{
				"required",
				"uuid",
			},
		},
	})

	return v.ValidateStruct()
}
