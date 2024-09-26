package peers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

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
	uih "github.com/ipfs/boxo/ipld/unixfs/importer/helpers" // UnixFS 导入
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/query"
	dsync "github.com/ipfs/go-datastore/sync"
	pebbleDs "github.com/ipfs/go-ds-pebble"
	ipld "github.com/ipfs/go-ipld-format"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	routinghelpers "github.com/libp2p/go-libp2p-routing-helpers"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/pnet"

	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	"github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-multicodec"
)

// NewPeerNode(nick, room, mdns, nodepriv, extAddr, datadir string, event HandleEvents, bootstrappeer []string) (Pm *PeerManager, h host.Host, err error)
type Peer struct {
	H         host.Host
	PM        *PeerManager
	bitclient *bsclient.Client
	bswap     *bsserver.Server
	ds        datastore.Batching
	bs        blockstore.Blockstore
	dsrv      ipld.DAGService
	dht       *dht.IpfsDHT
	swapstore *pebbleDs.Datastore
	cidstore  *pebbleDs.Datastore
	privnet   pnet.PSK

	port                string
	Nick                string
	room, mdns          string   //mdns
	nodepriv            string   //identity
	extAddr             []string //pub address
	datadir             string   //dir for database
	event               HandleEvents
	bootstrappeer       []string //pub bootstrap
	relaypeer           []string
	privatenet          string
	isBootstrap         bool
	isRelay             bool
	ispeermanage        bool
	enableBitSwapServer bool
	enableBitSwapClient bool
	storeIdentity       bool
	useprivnet          bool
}

const IDpriv = "peerid.priv"
const IDpub = "peerid.pub"
const IDpnet = "psk.key"

func NewPeer(nick, dir string, opts ...Option) (*Peer, error) {
	var p Peer
	p.Nick = nick
	p.datadir = dir
	if err := p.Apply(opts...); err != nil {
		return nil, err
	}
	p.start()
	return &p, nil
}

func (p *Peer) start() {

	var extMultiAddr []multiaddr.Multiaddr
	if len(p.extAddr) != 0 {
		for _, v := range p.extAddr {
			addr, err := multiaddr.NewMultiaddr(v)
			if err != nil {
				fmt.Printf("Error creating multiaddress: %v\n", err)
				// extMultiAddr = nil
				continue
			}
			extMultiAddr = append(extMultiAddr, addr)
		}
	}
	if p.storeIdentity {
		if FileExist(p.datadir + "/" + IDpriv) {
			data, err := os.ReadFile(p.datadir + "/" + IDpriv)
			if err != nil {
				panic(err)
			}
			p.nodepriv = string(data)
		} else {
			key := GenprivKey()
			p.nodepriv = key
			os.WriteFile(p.datadir+"/"+IDpriv, []byte(key), os.ModePerm)
		}
	}

	addressFactory := func(addrs []multiaddr.Multiaddr) []multiaddr.Multiaddr {
		if extMultiAddr != nil {
			addrs = append(addrs, extMultiAddr...)
		}
		return addrs
	}

	listen := libp2p.NoListenAddrs

	if p.port != "" {
		listen = libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/" + p.port)
	}
	var allconfig []libp2p.Option
	allconfig = append(allconfig, listen)
	allconfig = append(allconfig, libp2p.Identity(readconfigpriv(p.nodepriv)))
	allconfig = append(allconfig, libp2p.Ping(true))
	allconfig = append(allconfig, libp2p.EnableRelay())
	allconfig = append(allconfig, libp2p.EnableAutoNATv2())
	allconfig = append(allconfig, libp2p.EnableHolePunching())
	allconfig = append(allconfig, libp2p.AddrsFactory(addressFactory))

	if p.useprivnet {
		pkey, err := hex.DecodeString(p.privatenet)
		if err != nil {
			panic(err)
		}
		allconfig = append(allconfig, libp2p.PrivateNetwork(pkey))
	}
	// allconfig = append(allconfig, libp2p.PrivateNetwork(p.privnet))

	Host, err := libp2p.New(
		allconfig...,
	)

	if p.storeIdentity {
		if !FileExist(p.datadir + "/" + IDpub) {
			os.WriteFile(p.datadir+"/"+IDpub, []byte(Host.ID().String()), os.ModePerm)
		}
	}
	if err != nil {
		panic(err)
	}
	ctx := context.Background()

	var kademliaDHT *dht.IpfsDHT
	kademliaDHT = nil
	if len(p.bootstrappeer) != 0 && !p.isBootstrap {
		for _, s := range p.bootstrappeer {
			ma, err := multiaddr.NewMultiaddr(s)
			if err != nil {
				panic(err)
			}
			DefaultBootstrapPeers = append(DefaultBootstrapPeers, ma)
		}

		bootstrapPeers := make([]peer.AddrInfo, len(p.bootstrappeer))

		kademliaDHT, err = dht.New(ctx, Host, dht.BootstrapPeers(bootstrapPeers...))
		if err != nil {
			panic(err)
		}

	}
	if p.isBootstrap {
		kademliaDHT, err = dht.New(ctx, Host, dht.Mode(dht.ModeAutoServer))
		if err != nil {
			panic(err)
		}
	}
	p.dht = kademliaDHT

	if p.isRelay {
		_, err = relay.New(Host, relay.WithInfiniteLimits())
		// re.
		if err != nil {
			fmt.Printf("Failed to instantiate the relay: %v\n", err)
			panic(err)
		}

	}

	if p.enableBitSwapServer {
		//bitswap server
		p.startBitswapDataServer(ctx, Host, p.datadir)
	}
	if p.enableBitSwapClient {
		p.bitclient = p.startBitswapClient(ctx, Host)
	}

	dht_word := "find-dht"
	var routingDiscovery *drouting.RoutingDiscovery

	if kademliaDHT != nil {
		fmt.Println("Bootstrapping the DHT")
		if err = kademliaDHT.Bootstrap(ctx); err != nil {
			panic(err)
		}
		errchan := kademliaDHT.RefreshRoutingTable()
		<-errchan
		// Wait a bit to let bootstrapping finish (really bootstrap should block until it's ready, but that isn't the case yet.)
		time.Sleep(1 * time.Second)

		fmt.Println("Announcing ourselves...")
		routingDiscovery = drouting.NewRoutingDiscovery(kademliaDHT)
		dutil.Advertise(ctx, routingDiscovery, dht_word)
	}

	//using relay
	if len(p.relaypeer) > 0 {
		for _, v := range p.relaypeer {
			//relayv2 Reserve
			_, err = client.Reserve(context.Background(), Host, *getrelayinfo(v))
			if err != nil {
				fmt.Println(" client.Reserve ", err)
			}
		}
	}

	if len(p.bootstrappeer) > 0 {
		go func() {
			for {
				fmt.Println("Searching for other peers...")
				peerChan, err := routingDiscovery.FindPeers(ctx, dht_word)
				if err != nil {
					panic(err)
				}
				connectdhtpeers(Host, peerChan, p.relaypeer)
				time.Sleep(time.Minute * 10)
			}
		}()
	}

	// mdns
	// setup local mDNS discovery
	if p.mdns != "" {
		if err := setupDiscovery(Host, p.mdns); err != nil {
			panic(err)
		}
	}

	//pubsub

	// create a new PubSub service using the GossipSub router
	ps, err := pubsub.NewGossipSub(ctx, Host)
	if err != nil {
		panic(err)
	}

	// join the chat room
	if p.ispeermanage {
		cr, err := joinChatRoom(ctx, ps, Host.ID(), p.Nick, p.room)
		if err != nil {
			panic(err)
		}

		p.PM = newPeerManager(cr, p.event)
		if err = p.PM.run(); err != nil {
			return
		}
	}

	//conmanager
	connectmanager(Host)
	p.H = Host

}

func (p *Peer) startBitswapClient(ctx context.Context, h host.Host) *bsclient.Client {
	if p.dht != nil {
		n := bsnet.NewFromIpfsHost(h, p.dht)
		bswap := bsclient.New(ctx, n, blockstore.NewBlockstore(datastore.NewNullDatastore()))
		n.Start(bswap)
		// defer bswap.Close()
		return bswap
	} else {
		n := bsnet.NewFromIpfsHost(h, routinghelpers.Null{})
		bswap := bsclient.New(ctx, n, blockstore.NewBlockstore(datastore.NewNullDatastore()))
		n.Start(bswap)
		// defer bswap.Close()
		return bswap
	}

}

// Local Network peer finding
func SetPort(port string) Option {
	return func(cfg *Peer) error {
		cfg.port = port
		return nil
	}
}

// Local Network peer finding
func EnableBitSwap(server, client bool) Option {
	return func(cfg *Peer) error {
		cfg.enableBitSwapServer = server
		cfg.enableBitSwapClient = client
		return nil
	}
}

// use priv network
func EnablePrivNet(en bool, pk string) Option {
	return func(cfg *Peer) error {
		cfg.useprivnet = en
		cfg.privatenet = pk
		return nil
	}
}

// Local Network peer finding
func EnablePeerManage(event HandleEvents) Option {
	return func(cfg *Peer) error {
		cfg.event = event
		cfg.ispeermanage = true
		return nil
	}
}

// Local Network peer finding
func EnableMDNS(room, mdns string) Option {
	return func(cfg *Peer) error {
		if cfg.mdns != "" || cfg.room != "" {
			return fmt.Errorf("cannot specify multiple mdns")
		}

		cfg.mdns = mdns
		cfg.room = room
		return nil
	}
}

// Pubaddr
func Pubaddr(pubaddr ...string) Option {
	return func(cfg *Peer) error {
		cfg.extAddr = append(cfg.extAddr, pubaddr...)
		return nil
	}
}

// DHT bootstrap
func EnableDHT(isboot bool, bootstrap ...string) Option {
	return func(cfg *Peer) error {

		cfg.isBootstrap = isboot
		if !isboot {
			cfg.bootstrappeer = append(cfg.bootstrappeer, bootstrap...)
		}
		return nil
	}
}

// DHT bootstrap
func EnableRelay(isrelay bool, bootstrap ...string) Option {
	return func(cfg *Peer) error {
		cfg.isRelay = isrelay
		return nil
	}
}

// pub Relay address
func UseRelay(relay ...string) Option {
	return func(cfg *Peer) error {
		cfg.relaypeer = append(cfg.relaypeer, relay...)
		return nil
	}
}

// Identity configures libp2p to use the given private key to identify itself.
func IdentityCustom(sk string) Option {
	return func(cfg *Peer) error {
		if cfg.nodepriv != "" {
			return fmt.Errorf("cannot specify multiple identities")
		}
		cfg.nodepriv = sk
		return nil
	}
}

// Identity configures libp2p to use the given private key to identify itself.
func IdentityStore() Option {
	return func(cfg *Peer) error {
		if cfg.nodepriv != "" {
			return fmt.Errorf("cannot specify multiple identities")
		}
		cfg.storeIdentity = true
		return nil
	}
}

type Option func(p *Peer) error

// Apply applies the given options to the config, returning the first error
// encountered (if any).
func (p *Peer) Apply(opts ...Option) error {
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(p); err != nil {
			return err
		}
	}
	return nil
}

// 启动服务器的函数
func (p *Peer) startBitswapDataServer(ctx context.Context, h host.Host, datadir string) error {
	// datastore.New
	pebbledb, err := createDB(datadir + "/" + "bistswapdb")
	if err != nil {
		return err
	}
	p.swapstore = pebbledb
	p.cidstore, err = createDB(datadir + "/" + "ciddb")
	if err != nil {
		return err
	}
	p.ds = dsync.MutexWrap(pebbledb)      // 初始化datastore
	p.bs = blockstore.NewBlockstore(p.ds) // 初始化blockstore
	p.bs = blockstore.NewIdStore(p.bs)    // 增加对identity multihash支持

	bsrv := blockservice.New(p.bs, offline.Exchange(p.bs))
	p.dsrv = merkledag.NewDAGService(bsrv) // 将merkledag.DAGService存储为ipld.DAGService
	// 启动Bitswap协议

	if p.dht != nil {
		n := bsnet.NewFromIpfsHost(h, p.dht)
		p.bswap = bsserver.New(ctx, n, p.bs)
		n.Start(p.bswap) // 启动服务器

	} else {
		n := bsnet.NewFromIpfsHost(h, routinghelpers.Null{})
		p.bswap = bsserver.New(ctx, n, p.bs)
		n.Start(p.bswap) // 启动服务器
	}

	fmt.Println("Bitswap服务器已启动")
	if p.dht != nil {
		res := p.Restorecid()
		for _, v := range res {
			cidstring, _ := strings.CutPrefix(v, "/")
			cID, err := cid.Decode(cidstring)
			fmt.Println(cidstring)
			if err != nil {
				panic(err)
			}
			p.dht.Provide(context.Background(), cID, true)
		}
	}

	return nil
}

// 动态添加文件到服务器的方法
func (p *Peer) AddFileToServer(fileReader io.Reader, fname string) (cid.Cid, error) {
	// fileReader := bytes.NewReader(fileBytes) // 读取新的文件

	ufsImportParams := uih.DagBuilderParams{
		Maxlinks:  uih.DefaultLinksPerBlock,
		RawLeaves: true,
		CidBuilder: cid.V1Builder{
			Codec:    uint64(multicodec.DagPb),
			MhType:   uint64(multicodec.Sha2_256),
			MhLength: -1,
		},
		Dagserv: p.dsrv, // 使用ipld.DAGService接口
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
	p.Storecid(fileCID.String(), fname)
	p.swapstore.Sync(context.Background(), datastore.NewKey("")) //flush data
	if p.dht != nil {
		p.dht.Provide(context.Background(), fileCID, true)
	}
	return fileCID, nil
}

// 动态添加文件夹到服务器的方法
func (p *Peer) Folder(fileReader io.Reader, fname string) {

}

func (p *Peer) RemoveFile(id string) error {
	cidstring, _ := strings.CutPrefix(id, "/")
	cID, _ := cid.Decode(cidstring)
	// 从 DAGService 获取块
	node, err := p.dsrv.Get(context.Background(), cID)
	if err != nil {
		fmt.Println("not found", cidstring)
		return errors.New("not found block" + cidstring)
	}
	// 获取子块的 CIDs
	links := node.Links()
	i := 0
	for _, link := range links {
		childCID := link.Cid
		// 递归删除子块

		if err := p.RemoveFile(childCID.String()); err != nil {
			fmt.Println("child err", childCID)
			return err
		}
		i++
		// fmt.Println("remove child success", childCID)
	}

	// p.dsrv.Remove(context.Background(), cID)
	// has, err := p.bs.Has(context.Background(), cID)
	// fmt.Println("found root", cID, has, err)
	// 删除当前块

	if err := p.bs.DeleteBlock(context.Background(), cID); err != nil {
		return fmt.Errorf("failed to delete block %s: %w", cID.String(), err)
	}
	fmt.Printf("cid %s removed with %d links", cID, i)
	p.removecid(cidstring)
	return nil
}
func (p *Peer) Storecid(fid string, filename string) {
	p.cidstore.Put(context.TODO(), datastore.NewKey(fid), []byte(filename))
	p.cidstore.Sync(context.Background(), datastore.NewKey(""))
}

func (p *Peer) removecid(cid string) {
	p.cidstore.Delete(context.TODO(), datastore.NewKey(cid))
	p.cidstore.Sync(context.Background(), datastore.NewKey(""))
}

func (p *Peer) Restorecid() (res []string) {
	ctx := context.Background()
	q := query.Query{
		Prefix: "",
		Orders: []query.Order{query.OrderByKey{}}, // 按键排序
	}
	results, err := p.cidstore.Query(ctx, q)
	if err != nil {
		log.Fatalf("query error: %v", err)
	}
	defer results.Close()
	for result := range results.Next() {
		if result.Error != nil {
			log.Printf("error: %v", result.Error)
			continue
		}
		fmt.Printf("Key: %s, Value: %s\n", result.Entry.Key, result.Entry.Value)
		res = append(res, result.Entry.Key)
	}
	return res
}

func (p *Peer) DownloadfileViaSwap(ctx context.Context, c cid.Cid, tofile string) error {
	if p.dht != nil {
		go func() {
			time.Sleep(time.Second * 2)
			peers, err := p.dht.FindProviders(ctx, c)
			if err != nil {
				fmt.Println(err)
				return
			}
			cpeers := p.H.Network().Peers()

			connections := 0
			needrelaypeers := []peer.AddrInfo{}
			for _, peer1 := range peers {
				find := false
				for _, peer2 := range cpeers {
					if p.H.Network().Connectedness(peer2) == network.Connected {
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
					err := p.H.Connect(context.Background(), peer1)
					if err != nil {
						fmt.Println("connect err:", err)
						unreachable2relayinfo := buildrelayinfo(peer1.ID, p.relaypeer[0])
						if err := p.H.Connect(context.Background(), *unreachable2relayinfo); err != nil {
							log.Printf("Unexpected error here. Failed to connect unreachable1 and unreachable2: %v", err)
							continue
						} else {
							fmt.Println("success connect via relay")
						}
					}
				}
			}

		}()
	}

	dserv := merkledag.NewReadOnlyDagService(merkledag.NewSession(ctx, merkledag.NewDAGService(blockservice.New(blockstore.NewBlockstore(datastore.NewNullDatastore()), p.bitclient))))
	nd, err := dserv.Get(ctx, c)
	if err != nil {
		return err
	}

	unixFSNode, err := unixfile.NewUnixfsFile(ctx, dserv, nd)
	if err != nil {
		return err
	}
	// 创建新文件（目标文件）
	dstFile, err := os.Create(tofile)
	if err != nil {
		fmt.Println("Error creating destination file:", err)
		return err
	}
	defer dstFile.Close() // 确保在函数返回时关闭目标文件

	if f, ok := unixFSNode.(files.File); ok {
		fileSize, err := f.Size()
		if err != nil {
			return err
		}
		// 包装一个TeeReader来监控读取进度
		var totalRead int64
		progressReader := io.TeeReader(f, &progressWriter{dstFile, &totalRead, fileSize})
		if _, err := io.Copy(dstFile, progressReader); err != nil {
			return err
		}
	}

	return nil
}
func (p *Peer) ConnectPeer(id string) (*peer.AddrInfo, error) {
	addr, err := multiaddr.NewMultiaddr(id)
	if err != nil {
		return nil, err
	}
	peerinfo, err := peer.AddrInfoFromP2pAddr(addr)
	if err != nil {
		return nil, err
	}
	return peerinfo, p.H.Connect(context.TODO(), *peerinfo)
}

func FileExist(path string) bool {
	_, err := os.Lstat(path)
	return !os.IsNotExist(err)
}

func Genpsk() pnet.PSK {
	raw, err := GenerateRandomString()
	if err != nil {
		log.Fatal(err)
	}
	psk := pnet.PSK(raw)
	if err != nil {
		log.Fatal(err)
	}
	return psk
}

// GenerateRandomString 生成一个32字节的随机字符串
func GenerateRandomString() ([]byte, error) {
	// 创建一个32字节的切片
	b := make([]byte, 32)

	// 使用随机源填充字节切片
	_, err := rand.Read(b)
	if err != nil {
		return []byte{}, err
	}

	// 将字节切片转换为十六进制字符串
	return b, nil
}
