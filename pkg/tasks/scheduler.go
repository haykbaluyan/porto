// Package tasks provides an in-process scheduler for periodic tasks
// that uses the builder pattern for configuration.
// Schedule lets you run Golang functions periodically
// at pre-determined intervals using a simple, human-friendly syntax.
package tasks

import (
	"sort"
	"sync"
	"time"

	"github.com/effective-security/xlog"
	"github.com/pkg/errors"
)

var logger = xlog.NewPackageLogger("github.com/effective-security/porto/pkg", "tasks")

// DefaultTickerInterval for scheduler
const DefaultTickerInterval = time.Second

// Time location, default set by the time.Local (*time.Location)
var loc = time.Local

// SetGlobalLocation the time location for the package
func SetGlobalLocation(newLocation *time.Location) {
	loc = newLocation
}

// Scheduler defines the scheduler interface
type Scheduler interface {
	// Add adds a task to a pool of scheduled tasks
	Add(Task) Scheduler
	// Clear will delete all scheduled tasks
	Clear()
	// Count returns the number of registered tasks
	Count() int
	// IsRunning return the status
	IsRunning() bool
	// Start all the pending tasks
	Start() error
	// Stop the scheduler
	Stop() error
}

// scheduler provides a task scheduler functionality
type scheduler struct {
	dops options

	tasks   []Task
	running bool
	quit    chan bool
	lock    sync.RWMutex
}

// Scheduler implements the sort.Interface{} for sorting tasks, by the time nextRun
// The Len, Swap, Less are needed for the sort.Interface{}

// Len returns the lengths of tasks array for sorting interface
func (s *scheduler) Len() int {
	return len(s.tasks)
}

// Swap provides swap method for sorting interface
func (s *scheduler) Swap(i, j int) {
	s.tasks[i], s.tasks[j] = s.tasks[j], s.tasks[i]
}

// Less provides less-comparisson method for sorting interface
func (s *scheduler) Less(i, j int) bool {
	return s.tasks[j].NextScheduledTime().After(s.tasks[i].NextScheduledTime())
}

// NewScheduler creates a new scheduler
func NewScheduler(ops ...Option) Scheduler {
	s := &scheduler{
		tasks:   []Task{},
		running: false,
		quit:    make(chan bool, 1),
	}

	for _, op := range ops {
		op.apply(&s.dops)
	}

	return s
}

// Count returns the number of registered tasks
func (s *scheduler) Count() int {
	return len(s.tasks)
}

// Get the current runnable tasks, which shouldRun is True
func (s *scheduler) getRunnableTasks() []Task {
	s.lock.Lock()
	defer s.lock.Unlock()

	runnable := []Task{}
	sort.Sort(s)
	for _, j := range s.tasks {
		if j.ShouldRun() {
			runnable = append(runnable, j)
		} else {
			break
		}
	}
	return runnable
}

// Get the current runnable tasks, which shouldRun is True
func (s *scheduler) getAllTasks() []Task {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.tasks[:]
}

// Add adds a task to a pool of scheduled tasks
func (s *scheduler) Add(j Task) Scheduler {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.tasks = append(s.tasks, j)
	return s
}

// runPending will run all the tasks that are scheduled to run.
func (s *scheduler) runPending() {
	for _, task := range s.getRunnableTasks() {
		logger.KV(xlog.DEBUG, "status", "pending_run", "task", task.Name())
		go task.Run()
	}
}

// Clear will delete all scheduled tasks
func (s *scheduler) Clear() {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.tasks = []Task{}
}

// IsRunning return the status
func (s *scheduler) IsRunning() bool {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.running
}

// Start all the pending tasks,
// and create a second ticker
func (s *scheduler) Start() error {
	s.lock.Lock()
	defer s.lock.Unlock()
	if s.running {
		return errors.Errorf("schedule already started")
	}
	s.running = true

	interval := s.dops.tickerInterval
	if interval == 0 {
		// if not specified, then find a reasonable interval to schedule
		interval = DefaultTickerInterval
		for _, t := range s.tasks {
			in := t.Duration()
			if in < interval {
				interval = in / 10 // use 1/10 of a task schedule interval
			}
		}
	}

	if interval == 0 {
		interval = DefaultTickerInterval
	}

	logger.KV(xlog.DEBUG,
		"tasks", s.Count(),
		"schedule_interval", interval,
	)

	ticker := time.NewTicker(interval)
	go func() {
		for {
			select {
			case <-ticker.C:
				s.runPending()
			case <-s.quit:
				ticker.Stop()
				return
			}
		}
	}()

	return nil
}

// Stop the scheduler
func (s *scheduler) Stop() error {
	s.lock.Lock()
	defer s.lock.Unlock()
	if !s.running {
		return errors.Errorf("the scheduler is not running")
	}

	s.quit <- true

	return nil
}

// Option configures how we set up the client
type Option interface {
	apply(*options)
}

type options struct {
	tickerInterval time.Duration
}

type funcOption struct {
	f func(*options)
}

func (fo *funcOption) apply(o *options) {
	fo.f(o)
}

func newFuncOption(f func(*options)) *funcOption {
	return &funcOption{
		f: f,
	}
}

// WithTickerInterval option to provide ticker interval
func WithTickerInterval(tickerInterval time.Duration) Option {
	return newFuncOption(func(o *options) {
		o.tickerInterval = tickerInterval
	})
}
