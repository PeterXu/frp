// Copyright 2026 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package features

import (
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"

	"github.com/fatedier/frp/test/e2e/framework"
	"github.com/fatedier/frp/test/e2e/framework/consts"
	"github.com/fatedier/frp/test/e2e/mock/server/streamserver"
	"github.com/fatedier/frp/test/e2e/pkg/request"
)

var _ = ginkgo.Describe("[Feature: Socks5Visitor]", func() {
	f := framework.NewDefaultFramework()

	ginkgo.It("forwards SOCKS5 client through frpc1 visitor to frpc2 relay", func() {
		visitorPort := f.AllocPort()
		tcpEchoPort := f.AllocPort()
		f.RunServer("", streamserver.New(streamserver.TCP, streamserver.WithBindPort(tcpEchoPort)))

		serverConf := consts.DefaultServerConfig

		// NOTE: `group` lives at the top level of ClientCommonConfig, NOT inside [[proxies]].
		// `serverName` is required by visitor validation even when the SOCKS5 client
		// supplies its own group via the username field.
		frpc2Conf := consts.DefaultClientConfig + `
group = "echo-group"

[[proxies]]
name = "relay-echo"
type = "socks5_relay"
`
		frpc1Conf := consts.DefaultClientConfig + fmt.Sprintf(`
[[visitors]]
name = "socks5-entry"
type = "socks5"
bindAddr = "127.0.0.1"
bindPort = %d
authPassword = "shared-secret"
serverName = "echo-group"
`, visitorPort)

		f.RunProcesses(serverConf, []string{frpc2Conf, frpc1Conf})
		time.Sleep(2 * time.Second)

		framework.NewRequestExpect(f).RequestModify(func(r *request.Request) {
			// SOCKS5 username selects the group on frps. It must match the
			// `group` value configured on frpc2.
			r.Proxy(fmt.Sprintf("socks5://echo-group:shared-secret@127.0.0.1:%d", visitorPort)).
				TCP().
				Addr("127.0.0.1").
				Port(tcpEchoPort).
				Body([]byte("hello socks5 visitor"))
		}).ExpectResp([]byte("hello socks5 visitor")).Ensure()
	})

	ginkgo.It("round-robin across multiple frpc2 in group via SOCKS5 username", func() {
		visitorPort := f.AllocPort()
		tcpEchoPort := f.AllocPort()
		f.RunServer("", streamserver.New(streamserver.TCP, streamserver.WithBindPort(tcpEchoPort)))

		serverConf := consts.DefaultServerConfig

		// Two frpc2 instances in the same group, each with a distinct `user`
		// so frps can distinguish them.
		frpc2aConf := consts.DefaultClientConfig + `
user = "host-a"
group = "rr-group"

[[proxies]]
name = "relay-a"
type = "socks5_relay"
`
		frpc2bConf := consts.DefaultClientConfig + `
user = "host-b"
group = "rr-group"

[[proxies]]
name = "relay-b"
type = "socks5_relay"
`
		frpc1Conf := consts.DefaultClientConfig + fmt.Sprintf(`
[[visitors]]
name = "socks5-entry"
type = "socks5"
bindAddr = "127.0.0.1"
bindPort = %d
authPassword = "shared-secret"
serverName = "rr-group"
`, visitorPort)

		f.RunProcesses(serverConf, []string{frpc2aConf, frpc2bConf, frpc1Conf})
		time.Sleep(2 * time.Second)

		// SOCKS5 username is the group name; frps round-robins across members.
		for i := 0; i < 4; i++ {
			framework.NewRequestExpect(f).RequestModify(func(r *request.Request) {
				r.Proxy(fmt.Sprintf("socks5://rr-group:shared-secret@127.0.0.1:%d", visitorPort)).
					TCP().
					Addr("127.0.0.1").
					Port(tcpEchoPort).
					Body([]byte("rr"))
			}).ExpectResp([]byte("rr")).Ensure()
		}
	})

	ginkgo.It("direct targeting via group=targetUser", func() {
		visitorPort := f.AllocPort()
		tcpEchoPort := f.AllocPort()
		f.RunServer("", streamserver.New(streamserver.TCP, streamserver.WithBindPort(tcpEchoPort)))

		serverConf := consts.DefaultServerConfig

		frpc2aConf := consts.DefaultClientConfig + `
user = "host-a"
group = "dt-group"

[[proxies]]
name = "relay-a"
type = "socks5_relay"
`
		frpc2bConf := consts.DefaultClientConfig + `
user = "host-b"
group = "dt-group"

[[proxies]]
name = "relay-b"
type = "socks5_relay"
`
		frpc1Conf := consts.DefaultClientConfig + fmt.Sprintf(`
[[visitors]]
name = "socks5-entry"
type = "socks5"
bindAddr = "127.0.0.1"
bindPort = %d
authPassword = "shared-secret"
serverName = "dt-group"
`, visitorPort)

		f.RunProcesses(serverConf, []string{frpc2aConf, frpc2bConf, frpc1Conf})
		time.Sleep(2 * time.Second)

		// SOCKS5 username "dt-group=host-b" pins the request to frpc2 host-b
		// instead of round-robining across the group.
		framework.NewRequestExpect(f).RequestModify(func(r *request.Request) {
			r.Proxy(fmt.Sprintf("socks5://dt-group=host-b:shared-secret@127.0.0.1:%d", visitorPort)).
				TCP().
				Addr("127.0.0.1").
				Port(tcpEchoPort).
				Body([]byte("dt"))
		}).ExpectResp([]byte("dt")).Ensure()
	})

	ginkgo.It("rejects SOCKS5 client with wrong auth password", func() {
		visitorPort := f.AllocPort()
		serverConf := consts.DefaultServerConfig

		// No frpc2 is registered — the test fails at the SOCKS5 auth layer
		// before any group selection happens, so a missing relay is fine.
		frpc1Conf := consts.DefaultClientConfig + fmt.Sprintf(`
[[visitors]]
name = "socks5-entry"
type = "socks5"
bindAddr = "127.0.0.1"
bindPort = %d
authPassword = "shared-secret"
serverName = "any-group"
`, visitorPort)
		f.RunProcesses(serverConf, []string{frpc1Conf})
		time.Sleep(2 * time.Second)

		// SOCKS5 password "wrong-password" must not match the visitor's
		// configured AuthPassword "shared-secret" -> the visitor's SOCKS5
		// Authenticate step fails and the client sees a connection error.
		framework.NewRequestExpect(f).RequestModify(func(r *request.Request) {
			r.Proxy(fmt.Sprintf("socks5://x:wrong-password@127.0.0.1:%d", visitorPort)).
				TCP().
				Addr("127.0.0.1").
				Port(1).
				Body([]byte("nope"))
		}).ExpectError(true).Ensure()
	})

	ginkgo.It("returns SOCKS5 failure reply when no frpc2 in group", func() {
		visitorPort := f.AllocPort()
		tcpEchoPort := f.AllocPort()
		f.RunServer("", streamserver.New(streamserver.TCP, streamserver.WithBindPort(tcpEchoPort)))

		serverConf := consts.DefaultServerConfig

		// No frpc2 ever joins group "ghost". frps selectFrpcFn will return
		// "no available frpc in group" and the visitor must surface that as
		// a SOCKS5 general failure reply (0x01).
		frpc1Conf := consts.DefaultClientConfig + fmt.Sprintf(`
[[visitors]]
name = "socks5-entry"
type = "socks5"
bindAddr = "127.0.0.1"
bindPort = %d
authPassword = "shared-secret"
serverName = "ghost"
`, visitorPort)
		f.RunProcesses(serverConf, []string{frpc1Conf})
		time.Sleep(2 * time.Second)

		framework.NewRequestExpect(f).RequestModify(func(r *request.Request) {
			r.Proxy(fmt.Sprintf("socks5://ghost:shared-secret@127.0.0.1:%d", visitorPort)).
				TCP().
				Addr("127.0.0.1").
				Port(tcpEchoPort).
				Body([]byte("x"))
		}).ExpectError(true).Ensure()
	})
})
