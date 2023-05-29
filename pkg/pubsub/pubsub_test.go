package pubsub_test

import (
	pubsubpkg "cloud.google.com/go/pubsub"
	"context"
	"fmt"
	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-googlecloud/pkg/googlecloud"
	"github.com/ThreeDotsLabs/watermill-redisstream/pkg/redisstream"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/cirruslabs/core-services/pkg/pubsub"
	"github.com/cirruslabs/core-services/pkg/rpc"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"testing"
	"time"
)

func TestGoogleCloud(t *testing.T) {
	ctx := context.Background()

	const projectID = "test"

	// Start Google Cloud Pub/Sub emulator
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "gcr.io/google.com/cloudsdktool/google-cloud-cli:latest",
			Cmd: []string{
				"gcloud", "beta", "emulators", "pubsub", "start",
				"--project=" + projectID, "--host-port=0.0.0.0:8085",
			},
			ExposedPorts: []string{"8085"},
			WaitingFor:   wait.ForExposedPort(),
		},
		Started: true,
	})
	require.NoError(t, err)
	defer func() {
		_ = container.Terminate(ctx)
	}()

	// Configuer Google Cloud client to use the Google Cloud Pub/Sub emulator
	endpoint, err := container.Endpoint(ctx, "")
	require.NoError(t, err)

	t.Setenv("PUBSUB_EMULATOR_HOST", endpoint)

	// Instantiate publisher and subscriber
	logger := watermill.NewStdLogger(true, true)

	publisher, err := googlecloud.NewPublisher(googlecloud.PublisherConfig{
		ProjectID: projectID,
	}, logger)
	require.NoError(t, err)

	instantiateSubscriber := func(subscriberGroup string, ackDeadline time.Duration) (message.Subscriber, error) {
		return googlecloud.NewSubscriber(googlecloud.SubscriberConfig{
			GenerateSubscriptionName: func(topic string) string {
				return subscriberGroup
			},
			ProjectID: projectID,
			SubscriptionConfig: pubsubpkg.SubscriptionConfig{
				AckDeadline: ackDeadline,
			},
		}, logger)
	}

	// Run test
	testBase(ctx, t, publisher, instantiateSubscriber)
}

func TestRedisStreams(t *testing.T) {
	ctx := context.Background()

	// Start Redis server
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:latest",
			ExposedPorts: []string{"6379"},
			WaitingFor:   wait.ForExposedPort(),
		},
		Started: true,
	})
	require.NoError(t, err)
	defer func() {
		_ = container.Terminate(ctx)
	}()

	endpoint, err := container.Endpoint(ctx, "")
	require.NoError(t, err)

	// Instantiate publisher and subscriber
	logger := watermill.NewStdLogger(true, true)

	publisher, err := redisstream.NewPublisher(redisstream.PublisherConfig{
		Client: redis.NewUniversalClient(&redis.UniversalOptions{
			Addrs: []string{endpoint},
		}),
	}, logger)
	require.NoError(t, err)

	instantiateSubscriber := func(subscriberGroup string, ackDeadline time.Duration) (message.Subscriber, error) {
		return redisstream.NewSubscriber(redisstream.SubscriberConfig{
			Client: redis.NewUniversalClient(&redis.UniversalOptions{
				Addrs: []string{endpoint},
			}),
			ConsumerGroup: subscriberGroup,
			MaxIdleTime:   ackDeadline,
		}, logger)
	}

	// Run test
	testBase(ctx, t, publisher, instantiateSubscriber)
}

func testBase(
	ctx context.Context,
	t *testing.T,
	publisher message.Publisher,
	instantiateSubscriber pubsub.InstantiateSubscriberFunc,
) {
	pubsub, err := pubsub.New(publisher, instantiateSubscriber, pubsub.WithLogger(zap.Must(zap.NewDevelopment())))
	require.NoError(t, err)

	go func() {
		if err := pubsub.Run(ctx); err != nil {
			t.Error(err)
		}
	}()

	// Instantiate Core Services PubSub client
	conn, err := grpc.Dial(pubsub.Endpoint(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)

	client := rpc.NewPubSubClient(conn)

	subscribeClient, err := client.Subscribe(ctx)
	require.NoError(t, err)

	// Subscribe to a topic
	topic := fmt.Sprintf("topic-%s", uuid.New().String())

	err = subscribeClient.Send(&rpc.FromSubscriber{
		Request: &rpc.FromSubscriber_SubscriptionRequest_{
			SubscriptionRequest: &rpc.FromSubscriber_SubscriptionRequest{
				Topic:           topic,
				SubscriberGroup: "test",
			},
		},
	})
	require.NoError(t, err)

	// Wait for confirmation
	toSubscriber, err := subscribeClient.Recv()
	require.NoError(t, err)
	_, ok := toSubscriber.Response.(*rpc.ToSubscriber_SubscriptionResponse_)
	require.True(t, ok)

	// Publish a couple of poems
	poems := map[string]struct{}{
		"a wild dog appears":   {},
		"a white rabbit sings": {},
		"a blue dolphin swims": {},
	}

	for poem := range poems {
		_, err = client.Publish(ctx, &rpc.FromPublisher{
			Topic: topic,
			Messages: []*rpc.Message{
				{
					Payload: []byte(poem),
				},
			},
		})
		require.NoError(t, err)
	}

	// Make sure we receive all the poems
	for range poems {
		toSubscriber, err = subscribeClient.Recv()
		require.NoError(t, err)

		toSubscriberMessage, ok := toSubscriber.Response.(*rpc.ToSubscriber_Message)
		require.True(t, ok)
		message := toSubscriberMessage.Message

		err = subscribeClient.Send(&rpc.FromSubscriber{
			Request: &rpc.FromSubscriber_Ack_{
				Ack: &rpc.FromSubscriber_Ack{
					MessageId: message.Id,
				},
			},
		})
		require.NoError(t, err)

		delete(poems, string(message.Payload))
	}

	require.Empty(t, 0, poems)
}
