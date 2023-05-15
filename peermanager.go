package peers

import (
	"github.com/libp2p/go-libp2p/core/peer"
	"log"
	"time"
)

type Msg struct {
	Name string
	Data []byte
}

type HandleEvents interface {
	HandleEvent(msg *ChatMessage)
	HandleEventSelf(msg *ChatMessage)
}

type PeerManager struct {
	cr      *ChatRoom
	inputCh chan Msg //wait for message struct
	eventF  HandleEvents
	//doneCh  chan struct{}
}

func newPeerManager(cr *ChatRoom, event HandleEvents) *PeerManager {
	inputCh := make(chan Msg, 1024*1024)

	return &PeerManager{
		cr:      cr,
		inputCh: inputCh,
		eventF:  event,
	}
}

// run starts the chat event loop in the background, then starts
// the event loop for the text UI.
func (pm *PeerManager) run() error {
	go pm.handleEvents()
	return nil
}
func (pm *PeerManager) ListPeers() []peer.ID {
	peers := pm.cr.ListPeers()
	return peers

}
func (pm *PeerManager) SendMsg(msg Msg) {
	pm.inputCh <- msg

}

// handleEvents runs an event loop that sends user input to the chat room
func (pm *PeerManager) handleEvents() {
	peerRefreshTicker := time.NewTicker(time.Second)
	defer peerRefreshTicker.Stop()

	for {
		select {
		case input := <-pm.inputCh:
			// when the user types in a line, publish it to the chat room and print to the message window
			err := pm.cr.Publish(input)
			if err != nil {
				log.Printf("publish error: %s\n", err.Error())
			}
			//flush it self, pubsub can not receive from ownself
			pm.eventF.HandleEventSelf(&ChatMessage{
				Message:    input,
				SenderID:   pm.cr.self.Pretty(),
				SenderNick: pm.cr.nick,
			})
		case m := <-pm.cr.Messages:
			// when we receive a message from the chat room, print it to the message window
			//handle msgs
			//log.Println("received msg from ",m.SenderNick)
			//FlushDiskInfo(m.Message)
			pm.eventF.HandleEvent(m)

		case <-peerRefreshTicker.C:

		case <-pm.cr.ctx.Done():
			return

			//case <-pm.doneCh:
			//	return
		}
	}
}
