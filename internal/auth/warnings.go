package auth

import "sync"

// WarningSink receives token persistence failures outside the OAuth request.
type WarningSink func(error)

// Warnings retains the latest warning, including warnings emitted before UI startup.
// The channel is never closed; consumers should stop using their own context.
func (o *OAuthFlow) Warnings() <-chan error {
	return o.warnings.channel()
}

// LatestWarning retains a persistence reminder even after a UI command consumes
// the channel. Applications can display it after restoring the terminal, so a
// warning consumed concurrently with UI shutdown is not lost.
func (o *OAuthFlow) LatestWarning() error {
	o.warnings.mu.Lock()
	defer o.warnings.mu.Unlock()
	return o.warnings.latest
}

// SetWarningSink configures an optional observer. At most one callback runs at a
// time with one pending warning; a blocked callback cannot block token refresh.
// Callbacks must return for their worker to finish. The application uses Warnings
// directly, which creates no worker goroutines.
func (o *OAuthFlow) SetWarningSink(sink WarningSink) {
	o.warnings.mu.Lock()
	defer o.warnings.mu.Unlock()
	if sink == nil {
		o.warnings.sink = nil
		return
	}
	o.warnings.sink = &asyncWarningSink{callback: sink}
}

type warningQueue struct {
	mu     sync.Mutex
	ch     chan error
	sink   *asyncWarningSink
	latest error
}

func (q *warningQueue) channel() <-chan error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.ch == nil {
		q.ch = make(chan error, 1)
	}
	return q.ch
}

func (q *warningQueue) publish(err error) {
	if err == nil {
		return
	}
	q.mu.Lock()
	q.latest = err
	if q.ch == nil {
		q.ch = make(chan error, 1)
	}
	select {
	case <-q.ch:
	default:
	}
	q.ch <- err
	sink := q.sink
	q.mu.Unlock()
	if sink != nil {
		sink.publish(err)
	}
}

type asyncWarningSink struct {
	mu       sync.Mutex
	callback WarningSink
	running  bool
	latest   error
}

func (s *asyncWarningSink) publish(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latest = err
	if s.running {
		return
	}
	s.running = true
	go s.run()
}

func (s *asyncWarningSink) run() {
	for {
		s.mu.Lock()
		err := s.latest
		s.latest = nil
		if err == nil {
			s.running = false
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
		s.callback(err)
	}
}
