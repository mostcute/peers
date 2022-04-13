package main

import (
	"encoding/json"
	"fmt"
	"github.com/mostcute/peers"
	"os"
	"sync"
	"time"
)
import "github.com/mostcute/peers/util"

var Dogs sync.Map
var StartTime = time.Now().Unix()

//run env TEST=1 ./main
//run env TEST=2 ./main

func main() {
	var p Peermanager
	pm, _, err := peers.NewPeerNode(util.GetHostip("192.168"), "dogs", "dogs-mdns", p)
	if err != nil {
		return
	}

	go func() {
		printpeers()
	}()
	for {
		db := dogheartbeat{
			Name:      util.GetHostip("192.168") + ":" + os.Getenv("TEST"),
			StartTime: StartTime,
		}
		msgdata, err := db.ToJson()
		if err != nil {
			fmt.Println(err)
		}
		pm.SendMsg(peers.Msg{
			Name: "peerdiscover",
			Data: msgdata,
		})
		time.Sleep(time.Second * 5)
	}
}

type Peermanager struct {
}

func (p Peermanager) HandleEvent(msg *peers.ChatMessage) {
	//receive from event
	m := msg.Message
	switch m.Name {
	case "peerdiscover":
		var a dogheartbeat
		err := a.FromJson(m.Data)
		if err != nil {
			fmt.Println(err)
		}
		a.RecentTime = time.Now().Unix()
		Dogs.Store(a.Name, a)
		//case "othermsg":

	}
}

func (p Peermanager) HandleEventSelf(msg *peers.ChatMessage) {
	//receive from event
	//fmt.Println("handleself")
	m := msg.Message

	switch m.Name {
	case "peerdiscover":
		//fmt.Println(m)
		var a dogheartbeat
		err := a.FromJson(m.Data)
		//fmt.Println("selfname", a.Name)
		if err != nil {
			fmt.Println(err)
		}
		a.RecentTime = time.Now().Unix()
		Dogs.Store(a.Name, a)
		//case "othermsg":

	}
}

type dogheartbeat struct {
	Name       string
	RecentTime int64
	StartTime  int64
	Err        string
}

func (p dogheartbeat) ToJson() ([]byte, error) {
	return json.Marshal(p)
}

func (p *dogheartbeat) FromJson(input []byte) error {
	return json.Unmarshal(input, p)
}

func printpeers() {
	for {
		fmt.Println("online netpeers")
		Dogs.Range(func(key, value any) bool {
			v := value.(dogheartbeat)
			now := time.Now().Unix()
			//disk heartbeat > 10 sec == offline or < -1 sec == time not sync
			if now-v.RecentTime > 10 || now-v.RecentTime < -1 || v.Err != "" {
			} else {
				fmt.Println(v.Name)
			}
			return true
		})
		fmt.Println("offline netpeers")
		Dogs.Range(func(key, value any) bool {
			v := value.(dogheartbeat)
			now := time.Now().Unix()
			//disk heartbeat > 10 sec == offline or < -1 sec == time not sync
			if now-v.RecentTime > 10 || now-v.RecentTime < -1 || v.Err != "" {
				fmt.Println(v.Name)
			} else {

			}
			return true
		})
		time.Sleep(time.Second * 5)
	}

}
