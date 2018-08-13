package nats

import (
	"context"
	"errors"
	"fmt"
	"strings"

	stan "github.com/nats-io/go-nats-streaming"
	"github.com/nulloop/choo"
)

type NatsReceiver struct {
	provier     *NatsProvider
	path        string
	opts        *NatsOptions
	middlewares []func(choo.Handler) choo.Handler
}

var _ choo.Receiver = &NatsReceiver{}

func checkPath(path string) error {
	if path == "" {
		return errors.New("path can't be empty string")
	}

	if strings.Index(path, ".") == 0 {
		return errors.New("path should not start with '.'")
	}

	if strings.LastIndex(path, ".") == len(path)-1 {
		return errors.New("path should not end with '.'")
	}

	return nil
}

func mergePath(base, path string) string {
	err := checkPath(base)
	if err != nil {
		panic(err)
	}

	err = checkPath(path)
	if err != nil {
		panic(err)
	}

	if base != "" {
		path = fmt.Sprintf("%s.%s", base, path)
	}

	return path
}

func (n *NatsReceiver) Use(middlewares ...func(choo.Handler) choo.Handler) {
	n.middlewares = append(n.middlewares, middlewares...)
}

func (n *NatsReceiver) Route(path string, fn func(choo.Receiver)) choo.Receiver {
	path = mergePath(n.path, path)

	// need to copy middle ware from parent
	// middlewares should be stateless
	middlewares := make([]func(choo.Handler) choo.Handler, 0)
	middlewares = append(middlewares, n.middlewares...)

	receiver := &NatsReceiver{
		path:        path,
		provier:     n.provier,
		opts:        n.opts,
		middlewares: middlewares,
	}

	fn(receiver)

	return receiver
}

func (n *NatsReceiver) Handle(subject string, h choo.HandlerFunc) {
	path := mergePath(n.path, subject)

	options := []stan.SubscriptionOption{
		stan.SetManualAckMode(),
	}

	if n.opts.getSequence != nil {
		options = append(options, stan.StartAtSequence(n.opts.getSequence(path)))
	}

	if n.opts.genDurableName != nil {
		durableName := n.opts.genDurableName(path)
		if durableName != "" {
			stan.DurableName(path)
		}
	}

	// Here's the final handler is built up based on
	// layers of layers of middleware
	var handler choo.Handler = h
	for _, middleware := range n.middlewares {
		handler = middleware(handler)
	}

	n.provier.conn.Subscribe(path, func(msg *stan.Msg) {
		message := &NatsMessage{}

		// this will decode id and message as bytes
		err := message.decode(msg.Data)
		if err != nil {
			panic(err)
		}

		message.sequence = msg.Sequence
		message.subject = msg.Subject
		message.ctx = context.Background()
		message.timestamp = msg.Timestamp

		err = handler.ServeMessage(message)

		if err == nil {
			err = msg.Ack()
		}

		if err != nil && n.opts.logError != nil {
			n.opts.logError(err)
		}

		if err == nil {
			if n.opts.updateSequence != nil {
				n.opts.updateSequence(path, msg.Sequence)
			}
		}

	}, options...)
}

func (n *NatsReceiver) HandleQueue(subject string, queueName string, h choo.HandlerFunc) {

	path := mergePath(n.path, subject)

	options := []stan.SubscriptionOption{
		stan.SetManualAckMode(),
	}

	if n.opts.getSequence != nil {
		options = append(options, stan.StartAtSequence(n.opts.getSequence(path)))
	}

	if n.opts.genDurableName != nil {
		durableName := n.opts.genDurableName(path)
		if durableName != "" {
			stan.DurableName(path)
		}
	}

	if queueName == "" {
		panic("queue name is empy")
	}

	// Here's the final handler is built up based on
	// layers of layers of middleware
	var handler choo.Handler = h
	for _, middleware := range n.middlewares {
		handler = middleware(handler)
	}

	n.provier.conn.QueueSubscribe(path, queueName, func(msg *stan.Msg) {
		message := &NatsMessage{}

		// this will decode id and message as bytes
		err := message.decode(msg.Data)
		if err != nil {
			panic(err)
		}

		message.sequence = msg.Sequence
		message.subject = msg.Subject
		message.ctx = context.Background()
		message.timestamp = msg.Timestamp

		err = handler.ServeMessage(message)
		if err == nil {
			err = msg.Ack()
		}

		if err != nil && n.opts.logError != nil {
			n.opts.logError(err)
		}

		if err == nil {
			if n.opts.updateSequence != nil {
				n.opts.updateSequence(path, msg.Sequence)
			}
		}

	}, options...)
}