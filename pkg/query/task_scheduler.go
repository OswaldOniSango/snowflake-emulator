package query

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
)

// TaskScheduler periodically executes due tasks in STARTED state.
type TaskScheduler struct {
	repo         *metadata.Repository
	processor    *TaskProcessor
	pollInterval time.Duration
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	mu           sync.Mutex
}

func NewTaskScheduler(repo *metadata.Repository, executor *Executor, pollInterval time.Duration) *TaskScheduler {
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	return &TaskScheduler{repo: repo, processor: executor.taskProcessor, pollInterval: pollInterval}
}

func (s *TaskScheduler) Start(parent context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.run(ctx)
	}()
}

func (s *TaskScheduler) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		s.wg.Wait()
	}
}

func (s *TaskScheduler) run(ctx context.Context) {
	if err := s.RunDueTasks(ctx, time.Now()); err != nil {
		log.Printf("task scheduler error: %v", err)
	}
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := s.RunDueTasks(ctx, now); err != nil {
				log.Printf("task scheduler error: %v", err)
			}
		}
	}
}

// RunDueTasks performs one synchronous scheduler pass.
func (s *TaskScheduler) RunDueTasks(ctx context.Context, now time.Time) error {
	tasks, err := s.repo.ListTasks(ctx, "")
	if err != nil {
		return err
	}
	var executionErrors []string
	for _, task := range tasks {
		if task.State != "STARTED" {
			continue
		}
		interval, err := parseTaskSchedule(task.Schedule)
		if err != nil {
			executionErrors = append(executionErrors, fmt.Sprintf("task %s: %v", task.Name, err))
			continue
		}
		lastRun := task.CreatedAt
		if task.LastExecutedAt != nil {
			lastRun = *task.LastExecutedAt
		}
		if now.Before(lastRun.Add(interval)) {
			continue
		}
		if _, err := s.processor.executeStoredTask(ctx, task, ExecutionContext{}); err != nil {
			executionErrors = append(executionErrors, err.Error())
		}
	}
	if len(executionErrors) > 0 {
		return fmt.Errorf("scheduled task failures: %s", strings.Join(executionErrors, "; "))
	}
	return nil
}

func parseTaskSchedule(schedule string) (time.Duration, error) {
	fields := strings.Fields(strings.ToUpper(strings.TrimSpace(schedule)))
	if len(fields) != 2 {
		return 0, fmt.Errorf("unsupported schedule %q: expected '<number> SECOND|MINUTE|HOUR'", schedule)
	}
	amount, err := strconv.Atoi(fields[0])
	if err != nil || amount <= 0 {
		return 0, fmt.Errorf("invalid schedule amount %q", fields[0])
	}
	var unit time.Duration
	switch strings.TrimSuffix(fields[1], "S") {
	case "SECOND":
		unit = time.Second
	case "MINUTE":
		unit = time.Minute
	case "HOUR":
		unit = time.Hour
	default:
		return 0, fmt.Errorf("unsupported schedule unit %q", fields[1])
	}
	return time.Duration(amount) * unit, nil
}
