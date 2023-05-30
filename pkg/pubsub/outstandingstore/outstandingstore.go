package outstandingstore

import "github.com/ThreeDotsLabs/watermill/message"

type OutstandingStore struct {
	outstandingMessages map[uint64]*message.Message
	nextMessageID       uint64
}

func New() *OutstandingStore {
	return &OutstandingStore{
		outstandingMessages: map[uint64]*message.Message{},
		nextMessageID:       1,
	}
}

func (os *OutstandingStore) Add(message *message.Message) uint64 {
	messageID := os.nextMessageID
	os.nextMessageID++

	os.outstandingMessages[messageID] = message

	return messageID
}

func (os *OutstandingStore) GetAndForget(id uint64) *message.Message {
	m, ok := os.outstandingMessages[id]
	if !ok {
		return nil
	}

	delete(os.outstandingMessages, id)

	return m
}
