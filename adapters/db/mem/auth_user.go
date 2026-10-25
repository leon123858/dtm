package mem

import (
	"context"
	"dtm/adapters/db/db"
	"dtm/domain"
	"github.com/google/uuid"
	"sort"
	"strings"
	"time"
)

var _ db.AuthDBWrapper = (*inMemoryAuthDBWrapper)(nil)

func (s *inMemoryAuthDBWrapper) UpsertUser(ctx context.Context, gcid string) (domain.User, error) {
	if err := ctx.Err(); err != nil {
		return domain.User{}, err
	}
	if strings.TrimSpace(gcid) == "" {
		return domain.User{}, db.ErrInvalidGCID
	}
	s.memoryState.mu.Lock()
	defer s.memoryState.mu.Unlock()
	if id, ok := s.memoryState.userIDsByGCID[gcid]; ok {
		return s.memoryState.users[id], nil
	}
	u := domain.User{ID: uuid.New(), GCID: gcid}
	s.memoryState.users[u.ID] = u
	s.memoryState.userIDsByGCID[gcid] = u.ID
	s.memoryState.subscriptions[u.ID] = make(map[uuid.UUID]time.Time)
	return u, nil
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
	previous, ok := s.memoryState.subscriptions[uid][tid]
	if !ok {
		return domain.ErrSubscriptionNotFound
	}
	if now := time.Now().UTC(); now.After(previous) {
		s.memoryState.subscriptions[uid][tid] = now
	}
	return nil
}
