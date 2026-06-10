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

	"github.com/fatedier/frp/pkg/msg"
	httppkg "github.com/fatedier/frp/pkg/util/http"
	netpkg "github.com/fatedier/frp/pkg/util/net"
	pkgutil "github.com/fatedier/frp/pkg/util/util"
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
	subRouter.HandleFunc("/api/reload_tls", httppkg.MakeHTTPHandlerFunc(svr.apiReloadTLS)).Methods("POST")
	// socks5relay disable/enable routes
	subRouter.HandleFunc("/api/socks5relay/group/{group}/disable", httppkg.MakeHTTPHandlerFunc(svr.apiSocks5RelayGroupDisable)).Methods("PUT")
	subRouter.HandleFunc("/api/socks5relay/group/{group}/enable", httppkg.MakeHTTPHandlerFunc(svr.apiSocks5RelayGroupEnable)).Methods("PUT")
	subRouter.HandleFunc("/api/socks5relay/client/{key}/disable", httppkg.MakeHTTPHandlerFunc(svr.apiSocks5RelayClientDisable)).Methods("PUT")
	subRouter.HandleFunc("/api/socks5relay/client/{key}/enable", httppkg.MakeHTTPHandlerFunc(svr.apiSocks5RelayClientEnable)).Methods("PUT")

	// client config view routes
	subRouter.HandleFunc("/api/clients/{key}/config", httppkg.MakeHTTPHandlerFunc(svr.apiClientGetConfig)).Methods("GET")
	subRouter.HandleFunc("/api/clients/{key}/metrics", httppkg.MakeHTTPHandlerFunc(svr.apiClientGetMetrics)).Methods("GET")
	subRouter.HandleFunc("/api/clients/{key}/exit", httppkg.MakeHTTPHandlerFunc(svr.apiClientExit)).Methods("PUT")

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
		groupDisabled := svr.groupRegistry.IsGroupDisabled(group.Name)
		members := make([]model.Socks5RelayGroupMember, 0, len(group.Members))
		for _, member := range group.Members {
			members = append(members, model.Socks5RelayGroupMember{
				RunID:     member.RunID,
				ProxyName: member.ProxyName,
				Online:    member.Online,
				Key:       member.RunID,
				Disabled:  svr.groupRegistry.IsClientDisabled(member.RunID),
			})
		}
		resp = append(resp, model.Socks5RelayGroupInfo{
			Name:     group.Name,
			Members:  members,
			Disabled: groupDisabled,
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

func (svr *Service) apiReloadTLS(ctx *httppkg.Context) (any, error) {
	if err := svr.ReloadTLS(); err != nil {
		return nil, err
	}
	return map[string]string{"msg": "tls reload success"}, nil
}

// apiSocks5RelayEvents is a Server-Sent Events endpoint that streams connection events.
func (svr *Service) apiSocks5RelayEvents(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

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

// apiSocks5RelayGroupDisable disables a SOCKS5 relay group.
func (svr *Service) apiSocks5RelayGroupDisable(ctx *httppkg.Context) (any, error) {
	group := ctx.Param("group")
	if group == "" {
		return nil, fmt.Errorf("missing group name")
	}

	if err := svr.groupRegistry.DisableGroup(group); err != nil {
		return nil, err
	}

	return model.DisableStateResponse{
		Success:  true,
		Group:    group,
		Disabled: true,
	}, nil
}

// apiSocks5RelayGroupEnable enables a SOCKS5 relay group.
func (svr *Service) apiSocks5RelayGroupEnable(ctx *httppkg.Context) (any, error) {
	group := ctx.Param("group")
	if group == "" {
		return nil, fmt.Errorf("missing group name")
	}

	if err := svr.groupRegistry.EnableGroup(group); err != nil {
		return nil, err
	}

	return model.DisableStateResponse{
		Success:  true,
		Group:    group,
		Disabled: false,
	}, nil
}

// apiSocks5RelayClientDisable disables a SOCKS5 relay client.
func (svr *Service) apiSocks5RelayClientDisable(ctx *httppkg.Context) (any, error) {
	key := ctx.Param("key")
	if key == "" {
		return nil, fmt.Errorf("missing client key")
	}

	if err := svr.groupRegistry.DisableClient(key); err != nil {
		return nil, err
	}

	return model.DisableStateResponse{
		Success:  true,
		Key:      key,
		Disabled: true,
	}, nil
}

// apiSocks5RelayClientEnable enables a SOCKS5 relay client.
func (svr *Service) apiSocks5RelayClientEnable(ctx *httppkg.Context) (any, error) {
	key := ctx.Param("key")
	if key == "" {
		return nil, fmt.Errorf("missing client key")
	}

	if err := svr.groupRegistry.EnableClient(key); err != nil {
		return nil, err
	}

	return model.DisableStateResponse{
		Success:  true,
		Key:      key,
		Disabled: false,
	}, nil
}

func newTransactionID() string {
	id, _ := pkgutil.RandID()
	return fmt.Sprintf("%d%s", time.Now().Unix(), id)
}

func (svr *Service) lookupClientControl(key string) (*Control, error) {
	info, ok := svr.clientRegistry.GetByKey(key)
	if !ok {
		return nil, httppkg.NewError(http.StatusNotFound, fmt.Sprintf("client %s not found", key))
	}
	if !info.Online {
		return nil, httppkg.NewError(http.StatusNotFound, fmt.Sprintf("client %s is offline", key))
	}
	ctl, ok := svr.ctlManager.GetByID(info.RunID)
	if !ok {
		return nil, httppkg.NewError(http.StatusNotFound, fmt.Sprintf("client %s control not found", key))
	}
	return ctl, nil
}

func (svr *Service) apiClientGetConfig(ctx *httppkg.Context) (any, error) {
	key := ctx.Param("key")
	if key == "" {
		return nil, fmt.Errorf("missing client key")
	}

	ctl, err := svr.lookupClientControl(key)
	if err != nil {
		return nil, err
	}

	if !ctl.SupportsFeature(msg.FeatureConfig) {
		return nil, httppkg.NewError(http.StatusBadRequest, "client version does not support remote config")
	}

	timeoutCtx, cancel := context.WithTimeout(ctx.Req.Context(), 10*time.Second)
	defer cancel()

	txID := newTransactionID()
	ctl.xl.Debugf("[remote-config] sending GetClientConfig request to client [%s], txID: %s", ctl.runID, txID)
	resp, err := ctl.MsgTransporter().Do(timeoutCtx, &msg.GetClientConfig{TransactionID: txID}, txID, msg.TypeNameGetClientConfigResp)
	if err != nil {
		ctl.xl.Errorf("[remote-config] GetClientConfig request to client [%s] failed, txID: %s: %v", ctl.runID, txID, err)
		return nil, httppkg.NewError(http.StatusGatewayTimeout, fmt.Sprintf("timeout waiting for client response: %v", err))
	}
	ctl.xl.Debugf("[remote-config] GetClientConfig request to client [%s] succeeded, txID: %s", ctl.runID, txID)
	r := resp.(*msg.GetClientConfigResp)
	r.TransactionID = ""
	return r, nil
}

func (svr *Service) apiClientGetMetrics(ctx *httppkg.Context) (any, error) {
	key := ctx.Param("key")
	if key == "" {
		return nil, fmt.Errorf("missing client key")
	}

	ctl, err := svr.lookupClientControl(key)
	if err != nil {
		return nil, err
	}

	if !ctl.SupportsFeature(msg.FeatureMetrics) {
		return nil, httppkg.NewError(http.StatusBadRequest, "client version does not support metrics")
	}

	timeoutCtx, cancel := context.WithTimeout(ctx.Req.Context(), 3*time.Second)
	defer cancel()

	txID := newTransactionID()
	ctl.xl.Debugf("sending ReqClientMetrics request to client [%s], txID: %s", ctl.runID, txID)
	resp, err := ctl.MsgTransporter().Do(timeoutCtx, &msg.ReqClientMetrics{TransactionID: txID}, txID, msg.TypeNameClientMetricsResp)
	if err != nil {
		ctl.xl.Errorf("ReqClientMetrics request to client [%s] failed, txID: %s: %v", ctl.runID, txID, err)
		return nil, httppkg.NewError(http.StatusGatewayTimeout, fmt.Sprintf("timeout waiting for client response: %v", err))
	}
	ctl.xl.Debugf("ReqClientMetrics request to client [%s] succeeded, txID: %s", ctl.runID, txID)
	r := resp.(*msg.ClientMetricsResp)
	r.TransactionID = ""
	return r, nil
}

func (svr *Service) apiClientExit(ctx *httppkg.Context) (any, error) {
	key := ctx.Param("key")
	if key == "" {
		return nil, fmt.Errorf("missing client key")
	}

	ctl, err := svr.lookupClientControl(key)
	if err != nil {
		return nil, err
	}

	if !ctl.SupportsFeature(msg.FeatureExit) {
		return nil, httppkg.NewError(http.StatusBadRequest, "client version does not support remote exit")
	}

	timeoutCtx, cancel := context.WithTimeout(ctx.Req.Context(), 3*time.Second)
	defer cancel()

	txID := newTransactionID()
	ctl.xl.Infof("sending exit request to client [%s], txID: %s", ctl.runID, txID)
	_, err = ctl.MsgTransporter().Do(timeoutCtx, &msg.ReqClientExit{TransactionID: txID}, txID, msg.TypeNameClientExitResp)
	if err != nil {
		ctl.xl.Errorf("exit request to client [%s] failed, txID: %s: %v", ctl.runID, txID, err)
		return nil, httppkg.NewError(http.StatusGatewayTimeout, fmt.Sprintf("timeout waiting for client response: %v", err))
	}

	ctl.xl.Infof("client [%s] acknowledged exit request", ctl.runID)
	return map[string]string{"status": "ok"}, nil
}
