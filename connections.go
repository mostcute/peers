package peers

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/cockroachdb/pebble"
	bsclient "github.com/ipfs/boxo/bitswap/client"
	bsnet "github.com/ipfs/boxo/bitswap/network"
	bsserver "github.com/ipfs/boxo/bitswap/server"
	"github.com/ipfs/boxo/blockservice"
	blockstore "github.com/ipfs/boxo/blockstore"
	chunker "github.com/ipfs/boxo/chunker"
	offline "github.com/ipfs/boxo/exchange/offline"
	"github.com/ipfs/boxo/files"
	"github.com/ipfs/boxo/ipld/merkledag"
	unixfile "github.com/ipfs/boxo/ipld/unixfs/file"
	"github.com/ipfs/boxo/ipld/unixfs/importer/balanced"
	uih "github.com/ipfs/boxo/ipld/unixfs/importer/helpers"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	dsync "github.com/ipfs/go-datastore/sync"
	pebbleDs "github.com/ipfs/go-ds-pebble"
	ipld "github.com/ipfs/go-ipld-format"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	routinghelpers "github.com/libp2p/go-libp2p-routing-helpers"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	"github.com/mostcute/peers/util"
	"github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-multicodec"
)

// DiscoveryServiceTag is used in our mDNS advertisements to discover other chat peers.
//var DiscoveryServiceTag = "objstorage-mdns"

// var ServerdisksheartbeatRoom = "data-servers && data-disks pubsub channel"

var DefaultBootstrapPeers []multiaddr.Multiaddr

var (
	ds          datastore.Batching
	bs          blockstore.Blockstore
	dsrv        ipld.DAGService
	bswap       *bsserver.Server
	bswapclient *bsclient.Client
	relaypeer   string
)

func NewPeerNode(nick, room, mdns, nodepriv, extAddr, datadir string, event HandleEvents, bootstrappeer []string) (Pm *PeerManager, h host.Host, err error) {
	ctx := context.Background()
	// create a new libp2p Host that listens on a random TCP port
	///ip4/%s/tcp/%d
	var extMultiAddr multiaddr.Multiaddr
	if extAddr != "" {
		extMultiAddr, err = multiaddr.NewMultiaddr(extAddr)
		if err != nil {
			fmt.Printf("Error creating multiaddress: %v\n", err)
			extMultiAddr = nil
		}
	} else {
		extMultiAddr = nil
	}

	addressFactory := func(addrs []multiaddr.Multiaddr) []multiaddr.Multiaddr {
		if extMultiAddr != nil {
			addrs = append(addrs, extMultiAddr)
		}
		return addrs
	}
	// new host
	Host, err := libp2p.New(
		libp2p.Identity(readconfigpriv(nodepriv)),
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/26666"),
		libp2p.Ping(true),
		libp2p.EnableRelay(),
		libp2p.EnableAutoNATv2(),
		libp2p.EnableHolePunching(),
		libp2p.AddrsFactory(addressFactory),
	)

	if err != nil {
		panic(err)
	}
	fmt.Println("listen addrs ", Host.Addrs())

	//bitswap server
	startBitswapDataServer(ctx, Host, datadir)

	//bootstrap

	for _, s := range bootstrappeer {
		ma, err := multiaddr.NewMultiaddr(s)
		if err != nil {
			panic(err)
		}
		DefaultBootstrapPeers = append(DefaultBootstrapPeers, ma)
	}

	bootstrapPeers := make([]peer.AddrInfo, len(bootstrappeer))

	kademliaDHT, err := dht.New(ctx, h, dht.BootstrapPeers(bootstrapPeers...))
	if err != nil {
		panic(err)
	}

	bswapclient = startBitswapClient(ctx, Host, kademliaDHT)

	// Bootstrap the DHT. In the default configuration, this spawns a Background
	// thread that will refresh the peer table every five minutes.
	fmt.Println("Bootstrapping the DHT")
	if err = kademliaDHT.Bootstrap(ctx); err != nil {
		panic(err)
	}
	// Wait a bit to let bootstrapping finish (really bootstrap should block until it's ready, but that isn't the case yet.)
	time.Sleep(5 * time.Second)

	// We use a rendezvous point "meet me here" to announce our location.
	// This is like telling your friends to meet you at the Eiffel Tower.
	dht_word := mdns + "-dht"

	fmt.Println("Announcing ourselves...")
	routingDiscovery := drouting.NewRoutingDiscovery(kademliaDHT)
	dutil.Advertise(ctx, routingDiscovery, dht_word)

	//relayv2 Reserve
	_, err = client.Reserve(context.Background(), h, *getrelayinfo(bootstrappeer[0]))
	relaypeer = bootstrappeer[0]
	if err != nil {
		log.Println(" client.Reserve ", err)
	}

	// create a new PubSub service using the GossipSub router
	ps, err := pubsub.NewGossipSub(ctx, Host)
	if err != nil {
		panic(err)
	}

	go func() {
		for {
			fmt.Println("Searching for other peers...")
			peerChan, err := routingDiscovery.FindPeers(ctx, dht_word)
			if err != nil {
				panic(err)
			}
			connectdhtpeers(h, peerChan, bootstrappeer[0])
			time.Sleep(time.Minute * 10)
		}
	}()

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

func connectdhtpeers(h host.Host, peerChan <-chan peer.AddrInfo, relaypeer string) {
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
			unreachable2relayinfo := buildrelayinfo(peer.ID, relaypeer)
			if err := h.Connect(context.Background(), *unreachable2relayinfo); err != nil {
				log.Printf("Unexpected error here. Failed to connect unreachable1 and unreachable2: %v", err)
				continue
			} else {
				log.Println("Yep, that worked!")
			}

		} else {

		}
		fmt.Println("Connected to:", peer)
	}
	select {}
}

func GetHostAddress(h host.Host) string {
	// Build host multiaddress
	hostAddr, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/p2p/%s", h.ID().String()))

	// Now we can build a full multiaddress to reach this host
	// by encapsulating both addresses:
	addr := h.Addrs()[0]
	return addr.Encapsulate(hostAddr).String()
}

// 启动服务器的函数
func startBitswapDataServer(ctx context.Context, h host.Host, datadir string) error {
	// datastore.New
	pebbledb, err := createDB(datadir)
	if err != nil {
		return err
	}
	ds = dsync.MutexWrap(pebbledb)    // 初始化datastore
	bs = blockstore.NewBlockstore(ds) // 初始化blockstore
	bs = blockstore.NewIdStore(bs)    // 增加对identity multihash支持

	bsrv := blockservice.New(bs, offline.Exchange(bs))
	dsrv = merkledag.NewDAGService(bsrv) // 将merkledag.DAGService存储为ipld.DAGService

	// 启动Bitswap协议
	n := bsnet.NewFromIpfsHost(h, routinghelpers.Null{})
	bswap = bsserver.New(ctx, n, bs)
	n.Start(bswap) // 启动服务器

	fmt.Println("Bitswap服务器已启动")

	return nil
}

// 动态添加文件到服务器的方法
func AddFileToServer(fileReader io.Reader, dht *dht.IpfsDHT) (cid.Cid, error) {
	// fileReader := bytes.NewReader(fileBytes) // 读取新的文件

	ufsImportParams := uih.DagBuilderParams{
		Maxlinks:  uih.DefaultLinksPerBlock,
		RawLeaves: true,
		CidBuilder: cid.V1Builder{
			Codec:    uint64(multicodec.DagPb),
			MhType:   uint64(multicodec.Sha2_256),
			MhLength: -1,
		},
		Dagserv: dsrv, // 使用ipld.DAGService接口
		NoCopy:  false,
	}

	ufsBuilder, err := ufsImportParams.New(chunker.NewSizeSplitter(fileReader, chunker.DefaultBlockSize)) // 切分文件
	if err != nil {
		return cid.Undef, err
	}

	nd, err := balanced.Layout(ufsBuilder) // 使用平衡布局生成DAG图
	if err != nil {
		return cid.Undef, err
	}

	fileCID := nd.Cid() // 获取文件的CID
	fmt.Printf("已添加新文件,CID: %s\n", fileCID.String())
	dht.Provide(context.Background(), fileCID, true)
	return fileCID, nil
}

func startBitswapClient(ctx context.Context, h host.Host, dht *dht.IpfsDHT) *bsclient.Client {
	n := bsnet.NewFromIpfsHost(h, dht)
	bswap := bsclient.New(ctx, n, blockstore.NewBlockstore(datastore.NewNullDatastore()))
	n.Start(bswap)
	// defer bswap.Close()
	return bswap
}

func DownloadfileViaSwap(ctx context.Context, dht *dht.IpfsDHT, c cid.Cid, h host.Host, tofile string) (string, error) {
	go func() {
		time.Sleep(time.Second * 2)
		peers, err := dht.FindProviders(ctx, c)
		if err != nil {
			fmt.Println(err)
			return
		}
		cpeers := h.Network().Peers()

		connections := 0
		needrelaypeers := []peer.AddrInfo{}
		for _, peer1 := range peers {
			find := false
			for _, peer2 := range cpeers {
				if h.Network().Connectedness(peer2) == network.Connected {
					if peer2 == peer1.ID {
						connections++
						find = true
						break
					}
				}
			}
			if !find {
				needrelaypeers = append(needrelaypeers, peer1)
			}
		}
		if connections != 0 {
			fmt.Printf("have %d peers ,stop connect more\n", connections)
		} else {
			for _, peer1 := range needrelaypeers {
				err := h.Connect(context.Background(), peer1)
				if err != nil {
					fmt.Println("connect err:", err)
					unreachable2relayinfo := buildrelayinfo(peer1.ID, relaypeer)
					if err := h.Connect(context.Background(), *unreachable2relayinfo); err != nil {
						log.Printf("Unexpected error here. Failed to connect unreachable1 and unreachable2: %v", err)
						continue
					} else {
						fmt.Println("success connect via relay")
					}
				}
			}
		}

	}()

	dserv := merkledag.NewReadOnlyDagService(merkledag.NewSession(ctx, merkledag.NewDAGService(blockservice.New(blockstore.NewBlockstore(datastore.NewNullDatastore()), bswapclient))))
	nd, err := dserv.Get(ctx, c)
	if err != nil {
		return "", err
	}

	unixFSNode, err := unixfile.NewUnixfsFile(ctx, dserv, nd)
	if err != nil {
		return "", err
	}
	// 创建新文件（目标文件）
	dstFile, err := os.Create(tofile)
	if err != nil {
		fmt.Println("Error creating destination file:", err)
		return "", err
	}
	defer dstFile.Close() // 确保在函数返回时关闭目标文件

	if f, ok := unixFSNode.(files.File); ok {
		fileSize, err := f.Size()
		if err != nil {
			return "", err
		}
		// 包装一个TeeReader来监控读取进度
		var totalRead int64
		progressReader := io.TeeReader(f, &progressWriter{dstFile, &totalRead, fileSize})
		if _, err := io.Copy(dstFile, progressReader); err != nil {
			return "", err
		}
	}

	return "", nil
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
