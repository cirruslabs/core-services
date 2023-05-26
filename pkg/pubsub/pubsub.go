package pubsub

import (
	"context"
	"errors"
	"fmt"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/cirruslabs/cirrus-backbone-services/pkg/pubsub/outstandingstore"
	"github.com/cirruslabs/cirrus-backbone-services/pkg/rpc"
	"github.com/samber/lo"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"net"
	"strconv"
	"time"
)

var (
	ErrFailedToParseMessageID = errors.New("failed to parse message ID")
	ErrNoMessageExistsWithID  = errors.New("no message exists with ID")
)

type InstantiateSubscriberFunc func(subscriberGroup string, ackDeadline time.Duration) (message.Subscriber, error)

type PubSub struct {
	address  string
	listener net.Listener

	publisher             message.Publisher
	instantiateSubscriber InstantiateSubscriberFunc

	logger *zap.SugaredLogger

	rpc.UnimplementedPubSubServer
}

func New(
	publisher message.Publisher,
	instantiateSubscriber InstantiateSubscriberFunc,
	opts ...Option,
) (*PubSub, error) {
	pubsub := &PubSub{
		address: "127.0.0.1:0",

		publisher:             publisher,
		instantiateSubscriber: instantiateSubscriber,

		logger: zap.NewNop().Sugar(),
	}

	// Apply options
	for _, opt := range opts {
		opt(pubsub)
	}

	listener, err := net.Listen("tcp", pubsub.address)
	if err != nil {
		return nil, err
	}
	pubsub.listener = listener

	return pubsub, nil
}

func (pubsub *PubSub) Run(ctx context.Context) error {
	server := grpc.NewServer()
	rpc.RegisterPubSubServer(server, pubsub)

	errCh := make(chan error)

	go func() {
		if err := server.Serve(pubsub.listener); err != nil {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		server.Stop()

		return ctx.Err()
	}
}

func (pubsub *PubSub) Endpoint() string {
	return pubsub.listener.Addr().String()
}

func (pubsub *PubSub) Publish(ctx context.Context, request *rpc.FromPublisher) (*rpc.ToPublisher, error) {
	logger := pubsub.logger

	// Convert messages from RPC format into Watermill's format
	messages := lo.Map(request.Messages, func(rpcMessage *rpc.Message, index int) *message.Message {
		return &message.Message{
			Payload: rpcMessage.Payload,
		}
	})

	logger.Debugf("publishing %d messages on topic %q...",
		len(messages), request.Topic)

	if err := pubsub.publisher.Publish(request.Topic, messages...); err != nil {
		logger.Warnf("failed to publish %d messages on topic %q: %v",
			len(messages), request.Topic, err)

		return nil, err
	}

	logger.Debugf("successfully published %d messages on topic %q",
		len(messages), request.Topic)

	return &rpc.ToPublisher{}, nil
}

func (pubsub *PubSub) Subscribe(stream rpc.PubSub_SubscribeServer) error {
	logger := pubsub.logger

	logger.Debugf("waiting for the SubscriptionRequest from the subscriber...")

	// The first message from the subscriber should always be a SubscriptionRequest
	fromSubscriber, err := stream.Recv()
	if err != nil {
		logger.Warnf("failed to receive the first message from the subscriber: %v", err)

		return err
	}
	fromSubscriberSubscriptionRequest, ok := fromSubscriber.Request.(*rpc.FromSubscriber_SubscriptionRequest_)
	if !ok {
		logger.Warnf("expected a SubscriptionRequest from the subscriber, got %T",
			fromSubscriber.Request)

		return status.Errorf(codes.FailedPrecondition, "the first message should always be a "+
			"SubscriptionRequest, got %T", fromSubscriber.Request)
	}
	subscriptionRequest := fromSubscriberSubscriptionRequest.SubscriptionRequest

	logger.Debugf("subscription request received, instantiating subscriber for topic %q, "+
		"with a subscriber group %q and an ACK/NACK deadline of %d seconds", subscriptionRequest.Topic,
		subscriptionRequest.SubscriberGroup, subscriptionRequest.AckDeadlineSeconds)

	ackDeadline := time.Duration(subscriptionRequest.AckDeadlineSeconds) * time.Second

	subscriber, err := pubsub.instantiateSubscriber(subscriptionRequest.SubscriberGroup, ackDeadline)
	if err != nil {
		return err
	}
	defer func() {
		_ = subscriber.Close()
	}()

	logger.Debugf("subscriber successfully instantiated, subscribing it to topic %q...",
		subscriptionRequest.Topic)

	messageCh, err := subscriber.Subscribe(stream.Context(), subscriptionRequest.Topic)
	if err != nil {
		return err
	}

	logger.Debugf("subscriber successfully subscribed to topic %q, sending confirmation...",
		subscriptionRequest.Topic)

	if err = stream.Send(&rpc.ToSubscriber{
		Response: &rpc.ToSubscriber_SubscriptionResponse_{
			SubscriptionResponse: &rpc.ToSubscriber_SubscriptionResponse{
				// empty for now
			},
		},
	}); err != nil {
		return err
	}

	logger.Debugf("confirmation has been sent to the subscriber, transferring messages...")

	fromSubscriberCh := make(chan *rpc.FromSubscriber)
	errFromSubscriberCh := make(chan error)

	go func() {
		for {
			fromSubscriber, err := stream.Recv()
			if err != nil {
				errFromSubscriberCh <- err

				break
			}

			fromSubscriberCh <- fromSubscriber
		}
	}()

	outstandingStore := outstandingstore.New()

	for {
		select {
		case message := <-messageCh:
			// ID to be ACK/NACK'ed
			outstandingMessageID := outstandingStore.Add(message)

			if err := stream.Send(&rpc.ToSubscriber{
				Response: &rpc.ToSubscriber_Message{
					Message: &rpc.Message{
						Id:      strconv.FormatUint(outstandingMessageID, 10),
						Payload: message.Payload,
					},
				},
			}); err != nil {
				logger.Warnf("failed to send message to the subscriber: %v", err)

				return err
			}
		case fromSubscriber := <-fromSubscriberCh:
			switch fromSubscriberTyped := fromSubscriber.Request.(type) {
			case *rpc.FromSubscriber_Ack_:
				if err := ackNackHelper(outstandingStore, fromSubscriberTyped.Ack.MessageId, func(message *message.Message) {
					message.Ack()
				}); err != nil {
					logger.Warnf("failed to ACK message: %v", err)
				}
			case *rpc.FromSubscriber_Nack_:
				if err := ackNackHelper(outstandingStore, fromSubscriberTyped.Nack.MessageId, func(message *message.Message) {
					message.Nack()
				}); err != nil {
					logger.Warnf("failed to NACK message: %v", err)
				}
			}
		case errFromSubscriber := <-errFromSubscriberCh:
			logger.Debugf("closing stream because we've received an error while reading stream %v",
				stream.Context().Err())

			return errFromSubscriber
		case <-stream.Context().Done():
			logger.Debugf("closing stream because the context is done with error %v",
				stream.Context().Err())

			return stream.Context().Err()
		}
	}
}

func (pubsub *PubSub) Close() error {
	return pubsub.publisher.Close()
}

func ackNackHelper(
	outstandingStore *outstandingstore.OutstandingStore,
	rawMessageID string,
	action func(message *message.Message),
) error {
	messageID, err := strconv.ParseUint(rawMessageID, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: failed to parse message ID: %v", ErrFailedToParseMessageID, err)
	}

	m := outstandingStore.GetAndForget(messageID)
	if m == nil {
		return fmt.Errorf("%w %d", ErrNoMessageExistsWithID, messageID)
	}

	action(m)

	return nil
}
