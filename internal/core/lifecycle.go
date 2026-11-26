package core

import "context"

// Close cancels and joins shared provider work before its owner (CLI/MCP/REST)
// exits. Individual callers still cancel independently without poisoning peers.
func (s *Service) Close() {
	s.workMu.Lock()
	s.workClosed = true
	if s.workCancel != nil {
		s.workCancel()
	}
	s.workMu.Unlock()
	s.work.Wait()
}

func (s *Service) sharedContext(caller context.Context) (context.Context, func()) {
	s.workMu.Lock()
	defer s.workMu.Unlock()
	ctx, cancel := context.WithCancel(context.WithoutCancel(caller))
	if s.workClosed {
		cancel()
		return ctx, func() {}
	}
	if s.workContext == nil {
		s.workContext, s.workCancel = context.WithCancel(context.Background())
	}
	s.work.Add(1)
	stop := context.AfterFunc(s.workContext, cancel)
	return ctx, func() { stop(); cancel(); s.work.Done() }
}
