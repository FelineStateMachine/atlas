package workbench

import (
	"context"
	"sync"
	"time"

	"github.com/FelineStateMachine/atlas/internal/workbench/oprunner"
)

// supervisedRun is the durable in-memory view of the latest build. The
// subprocess belongs to the workbench, not to the HTTP request that started it,
// so navigating away and returning never cancels or loses its progress.
type supervisedRun struct {
	Name       string
	Command    string
	StartedAt  time.Time
	FinishedAt time.Time
	Running    bool
	Failed     bool
	Rows       []oprunner.Row
	Artifact   string
}

type runSupervisor struct {
	runner *oprunner.Runner
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu   sync.Mutex
	last supervisedRun
}

func newRunSupervisor(runner *oprunner.Runner) *runSupervisor {
	ctx, cancel := context.WithCancel(context.Background())
	return &runSupervisor{runner: runner, ctx: ctx, cancel: cancel}
}

func (s *runSupervisor) Start(op oprunner.Operation) error {
	if err := op.Validate(); err != nil {
		return err
	}
	release, ok := s.runner.Acquire(op.Name)
	if !ok {
		return oprunner.ErrBusy
	}
	s.mu.Lock()
	s.last = supervisedRun{
		Name: op.Name, Command: op.Command(), StartedAt: time.Now().UTC(), Running: true,
	}
	s.mu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer release()
		err := oprunner.Stream(s.ctx, op, func(row oprunner.Row) error {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.last.Rows = append(s.last.Rows, row)
			if row.Failed {
				s.last.Failed = true
			}
			for _, attr := range row.Attrs {
				if attr.Key == "artifact" {
					s.last.Artifact = attr.Value
				}
			}
			return nil
		})
		s.mu.Lock()
		s.last.Running = false
		s.last.FinishedAt = time.Now().UTC()
		if err != nil {
			s.last.Failed = true
			s.last.Rows = append(s.last.Rows, oprunner.Row{
				Seq: len(s.last.Rows) + 1, Kind: oprunner.KindResult,
				Message: err.Error(), Failed: true,
			})
		}
		s.mu.Unlock()
	}()
	return nil
}

func (s *runSupervisor) Close() {
	s.cancel()
	s.wg.Wait()
}

func (s *runSupervisor) Snapshot() supervisedRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	held := s.last
	held.Rows = append([]oprunner.Row(nil), held.Rows...)
	return held
}
