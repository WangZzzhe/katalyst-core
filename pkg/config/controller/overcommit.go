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

package controller

import (
	"net/http"
	"time"
)

type OvercommitConfig struct {
	Node NodeOvercommitConfig

	Prediction PredictionConfig
}

type NodeOvercommitConfig struct {
	// number of workers to sync overcommit config
	SyncWorkers int

	// time interval of reconcile overcommit config
	ConfigReconcilePeriod time.Duration
}

type PredictionConfig struct {
	Predictor       string
	PredictPeriod   time.Duration
	ReconcilePeriod time.Duration

	MaxTimeSeriesDuration time.Duration
	MinTimeSeriesDuration time.Duration

	ResourcePortraitNamespace string
	TargetReferenceNameKey    string
	TargetReferenceTypeKey    string
	CPUScaleFactor            float64
	MemoryScaleFactor         float64

	NodeCPUTargetLoad      float64
	NodeMemoryTargetLoad   float64
	PodEstimatedCPULoad    float64
	PodEstimatedMemoryLoad float64

	PromProviderConfig
	NSigmaPredictorConfig
}

type PromProviderConfig struct {
	// prometheus server address
	Address        string
	MaxPointsLimit int
	ClientAuth
}

type ClientAuth struct {
	Username    string
	BearerToken string
	Password    string
}

func (ca *ClientAuth) Apply(req *http.Request) {
	if ca == nil {
		return
	}

	if ca.BearerToken != "" {
		token := "Bearer " + ca.BearerToken
		req.Header.Add("Authorization", token)
	}

	if ca.Username != "" {
		req.SetBasicAuth(ca.Username, ca.Password)
	}
}

type NSigmaPredictorConfig struct {
	Factor  int
	Buckets int
}

func NewOvercommitConfig() *OvercommitConfig {
	return &OvercommitConfig{
		Node:       NodeOvercommitConfig{},
		Prediction: PredictionConfig{},
	}
}
