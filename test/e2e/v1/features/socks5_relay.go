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
		// Allocate ports
		socks5Port := f.AllocPort()
		tcpEchoPort := f.AllocPort()

		// Start a TCP echo server as the external target
		f.RunServer("", streamserver.New(streamserver.TCP, streamserver.WithBindPort(tcpEchoPort)))

		// Server config with SOCKS5 proxy port
		serverConf := consts.DefaultServerConfig + fmt.Sprintf(`
socks5ProxyPort = %d
socks5ProxyAuthPassword = "testpass"
`, socks5Port)

		// Client config with socks5_relay proxy
		clientConf := consts.DefaultClientConfig + fmt.Sprintf(`
group = "testgroup"

[[proxies]]
name = "relay-test"
type = "socks5_relay"
`)

		f.RunProcesses(serverConf, []string{clientConf})

		// Wait for proxy to be ready
		time.Sleep(2 * time.Second)

		// Test SOCKS5 proxy: connect to echo server through frps
		framework.NewRequestExpect(f).RequestModify(func(r *request.Request) {
			r.Proxy(fmt.Sprintf("socks5://testgroup:testpass@127.0.0.1:%d", socks5Port)).
				TCP().
				Addr("127.0.0.1").
				Port(tcpEchoPort).
				Body([]byte("hello socks5"))
		}).ExpectResp([]byte("hello socks5")).Ensure()
	})

	ginkgo.It("empty group returns error", func() {
		socks5Port := f.AllocPort()

		serverConf := consts.DefaultServerConfig + fmt.Sprintf(`
socks5ProxyPort = %d
socks5ProxyAuthPassword = "testpass"
`, socks5Port)

		// No client configured — group "nogroup" has no members
		f.RunProcesses(serverConf, []string{consts.DefaultClientConfig})

		time.Sleep(2 * time.Second)

		// SOCKS5 request should fail because no frpc is registered in group "nogroup"
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
