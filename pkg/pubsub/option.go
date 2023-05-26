package pubsub

import "go.uber.org/zap"

type Option func(pubsub *PubSub)

func WithAddress(address string) Option {
	return func(pubsub *PubSub) {
		pubsub.address = address
	}
}

func WithLogger(logger *zap.Logger) Option {
	return func(pubsub *PubSub) {
		pubsub.logger = logger.Sugar()
	}
}
