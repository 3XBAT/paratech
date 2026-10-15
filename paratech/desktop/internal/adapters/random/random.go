package random

import (
	"context"
	"crypto/rand"
	"math/big"
	"strings"

	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/shared"
)

// messageIDDomain is the right side of generated Message-IDs: a reserved name (RFC 6761), so the machine's host name
// never leaves it.
const messageIDDomain = "learning-assistant.invalid"

// permilleRange is the size of the Permille range 0..999.
const permilleRange = 1000

// IDs generates identifiers. The zero value is ready to use and safe for concurrent use.
type IDs struct{}

// token is 26 lower-case base32 characters with 130 bits of randomness: a safe file-name segment on every system.
func token() string { return strings.ToLower(rand.Text()) }

// NewRecordingID returns a fresh recording identifier.
func (IDs) NewRecordingID() shared.RecordingID { return shared.RecordingID(token()) }

// NewJobID returns a fresh job identifier.
func (IDs) NewJobID() shared.JobID { return shared.JobID(token()) }

// NewAssignmentID returns a fresh assignment identifier.
func (IDs) NewAssignmentID() shared.AssignmentID { return shared.AssignmentID(token()) }

// NewProposalID returns a fresh action proposal identifier.
func (IDs) NewProposalID() proposal.ID { return proposal.ID(token()) }

// NewLessonID returns a fresh lesson identifier.
func (IDs) NewLessonID() shared.LessonID { return shared.LessonID(token()) }

// NewSttModelID returns a fresh identifier of an installed speech recognition model.
func (IDs) NewSttModelID() shared.SttModelID { return shared.SttModelID(token()) }

// NewDeliveryID returns a fresh delivery identifier.
func (IDs) NewDeliveryID() shared.DeliveryID { return shared.DeliveryID(token()) }

// NewMessageID returns a fresh RFC 5322 Message-ID "<random@learning-assistant.invalid>".
func (IDs) NewMessageID() string { return "<" + token() + "@" + messageIDDomain + ">" }

// Jitter supplies the randomness of retry delays. The zero value is ready to use.
type Jitter struct{}

// Permille returns a uniformly random number from 0 to 999.
func (Jitter) Permille(context.Context) (int, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(permilleRange))
	if err != nil {
		return 0, err
	}
	return int(n.Int64()), nil
}
