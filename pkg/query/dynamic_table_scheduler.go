package query

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
)

// DynamicTableScheduler performs serial full refreshes for elapsed time-based
// freshness targets. DOWNSTREAM remains manual; no dependency graph is implied.
type DynamicTableScheduler struct {
	processor *DynamicTableProcessor
	interval  time.Duration
	mu        sync.Mutex
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

func NewDynamicTableScheduler(executor *Executor, interval time.Duration) *DynamicTableScheduler {
	if interval <= 0 {
		interval = time.Second
	}
	return &DynamicTableScheduler{processor: executor.dynamicTableProcessor, interval: interval}
}

func (s *DynamicTableScheduler) Start(parent context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.wg.Add(1)
	go func() { defer s.wg.Done(); s.run(ctx) }()
}

func (s *DynamicTableScheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.wg.Wait()
		s.cancel = nil
	}
}

func (s *DynamicTableScheduler) run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		if err := s.RunDueTables(ctx, time.Now()); err != nil && ctx.Err() == nil {
			log.Printf("dynamic table scheduler: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunDueTables is one deterministic pass. The processor mutex serializes it
// against other passes and manual refresh/create/drop, including pinned calls.
func (s *DynamicTableScheduler) RunDueTables(ctx context.Context, now time.Time) error {
	s.processor.mu.Lock()
	defer s.processor.mu.Unlock()
	return s.runDueLocked(ctx, now)
}

func (s *DynamicTableScheduler) runDueLocked(ctx context.Context, now time.Time) error {
	p := s.processor
	values, err := p.repo.ListDynamicTables(ctx, "")
	if err != nil {
		return err
	}
	var failures []error
	for _, value := range values {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		lag, lagErr := parseTargetLag(value.TargetLag)
		if lagErr != nil {
			failures = append(failures, lagErr)
			continue
		}
		if lag == 0 {
			continue
		}
		last := value.CreatedAt
		if value.LastRefreshedAt != nil {
			last = *value.LastRefreshedAt
		}
		if now.Before(last.Add(lag)) {
			continue
		}
		if value.LastAttemptedAt != nil && value.LastRefreshError != "" && now.Before(value.LastAttemptedAt.Add(30*time.Second)) {
			continue
		}
		// Bound warehouse queue waits and long materializations; one unavailable
		// warehouse must not indefinitely block every other dynamic table.
		attemptCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err = s.refresh(attemptCtx, value, now)
		cancel()
		if err != nil {
			failures = append(failures, fmt.Errorf("dynamic table %s: %w", value.Name, err))
			if recordErr := p.repo.RecordDynamicTableFailure(ctx, value.ID, now, err.Error()); recordErr != nil {
				failures = append(failures, recordErr)
			}
		}
	}
	return errors.Join(failures...)
}

func (s *DynamicTableScheduler) refresh(ctx context.Context, value *metadata.DynamicTable, now time.Time) error {
	p := s.processor
	schema, err := p.repo.GetSchema(ctx, value.SchemaID)
	if err != nil {
		return err
	}
	database, err := p.repo.GetDatabase(ctx, schema.DatabaseID)
	if err != nil {
		return err
	}
	ec := ExecutionContext{Database: database.Name, Schema: schema.Name, Warehouse: value.Warehouse}
	if p.executor.identityService != nil {
		if value.Owner == "" {
			return fmt.Errorf("dynamic table has no authenticated owner role")
		}
		role, roleErr := p.executor.identityService.RoleByID(ctx, value.Owner)
		if roleErr != nil {
			return roleErr
		}
		ec.Role = role.Name
		ec.Principal = &PrincipalContext{UserID: "DYNAMIC_TABLE:" + value.ID, Username: "DYNAMIC_TABLE:" + value.Name, RoleID: role.ID}
	}
	_, err = p.refreshLocked(ctx, ec, "ALTER DYNAMIC TABLE "+database.Name+"."+schema.Name+"."+value.Name+" REFRESH", now)
	return err
}

func parseTargetLag(value string) (time.Duration, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "DOWNSTREAM" {
		return 0, nil
	}
	fields := strings.Fields(value)
	if len(fields) != 2 {
		return 0, fmt.Errorf("unsupported TARGET_LAG %q", value)
	}
	amount, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil || amount <= 0 {
		return 0, fmt.Errorf("invalid TARGET_LAG %q", value)
	}
	units := map[string]time.Duration{"SECOND": time.Second, "MINUTE": time.Minute, "HOUR": time.Hour, "DAY": 24 * time.Hour}
	unit, ok := units[strings.TrimSuffix(fields[1], "S")]
	if !ok || amount > int64((1<<63-1)/unit) {
		return 0, fmt.Errorf("unsupported TARGET_LAG %q", value)
	}
	lag := time.Duration(amount) * unit
	if lag < time.Minute {
		return 0, fmt.Errorf("TARGET_LAG must be at least 60 seconds")
	}
	return lag, nil
}
