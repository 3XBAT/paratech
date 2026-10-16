package wails

import (
	"context"
	"errors"
	"log/slog"

	"paratech/desktop/internal/cases/googlesignin"
)

// Account statuses as the page reads them.
const (
	accountSignedIn  = "signedIn"
	accountSignedOut = "signedOut"
)

// Operation names of the log.
const (
	opGoogleSetClient   = "ui: set google client"
	opGoogleClearClient = "ui: clear google client"
	opGoogleSignIn      = "ui: google sign in"
	opGoogleCancel      = "ui: cancel google sign in"
	opGoogleSignOut     = "ui: google sign out"
)

// Fixed texts of the Google section (Russian UI).
const (
	msgGoogleClientStored  = "Клиент Google сохранён в Диспетчере учётных данных Windows."
	msgGoogleClientRemoved = "Клиент Google удалён."
	msgGoogleSignedIn      = "Вход в Google выполнен. Приложению доступны только созданные им календари."
	msgGoogleCancelled     = "Вход отменён."
	msgGoogleNotSigningIn  = "Вход не выполняется."
	msgGoogleSignedOut     = "Вы вышли из Google, доступ приложения отозван."
	msgGoogleFailed        = "Не удалось выполнить действие с Google."
)

// googleMessages are the answers other than success, in the order they are matched.
var googleMessages = []struct {
	err error
	msg string
}{
	{googlesignin.ErrInvalidClientID, "Идентификатор клиента должен оканчиваться на .apps.googleusercontent.com " +
		"(клиент типа «Desktop app» в Google Cloud Console)."},
	{googlesignin.ErrInvalidClientSecret, "Секрет клиента: от 1 до 255 латинских букв, цифр и знаков без пробелов."},
	{googlesignin.ErrSignedIn, "Сначала выйдите из Google: клиент нельзя менять, пока выполнен вход."},
	{googlesignin.ErrSignInInProgress, "Вход в Google уже выполняется."},
	{googlesignin.ErrClientNotConfigured, "Сначала сохраните идентификатор и секрет клиента Google."},
	{googlesignin.ErrSignInCancelled, msgGoogleCancelled},
	{googlesignin.ErrSignInTimeout, "Вход не был завершён вовремя. Попробуйте ещё раз."},
	{googlesignin.ErrBrowserUnavailable, "Не удалось открыть браузер."},
	{googlesignin.ErrAccessDenied, "Вы не разрешили доступ в Google."},
	{googlesignin.ErrScopeNotGranted, "Google не выдал доступ к календарям приложения: при входе разрешите доступ к календарю."},
	{googlesignin.ErrScopeTooBroad, "Google выдал больше прав, чем нужно приложению, поэтому доступ отозван. Попробуйте ещё раз."},
	{googlesignin.ErrNoRefreshToken, "Google не выдал постоянный доступ. Попробуйте войти ещё раз."},
	{googlesignin.ErrInvalidClient, "Google не принял идентификатор или секрет клиента. Проверьте их в Google Cloud Console."},
	{googlesignin.ErrGoogleRefused, "Google отклонил вход. Попробуйте ещё раз."},
	{googlesignin.ErrGoogleUnreachable, "Не удалось связаться с Google: проверьте подключение к интернету."},
	{googlesignin.ErrRevokeFailed, "Доступ удалён из приложения, но Google не подтвердил отзыв. " +
		"Отзовите доступ вручную: https://myaccount.google.com/linkedapps"},
	{googlesignin.ErrSecretsUnsupported, "Диспетчер учётных данных Windows недоступен в этой системе: данные Google хранить негде."},
	{googlesignin.ErrSecretsUnavailable, msgStoreDown},
}

// GoogleUseCase is the use case of the Google section (ARCH-42).
type GoogleUseCase interface {
	Status(ctx context.Context) googlesignin.Status
	SetClient(ctx context.Context, clientID, clientSecret string) (googlesignin.Status, error)
	ClearClient(ctx context.Context) (googlesignin.Status, error)
	SignIn(ctx context.Context) error
	CancelSignIn() bool
	SignOut(ctx context.Context) error
}

// GoogleState is the JSON form of the Google section. It never holds a secret or a token.
type GoogleState struct {
	Client    string `json:"client"`    // client id and secret: "set", "unset", "unsupported" or "unavailable"
	Account   string `json:"account"`   // "signedIn", "signedOut", "unsupported" or "unavailable"
	SigningIn bool   `json:"signingIn"` // a sign-in waits for the browser
}

// GoogleResult answers an action of the section: OK, a fixed text and the state afterwards.
type GoogleResult struct {
	OK      bool        `json:"ok"`
	Message string      `json:"message"`
	State   GoogleState `json:"state"`
}

// GoogleService is bound to the «Подключения» page like ConnectionsService ("<package path>.GoogleService.<Method>").
// Keep the method set to what the page may call.
type GoogleService struct {
	uc  GoogleUseCase
	log *slog.Logger
	// window is brought forward after a successful sign-in, from behind the browser; nil: none.
	window MainWindow
}

// MainWindow shows the main window and brings it to the front (Shell.ShowMain).
type MainWindow interface {
	ShowMain() error
}

// NewGoogleService returns the binding of the Google section.
func NewGoogleService(uc GoogleUseCase, log *slog.Logger) *GoogleService {
	return &GoogleService{uc: uc, log: log}
}

// GoogleStatus returns the section. It does not call Google.
func (s *GoogleService) GoogleStatus(ctx context.Context) (GoogleState, error) {
	return googleState(s.uc.Status(ctx)), nil
}

// SetGoogleClient stores the client id and secret in Credential Manager. Neither is ever returned or logged.
func (s *GoogleService) SetGoogleClient(ctx context.Context, clientID, clientSecret string) (GoogleResult, error) {
	st, err := s.uc.SetClient(ctx, clientID, clientSecret)
	return s.answer(ctx, opGoogleSetClient, &st, err, msgGoogleClientStored), nil
}

// ClearGoogleClient removes the client id and secret.
func (s *GoogleService) ClearGoogleClient(ctx context.Context) (GoogleResult, error) {
	st, err := s.uc.ClearClient(ctx)
	return s.answer(ctx, opGoogleClearClient, &st, err, msgGoogleClientRemoved), nil
}

// GoogleSignIn opens Google's consent page in the system browser and returns when the sign-in ends.
func (s *GoogleService) GoogleSignIn(ctx context.Context) (GoogleResult, error) {
	err := s.uc.SignIn(ctx)
	if err == nil && s.window != nil {
		// The user's own action, so taking the focus is wanted here (unlike the lesson popup, AC-01).
		if werr := s.window.ShowMain(); werr != nil {
			s.log.Warn(opGoogleSignIn+": the main window was not brought forward", "err", werr)
		}
	}
	return s.answer(ctx, opGoogleSignIn, nil, err, msgGoogleSignedIn), nil
}

// GoogleCancelSignIn stops a waiting sign-in; its GoogleSignIn call then answers "cancelled".
func (s *GoogleService) GoogleCancelSignIn(ctx context.Context) (GoogleResult, error) {
	msg := msgGoogleNotSigningIn
	if s.uc.CancelSignIn() {
		s.log.Info(opGoogleCancel)
		msg = msgGoogleCancelled
	}
	return GoogleResult{OK: true, Message: msg, State: googleState(s.uc.Status(ctx))}, nil
}

// GoogleSignOut revokes the app's access at Google and removes the stored token.
func (s *GoogleService) GoogleSignOut(ctx context.Context) (GoogleResult, error) {
	err := s.uc.SignOut(ctx)
	return s.answer(ctx, opGoogleSignOut, nil, err, msgGoogleSignedOut), nil
}

// answer turns a result into the page's answer; st is the status the use case returned, nil to read it again.
func (s *GoogleService) answer(ctx context.Context, op string, st *googlesignin.Status, err error, done string) GoogleResult {
	if st == nil {
		cur := s.uc.Status(ctx)
		st = &cur
	}
	out := GoogleResult{State: googleState(*st)}
	if err == nil {
		out.OK, out.Message = true, done
		return out
	}
	for _, m := range googleMessages {
		if errors.Is(err, m.err) {
			s.log.Info(op, "result", m.err)
			out.Message = m.msg
			return out
		}
	}
	s.log.Error(op, "err", err)
	out.Message = msgGoogleFailed
	return out
}

func googleState(st googlesignin.Status) GoogleState {
	account := accountStatus(st.SignedIn)
	return GoogleState{Client: googleSecret(st.Client), Account: account, SigningIn: st.SigningIn}
}

func googleSecret(s googlesignin.SecretStatus) string {
	switch s {
	case googlesignin.SecretSet:
		return secretSet
	case googlesignin.SecretUnset:
		return secretUnset
	case googlesignin.SecretUnsupported:
		return secretUnsupported
	}
	return secretUnavailable
}

func accountStatus(s googlesignin.SecretStatus) string {
	switch s {
	case googlesignin.SecretSet:
		return accountSignedIn
	case googlesignin.SecretUnset:
		return accountSignedOut
	case googlesignin.SecretUnsupported:
		return secretUnsupported
	}
	return secretUnavailable
}
