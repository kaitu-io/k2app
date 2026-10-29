//go:build linux

package gateway

import (
	"log/slog"
	"net"
	"strings"

	"github.com/kaitu-io/k2/safego"
	"github.com/miekg/dns"
)

const mdnsName = "k2.local."

// mdnsAnswerIP 从本机 IP 列表挑首个私有 IPv4 作为 k2.local 应答。
func mdnsAnswerIP(ips []net.IP) net.IP {
	for _, ip := range ips {
		v4 := ip.To4()
		if v4 == nil {
			continue
		}
		if v4.IsPrivate() {
			return v4
		}
	}
	return nil
}

// localPrivateV4s 枚举非虚拟接口上的 IP，供 mdnsAnswerIP 选出网关 LAN IP。
func localPrivateV4s() []net.IP {
	var out []net.IP
	for _, f := range enumerateIfaceAddrs() {
		if isVirtualInterface(f.Name) {
			continue
		}
		for _, a := range f.Addrs {
			if ipnet, ok := a.(*net.IPNet); ok {
				out = append(out, ipnet.IP)
			}
		}
	}
	return out
}

// startMDNS 起 mDNS responder,对 k2.local 的 A 查询回网关 LAN IP。ctx 取消即停。
func (g *Gateway) startMDNS() {
	answer := mdnsAnswerIP(localPrivateV4s())
	if answer == nil {
		slog.Info("mdns: no private v4, responder disabled")
		return
	}
	addr := &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}
	conn, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		slog.Warn("mdns: listen failed", "err", err)
		return
	}
	safego.Go(func() {
		<-g.ctx.Done()
		conn.Close()
	})
	safego.Go(func() {
		buf := make([]byte, 1500)
		for {
			n, src, err := conn.ReadFromUDP(buf)
			if err != nil {
				return // conn closed
			}
			var msg dns.Msg
			if msg.Unpack(buf[:n]) != nil || len(msg.Question) == 0 {
				continue
			}
			for _, q := range msg.Question {
				if q.Qtype == dns.TypeA && strings.EqualFold(q.Name, mdnsName) {
					resp := new(dns.Msg)
					resp.SetReply(&msg)
					resp.Answer = append(resp.Answer, &dns.A{
						Hdr: dns.RR_Header{Name: mdnsName, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 120},
						A:   answer,
					})
					if packed, err := resp.Pack(); err == nil {
						conn.WriteToUDP(packed, src)
					}
				}
			}
		}
	})
	slog.Info("mdns: responder started", "name", mdnsName, "ip", answer.String())
}
