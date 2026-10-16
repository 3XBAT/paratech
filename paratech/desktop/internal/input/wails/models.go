package wails

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"paratech/desktop/internal/cases/modelcatalog"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/services/modelselection"
	"paratech/desktop/internal/domain/transcription/sttmodel"
	"paratech/desktop/internal/domain/transcription/values"
)

// ModelsHomePage is the official page of the whisper.cpp models (docs/refs/whisper.md), the way to a model while the
// catalog lists none. It is opened like a catalog source page, never taken from the page.
const ModelsHomePage = "https://huggingface.co/ggerganov/whisper.cpp"

// modelFileExt is the extension of a whisper.cpp ggml model file.
const modelFileExt = ".bin"

// suggestedVersion is the version offered for a model imported outside the catalog.
const suggestedVersion = "1"

// modelError is an error of the models page: a fixed Russian text the page shows as is. Causes are logged, without
// paths.
type modelError string

func (e modelError) Error() string { return string(e) }

// Errors of the models page.
const (
	ErrModelsUnavailable        modelError = "Не удалось прочитать список моделей"
	ErrModelActionFailed        modelError = "Не удалось выполнить действие с моделью"
	ErrModelInvalidID           modelError = "Неверный идентификатор модели"
	ErrModelNotFound            modelError = "Модель не найдена: возможно, она уже удалена"
	ErrModelNotInCatalog        modelError = "Этой модели нет в каталоге"
	ErrModelAlreadyInstalled    modelError = "Модель с таким названием и версией уже установлена"
	ErrModelReservedName        modelError = "Это название и версия принадлежат модели из каталога: укажите другое название или версию"
	ErrModelFileInUse           modelError = "Этот файл уже используется другой установленной моделью"
	ErrModelPinned              modelError = "Модель нельзя удалить: её использует незавершённая задача распознавания"
	ErrModelDefaultRace         modelError = "Модель снова выбрали по умолчанию во время удаления: попробуйте ещё раз"
	ErrModelChecksum            modelError = "Контрольная сумма SHA-256 не совпадает: файл повреждён или это другой файл. Модель не установлена"
	ErrModelFileChanged         modelError = "Файл изменился после проверки: его SHA-256 не совпадает с подтверждённой. Модель не установлена, выберите файл заново"
	ErrModelSizeMismatch        modelError = "Размер файла не совпадает с каталогом: файл повреждён или это другой файл. Модель не установлена"
	ErrModelFileTooLarge        modelError = "Файл больше 8 ГБ: это не модель whisper"
	ErrModelFileEmpty           modelError = "Файл пустой: это не модель whisper"
	ErrModelFileNotRegular      modelError = "Это не обычный файл на локальном диске: выберите файл модели .bin"
	ErrModelFileType            modelError = "Выберите файл модели whisper с расширением .bin"
	ErrModelFileMissing         modelError = "Файл не найден"
	ErrModelNoAccess            modelError = "Нет доступа к файлу"
	ErrModelChoosing            modelError = "Файл уже выбирается"
	ErrModelNoSelection         modelError = "Нет выбранного файла: выберите файл заново"
	ErrModelConfirmMismatch     modelError = "Подтверждённая SHA-256 не совпадает с показанной: выберите файл заново"
	ErrModelBadName             modelError = "Название модели: латинские буквы, цифры, «.», «_» и «-», до 64 символов, начинается и заканчивается буквой или цифрой"
	ErrModelBadVersion          modelError = "Версия модели: латинские буквы, цифры, «.», «_» и «-», до 32 символов, начинается и заканчивается буквой или цифрой"
	ErrModelBadQuantization     modelError = "Квантизация: строчные латинские буквы, цифры и «_», до 16 символов, или пусто"
	ErrModelDownloading         modelError = "Модель уже скачивается"
	ErrModelDownloadInterrupted modelError = "Скачивание прервано: начните его снова, оно продолжится с места остановки"
	ErrModelDownloadStopped     modelError = "Скачивание остановлено: начните его снова, оно продолжится с места остановки"
	ErrModelSourceRefused       modelError = "Источник модели ответил не так, как ожидалось: скачивание отменено"
	ErrModelLinkFailed          modelError = "Не удалось открыть страницу в браузере"
	// ErrNotEnoughSpace matches every disk space error; the error of a check names the sizes in its text.
	ErrNotEnoughSpace modelError = "Недостаточно места на диске"
)

// spaceError is the disk space warning with the sizes (PRD section 8); errors.Is matches ErrNotEnoughSpace.
type spaceError struct{ required, available int64 }

func (e spaceError) Error() string {
	return fmt.Sprintf("%s: нужно %s, свободно %s", ErrNotEnoughSpace, sizeText(e.required), sizeText(e.available))
}

func (e spaceError) Is(target error) bool { return target == ErrNotEnoughSpace }

// ModelCatalogUseCase is the part of the model catalog the models page needs (ARCH-42).
type ModelCatalogUseCase interface {
	ListAvailable(ctx context.Context, machine values.Machine) ([]modelcatalog.AvailableModel, error)
	ListInstalled(ctx context.Context) ([]modelcatalog.InstalledModel, error)
	SourcePage(ctx context.Context, id values.CatalogID) (values.SourceURL, error)
	SetDefault(ctx context.Context, id shared.SttModelID) error
	Delete(ctx context.Context, id shared.SttModelID) error
	// CurrentModel is the model a new STT job pins (modelselection.Selector.Resolve), read without a transaction.
	CurrentModel(ctx context.Context) (modelcatalog.PinnedModel, error)
}

// ModelDownloads runs the download of a catalog model in the background, one at a time, for the page to poll; the
// app implements it over modelcatalog.Downloader. StartDownload refuses a second download with
// modelcatalog.ErrDownloadInProgress.
type ModelDownloads interface {
	Preflight(ctx context.Context, id values.CatalogID) (modelcatalog.DownloadPlan, error)
	StartDownload(id values.CatalogID) error
	CancelDownload()
	DownloadState() DownloadState
	DiscardPartial(ctx context.Context, id values.CatalogID) error
}

// DownloadState is the latest state of the current or last download; the zero value means none yet.
type DownloadState struct {
	CatalogID  values.CatalogID
	Phase      modelcatalog.DownloadPhase
	DoneBytes  int64
	TotalBytes int64
	Running    bool
	Cancelled  bool  // stopped by CancelDownload
	Err        error // why the last download failed
}

// ModelImports installs model files the user picked (ARCH-42).
type ModelImports interface {
	Inspect(ctx context.Context, path string) (modelcatalog.ImportPreview, error)
	ImportCatalog(ctx context.Context, id values.CatalogID, path string) (modelcatalog.InstalledModel, error)
	ImportCustom(ctx context.Context, req modelcatalog.CustomImport) (modelcatalog.InstalledModel, error)
}

// LinkOpener opens a web page in the default browser (the app wires adapters/weblink).
type LinkOpener interface {
	OpenLink(ctx context.Context, u values.SourceURL) error
}

// ModelFilePicker shows the native open dialog for a model file. It returns the chosen path, or "" when the user
// cancels. The shell implements it on Windows; the page never sends a path.
type ModelFilePicker interface {
	PickModelFile(ctx context.Context) (string, error)
}

// modelPickerFunc adapts a function to ModelFilePicker.
type modelPickerFunc func(ctx context.Context) (string, error)

// PickModelFile implements ModelFilePicker.
func (f modelPickerFunc) PickModelFile(ctx context.Context) (string, error) { return f(ctx) }

// ModelsDeps is what the models page binds to; every interface is required.
type ModelsDeps struct {
	Catalog   ModelCatalogUseCase
	Downloads ModelDownloads
	Imports   ModelImports
	Links     LinkOpener
	Picker    ModelFilePicker
	// SpeechReady tells whether whisper-cli was found at start; without it STT jobs wait until a restart.
	SpeechReady bool
}

// InstalledModelItem is the JSON form of an installed model. InstalledAt is RFC 3339 in UTC. InUse marks the model a
// new STT job pins; only List sets it.
type InstalledModelItem struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	SizeBytes    int64    `json:"sizeBytes"`
	SHA256       string   `json:"sha256"`
	Quantization string   `json:"quantization"`
	Capabilities []string `json:"capabilities"`
	IsDefault    bool     `json:"isDefault"`
	FromCatalog  bool     `json:"fromCatalog"`
	CatalogID    string   `json:"catalogId"`
	InstalledAt  string   `json:"installedAt"`
	InUse        bool     `json:"inUse"`
}

// AvailableModelItem is the JSON form of a catalog entry.
type AvailableModelItem struct {
	CatalogID     string   `json:"catalogId"`
	Name          string   `json:"name"`
	Version       string   `json:"version"`
	SizeBytes     int64    `json:"sizeBytes"`
	Quantization  string   `json:"quantization"`
	Capabilities  []string `json:"capabilities"`
	MinCPUCores   int      `json:"minCpuCores"`
	MinRAMMB      int64    `json:"minRamMb"`
	Recommended   bool     `json:"recommended"`
	LikelySlow    bool     `json:"likelySlow"`
	Installed     bool     `json:"installed"`
	InstalledID   string   `json:"installedId"`
	HasSourcePage bool     `json:"hasSourcePage"`
}

// ModelDownload is the JSON form of the download state. Phase is "" (none yet), "downloading", "verifying",
// "installed", "failed" or "cancelled"; Error is the text of a failure.
type ModelDownload struct {
	CatalogID  string `json:"catalogId"`
	Phase      string `json:"phase"`
	DoneBytes  int64  `json:"doneBytes"`
	TotalBytes int64  `json:"totalBytes"`
	Running    bool   `json:"running"`
	Error      string `json:"error"`
}

// phaseCancelled is the phase of a download stopped by the user.
const phaseCancelled = "cancelled"

// ModelsView is what the models page shows. MachineKnown is false while the hardware is not detected: then the
// lightest preferred catalog entry is recommended and nothing is marked slow. NeedsDefault is true when several
// models are installed and none is the default, so STT jobs wait until the user selects one.
type ModelsView struct {
	Installed      []InstalledModelItem `json:"installed"`
	Available      []AvailableModelItem `json:"available"`
	NeedsDefault   bool                 `json:"needsDefault"`
	SpeechReady    bool                 `json:"speechReady"`
	MachineKnown   bool                 `json:"machineKnown"`
	Download       ModelDownload        `json:"download"`
	ModelsHomePage string               `json:"modelsHomePage"` // to show; OpenModelsHomePage opens it
}

// DownloadPlanView is the disk space check before a download. Warning is the text when the space is not enough.
type DownloadPlanView struct {
	CatalogID      string `json:"catalogId"`
	TotalBytes     int64  `json:"totalBytes"`
	HaveBytes      int64  `json:"haveBytes"`
	RequiredBytes  int64  `json:"requiredBytes"`
	AvailableBytes int64  `json:"availableBytes"`
	Enough         bool   `json:"enough"`
	Warning        string `json:"warning"`
}

// ModelFileSelection is a picked model file to confirm (AC-23). Cancelled means the user closed the dialog; then
// every other field is empty. The suggestions come from the file name and may be empty.
type ModelFileSelection struct {
	Cancelled             bool   `json:"cancelled"`
	SelectionID           string `json:"selectionId"`
	FileName              string `json:"fileName"` // base name only
	SizeBytes             int64  `json:"sizeBytes"`
	SHA256                string `json:"sha256"`
	SuggestedName         string `json:"suggestedName"`
	SuggestedVersion      string `json:"suggestedVersion"`
	SuggestedQuantization string `json:"suggestedQuantization"`
}

// ModelImportConfirmation is the user's confirmation of a picked file: the SHA-256 the page showed and the identity.
type ModelImportConfirmation struct {
	SelectionID  string `json:"selectionId"`
	SHA256       string `json:"sha256"`
	Name         string `json:"name"`
	Version      string `json:"version"`
	Quantization string `json:"quantization"`
}

// CatalogImportResult is the outcome of importing a file as a catalog model; Model is null when cancelled.
type CatalogImportResult struct {
	Cancelled bool                `json:"cancelled"`
	Model     *InstalledModelItem `json:"model"`
}

// modelSelection is the inspected file the page may confirm.
type modelSelection struct {
	id   string
	path string
	sum  values.SHA256
}

// ModelsService is bound to the models page like SettingsService ("<package path>.ModelsService.<Method>"). The page
// sends only ids, never a path or a URL. It keeps one pending file selection: a new file replaces it, and a confirm or
// a discard ends it.
type ModelsService struct {
	deps ModelsDeps
	log  *slog.Logger

	mu       sync.Mutex
	choosing bool
	lastID   uint64
	pending  *modelSelection
}

// NewModelsService returns the binding of the models page; OpenServices checks the deps before calling it.
func NewModelsService(deps ModelsDeps, log *slog.Logger) *ModelsService {
	return &ModelsService{deps: deps, log: log}
}

// List returns the installed models, which of them new STT jobs use, the catalog, whether speech recognition is
// available and the download state (PRD section 8, AC-07, AC-08). The hardware is not detected yet, so the catalog is
// listed for an unknown machine. A failed read of any part fails the whole list.
func (s *ModelsService) List(ctx context.Context) (ModelsView, error) {
	installed, err := s.deps.Catalog.ListInstalled(ctx)
	if err != nil {
		s.log.Error("ui: list installed models", "err", err)
		return ModelsView{}, ErrModelsUnavailable
	}
	inUse, needsDefault, err := s.modelInUse(ctx)
	if err != nil {
		s.log.Error("ui: resolve the model in use", "err", err)
		return ModelsView{}, ErrModelsUnavailable
	}
	available, err := s.deps.Catalog.ListAvailable(ctx, values.Machine{})
	if err != nil {
		s.log.Error("ui: list catalog models", "err", err)
		return ModelsView{}, ErrModelsUnavailable
	}

	view := ModelsView{
		Installed: make([]InstalledModelItem, 0, len(installed)), Available: make([]AvailableModelItem, 0, len(available)),
		NeedsDefault: needsDefault, SpeechReady: s.deps.SpeechReady, Download: downloadView(s.deps.Downloads.DownloadState()),
		ModelsHomePage: ModelsHomePage,
	}
	for _, m := range installed {
		item := installedItem(m)
		item.InUse = inUse != "" && m.ID == inUse
		view.Installed = append(view.Installed, item)
	}
	for _, a := range available {
		e := a.Entry
		view.Available = append(view.Available, AvailableModelItem{
			CatalogID: string(e.ID()), Name: string(e.Name()), Version: string(e.Version()), SizeBytes: e.SizeBytes(),
			Quantization: string(e.Quantization()), Capabilities: capabilityTexts(e.Capabilities()),
			MinCPUCores: e.Recommendation().MinCPUCores, MinRAMMB: e.Recommendation().MinRAMMB,
			Recommended: a.Recommended, LikelySlow: a.LikelySlow, Installed: a.InstalledID != "",
			InstalledID: string(a.InstalledID), HasSourcePage: !e.SourcePage().IsZero(),
		})
	}
	return view, nil
}

// modelInUse asks the model selection which model a new STT job pins; the page does not repeat the rule (ARCH-44).
// No model installed is neither a model in use nor a missing default: the page tells it from the empty list.
func (s *ModelsService) modelInUse(ctx context.Context) (shared.SttModelID, bool, error) {
	pinned, err := s.deps.Catalog.CurrentModel(ctx)
	switch {
	case err == nil:
		return pinned.ID, false, nil
	case errors.Is(err, modelselection.ErrNoDefaultModel):
		return "", true, nil
	case errors.Is(err, modelselection.ErrModelNotInstalled):
		return "", false, nil
	}
	return "", false, err
}

// PreflightDownload tells whether the disk has room for the download of a catalog model (PRD section 8).
func (s *ModelsService) PreflightDownload(ctx context.Context, catalogID string) (DownloadPlanView, error) {
	id, err := catalogIDOf(catalogID)
	if err != nil {
		return DownloadPlanView{}, err
	}
	p, err := s.deps.Downloads.Preflight(ctx, id)
	if err != nil {
		return DownloadPlanView{}, s.fail("plan model download", err, ErrModelNotFound)
	}
	view := DownloadPlanView{
		CatalogID: string(p.CatalogID), TotalBytes: p.TotalBytes, HaveBytes: p.HaveBytes, RequiredBytes: p.RequiredBytes,
		AvailableBytes: p.AvailableBytes, Enough: p.Enough,
	}
	if !p.Enough {
		view.Warning = spaceError{required: p.RequiredBytes, available: p.AvailableBytes}.Error()
	}
	return view, nil
}

// StartDownload starts the download of a catalog model in the background; List shows its progress.
func (s *ModelsService) StartDownload(_ context.Context, catalogID string) error {
	id, err := catalogIDOf(catalogID)
	if err != nil {
		return err
	}
	if err := s.deps.Downloads.StartDownload(id); err != nil {
		return s.fail("start model download", err, ErrModelNotFound)
	}
	return nil
}

// CancelDownload stops the running download; the bytes are kept, so starting it again resumes.
func (s *ModelsService) CancelDownload() { s.deps.Downloads.CancelDownload() }

// DiscardPartial deletes the bytes kept from an interrupted download, so the next one starts from the beginning.
func (s *ModelsService) DiscardPartial(ctx context.Context, catalogID string) error {
	id, err := catalogIDOf(catalogID)
	if err != nil {
		return err
	}
	if err := s.deps.Downloads.DiscardPartial(ctx, id); err != nil {
		return s.fail("discard partial model download", err, ErrModelNotFound)
	}
	return nil
}

// OpenSourcePage opens the source page of a catalog model in the default browser (PRD section 8.1). The URL comes
// from the catalog, never from the page.
func (s *ModelsService) OpenSourcePage(ctx context.Context, catalogID string) error {
	id, err := catalogIDOf(catalogID)
	if err != nil {
		return err
	}
	u, err := s.deps.Catalog.SourcePage(ctx, id)
	if err != nil {
		return s.fail("model source page", err, ErrModelNotInCatalog)
	}
	return s.open(ctx, u)
}

// OpenModelsHomePage opens ModelsHomePage in the default browser.
func (s *ModelsService) OpenModelsHomePage(ctx context.Context) error {
	u, err := values.ParseSourceURL(ModelsHomePage)
	if err != nil {
		s.log.Error("ui: models home page", "err", err)
		return ErrModelLinkFailed
	}
	return s.open(ctx, u)
}

func (s *ModelsService) open(ctx context.Context, u values.SourceURL) error {
	if err := s.deps.Links.OpenLink(ctx, u); err != nil {
		s.log.Error("ui: open model page in the browser", "host", u.Host(), "err", err)
		return ErrModelLinkFailed
	}
	return nil
}

// ChooseImportFile shows the open dialog and computes the size and SHA-256 of the chosen file, for the user to
// confirm before ConfirmImport (PRD section 8, AC-23). Nothing is installed. A cancelled dialog keeps the current
// selection; anything after the dialog opened replaces or drops it.
func (s *ModelsService) ChooseImportFile(ctx context.Context) (ModelFileSelection, error) {
	if !s.beginChoose() {
		return ModelFileSelection{}, ErrModelChoosing
	}
	defer s.endChoose()

	path, err := s.deps.Picker.PickModelFile(ctx)
	if err != nil {
		s.setPending(nil)
		s.log.Error("ui: choose model file", "err", err)
		return ModelFileSelection{}, ErrModelActionFailed
	}
	if path == "" {
		return ModelFileSelection{Cancelled: true}, nil
	}

	s.setPending(nil)
	if !strings.EqualFold(filepath.Ext(path), modelFileExt) {
		return ModelFileSelection{}, ErrModelFileType
	}
	p, err := s.deps.Imports.Inspect(ctx, path)
	if err != nil {
		return ModelFileSelection{}, s.fail("inspect model file", err, ErrModelFileMissing)
	}

	name := filepath.Base(path)
	suggested, quant := suggestIdentity(name)
	return ModelFileSelection{
		SelectionID: s.hold(path, p.SHA256), FileName: name, SizeBytes: p.SizeBytes, SHA256: string(p.SHA256),
		SuggestedName: suggested, SuggestedVersion: suggestedVersion, SuggestedQuantization: quant,
	}, nil
}

// ConfirmImport installs the pending selection as a model outside the catalog (PRD section 8, AC-23). The name,
// version and quantization are checked first and a bad one keeps the selection. Then only the pending selection with
// the SHA-256 the page showed is imported; the importer hashes the file again and refuses a file that changed. The
// selection is gone afterwards, whatever the outcome.
func (s *ModelsService) ConfirmImport(ctx context.Context, c ModelImportConfirmation) (InstalledModelItem, error) {
	name := values.ModelName(strings.TrimSpace(c.Name))
	if name.Validate() != nil {
		return InstalledModelItem{}, ErrModelBadName
	}
	version := values.ModelVersion(strings.TrimSpace(c.Version))
	if version.Validate() != nil {
		return InstalledModelItem{}, ErrModelBadVersion
	}
	quant := values.Quantization(strings.ToLower(strings.TrimSpace(c.Quantization)))
	if quant.Validate() != nil {
		return InstalledModelItem{}, ErrModelBadQuantization
	}

	sel := s.take(c.SelectionID)
	if sel == nil {
		return InstalledModelItem{}, ErrModelNoSelection
	}
	confirmed, err := values.ParseSHA256(strings.TrimSpace(c.SHA256))
	if err != nil || confirmed != sel.sum {
		return InstalledModelItem{}, ErrModelConfirmMismatch
	}
	consent, err := values.ConfirmImport(confirmed)
	if err != nil {
		return InstalledModelItem{}, ErrModelConfirmMismatch
	}

	m, err := s.deps.Imports.ImportCustom(ctx, modelcatalog.CustomImport{
		Path: sel.path, Name: name, ModelVersion: version, Quantization: quant, Confirmation: consent,
	})
	if err != nil {
		return InstalledModelItem{}, s.fail("import model", err, ErrModelFileMissing)
	}
	return installedItem(m), nil
}

// DiscardImport drops the pending selection selectionID; another id is ignored.
func (s *ModelsService) DiscardImport(selectionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pending != nil && s.pending.id == selectionID {
		s.pending = nil
	}
}

// ImportCatalogFile shows the open dialog and installs the chosen file as the catalog model catalogID, when its size
// and SHA-256 match the catalog (PRD section 8.1, AC-23).
func (s *ModelsService) ImportCatalogFile(ctx context.Context, catalogID string) (CatalogImportResult, error) {
	id, err := catalogIDOf(catalogID)
	if err != nil {
		return CatalogImportResult{}, err
	}
	if !s.beginChoose() {
		return CatalogImportResult{}, ErrModelChoosing
	}
	defer s.endChoose()

	path, err := s.deps.Picker.PickModelFile(ctx)
	if err != nil {
		s.log.Error("ui: choose catalog model file", "err", err)
		return CatalogImportResult{}, ErrModelActionFailed
	}
	if path == "" {
		return CatalogImportResult{Cancelled: true}, nil
	}
	if !strings.EqualFold(filepath.Ext(path), modelFileExt) {
		return CatalogImportResult{}, ErrModelFileType
	}
	m, err := s.deps.Imports.ImportCatalog(ctx, id, path)
	if err != nil {
		return CatalogImportResult{}, s.fail("import catalog model", err, ErrModelFileMissing)
	}
	item := installedItem(m)
	return CatalogImportResult{Model: &item}, nil
}

// SetDefault makes an installed model the default (AC-07).
func (s *ModelsService) SetDefault(ctx context.Context, modelID string) error {
	id := shared.SttModelID(modelID)
	if id.Validate() != nil {
		return ErrModelInvalidID
	}
	if err := s.deps.Catalog.SetDefault(ctx, id); err != nil {
		return s.fail("set default model", err, ErrModelNotFound)
	}
	return nil
}

// Delete removes an installed model and its file, unless an unfinished job pins it (PRD section 8).
func (s *ModelsService) Delete(ctx context.Context, modelID string) error {
	id := shared.SttModelID(modelID)
	if id.Validate() != nil {
		return ErrModelInvalidID
	}
	if err := s.deps.Catalog.Delete(ctx, id); err != nil {
		return s.fail("delete model", err, ErrModelNotFound)
	}
	return nil
}

func (s *ModelsService) beginChoose() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.choosing {
		return false
	}
	s.choosing = true
	return true
}

func (s *ModelsService) endChoose() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.choosing = false
}

func (s *ModelsService) setPending(sel *modelSelection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = sel
}

// hold makes the inspected file the pending selection and returns its id: a counter, unique in the process.
func (s *ModelsService) hold(path string, sum values.SHA256) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastID++
	id := strconv.FormatUint(s.lastID, 10)
	s.pending = &modelSelection{id: id, path: path, sum: sum}
	return id
}

// take removes and returns the pending selection id, so one selection is imported at most once; another id gets nil
// and keeps the selection.
func (s *ModelsService) take(id string) *modelSelection {
	s.mu.Lock()
	defer s.mu.Unlock()

	sel := s.pending
	if sel == nil || id == "" || sel.id != id {
		return nil
	}
	s.pending = nil
	return sel
}

// fail maps err to the page's text and logs the cause; notFound is the text of a plain shared.ErrNotFound, which
// means the model in model actions and the picked file in imports.
func (s *ModelsService) fail(op string, err error, notFound modelError) error {
	pe := pageError(err, notFound)
	if errors.Is(pe, ErrModelActionFailed) {
		s.log.Error("ui: "+op, "err", err)
	} else {
		s.log.Info("ui: "+op+": refused", "err", err)
	}
	return pe
}

// pageError maps an error of the model manager to the page's text; unknown errors are ErrModelActionFailed. The
// narrow errors come before the ones they wrap.
func pageError(err error, notFound modelError) error {
	var space *values.InsufficientSpaceError
	if errors.As(err, &space) {
		return spaceError{required: space.Required, available: space.Available}
	}
	for _, m := range []struct {
		err  error
		text modelError
	}{
		{values.ErrConfirmationMismatch, ErrModelFileChanged},
		{values.ErrChecksumMismatch, ErrModelChecksum},
		{values.ErrSizeMismatch, ErrModelSizeMismatch},
		{values.ErrImportTooLarge, ErrModelFileTooLarge},
		{values.ErrInvalidSize, ErrModelFileEmpty},
		{values.ErrInvalidImportFile, ErrModelFileNotRegular},
		{values.ErrReservedIdentity, ErrModelReservedName},
		{modelcatalog.ErrAlreadyInstalled, ErrModelAlreadyInstalled},
		{sttmodel.ErrFileInUse, ErrModelFileInUse},
		{sttmodel.ErrPinned, ErrModelPinned},
		{modelcatalog.ErrDefaultReselected, ErrModelDefaultRace},
		{modelcatalog.ErrDownloadInProgress, ErrModelDownloading},
		{modelcatalog.ErrDownloadInterrupted, ErrModelDownloadInterrupted},
		{values.ErrHostNotAllowed, ErrModelSourceRefused},
		{values.ErrUnexpectedRange, ErrModelSourceRefused},
		{modelcatalog.ErrNotInCatalog, ErrModelNotInCatalog},
		{modelselection.ErrModelNotInstalled, ErrModelNotFound},
		{shared.ErrNotFound, notFound},
		{shared.ErrStorageFull, ErrNotEnoughSpace},
		{shared.ErrStoragePermission, ErrModelNoAccess},
	} {
		if errors.Is(err, m.err) {
			return m.text
		}
	}
	return ErrModelActionFailed
}

func catalogIDOf(s string) (values.CatalogID, error) {
	id := values.CatalogID(s)
	if id.Validate() != nil {
		return "", ErrModelInvalidID
	}
	return id, nil
}

func installedItem(m modelcatalog.InstalledModel) InstalledModelItem {
	return InstalledModelItem{
		ID: string(m.ID), Name: string(m.Name), Version: string(m.ModelVersion), SizeBytes: m.SizeBytes,
		SHA256: string(m.SHA256), Quantization: string(m.Quantization), Capabilities: capabilityTexts(m.Capabilities),
		IsDefault: m.IsDefault, FromCatalog: m.CatalogID != "", CatalogID: string(m.CatalogID),
		InstalledAt: timeText(m.InstalledAt),
	}
}

func capabilityTexts(caps []values.Capability) []string {
	out := make([]string, 0, len(caps))
	for _, c := range caps {
		out = append(out, string(c))
	}
	return out
}

func downloadView(st DownloadState) ModelDownload {
	v := ModelDownload{
		CatalogID: string(st.CatalogID), Phase: string(st.Phase), DoneBytes: st.DoneBytes, TotalBytes: st.TotalBytes,
		Running: st.Running,
	}
	switch {
	case st.Running:
	case st.Cancelled:
		v.Phase, v.Error = phaseCancelled, ErrModelDownloadStopped.Error()
	case st.Err != nil:
		v.Phase, v.Error = string(modelcatalog.PhaseFailed), pageError(st.Err, ErrModelNotInCatalog).Error()
	}
	return v
}

// suggestIdentity proposes a name and a quantization from a ggml file name: "ggml-base.en-q5_1.bin" is base.en and
// q5_1. Either is empty when the name does not fit the rules.
func suggestIdentity(file string) (name, quant string) {
	base := strings.TrimPrefix(strings.TrimSuffix(file, filepath.Ext(file)), "ggml-")
	if i := strings.LastIndex(base, "-"); i > 0 {
		if q := values.Quantization(strings.ToLower(base[i+1:])); quantLabel(q) {
			quant, base = string(q), base[:i]
		}
	}
	if values.ModelName(base).Validate() == nil {
		name = base
	}
	return name, quant
}

// quantLabel reports a ggml weight format label such as q5_1, q8_0 or f16.
func quantLabel(q values.Quantization) bool {
	return len(q) >= 2 && (q[0] == 'q' || q[0] == 'f') && q[1] >= '0' && q[1] <= '9' && q.Validate() == nil
}

// sizeText formats bytes for the page: "1,5 ГБ", "300 МБ", "12 КБ" (binary units, decimal comma).
func sizeText(n int64) string {
	const kb, mb, gb = 1 << 10, 1 << 20, 1 << 30
	var s string
	switch {
	case n >= gb:
		s = strconv.FormatFloat(float64(n)/gb, 'f', 1, 64) + " ГБ"
	case n >= mb:
		s = strconv.FormatFloat(float64(n)/mb, 'f', 0, 64) + " МБ"
	case n >= kb:
		s = strconv.FormatFloat(float64(n)/kb, 'f', 0, 64) + " КБ"
	default:
		s = strconv.FormatInt(n, 10) + " Б"
	}
	return strings.Replace(s, ".", ",", 1)
}
