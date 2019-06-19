// Copyright (c) 2018 CommerceBlock Team
// Use of this source code is governed by an MIT
// license that can be found in the LICENSE file.

package attestation

import (
	"fmt"
	"log"

	confpkg "mainstay/config"
	"mainstay/crypto"
	"mainstay/messengers"

	zmq "github.com/pebbe/zmq4"
)

// zmq communication consts
const (
	DefaultMainPublisherPort = 5000 // port used by main signer publisher

	// predefined topics for publishing/subscribing via zmq
	TopicNewTx         = "T"
	TopicConfirmedHash = "C"
	TopicSigs          = "S"
)

// AttestSignerZmq struct
//
// Implements AttestSigner interface and uses communication
// via zmq to publish data and listen to subscriptions and
// send commitments/new tx and receive signatures
type AttestSignerZmq struct {
	// zmq publisher interface used to publish hashes and txes to signers
	publisher *messengers.PublisherZmq

	// zmq subscribe interface to signers to receive tx signatures
	subscribers []*messengers.SubscriberZmq

	// store config for future later use when resubscribing
	config confpkg.SignerConfig
}

// poller to add all subscriber/publisher sockets
var poller *zmq.Poller

// Return new AttestSignerZmq instance
func NewAttestSignerZmq(config confpkg.SignerConfig) *AttestSignerZmq {
	// get publisher addr from config, if set
	publisherAddr := fmt.Sprintf("*:%d", DefaultMainPublisherPort)
	if config.Publisher != "" {
		publisherAddr = config.Publisher
	}

	// Initialise publisher for sending new hashes and txs
	// and subscribers to receive sig responses
	poller = zmq.NewPoller()
	publisher := messengers.NewPublisherZmq(publisherAddr, poller)
	var subscribers []*messengers.SubscriberZmq
	subtopics := []string{TopicSigs}
	for _, nodeaddr := range config.Signers {
		subscribers = append(subscribers, messengers.NewSubscriberZmq(nodeaddr, subtopics, poller))
	}

	return &AttestSignerZmq{publisher, subscribers, config}
}

// Zmq Resubscribe to the transaction signers
func (z *AttestSignerZmq) ReSubscribe() {
	// close current sockets
	for _, sub := range z.subscribers {
		sub.Close(poller)
	}
	z.subscribers = nil // empty slice

	// reconnect to signers
	var subscribers []*messengers.SubscriberZmq
	subtopics := []string{TopicSigs}
	for _, nodeaddr := range z.config.Signers {
		subscribers = append(subscribers, messengers.NewSubscriberZmq(nodeaddr, subtopics, poller))
	}
	z.subscribers = subscribers
}

// Use zmq publisher to send confirmed hash
func (z AttestSignerZmq) SendConfirmedHash(hash []byte) {
	z.publisher.SendMessage(hash, TopicConfirmedHash)
}

// Transform received list of bytes into a single byte
// slice with format: [len bytes0] [bytes0] [len bytes1] [bytes1]
func SerializeBytes(data [][]byte) []byte {

	// empty case return nothing
	if len(data) == 0 {
		return []byte{}
	}

	var serializedBytes []byte

	// iterate through each byte slice adding
	// length and data bytes to bytes slice
	for _, dataX := range data {
		serializedBytes = append(serializedBytes, byte(len(dataX)))
		serializedBytes = append(serializedBytes, dataX...)
	}

	return serializedBytes
}

// Transform single byte slice (result of SerializeBytes)
// into a list of byte slices excluding lengths
func UnserializeBytes(data []byte) [][]byte {

	// empty case return nothing
	if len(data) == 0 {
		return [][]byte{}
	}

	var dataList [][]byte

	// process data slice
	it := 0
	for it < len(data) {
		// get next data by reading byte size
		txSize := data[it]

		// check if next size excees the bounds and break
		// maybe TODO: error handling
		if (int(txSize) + 1 + it) > len(data) {
			break
		}

		dataX := append([]byte{}, data[it+1:it+1+int(txSize)]...)
		dataList = append(dataList, dataX)

		it += 1 + int(txSize)
	}

	return dataList
}

// Use zmq publisher to send new tx
func (z AttestSignerZmq) SendTxPreImages(txs [][]byte) {
	z.publisher.SendMessage(SerializeBytes(txs), TopicNewTx)
}

// Parse all received messages and create a sigs slice
// input:
// x dimension: subscriber
// y dimension: list of signatures of subscriber (one for each tx input)
// z dimension: slice of bytes (signature)
// output:
// x dimension: number of transaction inputs
// y dimension: number of signatures per input
func getSigsFromMsgs(msgs [][][]byte, numOfInputs int) [][]crypto.Sig {
	if numOfInputs == 0 {
		return [][]crypto.Sig{}
	}

	sigs := make([][]crypto.Sig, numOfInputs)
	for i := 0; i < numOfInputs; i++ {
		for _, msgSplit := range msgs {
			if len(msgSplit) > i {
				sigs[i] = append(sigs[i], crypto.Sig(msgSplit[i]))
			}
		}
	}
	return sigs
}

// Update num of transaction inputs from latest msg
func updateNumOfTxInputs(msgSplit [][]byte, numOfInputs int) int {
	if len(msgSplit) > numOfInputs {
		numOfInputs = len(msgSplit)
	}
	return numOfInputs
}

// Listen to zmq subscribers to receive tx signatures
func (z AttestSignerZmq) GetSigs() [][]crypto.Sig {

	var msgs [][][]byte
	numOfTxInputs := 0

	// Iterate through each subscriber to get the latest message sent
	// If there is more than one message in the subscriber queue the
	// last is retained by continuously polling the Poller to get that
	for _, sub := range z.subscribers {

		var subMsg [][]byte // store latest message

		// continously poll to get latest message
		// or stop if no message has been found
		for {
			sockets, pollErr := poller.Poll(-1)
			if pollErr != nil {
				log.Println(pollErr)
			}

			found := false
			// look for matching subscriber in polling results
			for _, socket := range sockets {
				if sub.Socket() == socket.Socket {
					found = true
					_, msg := sub.ReadMessage()
					subMsg = UnserializeBytes(msg)
				}
			}

			if !found {
				break
			}
		}

		// update received messages only if a subscriber message has been found
		// this check is probably unnecessary but better safe than sorry
		if len(subMsg) > 0 {
			numOfTxInputs = updateNumOfTxInputs(subMsg, numOfTxInputs)
			msgs = append(msgs, subMsg)
		}
	}

	return getSigsFromMsgs(msgs, numOfTxInputs) // bring messages into readable format for mainstay
}
