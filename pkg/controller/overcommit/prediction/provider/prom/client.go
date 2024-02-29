/*
Copyright 2022 The Katalyst Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package prom

import (
	"context"
	"net/http"
	"net/url"

	prometheus "github.com/prometheus/client_golang/api"

	"github.com/kubewharf/katalyst-core/pkg/config/controller"
)

type promAuthClient struct {
	auth   controller.ClientAuth
	client prometheus.Client
}

func newAuthClient(config prometheus.Config, auth controller.ClientAuth) (prometheus.Client, error) {
	client, err := prometheus.NewClient(config)
	if err != nil {
		return nil, err
	}

	return &promAuthClient{
		client: client,
		auth:   auth,
	}, nil
}

func (pc *promAuthClient) URL(ep string, args map[string]string) *url.URL {
	return pc.client.URL(ep, args)
}

func (pc *promAuthClient) Do(ctx context.Context, req *http.Request) (*http.Response, []byte, error) {
	pc.auth.Apply(req)

	return pc.client.Do(ctx, req)
}
