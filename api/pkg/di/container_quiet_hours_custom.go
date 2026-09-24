package di

import (
	"fmt"

	"github.com/NdoleStudio/httpsms/pkg/handlers"
	"github.com/NdoleStudio/httpsms/pkg/services"
)

// CUSTOM FILE — not upstream. Wires quiet hours (services/quiet_hours_custom.go)
// in. container.go calls registerQuietHoursCustom from RegisterPhoneRoutes and
// nowhere else, so an upstream merge touches one line.
func (container *Container) registerQuietHoursCustom() {
	quietHours := services.SetQuietHoursCustom(
		container.PhoneRepository(),
		container.MessageSendScheduleRepository(),
	)

	handler := handlers.NewQuietHoursHandlerCustom(container.Logger(), container.Tracer(), quietHours)
	container.logger.Debug(fmt.Sprintf("registering %T routes", handler))
	handler.RegisterPhoneAPIKeyRoutes(container.App(), container.PhoneAPIKeyMiddleware(), container.AuthenticatedMiddleware())
}
