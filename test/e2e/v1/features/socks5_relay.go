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

var _ = ginkgo.Describe("[Feature: Socks5Relay]", func() {
	f := framework.NewDefaultFramework()

	ginkgo.It("SOCKS5 proxy through frpc", func() {
		socks5Port := f.AllocPort()
		tcpEchoPort := f.AllocPort()

		f.RunServer("", streamserver.New(streamserver.TCP, streamserver.WithBindPort(tcpEchoPort)))

		serverConf := consts.DefaultServerConfig + fmt.Sprintf(`
socks5ProxyPort = %d
socks5ProxyAuthPassword = "testpass"
`, socks5Port)

		clientConf := consts.DefaultClientConfig + fmt.Sprintf(`
group = "testgroup"

[[proxies]]
name = "relay-test"
type = "socks5_relay"
`)

		f.RunProcesses(serverConf, []string{clientConf})
		time.Sleep(2 * time.Second)

		framework.NewRequestExpect(f).RequestModify(func(r *request.Request) {
			r.Proxy(fmt.Sprintf("socks5://testgroup:testpass@127.0.0.1:%d", socks5Port)).
				TCP().
				Addr("127.0.0.1").
				Port(tcpEchoPort).
				Body([]byte("hello socks5"))
		}).ExpectResp([]byte("hello socks5")).Ensure()
	})

	ginkgo.It("SOCKS5 session affinity with userID", func() {
		socks5Port := f.AllocPort()
		tcpEchoPort := f.AllocPort()

		f.RunServer("", streamserver.New(streamserver.TCP, streamserver.WithBindPort(tcpEchoPort)))

		serverConf := consts.DefaultServerConfig + fmt.Sprintf(`
socks5ProxyPort = %d
socks5ProxyAuthPassword = "testpass"
`, socks5Port)

		clientConf := consts.DefaultClientConfig + fmt.Sprintf(`
group = "affinitygroup"

[[proxies]]
name = "relay-affinity"
type = "socks5_relay"
`)

		f.RunProcesses(serverConf, []string{clientConf})
		time.Sleep(2 * time.Second)

		// First connection with userID should succeed and create a binding
		framework.NewRequestExpect(f).RequestModify(func(r *request.Request) {
			r.Proxy(fmt.Sprintf("socks5://affinitygroup@userA:testpass@127.0.0.1:%d", socks5Port)).
				TCP().
				Addr("127.0.0.1").
				Port(tcpEchoPort).
				Body([]byte("first"))
		}).ExpectResp([]byte("first")).Ensure()

		// Second connection with same userID should also succeed (session affinity)
		framework.NewRequestExpect(f).RequestModify(func(r *request.Request) {
			r.Proxy(fmt.Sprintf("socks5://affinitygroup@userA:testpass@127.0.0.1:%d", socks5Port)).
				TCP().
				Addr("127.0.0.1").
				Port(tcpEchoPort).
				Body([]byte("second"))
		}).ExpectResp([]byte("second")).Ensure()
	})

	ginkgo.It("HTTP CONNECT proxy through frpc", func() {
		httpConnectPort := f.AllocPort()
		tcpEchoPort := f.AllocPort()

		f.RunServer("", streamserver.New(streamserver.TCP, streamserver.WithBindPort(tcpEchoPort)))

		serverConf := consts.DefaultServerConfig + fmt.Sprintf(`
httpConnectProxyPort = %d
httpConnectProxyAuthPassword = "testpass"
`, httpConnectPort)

		clientConf := consts.DefaultClientConfig + fmt.Sprintf(`
group = "httpgroup"

[[proxies]]
name = "relay-http"
type = "socks5_relay"
`)

		f.RunProcesses(serverConf, []string{clientConf})
		time.Sleep(2 * time.Second)

		// HTTP CONNECT through the proxy
		framework.NewRequestExpect(f).RequestModify(func(r *request.Request) {
			r.Proxy(fmt.Sprintf("http://httpgroup:testpass@127.0.0.1:%d", httpConnectPort)).
				TCP().
				Addr("127.0.0.1").
				Port(tcpEchoPort).
				Body([]byte("hello http connect"))
		}).ExpectResp([]byte("hello http connect")).Ensure()
	})

	ginkgo.It("no available frpc returns error", func() {
		socks5Port := f.AllocPort()

		serverConf := consts.DefaultServerConfig + fmt.Sprintf(`
socks5ProxyPort = %d
socks5ProxyAuthPassword = "testpass"
`, socks5Port)

		// No client configured — group "nogroup" has no members
		f.RunProcesses(serverConf, []string{consts.DefaultClientConfig})
		time.Sleep(2 * time.Second)

		framework.NewRequestExpect(f).RequestModify(func(r *request.Request) {
			r.Proxy(fmt.Sprintf("socks5://nogroup:testpass@127.0.0.1:%d", socks5Port)).
				TCP().
				Addr("127.0.0.1").
				Port(80).
				Body([]byte("test")).
				Timeout(5 * time.Second)
		}).ExpectError(true).Ensure()
	})
})
