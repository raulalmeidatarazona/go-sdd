// Package rabbitmq is the event bus adapter. Topology:
//
//	oms.events      (topic)  <- relay publishes with routing key = event type
//	  └─ <queue>    (quorum, bound by each subscription, delivery-limit N)
//	oms.events.dlx  (direct) <- messages that exhausted their deliveries
//	  └─ <queue>.dlq
//
// The client is crash-only: if the connection drops, Closed() fires and the
// process exits so the orchestrator restarts it with a clean state.
package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	EventsExchange     = "oms.events"
	DeadLetterExchange = "oms.events.dlx"

	HeaderTenantID         = "x-tenant-id"
	HeaderAggregateID      = "x-aggregate-id"
	HeaderAggregateVersion = "x-aggregate-version"
)

var tracer = otel.Tracer("github.com/raulalmeidatarazona/go-sdd/internal/platform/rabbitmq")

type Client struct {
	conn   *amqp.Connection
	closed chan *amqp.Error
}

func Dial(url string) (*Client, error) {
	conn, err := amqp.DialConfig(url, amqp.Config{Heartbeat: 10 * time.Second, Properties: amqp.Table{"connection_name": "oms"}})
	if err != nil {
		return nil, fmt.Errorf("dial rabbitmq: %w", err)
	}
	c := &Client{conn: conn, closed: conn.NotifyClose(make(chan *amqp.Error, 1))}
	if err := c.declareExchanges(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return c, nil
}

// Closed fires when the broker connection is lost.
func (c *Client) Closed() <-chan *amqp.Error { return c.closed }

func (c *Client) Healthy() bool { return !c.conn.IsClosed() }

func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) declareExchanges() error {
	ch, err := c.conn.Channel()
	if err != nil {
		return err
	}
	defer func() { _ = ch.Close() }()
	if err := ch.ExchangeDeclare(EventsExchange, amqp.ExchangeTopic, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare %s: %w", EventsExchange, err)
	}
	if err := ch.ExchangeDeclare(DeadLetterExchange, amqp.ExchangeDirect, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare %s: %w", DeadLetterExchange, err)
	}
	return nil
}

// Message is a received or outgoing integration event.
type Message struct {
	ID               string
	Type             string
	TenantID         string
	AggregateID      string
	AggregateVersion int64
	Body             []byte
	Timestamp        time.Time
	// TraceHeaders carries W3C trace context (traceparent, tracestate).
	TraceHeaders map[string]string
}

// Publisher publishes with publisher confirms: Publish returns only after the
// broker has taken responsibility for the message.
type Publisher struct {
	mu sync.Mutex
	ch *amqp.Channel
}

func (c *Client) NewPublisher() (*Publisher, error) {
	ch, err := c.conn.Channel()
	if err != nil {
		return nil, err
	}
	if err := ch.Confirm(false); err != nil {
		return nil, fmt.Errorf("enable confirms: %w", err)
	}
	return &Publisher{ch: ch}, nil
}

func (p *Publisher) Publish(ctx context.Context, m Message) error {
	headers := amqp.Table{
		HeaderTenantID:         m.TenantID,
		HeaderAggregateID:      m.AggregateID,
		HeaderAggregateVersion: m.AggregateVersion,
	}
	for k, v := range m.TraceHeaders {
		headers[k] = v
	}
	p.mu.Lock()
	confirmation, err := p.ch.PublishWithDeferredConfirmWithContext(ctx, EventsExchange, m.Type, false, false, amqp.Publishing{
		MessageId:    m.ID,
		Type:         m.Type,
		ContentType:  "application/x-protobuf",
		DeliveryMode: amqp.Persistent,
		Timestamp:    m.Timestamp,
		Headers:      headers,
		Body:         m.Body,
	})
	p.mu.Unlock()
	if err != nil {
		return fmt.Errorf("publish %s: %w", m.Type, err)
	}
	acked, err := confirmation.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("await confirm %s: %w", m.Type, err)
	}
	if !acked {
		return fmt.Errorf("broker nacked %s", m.ID)
	}
	return nil
}

// Subscription binds a quorum queue to event types and handles its messages.
type Subscription struct {
	Queue string
	// RoutingKeys are topic patterns, e.g. "oms.order.v1.*".
	RoutingKeys []string
	// DeliveryLimit is how many times a failing message is redelivered before
	// it is dead-lettered to <Queue>.dlq.
	DeliveryLimit int
	Handle        func(ctx context.Context, m Message) error
}

// Consume declares the subscription topology and processes messages until ctx
// ends or the channel closes. Handlers run sequentially to preserve order.
func (c *Client) Consume(ctx context.Context, logger *slog.Logger, sub Subscription, prefetch int) error {
	ch, err := c.conn.Channel()
	if err != nil {
		return err
	}
	defer func() { _ = ch.Close() }()
	if err := declareQueue(ch, sub); err != nil {
		return err
	}
	if err := ch.Qos(prefetch, 0, false); err != nil {
		return err
	}
	deliveries, err := ch.ConsumeWithContext(ctx, sub.Queue, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume %s: %w", sub.Queue, err)
	}
	logger.Info("consumer started", slog.String("queue", sub.Queue))
	for {
		select {
		case <-ctx.Done():
			return nil
		case d, ok := <-deliveries:
			if !ok {
				if ctx.Err() != nil {
					return nil
				}
				return errors.New("delivery channel closed for " + sub.Queue)
			}
			handleDelivery(ctx, logger, sub, d)
		}
	}
}

func handleDelivery(ctx context.Context, logger *slog.Logger, sub Subscription, d amqp.Delivery) {
	m := toMessage(d)
	ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(m.TraceHeaders))
	ctx, span := tracer.Start(ctx, sub.Queue+" process", trace.WithSpanKind(trace.SpanKindConsumer), trace.WithAttributes(
		attribute.String("messaging.system", "rabbitmq"),
		attribute.String("messaging.destination.name", sub.Queue),
		attribute.String("messaging.message.id", m.ID),
		attribute.String("oms.event_type", m.Type),
		attribute.String("oms.tenant_id", m.TenantID),
	))
	defer span.End()

	if err := sub.Handle(ctx, m); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		logger.ErrorContext(ctx, "message handling failed; requeued until delivery limit",
			slog.String("queue", sub.Queue), slog.String("message_id", m.ID), slog.String("type", m.Type), slog.Any("error", err))
		if nackErr := d.Nack(false, true); nackErr != nil {
			logger.ErrorContext(ctx, "nack failed", slog.Any("error", nackErr))
		}
		return
	}
	if err := d.Ack(false); err != nil {
		logger.ErrorContext(ctx, "ack failed", slog.Any("error", err))
	}
}

func declareQueue(ch *amqp.Channel, sub Subscription) error {
	dlq := sub.Queue + ".dlq"
	if _, err := ch.QueueDeclare(dlq, true, false, false, false, amqp.Table{"x-queue-type": "quorum"}); err != nil {
		return fmt.Errorf("declare %s: %w", dlq, err)
	}
	if err := ch.QueueBind(dlq, dlq, DeadLetterExchange, false, nil); err != nil {
		return err
	}
	limit := sub.DeliveryLimit
	if limit <= 0 {
		limit = 5
	}
	if _, err := ch.QueueDeclare(sub.Queue, true, false, false, false, amqp.Table{
		"x-queue-type":              "quorum",
		"x-delivery-limit":          limit,
		"x-dead-letter-exchange":    DeadLetterExchange,
		"x-dead-letter-routing-key": dlq,
	}); err != nil {
		return fmt.Errorf("declare %s: %w", sub.Queue, err)
	}
	for _, key := range sub.RoutingKeys {
		if err := ch.QueueBind(sub.Queue, key, EventsExchange, false, nil); err != nil {
			return fmt.Errorf("bind %s to %s: %w", sub.Queue, key, err)
		}
	}
	return nil
}

func toMessage(d amqp.Delivery) Message {
	m := Message{ID: d.MessageId, Type: d.Type, Body: d.Body, Timestamp: d.Timestamp, TraceHeaders: map[string]string{}}
	for k, v := range d.Headers {
		switch k {
		case HeaderTenantID:
			m.TenantID, _ = v.(string)
		case HeaderAggregateID:
			m.AggregateID, _ = v.(string)
		case HeaderAggregateVersion:
			switch n := v.(type) {
			case int64:
				m.AggregateVersion = n
			case int32:
				m.AggregateVersion = int64(n)
			}
		default:
			if s, ok := v.(string); ok {
				m.TraceHeaders[k] = s
			}
		}
	}
	return m
}
