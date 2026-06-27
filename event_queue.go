package main

type EventQueue struct {
	ch   chan func()
	done chan struct{}
}

func newEventQueue() *EventQueue {
	q := &EventQueue{ch: make(chan func(), 4096), done: make(chan struct{})}

	go q.loop()

	return q
}

func (q *EventQueue) loop() {
	for fn := range q.ch {
		fn()
	}

	close(q.done)
}

func (q *EventQueue) post(fn func()) {
	q.ch <- fn
}

func (q *EventQueue) close() {
	close(q.ch)
	<-q.done
}
