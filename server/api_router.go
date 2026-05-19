// Copyright 2017 fatedier, fatedier@gmail.com
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

package server

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	httppkg "github.com/fatedier/frp/pkg/util/http"
	netpkg "github.com/fatedier/frp/pkg/util/net"
	adminapi "github.com/fatedier/frp/server/http"
	"github.com/fatedier/frp/server/http/model"
)

func (svr *Service) registerRouteHandlers(helper *httppkg.RouterRegisterHelper) {
	helper.Router.HandleFunc("/healthz", healthz)
	subRouter := helper.Router.NewRoute().Subrouter()

	subRouter.Use(helper.AuthMiddleware)
	subRouter.Use(httppkg.NewRequestLogger)

	// metrics
	if svr.cfg.EnablePrometheus {
		subRouter.Handle("/metrics", promhttp.Handler())
	}

	apiController := adminapi.NewController(svr.cfg, svr.clientRegistry, svr.pxyManager)

	// apis
	subRouter.HandleFunc("/api/serverinfo", httppkg.MakeHTTPHandlerFunc(apiController.APIServerInfo)).Methods("GET")
	subRouter.HandleFunc("/api/proxy/{type}", httppkg.MakeHTTPHandlerFunc(apiController.APIProxyByType)).Methods("GET")
	subRouter.HandleFunc("/api/proxy/{type}/{name}", httppkg.MakeHTTPHandlerFunc(apiController.APIProxyByTypeAndName)).Methods("GET")
	subRouter.HandleFunc("/api/proxies/{name}", httppkg.MakeHTTPHandlerFunc(apiController.APIProxyByName)).Methods("GET")
	subRouter.HandleFunc("/api/traffic/{name}", httppkg.MakeHTTPHandlerFunc(apiController.APIProxyTraffic)).Methods("GET")
	subRouter.HandleFunc("/api/clients", httppkg.MakeHTTPHandlerFunc(apiController.APIClientList)).Methods("GET")
	subRouter.HandleFunc("/api/clients/{key}", httppkg.MakeHTTPHandlerFunc(apiController.APIClientDetail)).Methods("GET")
	subRouter.HandleFunc("/api/proxies", httppkg.MakeHTTPHandlerFunc(apiController.DeleteProxies)).Methods("DELETE")
	subRouter.HandleFunc("/api/socks5relay/groups", httppkg.MakeHTTPHandlerFunc(svr.apiSocks5RelayGroups)).Methods("GET")
	subRouter.HandleFunc("/api/socks5relay/sessions", httppkg.MakeHTTPHandlerFunc(svr.apiSocks5RelaySessions)).Methods("GET")

	// view
	subRouter.Handle("/favicon.ico", http.FileServer(helper.AssetsFS)).Methods("GET")
	subRouter.PathPrefix("/static/").Handler(
		netpkg.MakeHTTPGzipHandler(http.StripPrefix("/static/", http.FileServer(helper.AssetsFS))),
	).Methods("GET")

	subRouter.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/static/", http.StatusMovedPermanently)
	})
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(200)
}

func (svr *Service) apiSocks5RelayGroups(ctx *httppkg.Context) (any, error) {
	groups := svr.groupRegistry.GetAllGroups(func(runID string) bool {
		_, ok := svr.ctlManager.GetByID(runID)
		return ok
	})

	resp := make([]model.Socks5RelayGroupInfo, 0, len(groups))
	for _, group := range groups {
		members := make([]model.Socks5RelayGroupMember, 0, len(group.Members))
		for _, member := range group.Members {
			members = append(members, model.Socks5RelayGroupMember{
				RunID:     member.RunID,
				ProxyName: member.ProxyName,
				Online:    member.Online,
			})
		}
		resp = append(resp, model.Socks5RelayGroupInfo{
			Name:    group.Name,
			Members: members,
		})
	}
	return resp, nil
}

func (svr *Service) apiSocks5RelaySessions(ctx *httppkg.Context) (any, error) {
	sessions := svr.sessionManager.GetAllSessions()

	resp := make([]model.Socks5RelaySessionInfo, 0, len(sessions))
	for username, runID := range sessions {
		resp = append(resp, model.Socks5RelaySessionInfo{
			Username: username,
			RunID:    runID,
		})
	}
	return resp, nil
}
