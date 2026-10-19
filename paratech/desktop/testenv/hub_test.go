package testenv

import (
	"testing"

	"paratech/desktop/internal/domain/coursework/values"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
)

// Every operation of the closed catalog has its contract value, so FakeHub.Answer can script it.
func TestHubOperationCoversTheCatalog(t *testing.T) {
	cases := map[values.Operation]hubv1.OperationType{
		values.OperationSummary:         hubv1.OperationType_OPERATION_TYPE_SUMMARY,
		values.OperationExtractHomework: hubv1.OperationType_OPERATION_TYPE_EXTRACT_HOMEWORK_DEADLINES,
		values.OperationHomeworkDraft:   hubv1.OperationType_OPERATION_TYPE_HOMEWORK_DRAFT,
		values.OperationLectureText:     hubv1.OperationType_OPERATION_TYPE_LECTURE_TEXT,
		values.Operation("other"):       hubv1.OperationType_OPERATION_TYPE_UNSPECIFIED,
	}
	for op, want := range cases {
		if got := hubOperation(op); got != want {
			t.Errorf("hubOperation(%q) = %v, want %v", op, got, want)
		}
	}
}
