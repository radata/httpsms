package handlers

import (
	"context"
	"fmt"
	"html"
	"net/mail"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/NdoleStudio/httpsms/pkg/emails"
	"github.com/NdoleStudio/httpsms/pkg/telemetry"
	"github.com/NdoleStudio/stacktrace"
	"github.com/gofiber/fiber/v3"
)

// CUSTOM FILE — not upstream. POST /v1/app-access-requests lets a signed-in
// user ask for the Android app.
//
// WHY
//
// Upstream links straight to its own APK (apk.httpsms.com and the GitHub
// release). That build is tied to upstream's Firebase project, so against this
// server it can neither sign in nor receive push — it cannot work. Our own
// build is distributed as a Google Play test app, and Play only lets a tester
// in once their Google account is added in the Play Console. So instead of a
// download, the web app shows a form (web/app/pages/app-access_custom.vue) and
// this handler emails the request to the operator, who adds the tester by hand.
//
// Nothing is stored: the email is the whole record.

// appAccessCooldownCustom is how long a user must wait between requests, so a
// double click or a stuck retry cannot flood the operator's inbox.
const appAccessCooldownCustom = 10 * time.Minute

// AppAccessHandlerCustom forwards app-access requests to the operator.
type AppAccessHandlerCustom struct {
	handler
	logger      telemetry.Logger
	tracer      telemetry.Tracer
	mailer      emails.Mailer
	notifyEmail string
	lastSent    sync.Map // entities.UserID -> time.Time
}

// AppAccessRequestCustom is the form the web app posts.
type AppAccessRequestCustom struct {
	Name       string `json:"name"`
	GooglePlay string `json:"google_play_email"`
	Note       string `json:"note"`
}

// NewAppAccessHandlerCustom creates a new AppAccessHandlerCustom. notifyEmail
// is where requests are sent.
func NewAppAccessHandlerCustom(logger telemetry.Logger, tracer telemetry.Tracer, mailer emails.Mailer, notifyEmail string) (h *AppAccessHandlerCustom) {
	return &AppAccessHandlerCustom{
		logger:      logger.WithService(fmt.Sprintf("%T", h)),
		tracer:      tracer,
		mailer:      mailer,
		notifyEmail: strings.TrimSpace(notifyEmail),
	}
}

// RegisterRoutes registers the route for signed-in users.
func (h *AppAccessHandlerCustom) RegisterRoutes(router fiber.Router, middlewares ...fiber.Handler) {
	h.register(router, fiber.MethodPost, "/v1/app-access-requests", middlewares, h.Store)
}

// Store validates the form and emails it to the operator.
func (h *AppAccessHandlerCustom) Store(c fiber.Ctx) error {
	ctx, span := h.tracer.StartFromFiberCtx(c)
	defer span.End()

	ctxLogger := h.tracer.CtxLogger(h.logger, span)

	var request AppAccessRequestCustom
	if err := c.Bind().Body(&request); err != nil {
		return h.responseBadRequest(c, stacktrace.Propagatef(err, "cannot parse app access request"))
	}
	request.Name = strings.TrimSpace(request.Name)
	request.GooglePlay = strings.TrimSpace(request.GooglePlay)
	request.Note = strings.TrimSpace(request.Note)

	if errors := h.validate(request); len(errors) > 0 {
		return h.responseUnprocessableEntity(c, errors, "validation errors while requesting app access")
	}

	user := h.userFromContext(c)
	if last, ok := h.lastSent.Load(user.ID); ok && time.Since(last.(time.Time)) < appAccessCooldownCustom {
		return h.responseAccepted(c, "your request was already sent, we will be in touch")
	}

	if h.notifyEmail == "" {
		ctxLogger.Error(stacktrace.NewError("APP_ACCESS_NOTIFY_EMAIL and SMTP_FROM_EMAIL are both empty, cannot forward app access request"))
		return h.responseInternalServerError(c)
	}

	if err := h.send(ctx, user.Email, string(user.ID), request); err != nil {
		ctxLogger.Error(stacktrace.Propagatef(err, "cannot send app access request for user [%s]", user.ID))
		return h.responseInternalServerError(c)
	}

	h.lastSent.Store(user.ID, time.Now())
	return h.responseAccepted(c, "your request has been sent, we will be in touch")
}

func (h *AppAccessHandlerCustom) validate(request AppAccessRequestCustom) url.Values {
	errors := url.Values{}
	// The name goes into the Subject header, so no control characters (CR/LF
	// would let a user inject headers).
	if request.Name == "" || len(request.Name) > 100 || strings.ContainsFunc(request.Name, unicode.IsControl) {
		errors.Add("name", "The name field is required and must be at most 100 characters")
	}
	if address, err := mail.ParseAddress(request.GooglePlay); err != nil || address.Address != request.GooglePlay || len(request.GooglePlay) > 254 {
		errors.Add("google_play_email", "The google play email field must be a valid email address")
	}
	if len(request.Note) > 1000 {
		errors.Add("note", "The note field must be at most 1000 characters")
	}
	return errors
}

func (h *AppAccessHandlerCustom) send(ctx context.Context, accountEmail, userID string, request AppAccessRequestCustom) error {
	note := request.Note
	if note == "" {
		note = "-"
	}

	rows := [][2]string{
		{"Name", request.Name},
		{"Google Play email", request.GooglePlay},
		{"Account email", accountEmail},
		{"User ID", userID},
		{"Note", note},
	}

	var text, table strings.Builder
	for _, row := range rows {
		text.WriteString(fmt.Sprintf("%s: %s\n", row[0], row[1]))
		table.WriteString(fmt.Sprintf("<tr><th align=\"left\" style=\"padding:4px 12px 4px 0\">%s</th><td style=\"padding:4px 0\">%s</td></tr>", row[0], html.EscapeString(row[1])))
	}
	text.WriteString("\nAdd the Google Play email as a tester in the Play Console to grant access.\n")

	return h.mailer.Send(ctx, &emails.Email{
		ToEmail: h.notifyEmail,
		Subject: fmt.Sprintf("App access request from %s", request.Name),
		Text:    text.String(),
		HTML: fmt.Sprintf(
			"<p>A user has asked for access to the Android app.</p><table>%s</table><p>Add the Google Play email as a tester in the Play Console to grant access.</p>",
			table.String(),
		),
	})
}
