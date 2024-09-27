package main

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	gorpc "github.com/libp2p/go-libp2p-gorpc"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/mostcute/peers"
	"github.com/mostcute/peers/util"
)

var Dogs sync.Map
var StartTime = time.Now().Unix()

//run env TEST=1 ./main
//run env TEST=2 ./main

func main() {
	// server()
	client()
}

var protocolID = protocol.ID("/p2p/rpc/ping")

func server() {

	peer, err := peers.NewPeer(util.GetHostip("192.168"), "/root/code/data/store1",
		peers.SetPort("6663"),
		peers.EnablePrivNet(true,""),
	)
	if err != nil {
		return
	}

	fmt.Println(peers.GetHostAddress(peer.H))
	rpcHost := gorpc.NewServer(peer.H, protocol.ID("/p2p/rpc/ping"))
	svc := PingService{}
	err = rpcHost.Register(&svc)
	if err != nil {
		panic(err)
	}

	fmt.Println("Done")

	select {}
}

func client() {
	peer, err := peers.NewPeer(util.GetHostip("192.168"), "/root/code/data/store1",
		// peers.SetPort("6663"),
		peers.EnablePrivNet(true,""),
	)
	if err != nil {
		return
	}
	peerInfo, _ := peer.ConnectPeer("/ip4/127.0.0.1/tcp/6663/p2p/12D3KooWL1sL75eeLKotae2c35J4onfCAa5z6zCs4k1U1KQyVgrA")

	// peerid := "12D3KooWL1sL75eeLKotae2c35J4onfCAa5z6zCs4k1U1KQyVgrA"
	rpcClient := gorpc.NewClient(peer.H, protocolID)

	numCalls := 0
	durations := []time.Duration{}
	betweenPingsSleep := time.Second * 1

	pingCount := 100
	for numCalls < pingCount {
		var reply PingReply
		var args PingArgs

		c := 64
		b := make([]byte, c)
		_, err := rand.Read(b)
		if err != nil {
			panic(err)
		}

		args.Data = b

		time.Sleep(betweenPingsSleep)
		startTime := time.Now()
		err = rpcClient.Call(peerInfo.ID, "PingService", "Ping", args, &reply)
		if err != nil {
			panic(err)
		}
		if !bytes.Equal(reply.Data, b) {
			panic("Received wrong amount of bytes back!")
		}
		endTime := time.Now()
		diff := endTime.Sub(startTime)
		fmt.Printf("%d bytes from %s (%s): seq=%d time=%s\n", c, peerInfo.ID.String(), peerInfo.Addrs[0].String(), numCalls+1, diff)
		numCalls += 1
		durations = append(durations, diff)
	}

	totalDuration := int64(0)
	for _, dur := range durations {
		totalDuration = totalDuration + dur.Nanoseconds()
	}
	averageDuration := totalDuration / int64(len(durations))
	fmt.Printf("Average duration for ping reply: %s\n", time.Duration(averageDuration))

}
