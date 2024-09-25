package peers

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/cockroachdb/pebble"
	pebbleDs "github.com/ipfs/go-ds-pebble"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/mostcute/peers/util"
	"github.com/multiformats/go-multiaddr"
)

// DiscoveryServiceTag is used in our mDNS advertisements to discover other chat peers.
//var DiscoveryServiceTag = "objstorage-mdns"

// var ServerdisksheartbeatRoom = "data-servers && data-disks pubsub channel"

var DefaultBootstrapPeers []multiaddr.Multiaddr

var (
// ds          datastore.Batching
// bs          blockstore.Blockstore
// dsrv        ipld.DAGService
// bswap       *bsserver.Server
// bswapclient *bsclient.Client
// relaypeer string
)

// func NewPeerNode(nick, room, mdns, nodepriv, extAddr, datadir string, event HandleEvents, bootstrappeer []string) (Pm *PeerManager, h host.Host, err error) {
// 	ctx := context.Background()
// 	// create a new libp2p Host that listens on a random TCP port
// 	///ip4/%s/tcp/%d
// 	var extMultiAddr multiaddr.Multiaddr
// 	if extAddr != "" {
// 		extMultiAddr, err = multiaddr.NewMultiaddr(extAddr)
// 		if err != nil {
// 			fmt.Printf("Error creating multiaddress: %v\n", err)
// 			extMultiAddr = nil
// 		}
// 	} else {
// 		extMultiAddr = nil
// 	}

// 	addressFactory := func(addrs []multiaddr.Multiaddr) []multiaddr.Multiaddr {
// 		if extMultiAddr != nil {
// 			addrs = append(addrs, extMultiAddr)
// 		}
// 		return addrs
// 	}
// 	// new host
// 	Host, err := libp2p.New(
// 		libp2p.Identity(readconfigpriv(nodepriv)),
// 		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/26666"),
// 		libp2p.Ping(true),
// 		libp2p.EnableRelay(),
// 		libp2p.EnableAutoNATv2(),
// 		libp2p.EnableHolePunching(),
// 		libp2p.AddrsFactory(addressFactory),
// 	)
// 	if err != nil {
// 		panic(err)
// 	}
// 	h = Host
// 	fmt.Println("listen addrs ", Host.Addrs())
// 	fmt.Println("mutiaddr: ", GetHostAddress(Host))
// 	//bitswap server
// 	startBitswapDataServer(ctx, Host, datadir)

// 	//bootstrap

// 	for _, s := range bootstrappeer {
// 		ma, err := multiaddr.NewMultiaddr(s)
// 		if err != nil {
// 			panic(err)
// 		}
// 		DefaultBootstrapPeers = append(DefaultBootstrapPeers, ma)
// 	}

// 	bootstrapPeers := make([]peer.AddrInfo, len(bootstrappeer))

// 	kademliaDHT, err := dht.New(ctx, h, dht.BootstrapPeers(bootstrapPeers...))
// 	if err != nil {
// 		panic(err)
// 	}

// 	bswapclient = startBitswapClient(ctx, Host, kademliaDHT)

// 	// Bootstrap the DHT. In the default configuration, this spawns a Background
// 	// thread that will refresh the peer table every five minutes.
// 	fmt.Println("Bootstrapping the DHT")
// 	if err = kademliaDHT.Bootstrap(ctx); err != nil {
// 		panic(err)
// 	}
// 	// Wait a bit to let bootstrapping finish (really bootstrap should block until it's ready, but that isn't the case yet.)
// 	time.Sleep(5 * time.Second)

// 	// We use a rendezvous point "meet me here" to announce our location.
// 	// This is like telling your friends to meet you at the Eiffel Tower.
// 	dht_word := mdns + "-dht"

// 	fmt.Println("Announcing ourselves...")
// 	routingDiscovery := drouting.NewRoutingDiscovery(kademliaDHT)
// 	dutil.Advertise(ctx, routingDiscovery, dht_word)

// 	//relayv2 Reserve
// 	_, err = client.Reserve(context.Background(), h, *getrelayinfo(bootstrappeer[0]))
// 	relaypeer = bootstrappeer[0]
// 	if err != nil {
// 		log.Println(" client.Reserve ", err)
// 	}

// 	// create a new PubSub service using the GossipSub router
// 	ps, err := pubsub.NewGossipSub(ctx, Host)
// 	if err != nil {
// 		panic(err)
// 	}

// 	go func() {
// 		for {
// 			fmt.Println("Searching for other peers...")
// 			peerChan, err := routingDiscovery.FindPeers(ctx, dht_word)
// 			if err != nil {
// 				panic(err)
// 			}
// 			connectdhtpeers(h, peerChan, bootstrappeer[0])
// 			time.Sleep(time.Minute * 10)
// 		}
// 	}()

// 	// setup local mDNS discovery
// 	if mdns == "" {
// 		mdns = "peer-mdns"
// 	}
// 	if err := setupDiscovery(Host, mdns); err != nil {
// 		return nil, nil, err
// 	}

// 	// join the chat room
// 	cr, err := joinChatRoom(ctx, ps, Host.ID(), nick, room)
// 	if err != nil {
// 		return nil, nil, err
// 	}

// 	pm := newPeerManager(cr, event)
// 	if err = pm.run(); err != nil {
// 		return nil, nil, err
// 	}

// 	connectmanager(Host)

// 	return pm, Host, nil
// 	//go DiskHeartBeat(PM.inputCh)

// }

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
			if remaincons == 0 {
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
			fmt.Printf("error connecting to peer %s: ,addrs %s: %s\n", pi.ID.String(), pi.Addrs, err)
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

func readconfigpriv(priv string) crypto.PrivKey {
	privbyte, _ := crypto.ConfigDecodeKey(priv)
	priv2, _ := crypto.UnmarshalPrivateKey(privbyte)
	return priv2
}

func GenprivKey() string {
	// Set your own keypair
	priv, _, _ := crypto.GenerateKeyPair(
		crypto.Ed25519, // Select your key type. Ed25519 are nice short
		-1,             // Select key length when possible (i.e. RSA).
	)
	privbyte, _ := crypto.MarshalPrivateKey(priv)

	privstr := crypto.ConfigEncodeKey(privbyte)
	return privstr
}

func getrelayinfo(relaypeer string) *peer.AddrInfo {
	relay1info, err := peer.AddrInfoFromString(relaypeer)
	if err != nil {
		log.Printf("Failed to create relay1addr: %v", err)
		return nil
	}
	return relay1info
}

func buildrelayinfo(targetID peer.ID, relaypeer string) *peer.AddrInfo {
	relayaddr, err := multiaddr.NewMultiaddr("/p2p/" + getrelayinfo(relaypeer).ID.String() + "/p2p-circuit/p2p/" + targetID.String())
	if err != nil {
		log.Println(err)
		return nil
	}
	fmt.Println(relayaddr.String())

	// unreachable2relayinfo := peer.AddrInfo{
	// 	ID:    peer.ID(unreachable2ID),
	// 	Addrs: []multiaddr.Multiaddr{relayaddr},
	// }
	unreachable2relayinfo, err := peer.AddrInfoFromString(relayaddr.String())
	if err != nil {
		log.Println(err)
		return nil
	}
	return unreachable2relayinfo
}

func connectdhtpeers(h host.Host, peerChan <-chan peer.AddrInfo, relaypeer []string) {
	ctx := context.Background()
	for peer := range peerChan {
		if peer.ID == h.ID() {
			continue
		}
		fmt.Println("Found peer from dht:", peer)
		fmt.Println("Connecting to:", peer)
		err := h.Connect(ctx, peer)
		if err != nil {
			fmt.Println("Connection failed:", err)
			fmt.Println("try with realy connect")
			for _, v := range relaypeer {
				unreachable2relayinfo := buildrelayinfo(peer.ID, v)
				if err := h.Connect(context.Background(), *unreachable2relayinfo); err != nil {
					log.Printf("Unexpected error here. Failed to connect unreachable1 and unreachable2: %v", err)
					continue
				} else {
					log.Println("Yep, that worked!")
					break
				}
			}

		} else {

		}
		fmt.Println("Connected to:", peer)
	}
}

func GetHostAddress(h host.Host) string {
	// Build host multiaddress
	hostAddr, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/p2p/%s", h.ID().String()))

	// Now we can build a full multiaddress to reach this host
	// by encapsulating both addresses:
	addr := h.Addrs()[0]
	return addr.Encapsulate(hostAddr).String()
}

type progressWriter struct {
	writer    io.Writer
	totalRead *int64
	fileSize  int64
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	// n, err := pw.writer.Write(p)
	// if err != nil {
	// 	return n, err
	// }
	n := len(p)

	*pw.totalRead += int64(n)
	progress := float64(*pw.totalRead) / float64(pw.fileSize) * 100
	fmt.Printf("\rDownloading... %.2f%% complete", progress)

	return n, nil
}

// createDB creates a PebbleDB DB in the specified directory.
func createDB(dbdir string) (*pebbleDs.Datastore, error) {
	// ro := &pebble.IterOptions{}
	// wo := &pebble.WriteOptions{Sync: false}
	// syncwo := &pebble.WriteOptions{Sync: false}
	cache := pebble.NewCache(0)
	opts := &pebble.Options{
		MaxManifestFileSize: 1024 * 32 * 1024,
		MemTableSize:        1024 * 32 * 1024,
		Cache:               cache,
	}
	if err := os.MkdirAll(dbdir, 0755); err != nil {
		return nil, err
	}

	ds, err := pebbleDs.NewDatastore(dbdir, opts)
	if err != nil {
		return nil, err
	}

	cache.Unref()
	return ds, nil
}
