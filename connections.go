package peers

import (
	"context"
	"fmt"
	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/mostcute/peers/util"
	"log"
	"time"
)

// DiscoveryServiceTag is used in our mDNS advertisements to discover other chat peers.
//var DiscoveryServiceTag = "objstorage-mdns"

//var ServerdisksheartbeatRoom = "data-servers && data-disks pubsub channel"

func NewPeerNode(nick, room, mdns string, event HandleEvents) (Pm *PeerManager, h host.Host, err error) {
	ctx := context.Background()
	// create a new libp2p Host that listens on a random TCP port

	Host, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"))

	if err != nil {
		panic(err)
	}
	fmt.Println("listen addrs ", Host.Addrs())
	// create a new PubSub service using the GossipSub router
	ps, err := pubsub.NewGossipSub(ctx, Host)
	if err != nil {
		panic(err)
	}

	// setup local mDNS discovery
	if mdns == "" {
		mdns = "peer-mdns"
	}
	if err := setupDiscovery(Host, mdns); err != nil {
		return nil, nil, err
	}

	// join the chat room
	cr, err := joinChatRoom(ctx, ps, Host.ID(), nick, room)
	if err != nil {
		return nil, nil, err
	}

	pm := newPeerManager(cr, event)
	if err = pm.run(); err != nil {
		return nil, nil, err
	}

	connectmanager(Host)

	return pm, Host, nil
	//go DiskHeartBeat(PM.inputCh)

}

func connectmanager(h host.Host) {
	//Host.Network().SetConnHandler(test)
	h.Network().Notify(&network.NotifyBundle{
		//ConnectedF: func(n network.Network, conn network.Conn) {
		//	fmt.Println("connect ",conn.RemotePeer().String() ,"have con",len(n.ConnsToPeer(conn.RemotePeer())))
		//},
		//TODO: wait for  PeerDisconnected Func
		DisconnectedF: func(n network.Network, conn network.Conn) {
			remotepeer := conn.RemotePeer()
			remoteaddr := conn.RemoteMultiaddr()
			remaincons := len(n.ConnsToPeer(remotepeer))
			//fmt.Println("diconnect ", remoteaddr.String(), "have con ", remaincons)
			if remaincons == 0 {
				//trigger peerdisconnect sig
				//fmt.Println(Host.Network().Connectedness(remotepeer))
				// try to reconnect
				go func() {
					retry := 0
					for {
						time.Sleep(2 * time.Second)
						err := h.Connect(context.Background(), h.Peerstore().PeerInfo(remotepeer))
						if err != nil {
							//log.Println("reconnect ", remoteaddr.String(), " failed ",err)
						} else {
							log.Println("reconnect ", remoteaddr.String(), " success")
							break
						}
						retry++
						if retry > 30 {
							//log.Println("reconnect ", remoteaddr.String(), " cancel failed too many times ")
							break
						}
					}
				}()
			}
		},
	})

}

func test(a network.Conn) {
	//fmt.Println("some peer connected ",a.RemotePeer().String())
	//cons := Host.Network().ConnsToPeer(a.RemotePeer())
	//fmt.Println("cons ",cons)

}

// discoveryNotifee gets notified when we find a new peer via mDNS discovery
type discoveryNotifee struct {
	h host.Host
}

// HandlePeerFound connects to peers discovered via mDNS. Once they're connected,
// the PubSub system will automatically start interacting with them if they also
// support PubSub.
func (n *discoveryNotifee) HandlePeerFound(pi peer.AddrInfo) {
	//fmt.Printf("discovered new peer %s %v\n", pi.ID.Pretty(), pi.Addrs)
	err := n.h.Connect(context.Background(), pi)
	if err != nil {
		if util.ErrorContains(err.Error(), util.ErrDialSelf) {

		} else {
			fmt.Printf("error connecting to peer %s: ,addrs %s: %s\n", pi.ID.Pretty(), pi.Addrs, err)
		}
	}
}

// setupDiscovery creates an mDNS discovery service and attaches it to the libp2p Host.
// This lets us automatically discover peers on the same LAN and connect to them.
func setupDiscovery(h host.Host, servicename string) error {
	// setup mDNS discovery to find local peers
	s := mdns.NewMdnsService(h, servicename, &discoveryNotifee{h: h})
	return s.Start()
}
