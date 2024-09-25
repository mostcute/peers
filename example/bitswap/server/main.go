package main

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/mostcute/peers"
	"github.com/mostcute/peers/util"
)

var Dogs sync.Map
var StartTime = time.Now().Unix()

//run env TEST=1 ./main
//run env TEST=2 ./main

func main() {

	peer, err := peers.NewPeer(util.GetHostip("192.168"), "/root/code/data/store1",
		peers.SetPort("6663"),
		peers.EnableBitSwap(true, false),
		peers.EnablePrivNet(true),
	)
	if err != nil {
		return
	}
	// fmt.Println(peer.H.ID())
	fmt.Println(peers.GetHostAddress(peer.H))
	// peer.RemoveFile("bafybeie3rjopp2q6clmrs2tsa3blb73f4tamzv7yp5c723n64pqwqryuou")

	file := "/root/share/verify_11.08.14.35.apk"
	f, _ := os.Open(file)
	res, _ := peer.AddFileToServer(f, file)
	fmt.Println("added ", res)

	select {}

}
