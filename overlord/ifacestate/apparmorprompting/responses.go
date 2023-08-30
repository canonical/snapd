package apparmorprompting

import (
	"sync"

	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/accessrules"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/promptrequests"
)

type FollowRequestsSeqResponseWriter struct {
	requestsCh      chan *promptrequests.PromptRequest
	stoppingCh      chan struct{} // Should only ever be closed by Stop().
	stoppingChMutex sync.Mutex    // Prevent stoppingCh from being closed twice.
	writeWG         sync.WaitGroup
	writeWGMutex    sync.Mutex
}

func newFollowRequestsSeqResponseWriter(requestsCh chan *promptrequests.PromptRequest) *FollowRequestsSeqResponseWriter {
	rw := &FollowRequestsSeqResponseWriter{
		requestsCh: requestsCh,
		stoppingCh: make(chan struct{}),
	}
	return rw
}

// WriteRequest writes the given prompt request to the response data channel.
// If the response writer has already been stopped, do not write anything.
// Returns true if the write was completed successfully, else false.
func (rw *FollowRequestsSeqResponseWriter) WriteRequest(req *promptrequests.PromptRequest) bool {
	rw.writeWGMutex.Lock()
	rw.writeWG.Add(1)
	rw.writeWGMutex.Unlock()
	defer rw.writeWG.Done()

	// If rw.stoppingCh has already been closed, return immediately.
	// Don't try to write to rw.requestsCh, because if rw.stoppingCh was
	// closed before this function began, then rw.requestsCh may already be
	// closed, and writing to it would result in a panic.
	select {
	case <-rw.stoppingCh:
		return false
	default:
	}

	// Once here, we know that rw.stoppingCh was not closed at the start of
	// this function call. Thus, it is safe to write to rw.requestsCh,
	// since we called rw.writeWG.Add(1), and Stop() cannot close before
	// rw.writeWG.Wait() returns. Attempt to write to rw.requestsCh.
	// If rw.stoppingCh closes while blocked on the write, return.
	select {
	case <-rw.stoppingCh:
		return false
	case rw.requestsCh <- req:
		return true
	}
}

// Close stoppingCh for the given FollowRequestsSeqResponseWriter if it has not
// yet been closed. Returns true if it was already closed, else returns false.
func (rw *FollowRequestsSeqResponseWriter) closeStoppingChAlready() bool {
	rw.stoppingChMutex.Lock()
	defer rw.stoppingChMutex.Unlock()
	select {
	case <-rw.stoppingCh:
		return true
	default:
	}
	close(rw.stoppingCh)
	return false
}

// Stop tells the writer to close rw.stoppingCh, and thus to stop accepting
// new writes. Stop() should be the only place rw.stoppingCh can be closed.
// If rw.stoppingCh has already been closed, this indicates Stop() has already
// been called, so return immediately.
func (rw *FollowRequestsSeqResponseWriter) Stop() {
	if rw.closeStoppingChAlready() {
		return
	}

	// XXX: what if current goroutine terminates before closing rw.requestsCh?
	// Will this spawned goroutine never exit?
	go func() {
		for range rw.requestsCh {
			// Consume entries until rw.requestsCh closes
		}
	}()

	rw.writeWGMutex.Lock()
	rw.writeWG.Wait()
	rw.writeWGMutex.Unlock()

	close(rw.requestsCh)
}

// Stopping returns a channel on which reads block until Stop() has been called.
func (rw *FollowRequestsSeqResponseWriter) Stopping() chan struct{} {
	return rw.stoppingCh
}

type FollowRulesSeqResponseWriter struct {
	rulesCh         chan *accessrules.AccessRule
	stoppingCh      chan struct{} // Should only ever be closed by Stop().
	stoppingChMutex sync.Mutex    // Prevent stoppingCh from being closed twice.
	writeWG         sync.WaitGroup
	writeWGMutex    sync.Mutex
}

func newFollowRulesSeqResponseWriter(rulesCh chan *accessrules.AccessRule) *FollowRulesSeqResponseWriter {
	rw := &FollowRulesSeqResponseWriter{
		rulesCh:    rulesCh,
		stoppingCh: make(chan struct{}),
	}
	return rw
}

// WriteRequest writes the given access rule to the response data channel.
// If the response writer has already been stopped, do not write anything.
// Returns true if the write was completed successfully, else false.
func (rw *FollowRulesSeqResponseWriter) WriteRule(rule *accessrules.AccessRule) bool {
	rw.writeWGMutex.Lock()
	rw.writeWG.Add(1)
	rw.writeWGMutex.Unlock()
	defer rw.writeWG.Done()

	// If rw.stoppingCh has already been closed, return immediately.
	// Don't try to write to rw.rulesCh, because if rw.stoppingCh was
	// closed before this function began, then rw.rulesCh may already be
	// closed, and writing to it would result in a panic.
	select {
	case <-rw.stoppingCh:
		return false
	default:
	}

	// Once here, we know that rw.stoppingCh was not closed at the start of
	// this function call. Thus, it is safe to write to rw.rulesCh,
	// since we called rw.writeWG.Add(1), and Stop() cannot close before
	// rw.writeWG.Wait() returns. Attempt to write to rw.rulesCh.
	// If rw.stoppingCh closes while blocked on the write, return.
	select {
	case <-rw.stoppingCh:
		return false
	case rw.rulesCh <- rule:
		return true
	}
}

// Close stoppingCh for the given FollowRulesSeqResponseWriter if it has not
// yet been closed. Returns true if it was already closed, else returns false.
func (rw *FollowRulesSeqResponseWriter) closeStoppingChAlready() bool {
	rw.stoppingChMutex.Lock()
	defer rw.stoppingChMutex.Unlock()
	select {
	case <-rw.stoppingCh:
		return true
	default:
	}
	close(rw.stoppingCh)
	return false
}

// Stop tells the writer to close rw.stoppingCh, and thus to stop accepting
// new writes. Stop() should be the only place rw.stoppingCh can be closed.
// If rw.stoppingCh has already been closed, this indicates Stop() has already
// been called, so return immediately.
func (rw *FollowRulesSeqResponseWriter) Stop() {
	if rw.closeStoppingChAlready() {
		return
	}

	// XXX: what if current goroutine terminates before closing rw.rulesCh?
	// Will this spawned goroutine never exit?
	go func() {
		for range rw.rulesCh {
			// Consume entries until rw.rulesCh closes
		}
	}()

	rw.writeWGMutex.Lock()
	rw.writeWG.Wait()
	rw.writeWGMutex.Unlock()

	close(rw.rulesCh)
}

// Stopping returns a channel on which reads block until Stop() has been called.
func (rw *FollowRulesSeqResponseWriter) Stopping() chan struct{} {
	return rw.stoppingCh
}
