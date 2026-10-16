package googlesignin

import "context"

// SecretWriter is the write side of the google credential store (the wincred adapter in the app; ARCH-34). There is
// no Get: no secret travels from the store into the core. Errors never contain the secret.
type SecretWriter interface {
	// Set stores or replaces the secret under name; an empty or too large secret yields shared.ErrInvalidSecret.
	Set(ctx context.Context, name, secret string) error

	// Exists reports whether a secret is stored under name, without revealing it.
	Exists(ctx context.Context, name string) (bool, error)

	// Delete removes the secret under name; a missing secret is not an error.
	Delete(ctx context.Context, name string) error
}

// Authorizer talks to Google with the stored client (adapters/googleauth through a bridge in the app). Its errors are
// the results of this package (ErrClientNotConfigured ... ErrRevokeFailed), a ctx error or an unclassified error; they
// never contain a token, a code or the secret.
type Authorizer interface {
	// SignIn runs the browser sign-in and stores the refresh token in the same credential store. It returns when the
	// user finished, the sign-in timed out (ErrSignInTimeout) or ctx is done.
	SignIn(ctx context.Context) error

	// SignOut revokes the grant at Google and always removes the stored refresh token; ErrRevokeFailed means the local
	// removal happened but Google did not confirm the revocation.
	SignOut(ctx context.Context) error
}
