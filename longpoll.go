package longpoll

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/frncscsrcc/resthelper"
)

var seededRand = rand.New(rand.NewSource(time.Now().UnixNano()))

type clientExist map[string]bool
type feedToClients map[string]clientExist
type event struct {
	Data      interface{}
	Feed      string
	Timestamp int32
}
type events map[int]event
type clientToNewEvents map[string][]int
type clientToConnection map[string]int
type connectionChannel map[int]chan string

// LongPoll is the exported basic package structure:
type LongPoll struct {
	globalClients            clientExist
	globalEvents             events
	globalClientToNewEvents  clientToNewEvents
	globalFeedToClients      feedToClients
	globalClientToConnection clientToConnection
	globalConnectionChannel  connectionChannel
	globalLastConnection     int
}

// SubscriptionResponse is the standard response returned after a succesfull
// subscription. It returns the ConnectionID and the list of subscribet tokens
type SubscriptionResponse struct {
	SubscriptionID string
	Feeds          []string
}

// EventResponse contains the field Events, that is a slice of all the events
// that are passed to a listening subscriber.
type EventResponse struct {
	Events []event
}

// New is the constructor, it returns a pointer to a longpoll struct
func New() *LongPoll {
	lp := LongPoll{
		globalClients:            make(clientExist),
		globalEvents:             make(events),
		globalClientToNewEvents:  make(clientToNewEvents),
		globalFeedToClients:      make(map[string]clientExist),
		globalClientToConnection: make(clientToConnection),
		globalConnectionChannel:  make(connectionChannel),
	}
	return &lp
}

// AddFeed registers one feed. A client can subscribe and listen only
// to existing feeds.
func (lp *LongPoll) AddFeed(feed string) error {
	if len(feed) > 0 {
		lp.globalFeedToClients[feed] = make(clientExist)
	}
	return nil
}

// AddFeeds registers more feeds. A client can subscribe and listen only
// to existing feeds.
func (lp *LongPoll) AddFeeds(feeds []string) error {
	if feeds != nil && len(feeds) > 0 {
		for _, feed := range feeds {
			lp.AddFeed(feed)
		}
	}
	return nil
}

// SubscribeHandler handles the subscription client request. It expects one or
// more feeds in the query-string and, in case of success, it returns an object
// of type SubscriptionResponse
func (lp *LongPoll) SubscribeHandler(w http.ResponseWriter, r *http.Request) {
	feeds := getFeeds(r)
	if len(feeds) == 0 {
		resthelper.SendError(w, 400, "Missing feed")
		return
	}
	// If a subscriptionID is present, use subscriptionID ID as user token,
	// otherwhise create a new one
	subscriptionID := getSubscriptionID(r)
	if subscriptionID == "" {
		subscriptionID = resthelper.GetNewToken(32)
	}

	// Client is not pending
	lp.globalClients[subscriptionID] = false

	// Feeds validation
	for _, feed := range feeds {
		if _, ok := lp.globalFeedToClients[feed]; ok == false {
			resthelper.SendError(w, 500, fmt.Sprintf("Feed %s is not available", feed))
			return
		}
	}

	// Client subscription
	for _, feed := range feeds {
		lp.globalFeedToClients[feed][subscriptionID] = true
	}

	resthelper.SendResponse(w, SubscriptionResponse{subscriptionID, feeds})
}

// ListenHandler handles the listening requests from a client.
// It cloud respond with:
// - 400: Missing or invalid SubscriptionID
// - 401: Does not exists a valid subscription for the passed subscriptionID.
// - 200: EventResponse type: the list of events triggered since the last time
//        an EventResponse was sent for this subscriptionID
// - 204: ConnectionAborted: a new request come with the same SubscriptionID
//        before the current one was resolved (or went timeout)
// - 408: Request timeout: the client should implement a new request on the same
//        endpoint.
func (lp *LongPoll) ListenHandler(w http.ResponseWriter, r *http.Request) {
	subscriptionID := getSubscriptionID(r)
	if subscriptionID == "" {
		resthelper.SendError(w, 400, "Missing subscriptionID")
		return
	}

	// Check if subscriptionID exists
	if _, clientExists := lp.globalClients[subscriptionID]; clientExists == false {
		resthelper.SendError(w, 401, "Unauthorized")
		return
	}

	log.Printf("Received request from %s\n", subscriptionID)

	// Protect with mutex
	lp.globalLastConnection = lp.globalLastConnection + 1
	currentConnection := lp.globalLastConnection
	// Check if there is a previous listen connection, in this case
	if previousConnectionIndex, ok := lp.globalClientToConnection[subscriptionID]; ok == true {
		// Send a ABORT signal to previous connection
		log.Printf("Closing previous connection (%d) from the same client (%s)\n", previousConnectionIndex, subscriptionID)
		log.Printf("%+v\n", lp.globalConnectionChannel[previousConnectionIndex])
		lp.globalConnectionChannel[previousConnectionIndex] <- "ABORT"
		log.Printf("Closed previous connection (%d) from the same client (%s)\n", previousConnectionIndex, subscriptionID)
	}

	// Save the active connection for this client
	lp.globalClientToConnection[subscriptionID] = currentConnection

	// Create a comunication channel to receive async events
	comunicationChannel := make(chan string)
	lp.globalConnectionChannel[currentConnection] = comunicationChannel

	// If they are no event, wait for the next one
	if len(lp.globalClientToNewEvents[subscriptionID]) == 0 {
		// Client is pending
		lp.globalClients[subscriptionID] = true

		// Set a timeout every 5 seconds
		go lp.notifyTimeout(comunicationChannel, 5)

		log.Printf("Client %s (%d) waits for connection\n", subscriptionID, currentConnection)
		operation := <-comunicationChannel
		log.Printf("Client %s (%d) received signal %s\n", subscriptionID, currentConnection, operation)

		// Another connection from the same client, this one should be disharged
		if operation == "ABORT" {
			resthelper.SendError(w, 204, "Connection aborted")
			log.Printf("Sent abort signal to %s (%d)\n", subscriptionID, currentConnection)
			return
		}
		// Timeout
		if operation == "TIMEOUT" {
			resthelper.SendError(w, 408, "Request timeout")
			log.Printf("Sent timeout signal to %s (%d)\n", subscriptionID, currentConnection)
			// Delete the connection, or next client will try to closed this one
			// but it does not exist anymore and it would lock
			delete(lp.globalClientToConnection, subscriptionID)
			return
		}
	}

	// Fetch the events
	var eventResponse EventResponse
	eventResponse.Events = make([]event, 0)
	for _, eventID := range lp.globalClientToNewEvents[subscriptionID] {
		eventResponse.Events = append(eventResponse.Events, lp.globalEvents[eventID])
	}

	// Clean the event list
	lp.globalClientToNewEvents[subscriptionID] = make([]int, 0)

	resthelper.SendResponse(w, eventResponse)
	delete(lp.globalClientToConnection, subscriptionID)

}

// NewEvent sends an event (a generic object) to all the listening subscribers-
func (lp *LongPoll) NewEvent(feed string, object interface{}) error {
	newIndex := len(lp.globalEvents)
	lp.globalEvents[newIndex] = event{
		Feed:      feed,
		Data:      object,
		Timestamp: int32(time.Now().Unix()),
	}

	// Find listening clients
	waitingClients := make(map[string]bool)
	for client := range lp.globalFeedToClients[feed] {
		lp.globalClientToNewEvents[client] = append(lp.globalClientToNewEvents[client], newIndex)
		waitingClients[client] = true
	}

	for client := range waitingClients {
		go lp.notifyEvent(client)
	}

	return nil
}

func (lp *LongPoll) notifyEvent(client string) {
	if lp.globalClients[client] == true {
		connection, ok := lp.globalClientToConnection[client]
		if ok != true {
			return
		}
		lp.globalConnectionChannel[connection] <- "DONE"
		lp.globalClients[client] = false
	}
}

func (lp *LongPoll) notifyTimeout(comunicationChanel chan string, seconds int) {
	time.Sleep(time.Duration(seconds) * time.Second)
	comunicationChanel <- "TIMEOUT"
}
