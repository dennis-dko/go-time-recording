package memory

import (
	"context"
	"strconv"
	"sync"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// OpenIDIdentityRepository is an in-memory repository.OpenIDIdentityRepository.
type OpenIDIdentityRepository struct {
	mu         sync.RWMutex
	identities map[uint]model.OpenIDIdentity
	nextID     uint
}

// NewOpenIDIdentityRepository creates an empty one.
func NewOpenIDIdentityRepository() *OpenIDIdentityRepository {
	return &OpenIDIdentityRepository{identities: make(map[uint]model.OpenIDIdentity)}
}

var _ repository.OpenIDIdentityRepository = (*OpenIDIdentityRepository)(nil)

func (r *OpenIDIdentityRepository) ForUser(_ context.Context, userID uint) (*model.OpenIDIdentity, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	identity, found := r.identities[userID]
	if !found {
		return nil, apperror.NotFound("identity", strconv.FormatUint(uint64(userID), 10))
	}

	return &identity, nil
}

func (r *OpenIDIdentityRepository) BySubject(_ context.Context, issuer, subject string) (*model.OpenIDIdentity, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, identity := range r.identities {
		if identity.Issuer == issuer && identity.Subject == subject {
			return &identity, nil
		}
	}

	return nil, apperror.NotFound("identity", "")
}

func (r *OpenIDIdentityRepository) Link(_ context.Context, identity *model.OpenIDIdentity) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// The table's second unique constraint, which the SQL repository has the
	// database enforce: one subject, one account.
	for userID, linked := range r.identities {
		if userID != identity.UserID && linked.Issuer == identity.Issuer && linked.Subject == identity.Subject {
			return apperror.Conflictf("this identity is linked to another account")
		}
	}

	r.nextID++

	stored := *identity
	stored.ID = r.nextID
	r.identities[identity.UserID] = stored

	return nil
}

func (r *OpenIDIdentityRepository) Unlink(_ context.Context, userID uint) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, found := r.identities[userID]
	delete(r.identities, userID)

	return found, nil
}
