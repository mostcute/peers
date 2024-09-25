package main

import (
	"context"
	"fmt"

	"github.com/ipfs/go-cid"
	"github.com/mostcute/peers"
	"github.com/mostcute/peers/util"
)

func main() {

	peer, err := peers.NewPeer(util.GetHostip("192.168"), "/root/code/data/store1",
		peers.EnableBitSwap(false, true),
		peers.IdentityStore(),
		peers.EnablePrivNet(true),
	)
	if err != nil {
		return
	}
	//direct connect to resource server
	//in other case,you can use dht/mdns to connect resource peer
	peer.ConnectPeer("/ip4/127.0.0.1/tcp/6663/p2p/12D3KooWLGiv65p7AfTyasMLdGBkE1F6tDdWtFFUXDHEbVY8hDqR")
	err = peer.DownloadfileViaSwap(context.TODO(), cid.MustParse("bafybeie3rjopp2q6clmrs2tsa3blb73f4tamzv7yp5c723n64pqwqryuou"), "/root/code/data/testdownfile")
	if err != nil {
		fmt.Println("err", err)
		return
	}
	fmt.Println("download  success")
	select {}
}
