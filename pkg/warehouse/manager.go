// Package warehouse provides virtual warehouse lifecycle and workload admission.
package warehouse

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
)

type State string

const (
	StateSuspended  State = "SUSPENDED"
	StateResuming   State = "RESUMING"
	StateActive     State = "ACTIVE"
	StateSuspending State = "SUSPENDING"
)

type Warehouse struct {
	ID              string
	Name            string
	State           State
	Size            string
	Comment         string
	Owner           string
	CreatedAt       time.Time
	AutoResume      bool
	AutoSuspend     int
	Running         int
	Queued          int
	LastResumedAt   *time.Time
	LastSuspendedAt *time.Time
	LastActivityAt  *time.Time
}

type Settings struct {
	Size        string
	AutoResume  bool
	AutoSuspend int
}
type waiter struct {
	ready    chan error
	admitted bool
}
type runtimeWarehouse struct {
	value Warehouse
	queue []*waiter
}

type Manager struct {
	mu         sync.Mutex
	warehouses map[string]*runtimeWarehouse
	repo       *metadata.Repository
	now        func() time.Time
}

type Lease struct {
	manager *Manager
	name    string
	once    sync.Once
}

func (l *Lease) Release() {
	if l != nil && l.manager != nil {
		l.once.Do(func() { l.manager.release(l.name) })
	}
}

func NewManager() *Manager {
	return &Manager{warehouses: map[string]*runtimeWarehouse{}, now: time.Now}
}

func NewPersistentManager(ctx context.Context, repo *metadata.Repository) (*Manager, error) {
	m := NewManager()
	m.repo = repo
	records, err := repo.ListWarehouseRecords(ctx)
	if err != nil {
		return nil, err
	}
	for i := range records {
		v := fromRecord(&records[i])
		v.State = StateSuspended
		v.Running, v.Queued = 0, 0
		m.warehouses[v.Name] = &runtimeWarehouse{value: v}
		if err := m.persist(ctx, &v); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func (m *Manager) CreateWarehouse(ctx context.Context, name, size, comment string) (*Warehouse, error) {
	return m.CreateWarehouseWithSettings(ctx, name, comment, Settings{Size: size, AutoResume: true, AutoSuspend: 600})
}

func (m *Manager) CreateWarehouseWithSettings(ctx context.Context, name, comment string, s Settings) (*Warehouse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name, s.Size = normalizeName(name), normalizeSize(s.Size)
	if name == "" {
		return nil, fmt.Errorf("warehouse name cannot be empty")
	}
	if _, ok := m.warehouses[name]; ok {
		return nil, fmt.Errorf("warehouse %s already exists", name)
	}
	if !validSize(s.Size) {
		return nil, fmt.Errorf("invalid warehouse size: %s", s.Size)
	}
	if s.AutoSuspend < 0 {
		return nil, fmt.Errorf("auto suspend cannot be negative")
	}
	v := Warehouse{ID: uuid.NewString(), Name: name, State: StateSuspended, Size: s.Size, Comment: comment, CreatedAt: m.now(), AutoResume: s.AutoResume, AutoSuspend: s.AutoSuspend}
	if err := m.persist(ctx, &v); err != nil {
		return nil, err
	}
	m.warehouses[name] = &runtimeWarehouse{value: v}
	return copyWarehouse(&v), nil
}

func (m *Manager) GetWarehouse(_ context.Context, name string) (*Warehouse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, err := m.lookup(name)
	if err != nil {
		return nil, err
	}
	return copyWarehouse(&item.value), nil
}

func (m *Manager) ListWarehouses(_ context.Context) ([]*Warehouse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Warehouse, 0, len(m.warehouses))
	for _, item := range m.warehouses {
		out = append(out, copyWarehouse(&item.value))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *Manager) ResumeWarehouse(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, err := m.lookup(name)
	if err != nil {
		return err
	}
	if item.value.State == StateActive {
		return nil
	}
	if item.value.State != StateSuspended {
		return fmt.Errorf("warehouse %s is in %s state, cannot resume", item.value.Name, item.value.State)
	}
	m.resume(item)
	return m.persist(ctx, &item.value)
}

func (m *Manager) SuspendWarehouse(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, err := m.lookup(name)
	if err != nil {
		return err
	}
	if item.value.State == StateSuspended {
		return nil
	}
	item.value.State = StateSuspending
	rejection := fmt.Errorf("warehouse %s was suspended while statement was queued", item.value.Name)
	for _, w := range item.queue {
		w.ready <- rejection
		close(w.ready)
	}
	item.queue = nil
	item.value.Queued = 0
	if item.value.Running == 0 {
		m.suspend(item)
	}
	return m.persist(ctx, &item.value)
}

func (m *Manager) AlterWarehouse(ctx context.Context, name string, s Settings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, err := m.lookup(name)
	if err != nil {
		return err
	}
	s.Size = normalizeSize(s.Size)
	if !validSize(s.Size) {
		return fmt.Errorf("invalid warehouse size: %s", s.Size)
	}
	if s.AutoSuspend < 0 {
		return fmt.Errorf("auto suspend cannot be negative")
	}
	item.value.Size, item.value.AutoResume, item.value.AutoSuspend = s.Size, s.AutoResume, s.AutoSuspend
	m.admit(item)
	return m.persist(ctx, &item.value)
}

func (m *Manager) DropWarehouse(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, err := m.lookup(name)
	if err != nil {
		return err
	}
	if item.value.Running > 0 || item.value.Queued > 0 {
		return fmt.Errorf("warehouse %s cannot be dropped while it has running or queued statements", item.value.Name)
	}
	if m.repo != nil {
		if err := m.repo.DeleteWarehouseRecord(ctx, item.value.Name); err != nil {
			return err
		}
	}
	delete(m.warehouses, item.value.Name)
	return nil
}

func (m *Manager) Acquire(ctx context.Context, name string, onQueued func()) (*Lease, error) {
	m.mu.Lock()
	item, err := m.lookup(name)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if item.value.State == StateSuspended {
		if !item.value.AutoResume {
			m.mu.Unlock()
			return nil, fmt.Errorf("warehouse %s is suspended and AUTO_RESUME is disabled", item.value.Name)
		}
		m.resume(item)
		if err := m.persist(ctx, &item.value); err != nil {
			m.mu.Unlock()
			return nil, err
		}
	}
	if item.value.State != StateActive {
		m.mu.Unlock()
		return nil, fmt.Errorf("warehouse %s is %s and cannot accept statements", item.value.Name, item.value.State)
	}
	if len(item.queue) == 0 && item.value.Running < capacity(item.value.Size) {
		item.value.Running++
		n := item.value.Name
		m.mu.Unlock()
		return &Lease{manager: m, name: n}, nil
	}
	w := &waiter{ready: make(chan error, 1)}
	item.queue = append(item.queue, w)
	item.value.Queued++
	normalized := item.value.Name
	m.mu.Unlock()
	if onQueued != nil {
		onQueued()
	}
	select {
	case err := <-w.ready:
		if err != nil {
			return nil, err
		}
		return &Lease{manager: m, name: normalized}, nil
	case <-ctx.Done():
		m.mu.Lock()
		item, ok := m.warehouses[normalized]
		if ok {
			for i, q := range item.queue {
				if q == w {
					item.queue = append(item.queue[:i], item.queue[i+1:]...)
					item.value.Queued--
					m.mu.Unlock()
					return nil, ctx.Err()
				}
			}
			if w.admitted && item.value.Running > 0 {
				m.releaseLocked(item)
				if err := m.persist(context.Background(), &item.value); err != nil {
					log.Printf("failed to persist warehouse %s after canceled admission: %v", item.value.Name, err)
				}
			}
		}
		m.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (m *Manager) CheckAutoSuspend(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	for _, item := range m.warehouses {
		v := &item.value
		if v.State == StateActive && v.AutoSuspend > 0 && v.Running == 0 && v.Queued == 0 && v.LastActivityAt != nil && now.Sub(*v.LastActivityAt) >= time.Duration(v.AutoSuspend)*time.Second {
			m.suspend(item)
			if err := m.persist(ctx, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Manager) StartAutoSuspend(ctx context.Context, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := m.CheckAutoSuspend(ctx); err != nil {
					log.Printf("warehouse auto-suspend check failed: %v", err)
				}
			}
		}
	}()
}

func (m *Manager) release(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.warehouses[name]
	if !ok || item.value.Running == 0 {
		return
	}
	m.releaseLocked(item)
	if err := m.persist(context.Background(), &item.value); err != nil {
		log.Printf("failed to persist warehouse %s after release: %v", item.value.Name, err)
	}
}

func (m *Manager) releaseLocked(item *runtimeWarehouse) {
	item.value.Running--
	now := m.now()
	item.value.LastActivityAt = &now
	if item.value.State == StateSuspending && item.value.Running == 0 {
		m.suspend(item)
	} else {
		m.admit(item)
	}
}

func (m *Manager) admit(item *runtimeWarehouse) {
	for item.value.State == StateActive && item.value.Running < capacity(item.value.Size) && len(item.queue) > 0 {
		w := item.queue[0]
		item.queue = item.queue[1:]
		item.value.Queued--
		item.value.Running++
		w.admitted = true
		w.ready <- nil
		close(w.ready)
	}
}

func (m *Manager) resume(item *runtimeWarehouse) {
	item.value.State = StateResuming
	now := m.now()
	item.value.LastResumedAt = &now
	item.value.LastActivityAt = &now
	item.value.State = StateActive
}

func (m *Manager) suspend(item *runtimeWarehouse) {
	item.value.State = StateSuspending
	now := m.now()
	item.value.LastSuspendedAt = &now
	item.value.State = StateSuspended
}

func (m *Manager) lookup(name string) (*runtimeWarehouse, error) {
	name = normalizeName(name)
	item, ok := m.warehouses[name]
	if !ok {
		return nil, fmt.Errorf("warehouse %s not found", name)
	}
	return item, nil
}

func (m *Manager) persist(ctx context.Context, v *Warehouse) error {
	if m.repo == nil {
		return nil
	}
	return m.repo.UpsertWarehouse(ctx, &metadata.WarehouseRecord{
		ID:              v.ID,
		Name:            v.Name,
		State:           string(v.State),
		Size:            v.Size,
		Comment:         v.Comment,
		CreatedAt:       v.CreatedAt,
		Owner:           v.Owner,
		AutoResume:      v.AutoResume,
		AutoSuspend:     v.AutoSuspend,
		LastResumedAt:   v.LastResumedAt,
		LastSuspendedAt: v.LastSuspendedAt,
		LastActivityAt:  v.LastActivityAt,
	})
}

func fromRecord(r *metadata.WarehouseRecord) Warehouse {
	return Warehouse{
		ID:              r.ID,
		Name:            r.Name,
		State:           State(r.State),
		Size:            r.Size,
		Comment:         r.Comment,
		CreatedAt:       r.CreatedAt,
		Owner:           r.Owner,
		AutoResume:      r.AutoResume,
		AutoSuspend:     r.AutoSuspend,
		LastResumedAt:   r.LastResumedAt,
		LastSuspendedAt: r.LastSuspendedAt,
		LastActivityAt:  r.LastActivityAt,
	}
}

func copyWarehouse(v *Warehouse) *Warehouse {
	c := *v
	return &c
}

func normalizeName(v string) string {
	return strings.ToUpper(strings.TrimSpace(v))
}

func normalizeSize(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	if v == "" {
		return "X-SMALL"
	}
	return v
}

var sizes = []string{"X-SMALL", "SMALL", "MEDIUM", "LARGE", "X-LARGE", "2X-LARGE", "3X-LARGE", "4X-LARGE", "5X-LARGE", "6X-LARGE"}

func validSize(v string) bool {
	for _, s := range sizes {
		if v == s {
			return true
		}
	}
	return false
}

func capacity(v string) int {
	for i, s := range sizes {
		if v == s {
			return 1 << i
		}
	}
	return 1
}
