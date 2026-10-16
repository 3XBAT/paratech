package googlesignin

import "log/slog"

// Defaults of Config: the entries the googleauth adapter reads (googleauth.ClientIDName and so on).
const (
	DefaultClientIDName     = "client-id"
	DefaultClientSecretName = "client-secret"
	DefaultRefreshTokenName = "refresh-token"
)

// Deps are the ports the Service needs. Log is optional.
type Deps struct {
	Secrets SecretWriter // the google store, which the adapter reads
	Auth    Authorizer
	Log     *slog.Logger
}

// Config names the entries of the store; "" takes the default.
type Config struct {
	ClientIDName     string
	ClientSecretName string
	RefreshTokenName string
}

// SecretStatus is what the page may know about a stored value: whether it is there, never the value.
type SecretStatus int

// Secret statuses. The zero value never claims a stored secret.
const (
	SecretUnavailable SecretStatus = iota // the store failed, the answer is unknown
	SecretUnset
	SecretSet
	SecretUnsupported // no credential store on this system
)

// Status is the Google section as the page shows it. It never holds a secret.
type Status struct {
	Client    SecretStatus // SecretSet: both the client id and the client secret are stored
	SignedIn  SecretStatus // SecretSet: a refresh token is stored
	SigningIn bool         // a sign-in waits for the browser
}
