package agent

// A generic listener construct.
// This works around race conditions that can occur when sending notifications
// to channels that are no longer being serviced.
type Watcher struct {
	changes chan<- interface{}
	done    chan struct{}
}

func NewWatcher(changes chan<- interface{}) *Watcher {
	return &Watcher{
		changes: changes,
		done:    make(chan struct{}, 1),
	}
}

// Called when the listener will no longer be processing the changes channel.
func (w *Watcher) Done() {
	select {
	case w.done <- struct{}{}:
	default:
		// done request already sent
	}
}

// Sends a notification to this listener.
// This will block until the listener processes the message or indicates
// it is no longer processing messages via Done(). If the listener is no
// longer processing messages, the notifier *must not* make any additional
// calls to this instance.
func (w *Watcher) Notify(change interface{}) bool {
	// don't bother adding the change notification if the listener is done
	select {
	case <-w.done:
		return false
	default:
		// listener is not done yet
	}
	select {
	case w.changes <- change:
		return true
	case <-w.done:
		// break out of the notify request if the listener has marked itself as done,
		// in case it does this while the changes channel is full
		return false
	}
}
