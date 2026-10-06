// Package ddd holds the tactical DDD building blocks shared by every bounded context.
package ddd

// Event is a fact raised by an aggregate. EventName is the fully qualified
// integration event name, e.g. "oms.order.v1.OrderPlaced".
type Event interface {
	EventName() string
}

// AggregateRoot tracks the persisted version and the events raised since the
// aggregate was loaded. Embed it in every aggregate.
type AggregateRoot struct {
	version int64
	events  []Event
}

// Version is the version stored in the database (0 for a new aggregate).
func (a *AggregateRoot) Version() int64 { return a.version }

// Record appends an event. Call it only after the state change succeeded.
func (a *AggregateRoot) Record(event Event) { a.events = append(a.events, event) }

// Events returns the events raised since load, without clearing them.
func (a *AggregateRoot) Events() []Event { return a.events }

// MarkPersisted is called by repositories after a successful save.
func (a *AggregateRoot) MarkPersisted(version int64) {
	a.version = version
	a.events = nil
}
