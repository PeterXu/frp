// Copyright 2026 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package proxy

import (
	"reflect"

	v1 "github.com/fatedier/frp/pkg/config/v1"
)

func init() {
	RegisterProxyFactory(reflect.TypeFor[*v1.Socks5RelayProxyConfig](), NewSocks5RelayServerProxy)
}

type Socks5RelayServerProxy struct {
	*BaseProxy
	cfg *v1.Socks5RelayProxyConfig
}

func NewSocks5RelayServerProxy(baseProxy *BaseProxy) Proxy {
	unwrapped, ok := baseProxy.GetConfigurer().(*v1.Socks5RelayProxyConfig)
	if !ok {
		return nil
	}
	return &Socks5RelayServerProxy{
		BaseProxy: baseProxy,
		cfg:       unwrapped,
	}
}

func (pxy *Socks5RelayServerProxy) Run() (remoteAddr string, err error) {
	rc := pxy.GetResourceController()
	runID := pxy.GetLoginMsg().RunID

	rc.Socks5RelayGroupRegistry.Register(pxy.cfg.Group, runID, pxy.cfg.Name)
	pxy.xl.Infof("socks5_relay proxy registered group [%s] with runID [%s]", pxy.cfg.Group, runID)
	return "", nil
}

func (pxy *Socks5RelayServerProxy) Close() {
	rc := pxy.GetResourceController()
	runID := pxy.GetLoginMsg().RunID

	rc.Socks5RelayGroupRegistry.Unregister(runID)
	rc.Socks5SessionManager.RemoveSession(runID)
	pxy.xl.Infof("socks5_relay proxy unregistered runID [%s]", runID)
	pxy.BaseProxy.Close()
}