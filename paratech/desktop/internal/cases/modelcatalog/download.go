package modelcatalog

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

// Operation names of the download flow, used in error messages.
const (
	opDownload       = "modelcatalog: download model"
	opPreflight      = "modelcatalog: plan model download"
	opDiscardPartial = "modelcatalog: discard partial download"
)

// Layout of downloads below models/.
const (
	partialDir      = "downloads"
	partialExt      = ".partial"
	installedExt    = ".bin"
	partialSumChars = 16
)

// maxFetches bounds the requests of one Download: a server that cannot continue the partial file gets one request
// from byte 0.
const maxFetches = 2

// Downloader downloads catalog models into the model manager (PRD sections 8 and 8.1): resumable, with progress for
// subscribers, a disk space check before it starts, the SHA-256 check and the hosts of the catalog allowlist only.
// A verified file is installed by Service.Register. It is safe for concurrent use; one catalog model downloads once at
// a time.
type Downloader struct {
	svc    *Service
	source ModelSource
	space  DiskSpace

	mu       sync.Mutex
	inflight map[values.CatalogID]bool
	subs     map[*subscriber]bool
}

type subscriber struct {
	ch   chan DownloadProgress
	once sync.Once
}

// NewDownloader builds a Downloader on top of the Service.
func NewDownloader(svc *Service, d DownloadDeps) (*Downloader, error) {
	if svc == nil || d.Source == nil || d.Space == nil {
		return nil, fmt.Errorf("%w: the service and every download port are required", ErrInvalidConfig)
	}
	return &Downloader{
		svc: svc, source: d.Source, space: d.Space,
		inflight: map[values.CatalogID]bool{}, subs: map[*subscriber]bool{},
	}, nil
}

// Subscribe returns a channel of progress events of all downloads and a function that ends the subscription and
// closes the channel. The channel keeps only the latest event, so a slow subscriber never stalls a download; it
// still always sees the last event of a download (PhaseInstalled or PhaseFailed).
func (d *Downloader) Subscribe() (<-chan DownloadProgress, func()) {
	s := &subscriber{ch: make(chan DownloadProgress, 1)}

	d.mu.Lock()
	d.subs[s] = true
	d.mu.Unlock()

	return s.ch, func() {
		s.once.Do(func() {
			d.mu.Lock()
			delete(d.subs, s)
			close(s.ch)
			d.mu.Unlock()
		})
	}
}

// publish replaces the pending event of every subscriber; it never blocks.
func (d *Downloader) publish(p DownloadProgress) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for s := range d.subs {
		select {
		case <-s.ch:
		default:
		}
		s.ch <- p // the buffer has room: only publish sends, under d.mu
	}
}

// Preflight tells how much disk space the download of a catalog model needs and whether the disk has it, so the
// user can be warned before it starts (PRD section 8). Bytes kept from an interrupted download count as done.
func (d *Downloader) Preflight(ctx context.Context, id values.CatalogID) (DownloadPlan, error) {
	entry, _, err := d.entry(ctx, id)
	if err != nil {
		return DownloadPlan{}, fmt.Errorf("%s %s: %w", opPreflight, id, err)
	}
	have, err := d.partialSize(ctx, entry)
	if err != nil {
		return DownloadPlan{}, fmt.Errorf("%s %s: %w", opPreflight, id, err)
	}
	free, err := d.space.FreeBytes(ctx)
	if err != nil {
		return DownloadPlan{}, fmt.Errorf("%s %s: %w", opPreflight, id, err)
	}

	offset, _ := entry.ResumeOffset(have)
	return DownloadPlan{
		CatalogID: id, TotalBytes: entry.SizeBytes(), HaveBytes: offset, RequiredBytes: entry.RequiredSpace(have),
		AvailableBytes: free, Enough: entry.CheckDiskSpace(have, free) == nil,
	}, nil
}

// Download fetches a catalog model, verifies it and installs it (PRD sections 8 and 8.1, AC-07).
//
//   - Too little free disk space is a *values.InsufficientSpaceError before anything is fetched.
//   - The file grows in a partial file below models/downloads; a broken transfer or a cancelled ctx returns
//     ErrDownloadInterrupted and keeps it, and the next Download resumes it with a ranged request.
//   - Only hosts of the catalog allowlist are contacted (values.ErrHostNotAllowed otherwise).
//   - A file that does not match the catalog size or SHA-256 is deleted and not installed
//     (values.ErrSizeMismatch, values.ErrChecksumMismatch); the next Download starts from byte 0.
//   - A verified file moves to a name of its own and Register installs it.
//
// A model installed from the entry already yields ErrAlreadyInstalled, a second Download of the same entry while one
// runs ErrDownloadInProgress.
func (d *Downloader) Download(ctx context.Context, id values.CatalogID) (InstalledModel, error) {
	if !d.claim(id) {
		return InstalledModel{}, fmt.Errorf("%s %s: %w", opDownload, id, ErrDownloadInProgress)
	}
	defer d.release(id)

	m, err := d.download(ctx, id)
	if err != nil {
		d.publish(DownloadProgress{CatalogID: id, Phase: PhaseFailed})
		return InstalledModel{}, fmt.Errorf("%s %s: %w", opDownload, id, err)
	}
	d.publish(DownloadProgress{CatalogID: id, Phase: PhaseInstalled, DoneBytes: m.SizeBytes, TotalBytes: m.SizeBytes})
	return m, nil
}

func (d *Downloader) download(ctx context.Context, id values.CatalogID) (InstalledModel, error) {
	entry, allow, err := d.entry(ctx, id)
	if err != nil {
		return InstalledModel{}, err
	}
	if err := d.svc.checkNotInstalled(ctx, id); err != nil {
		return InstalledModel{}, err
	}
	if err := allow.Check(entry.SourceURL().String()); err != nil {
		return InstalledModel{}, err
	}

	partial, err := partialRef(entry)
	if err != nil {
		return InstalledModel{}, err
	}
	have, err := d.partialSize(ctx, entry)
	if err != nil {
		return InstalledModel{}, err
	}
	offset, complete := entry.ResumeOffset(have)
	if offset != have {
		if err := d.svc.files.Delete(ctx, partial); err != nil {
			return InstalledModel{}, err
		}
	}

	free, err := d.space.FreeBytes(ctx)
	if err != nil {
		return InstalledModel{}, err
	}
	if err := entry.CheckDiskSpace(have, free); err != nil {
		return InstalledModel{}, err
	}

	if !complete {
		if err := d.transfer(ctx, entry, allow, partial, offset); err != nil {
			return InstalledModel{}, err
		}
	}

	if err := d.verify(ctx, entry, partial); err != nil {
		return InstalledModel{}, err
	}
	return d.install(ctx, entry, partial)
}

// transfer fetches the rest of the file into the partial file.
func (d *Downloader) transfer(ctx context.Context, entry values.CatalogEntry, allow values.HostAllowlist,
	partial shared.ArtifactRef, offset int64) error {
	for range maxFetches {
		d.publish(DownloadProgress{
			CatalogID: entry.ID(), Phase: PhaseDownloading, DoneBytes: offset, TotalBytes: entry.SizeBytes(),
		})

		reply, body, err := d.source.Fetch(ctx, entry.SourceURL(), offset, allow)
		if err != nil {
			if errors.Is(err, values.ErrHostNotAllowed) || errors.Is(err, values.ErrInvalidSourceURL) {
				return err
			}
			return fmt.Errorf("%w: %w", ErrDownloadInterrupted, err)
		}

		step, err := entry.PlanResume(offset, reply)
		if err != nil {
			_ = body.Close()
			return errors.Join(err, d.svc.files.Delete(ctx, partial))
		}
		if step == values.StepRestart || step == values.StepReplace {
			if err := d.svc.files.Delete(ctx, partial); err != nil {
				_ = body.Close()
				return err
			}
			offset = 0
		}
		if step == values.StepRestart {
			_ = body.Close()
			continue
		}

		return d.receive(ctx, entry, partial, offset, body)
	}
	return fmt.Errorf("%w: the source cannot continue the file", values.ErrUnexpectedRange)
}

// receive appends the body to the partial file, never past the catalog size, and closes the body.
func (d *Downloader) receive(ctx context.Context, entry values.CatalogEntry, partial shared.ArtifactRef, offset int64,
	body io.ReadCloser) error {
	defer func() { _ = body.Close() }()

	remaining := entry.SizeBytes() - offset
	pr := &progressReader{r: body, done: offset, report: func(done int64) {
		d.publish(DownloadProgress{CatalogID: entry.ID(), Phase: PhaseDownloading, DoneBytes: done, TotalBytes: entry.SizeBytes()})
	}}

	n, err := d.svc.files.Append(ctx, partial, io.LimitReader(pr, remaining))
	if err != nil {
		if pr.err != nil || ctx.Err() != nil {
			return fmt.Errorf("%w: %d of %d bytes stored: %w", ErrDownloadInterrupted, offset+n, entry.SizeBytes(), err)
		}
		return err // the disk failed, not the transfer
	}
	if n < remaining {
		return fmt.Errorf("%w: the source ended at %d of %d bytes", ErrDownloadInterrupted, offset+n, entry.SizeBytes())
	}

	// More bytes than the catalog size: the source serves another file.
	var extra [1]byte
	if k, _ := io.ReadFull(body, extra[:]); k > 0 {
		return errors.Join(
			fmt.Errorf("%w: %s %s: the source sends more than %d bytes", values.ErrSizeMismatch, entry.Name(), entry.Version(), entry.SizeBytes()),
			d.svc.files.Delete(ctx, partial))
	}
	return nil
}

// verify checks the complete partial file against the catalog digest and deletes it on a mismatch.
func (d *Downloader) verify(ctx context.Context, entry values.CatalogEntry, partial shared.ArtifactRef) error {
	d.publish(DownloadProgress{
		CatalogID: entry.ID(), Phase: PhaseVerifying, DoneBytes: entry.SizeBytes(), TotalBytes: entry.SizeBytes(),
	})

	sum, err := d.svc.digestFile(ctx, partial, entry.SizeBytes())
	if err == nil {
		err = entry.Verify(sum, entry.SizeBytes())
	}
	if errors.Is(err, values.ErrChecksumMismatch) || errors.Is(err, values.ErrSizeMismatch) {
		return errors.Join(err, d.svc.files.Delete(ctx, partial))
	}
	return err
}

// install moves the verified file to a fresh name and registers it; the file is deleted when Register refuses it.
func (d *Downloader) install(ctx context.Context, entry values.CatalogEntry, partial shared.ArtifactRef) (InstalledModel, error) {
	final, err := shared.NewArtifactRef(shared.ArtifactModels,
		entry.ID().String()+"-"+string(d.svc.ids.NewSttModelID())+installedExt)
	if err != nil {
		return InstalledModel{}, err
	}
	if err := d.svc.files.Rename(ctx, partial, final); err != nil {
		return InstalledModel{}, err
	}

	m, err := d.svc.Register(ctx, Registration{CatalogID: entry.ID(), File: final})
	if err != nil {
		// A cancelled ctx must not keep the file from being removed.
		return InstalledModel{}, errors.Join(err, d.svc.files.Delete(context.WithoutCancel(ctx), final))
	}
	return m, nil
}

// DiscardPartial deletes the bytes kept from an interrupted download of a catalog model, so the next Download starts
// from byte 0 (PRD section 8). It refuses while the model downloads.
func (d *Downloader) DiscardPartial(ctx context.Context, id values.CatalogID) error {
	if !d.claim(id) {
		return fmt.Errorf("%s %s: %w", opDiscardPartial, id, ErrDownloadInProgress)
	}
	defer d.release(id)

	entry, _, err := d.entry(ctx, id)
	if err != nil {
		return fmt.Errorf("%s %s: %w", opDiscardPartial, id, err)
	}
	partial, err := partialRef(entry)
	if err != nil {
		return fmt.Errorf("%s %s: %w", opDiscardPartial, id, err)
	}
	if err := d.svc.files.Delete(ctx, partial); err != nil {
		return fmt.Errorf("%s %s: %w", opDiscardPartial, id, err)
	}
	return nil
}

func (d *Downloader) claim(id values.CatalogID) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.inflight[id] {
		return false
	}
	d.inflight[id] = true
	return true
}

func (d *Downloader) release(id values.CatalogID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.inflight, id)
}

// entry loads the catalog and returns the entry and the download allowlist.
func (d *Downloader) entry(ctx context.Context, id values.CatalogID) (values.CatalogEntry, values.HostAllowlist, error) {
	cat, err := d.svc.loadCatalog(ctx)
	if err != nil {
		return values.CatalogEntry{}, values.HostAllowlist{}, err
	}
	entry, ok := cat.Entry(id)
	if !ok {
		return values.CatalogEntry{}, values.HostAllowlist{}, fmt.Errorf("%q: %w", id, ErrNotInCatalog)
	}
	return entry, cat.DownloadHosts(), nil
}

// partialSize returns the bytes kept from an earlier download of the entry; none is 0.
func (d *Downloader) partialSize(ctx context.Context, entry values.CatalogEntry) (int64, error) {
	partial, err := partialRef(entry)
	if err != nil {
		return 0, err
	}
	n, err := d.svc.files.Size(ctx, partial)
	if errors.Is(err, shared.ErrNotFound) {
		return 0, nil
	}
	return n, err
}

// partialRef names the partial file of an entry. The digest is part of the name, so a catalog update that changes
// the file never resumes on the bytes of the old one.
func partialRef(entry values.CatalogEntry) (shared.ArtifactRef, error) {
	sum := string(entry.SHA256())
	return shared.NewArtifactRef(shared.ArtifactModels,
		partialDir+"/"+entry.ID().String()+"-"+sum[:partialSumChars]+partialExt)
}

// progressReader reports the stored bytes after every read and remembers a read error of the source.
type progressReader struct {
	r      io.Reader
	done   int64
	report func(done int64)
	err    error
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.done += int64(n)
		p.report(p.done)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		p.err = err
	}
	return n, err
}
