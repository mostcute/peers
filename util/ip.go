package util

import (
	"net"
	"strings"
)

func GetHostip(prefer string) string {
	addrs, err := net.InterfaceAddrs()
	var ip []string
	var ipret string
	if err == nil {
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					//log.Infof("get_hostip:%s", ipnet.IP.String())
					ip = append(ip, ipnet.IP.String())
					//return ipnet.IP.String()
				}
			}
		}
	}
	var ipnil string
	for _, ipmem := range ip { //优先选择10.1网段
		dataindex := strings.Index(ipmem, prefer)
		if dataindex == 0 { //10.1xxx
			//fmt.Println(dataindex)
			ipret = ipmem
		} else {
			ipnil = ipmem
			continue
		}
	}

	if ipret == "" {
		ipret = ipnil
	}
	return ipret
}

func GetHostipList() []string {
	addrs, err := net.InterfaceAddrs()
	var ip []string
	if err == nil {
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					//log.Infof("get_hostip:%s", ipnet.IP.String())
					ip = append(ip, ipnet.IP.String())
					//return ipnet.IP.String()
				}
			}
		}
	}
	return ip
}
