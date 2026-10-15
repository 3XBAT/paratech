package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"paratech/hub/internal/cases/keys"
	"paratech/hub/internal/domain/access/apikey"
	"paratech/hub/internal/domain/shared"
)

// KeyService is what the commands need from the keys use case (ARCH-42).
type KeyService interface {
	Issue(ctx context.Context, label string) (keys.Issued, error)
	Revoke(ctx context.Context, id string) (keys.KeyInfo, error)
	List(ctx context.Context) ([]keys.KeyInfo, error)
}

// Keys runs the key commands and writes their output to out.
type Keys struct {
	svc KeyService
	out io.Writer
}

// NewKeys builds the commands.
func NewKeys(svc KeyService, out io.Writer) (*Keys, error) {
	if svc == nil || out == nil {
		return nil, errors.New("cli: key service and output are required")
	}
	return &Keys{svc: svc, out: out}, nil
}

// Create issues a key and prints it once.
func (k *Keys) Create(ctx context.Context, label string) error {
	issued, err := k.svc.Issue(ctx, label)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(k.out,
		"API key %s issued for %q at %s.\n"+
			"It is shown only this once and cannot be recovered; hand it to the tester over a private channel:\n"+
			"%s\n",
		issued.ID, issued.Label, stamp(issued.CreatedAt), issued.Key)
	if err != nil {
		return fmt.Errorf("cli: print the issued key %s (revoke it and issue a new one): %w", issued.ID, err)
	}
	return nil
}

// Revoke revokes a key. Revoking a revoked key is reported and is not an error, so the command can be repeated.
func (k *Keys) Revoke(ctx context.Context, id string) error {
	info, err := k.svc.Revoke(ctx, id)
	switch {
	case errors.Is(err, apikey.ErrAlreadyRevoked):
		return k.printf("API key %s was already revoked at %s.\n", info.ID, stamp(info.RevokedAt))
	case errors.Is(err, shared.ErrNotFound):
		return fmt.Errorf("cli: no API key with id %s", id)
	case err != nil:
		return err
	}
	return k.printf("API key %s revoked at %s.\n", info.ID, stamp(info.RevokedAt))
}

// List prints every key, oldest first. Secrets and hashes are never part of the output.
func (k *Keys) List(ctx context.Context) error {
	all, err := k.svc.List(ctx)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(k.out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tLABEL\tSTATUS\tCREATED\tREVOKED")
	for _, i := range all {
		status, revoked := string(apikey.StatusActive), "-"
		if i.Revoked {
			status, revoked = string(apikey.StatusRevoked), stamp(i.RevokedAt)
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", i.ID, i.Label, status, stamp(i.CreatedAt), revoked)
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("cli: print the key list: %w", err)
	}
	return nil
}

func (k *Keys) printf(format string, args ...any) error {
	if _, err := fmt.Fprintf(k.out, format, args...); err != nil {
		return fmt.Errorf("cli: print: %w", err)
	}
	return nil
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }
