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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	httppkg "github.com/fatedier/frp/pkg/util/http"
	netpkg "github.com/fatedier/frp/pkg/util/net"
	adminapi "github.com/fatedier/frp/server/http"
	"github.com/fatedier/frp/server/http/model"
	"github.com/fatedier/frp/server/socks5proxy"
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
	subRouter.HandleFunc("/api/socks5relay/connections", httppkg.MakeHTTPHandlerFunc(svr.apiSocks5RelayConnections)).Methods("GET")
	subRouter.HandleFunc("/api/socks5relay/stats", httppkg.MakeHTTPHandlerFunc(svr.apiSocks5RelayStats)).Methods("GET")
	subRouter.HandleFunc("/api/socks5relay/events", svr.apiSocks5RelayEvents).Methods("GET")
	subRouter.HandleFunc("/api/socks5relay/retention", httppkg.MakeHTTPHandlerFunc(svr.apiSocks5RelayRetention)).Methods("GET")
	subRouter.HandleFunc("/api/socks5relay/retention", httppkg.MakeHTTPHandlerFunc(svr.apiSocks5RelaySetRetention)).Methods("PUT")

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

func (svr *Service) apiSocks5RelayConnections(ctx *httppkg.Context) (any, error) {
	var conns []socks5proxy.RelayConnInfo
	if ctx.Req.URL.Query().Get("active") == "true" {
		conns = svr.connTracker.GetAll()
	} else {
		conns = svr.connTracker.GetAllIncludingClosed()
	}

	resp := make([]model.RelayConnectionInfo, 0, len(conns))
	for _, c := range conns {
		var endTime *int64
		if c.EndTime != nil {
			unix := c.EndTime.Unix()
			endTime = &unix
		}
		resp = append(resp, model.RelayConnectionInfo{
			ID:        c.ID,
			SourceIP:  c.SourceIP,
			Protocol:  c.Protocol,
			Group:     c.Group,
			UserID:    c.UserID,
			DstAddr:   c.DstAddr,
			DstPort:   int(c.DstPort),
			ProxyName: c.ProxyName,
			RunID:     c.RunID,
			StartTime: c.StartTime.Unix(),
			EndTime:   endTime,
			BytesIn:   c.BytesIn,
			BytesOut:  c.BytesOut,
			IsActive:  c.IsActive,
		})
	}
	return resp, nil
}

func (svr *Service) apiSocks5RelayStats(ctx *httppkg.Context) (any, error) {
	conns := svr.connTracker.GetAll()
	stats := model.RelayConnectionStats{
		TotalConnections: len(conns),
	}
	for _, c := range conns {
		stats.TotalBytesIn += c.BytesIn
		stats.TotalBytesOut += c.BytesOut
	}
	return stats, nil
}

func (svr *Service) apiSocks5RelayRetention(ctx *httppkg.Context) (any, error) {
	duration := svr.connTracker.GetRetentionDuration()
	return map[string]int64{"retentionSeconds": int64(duration.Seconds())}, nil
}

func (svr *Service) apiSocks5RelaySetRetention(ctx *httppkg.Context) (any, error) {
	var req struct {
		RetentionSeconds int64 `json:"retentionSeconds"`
	}
	if err := json.NewDecoder(ctx.Req.Body).Decode(&req); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("request body is required")
		}
		return nil, err
	}

	// Validate range: 0 (disabled) to 1 hour
	if req.RetentionSeconds < 0 || req.RetentionSeconds > 3600 {
		return nil, fmt.Errorf("retentionSeconds must be between 0 and 3600")
	}

	svr.connTracker.SetRetentionDuration(time.Duration(req.RetentionSeconds) * time.Second)
	return map[string]int64{"retentionSeconds": req.RetentionSeconds}, nil
}

// apiSocks5RelayEvents is a Server-Sent Events endpoint that streams connection events.
func (svr *Service) apiSocks5RelayEvents(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Create context for this connection
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Flush headers
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Subscribe to connection events
	eventCh := svr.connTracker.Subscribe(ctx)
	defer flusher.Flush()

	// Send initial connected event
	fmt.Fprintf(w, "event: connected\ndata: {}\n\n")
	flusher.Flush()

	// Stream events to client
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-eventCh:
			if !ok {
				return
			}

			// Convert event to SSE format
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}

			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
			flusher.Flush()
		}
	}
}
