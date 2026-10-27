package mem

import (
	"context"
	"fmt"
	"sort"
	"time"

	"dtm/adapters/db/db"
	"dtm/adapters/db/utils"
	"dtm/domain"
	"dtm/libs/textvalidate"

	"github.com/google/uuid"
)

var _ db.AuthDBWrapper = (*inMemoryAuthDBWrapper)(nil)

func (s *inMemoryAuthDBWrapper) UpsertUser(ctx context.Context, gcid string, email ...string) (domain.User, error) {
	if err := ctx.Err(); err != nil {
		return domain.User{}, err
	}
	if textvalidate.ValidateNonBlank(gcid) != nil {
		return domain.User{}, db.ErrInvalidGCID
	}
	s.memoryState.mu.Lock()
	defer s.memoryState.mu.Unlock()
	if id, ok := s.memoryState.userIDsByGCID[gcid]; ok {
		u := s.memoryState.users[id]
		if len(email) > 0 && email[0] != "" {
			e := email[0]
			u.Email = &e
			s.memoryState.users[id] = u
		}
		return cloneUser(u), nil
	}
	u := domain.User{ID: uuid.New(), GCID: gcid}
	if len(email) > 0 && email[0] != "" {
		e := email[0]
		u.Email = &e
	}
	s.memoryState.users[u.ID] = u
	s.memoryState.userIDsByGCID[gcid] = u.ID
	s.memoryState.subscriptions[u.ID] = make(map[uuid.UUID]time.Time)
	return cloneUser(u), nil
}
func (s *inMemoryAuthDBWrapper) UserGCID2UserId(ctx context.Context, gcid string) (uuid.UUID, error) {
	if err := ctx.Err(); err != nil {
		return uuid.Nil, err
	}
	s.memoryState.mu.RLock()
	defer s.memoryState.mu.RUnlock()
	if id, ok := s.memoryState.userIDsByGCID[gcid]; ok {
		return id, nil
	}
	return uuid.Nil, domain.ErrUserNotFound
}
func (s *inMemoryAuthDBWrapper) SubscribedTrips(ctx context.Context, id uuid.UUID) ([]domain.SubscribedTrip, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.memoryState.mu.RLock()
	defer s.memoryState.mu.RUnlock()
	trips, ok := s.memoryState.subscriptions[id]
	if !ok {
		return nil, domain.ErrUserNotFound
	}
	out := []domain.SubscribedTrip{}
	for tid, viewed := range trips {
		out = append(out, domain.SubscribedTrip{ID: tid, Name: s.memoryState.tripsInfo[tid].Name, LastViewedAt: viewed})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID.String() < out[j].ID.String() })
	return out, nil
}
func (s *inMemoryAuthDBWrapper) SubscribeTrip(ctx context.Context, uid, tid uuid.UUID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.memoryState.mu.Lock()
	defer s.memoryState.mu.Unlock()
	if err := s.accessLocked(ctx, tid); err != nil {
		return err
	}
	if _, ok := s.memoryState.subscriptions[uid]; !ok {
		return domain.ErrUserNotFound
	}
	if _, ok := s.memoryState.tripsInfo[tid]; !ok {
		return domain.ErrTripNotFound
	}
	if _, exists := s.memoryState.subscriptions[uid][tid]; !exists {
		s.memoryState.subscriptions[uid][tid] = time.Now().UTC()
	}
	return nil
}
func (s *inMemoryAuthDBWrapper) UnsubscribeTrip(ctx context.Context, uid, tid uuid.UUID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.memoryState.mu.Lock()
	defer s.memoryState.mu.Unlock()
	if _, ok := s.memoryState.subscriptions[uid]; !ok {
		return domain.ErrUserNotFound
	}
	delete(s.memoryState.subscriptions[uid], tid)
	return nil
}
func (s *inMemoryAuthDBWrapper) CreateSubscribedTrip(ctx context.Context, uid uuid.UUID, info *domain.TripInfo) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.memoryState.mu.Lock()
	defer s.memoryState.mu.Unlock()
	if _, ok := s.memoryState.subscriptions[uid]; !ok {
		return domain.ErrUserNotFound
	}
	if err := s.memoryState.createTripLocked(info); err != nil {
		return err
	}
	s.memoryState.subscriptions[uid][info.ID] = time.Now().UTC()
	return nil
}

func (s *inMemoryAuthDBWrapper) TouchSubscribedTrip(ctx context.Context, uid, tid uuid.UUID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.memoryState.mu.Lock()
	defer s.memoryState.mu.Unlock()
	if err := s.accessLocked(ctx, tid); err != nil {
		return err
	}
	previous, ok := s.memoryState.subscriptions[uid][tid]
	if !ok {
		return domain.ErrSubscriptionNotFound
	}
	if now := time.Now().UTC(); now.After(previous) {
		s.memoryState.subscriptions[uid][tid] = now
	}
	return nil
}

func cloneUser(u domain.User) domain.User {
	if u.Name != nil {
		v := *u.Name
		u.Name = &v
	}
	if u.Email != nil {
		v := *u.Email
		u.Email = &v
	}
	return u
}
func (s *inMemoryAuthDBWrapper) GetUser(ctx context.Context, id uuid.UUID) (domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return domain.User{}, err
	}
	u, ok := s.users[id]
	if !ok {
		return u, domain.ErrUserNotFound
	}
	return cloneUser(u), nil
}
func (s *inMemoryAuthDBWrapper) UpdateUser(ctx context.Context, id uuid.UUID, name *string, sub *bool) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return domain.User{}, err
	}
	u, ok := s.users[id]
	if !ok {
		return u, domain.ErrUserNotFound
	}
	if err := utils.ValidateProfile(name, sub, u.Email); err != nil {
		return u, err
	}
	if name != nil {
		n := *name
		u.Name = &n
	}
	if sub != nil {
		u.SubMail = *sub
	}
	s.users[id] = u
	return cloneUser(u), nil
}

// accessLocked must run under the shared state lock, including all writes.
func (s *memoryState) accessLocked(ctx context.Context, id uuid.UUID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info := s.tripsInfo[id]
	if info == nil {
		return domain.ErrTripNotFound
	}
	if !info.Private {
		return nil
	}
	uid, ok := domain.UserIDFromContext(ctx)
	if !ok {
		return domain.ErrUnauthenticated
	}
	if _, ok := s.bindings[id][uid]; !ok {
		return domain.ErrForbidden
	}
	return nil
}
func (s *memoryState) memberUser(ctx context.Context) (uuid.UUID, error) {
	uid, ok := domain.UserIDFromContext(ctx)
	if !ok {
		return uuid.Nil, domain.ErrUnauthenticated
	}
	if _, ok := s.users[uid]; !ok {
		return uuid.Nil, domain.ErrUserNotFound
	}
	return uid, ctx.Err()
}
func (s *inMemoryTripDBWrapper) CheckTripAccess(ctx context.Context, id uuid.UUID) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.accessLocked(ctx, id)
}
func (s *inMemoryTripDBWrapper) Binding(ctx context.Context, id uuid.UUID) (*domain.TripBinding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	uid, err := s.memberUser(ctx)
	if err != nil {
		return nil, err
	}
	aid, ok := s.bindings[id][uid]
	if !ok {
		return nil, nil
	}
	out := &domain.TripBinding{TripID: id}
	if aid != nil {
		for _, a := range s.tripsData[id].AddressList {
			if a.ID == *aid {
				a := s.displayLocked(id, a)
				out.Address = &a
				break
			}
		}
	}
	return out, nil
}
func (s *inMemoryTripDBWrapper) Bind(ctx context.Context, id uuid.UUID, aid *uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	uid, err := s.memberUser(ctx)
	if err != nil {
		return err
	}
	if err := s.accessLocked(ctx, id); err != nil {
		return err
	}
	if aid != nil {
		found := false
		for _, a := range s.tripsData[id].AddressList {
			if a.ID == *aid {
				found = true
			}
		}
		if !found {
			return domain.ErrBadUserInput
		}
		for u, a := range s.bindings[id] {
			if u != uid && a != nil && *a == *aid {
				return domain.ErrConflict
			}
		}
		copy := *aid
		aid = &copy
	}
	if s.bindings[id] == nil {
		s.bindings[id] = make(map[uuid.UUID]*uuid.UUID)
	}
	s.bindings[id][uid] = aid
	return nil
}
func (s *inMemoryTripDBWrapper) Unbind(ctx context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	uid, err := s.memberUser(ctx)
	if err != nil {
		return err
	}
	if _, ok := s.bindings[id][uid]; !ok {
		return nil
	}
	if s.tripsInfo[id].Private && len(s.bindings[id]) == 1 {
		return domain.ErrConflict
	}
	delete(s.bindings[id], uid)
	return nil
}
func (s *inMemoryTripDBWrapper) SetTripPrivate(ctx context.Context, id uuid.UUID, private bool) (*domain.TripInfo, error) {
	return s.PatchTripInfo(ctx, id, nil, &private)
}
func (s *inMemoryTripDBWrapper) PatchTripInfo(ctx context.Context, id uuid.UUID, name *string, private *bool) (*domain.TripInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.accessLocked(ctx, id); err != nil {
		return nil, err
	}
	if name != nil {
		if err := textvalidate.ValidateName(*name); err != nil {
			return nil, fmt.Errorf("%w: %v", domain.ErrBadUserInput, err)
		}
	}
	if private != nil {
		uid, err := s.memberUser(ctx)
		if err != nil {
			return nil, err
		}
		if _, ok := s.bindings[id][uid]; !ok {
			return nil, domain.ErrForbidden
		}
	}
	if name != nil {
		s.tripsInfo[id].Name = *name
	}
	if private != nil {
		s.tripsInfo[id].Private = *private
	}
	out := *s.tripsInfo[id]
	return &out, nil
}
