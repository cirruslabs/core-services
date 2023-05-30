package pubsub

import (
	gcppubsub "cloud.google.com/go/pubsub"
	"errors"
	"fmt"
	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-googlecloud/pkg/googlecloud"
	"github.com/ThreeDotsLabs/watermill-redisstream/pkg/redisstream"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/cirruslabs/core-services/pkg/pubsub"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"time"
)

var ErrRun = errors.New("failed to run Pub/Sub adapter")

var port uint16
var gcpPubsubProjectID string
var redisServers []string
var verbose bool

func newRunCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run publish/subscribe service adapter",
		RunE:  runPubsub,
	}

	cmd.PersistentFlags().Uint16VarP(&port, "port", "p", 8080,
		"port on which the gRPC server should listen on")
	cmd.PersistentFlags().StringVar(&gcpPubsubProjectID, "gcp-pubsub-project-id", "",
		"GCP project ID in which you'd want to use the Google Cloud Pub/Sub")
	cmd.PersistentFlags().StringArrayVar(&redisServers, "redis-server", []string{},
		"Redis server address (repeat the argument multiple times to specify multiple servers)")
	cmd.PersistentFlags().BoolVarP(&verbose, "v", "verbose", false,
		"produce more verbose output")

	return cmd
}

func runPubsub(cmd *cobra.Command, args []string) error {
	var logger *zap.Logger
	var err error

	if verbose {
		logger, err = zap.NewDevelopment()
	} else {
		logger, err = zap.NewProduction()
	}
	if err != nil {
		return err
	}

	var watermillLogger watermill.LoggerAdapter
	if verbose {
		watermillLogger = watermill.NewStdLogger(true, true)
	}

	var publisher message.Publisher
	var instantiateSubscriber pubsub.InstantiateSubscriberFunc

	switch {
	case gcpPubsubProjectID != "":
		// Google Cloud Pub/Sub publisher
		publisher, err = googlecloud.NewPublisher(googlecloud.PublisherConfig{
			ProjectID: gcpPubsubProjectID,
		}, watermillLogger)
		if err != nil {
			return err
		}

		// Google Cloud Pub/Sub subscriber function
		instantiateSubscriber = func(consumerGroup string, ackDeadline time.Duration) (message.Subscriber, error) {
			return googlecloud.NewSubscriber(googlecloud.SubscriberConfig{
				GenerateSubscriptionName: func(topic string) string {
					return consumerGroup
				},
				ProjectID: gcpPubsubProjectID,
				SubscriptionConfig: gcppubsub.SubscriptionConfig{
					AckDeadline: ackDeadline,
				},
			}, watermillLogger)
		}
	case len(redisServers) != 0:
		// Redis publisher
		client := redis.NewUniversalClient(&redis.UniversalOptions{
			Addrs: redisServers,
		})

		publisher, err = redisstream.NewPublisher(redisstream.PublisherConfig{
			Client: client,
		}, watermillLogger)
		if err != nil {
			return err
		}

		// Redis subscriber function
		instantiateSubscriber = func(consumerGroup string, ackDeadline time.Duration) (message.Subscriber, error) {
			client := redis.NewUniversalClient(&redis.UniversalOptions{
				Addrs: redisServers,
			})

			return redisstream.NewSubscriber(redisstream.SubscriberConfig{
				Client:        client,
				ConsumerGroup: consumerGroup,
				MaxIdleTime:   ackDeadline,
			}, watermillLogger)
		}
	default:
		return fmt.Errorf("%w: no Pub/Sub backend was specified (please specify either "+
			"--gcp-pubsub-project-id or --redis-servers", ErrRun)
	}

	pubsub, err := pubsub.New(publisher, instantiateSubscriber,
		pubsub.WithAddress(fmt.Sprintf(":%d", port)),
		pubsub.WithLogger(logger))
	if err != nil {
		return err
	}

	return pubsub.Run(cmd.Context())
}
