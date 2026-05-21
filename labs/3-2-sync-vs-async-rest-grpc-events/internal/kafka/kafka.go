// Package kafka is a thin wrapper around franz-go that the producer
// and consumer share. It centralises broker config, retries, and the
// event envelope so handler code stays focused on flow control + DB.
package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/twmb/franz-go/pkg/kgo"
)

// Event is the JSON envelope all events use. event_id is the dedupe
// key; order_id is the partition key. amount and emitted_at are
// pass-throughs for the consumer's audit row.
type Event struct {
	EventID   string  `json:"event_id"`
	OrderID   string  `json:"order_id"`
	Amount    int64   `json:"amount"`
	EmittedAt int64   `json:"emitted_at_ns"`
	Source    string  `json:"source,omitempty"`
}

// MarshalJSON encodes an event; isolated so callers don't need to know
// the wire format.
func (e *Event) MarshalJSON() ([]byte, error) {
	type alias Event
	return json.Marshal(struct {
		*alias
	}{alias: (*alias)(e)})
}

// NewProducer builds a franz-go client suitable for the lab's small
// throughputs. Idempotence is on; acks=all; batching is enabled.
func NewProducer(brokers, clientID string) (*kgo.Client, error) {
	opts := []kgo.Opt{
		kgo.SeedBrokers(splitBrokers(brokers)...),
		kgo.ClientID(clientID),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.RequestTimeoutOverhead(0),
		kgo.ProducerBatchMaxBytes(1 << 20),
	}
	return kgo.NewClient(opts...)
}

// NewConsumer builds a consumer for the given group + topic. start
// controls whether a fresh group starts from beginning (true) or end
// (false). The lab uses start=true for the replay scenarios.
func NewConsumer(brokers, group, topic string, fromBeginning bool) (*kgo.Client, error) {
	resetOffset := kgo.NewOffset().AtEnd()
	if fromBeginning {
		resetOffset = kgo.NewOffset().AtStart()
	}
	opts := []kgo.Opt{
		kgo.SeedBrokers(splitBrokers(brokers)...),
		kgo.ConsumerGroup(group),
		kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(resetOffset),
		kgo.DisableAutoCommit(),
	}
	return kgo.NewClient(opts...)
}

// Produce a single event with order_id as the partition key.
func Produce(ctx context.Context, cl *kgo.Client, topic string, ev *Event) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	rec := &kgo.Record{
		Topic: topic,
		Key:   []byte(ev.OrderID),
		Value: payload,
	}
	if res := cl.ProduceSync(ctx, rec); len(res) > 0 {
		return res[0].Err
	}
	return nil
}

func splitBrokers(brokers string) []string {
	out := []string{}
	for _, p := range strings.Split(brokers, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		out = []string{"redpanda:29092"}
	}
	return out
}
