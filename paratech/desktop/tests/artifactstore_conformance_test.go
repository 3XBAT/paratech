package tests

import (
	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/modelcatalog"
	"paratech/desktop/internal/cases/recordingsession"
	"paratech/desktop/internal/cases/senddelivery"
	"paratech/desktop/internal/cases/transcribe"
)

// *filestore.Store must satisfy every ArtifactStore port; a port change breaks the build here.
var (
	_ transcribe.ArtifactStore       = (*filestore.Store)(nil)
	_ modelcatalog.ArtifactStore     = (*filestore.Store)(nil)
	_ recordingsession.ArtifactStore = (*filestore.Store)(nil)
	_ homeworkflow.ArtifactStore     = (*filestore.Store)(nil)
	_ senddelivery.ArtifactStore     = (*filestore.Store)(nil)
)
