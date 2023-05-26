package outstandingstore_test

import (
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/cirruslabs/cirrus-backbone-services/pkg/pubsub/outstandingstore"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"math"
	"testing"
)

func TestAddsRetrievesForgets(t *testing.T) {
	os := outstandingstore.New()

	m1 := &message.Message{UUID: uuid.New().String()}
	m2 := &message.Message{UUID: uuid.New().String()}
	m3 := &message.Message{UUID: uuid.New().String()}

	require.EqualValues(t, 1, os.Add(m1))
	require.EqualValues(t, 2, os.Add(m2))
	require.EqualValues(t, 3, os.Add(m3))

	require.NotNil(t, m3, os.GetAndForget(3))
	require.Equal(t, m1, os.GetAndForget(1))
	require.NotNil(t, m2, os.GetAndForget(2))
}

func TestEmptyDoesNotReturnAnything(t *testing.T) {
	os := outstandingstore.New()

	require.Nil(t, os.GetAndForget(0))
	require.Nil(t, os.GetAndForget(1))
	require.Nil(t, os.GetAndForget(2))
	require.Nil(t, os.GetAndForget(3))
	require.Nil(t, os.GetAndForget(math.MaxUint64))
}
