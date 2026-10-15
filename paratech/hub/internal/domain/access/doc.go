// Package access is the bounded context of Hub access control: per-tester API keys that guard every Hub request
// (PRD section 12, section 13.2, AC-25). There is no user login in MVP; a key is a shared secret issued manually by the
// author to a known tester, and it can be revoked.
//
// Layout (docs/architecture.md section 3.1): access/apikey is the API key aggregate.
//
// Rules that apply: ARCH-10..19. The context imports only std, itself and domain/shared.
//
// # Glossary
//
//   - API key: the text a tester's client sends in "Authorization: Bearer <key>". It has a public id part and a
//     secret part (format in apikey.ParseCredential).
//   - Key id: the public part; it names the key in the CLI, in storage and as the caller identity (shared.APIKeyID).
//   - Secret: the random part. The Hub stores only its hash and shows the plaintext key once, when it is issued.
//   - Label: a short pseudonymous name of the tester the key was issued to (PRD section 13.2); never personal data.
//   - Revocation: the one-way change that makes a key unusable from then on.
package access
