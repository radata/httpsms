package di

import (
	"fmt"
	"os"

	"github.com/NdoleStudio/httpsms/pkg/handlers"
)

// CUSTOM FILE — not upstream. Wires the app-access request form
// (handlers/app_access_handler_custom.go) in. container.go calls
// registerAppAccessCustom from RegisterUserRoutes and nowhere else, so an
// upstream merge touches one line.
//
// Requests go to APP_ACCESS_NOTIFY_EMAIL, falling back to SMTP_FROM_EMAIL (the
// operator's own sending address) so an install that sets only SMTP still works.
func (container *Container) registerAppAccessCustom() {
	notifyEmail := os.Getenv("APP_ACCESS_NOTIFY_EMAIL")
	if notifyEmail == "" {
		notifyEmail = os.Getenv("SMTP_FROM_EMAIL")
	}

	handler := handlers.NewAppAccessHandlerCustom(container.Logger(), container.Tracer(), container.Mailer(), notifyEmail)
	container.logger.Debug(fmt.Sprintf("registering %T routes", handler))
	handler.RegisterRoutes(container.App(), container.AuthenticatedMiddleware())
}
