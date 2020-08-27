// Copyright 2017 Oracle and/or its affiliates. All rights reserved.
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

package oci

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	providercfg "github.com/oracle/oci-cloud-controller-manager/pkg/cloudprovider/providers/oci/config"
	"github.com/oracle/oci-go-sdk/common"
	"github.com/oracle/oci-go-sdk/loadbalancer"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

var (
	backendSecret  = "backendsecret"
	listenerSecret = "listenersecret"
)

type mockSSLSecretReader struct {
	returnError bool

	returnMap map[struct {
		namespaceArg string
		nameArg      string
	}]*certificateData
}

func (ssr mockSSLSecretReader) readSSLSecret(ns, name string) (sslSecret *certificateData, err error) {
	if ssr.returnError {
		return nil, errors.New("Oops, something went wrong")
	}
	for key, returnValue := range ssr.returnMap {
		if key.namespaceArg == ns && key.nameArg == name {
			return returnValue, nil
		}
	}
	return nil, nil
}

func TestNewLBSpecSuccess(t *testing.T) {
	testCases := map[string]struct {
		defaultSubnetOne string
		defaultSubnetTwo string
		nodes            []*v1.Node
		service          *v1.Service
		expected         *LBSpec
		sslConfig        *SSLConfig
	}{
		"defaults": {
			defaultSubnetOne: "one",
			defaultSubnetTwo: "two",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "kube-system",
					Name:        "testservice",
					UID:         "test-uid",
					Annotations: map[string]string{},
				},
				Spec: v1.ServiceSpec{
					SessionAffinity: v1.ServiceAffinityNone,
					Ports: []v1.ServicePort{
						{
							Protocol: v1.ProtocolTCP,
							Port:     int32(80),
						},
					},
				},
			},
			expected: &LBSpec{
				Name:     "test-uid",
				Shape:    "100Mbps",
				Internal: false,
				Subnets:  []string{"one", "two"},
				Listeners: map[string]loadbalancer.ListenerDetails{
					"TCP-80": {
						DefaultBackendSetName: common.String("TCP-80"),
						Port:                  common.Int(80),
						Protocol:              common.String("TCP"),
					},
				},
				BackendSets: map[string]loadbalancer.BackendSetDetails{
					"TCP-80": {
						Backends: []loadbalancer.BackendDetails{},
						HealthChecker: &loadbalancer.HealthCheckerDetails{
							Protocol:         common.String("HTTP"),
							Port:             common.Int(10256),
							UrlPath:          common.String("/healthz"),
							Retries:          common.Int(3),
							TimeoutInMillis:  common.Int(3000),
							IntervalInMillis: common.Int(10000),
						},
						Policy: common.String("ROUND_ROBIN"),
					},
				},
				SourceCIDRs: []string{"0.0.0.0/0"},
				Ports: map[string]portSpec{
					"TCP-80": {
						ListenerPort:      80,
						HealthCheckerPort: 10256,
					},
				},
				securityListManager: newSecurityListManagerNOOP(),
			},
		},
		"internal with default subnet": {
			defaultSubnetOne: "one",
			defaultSubnetTwo: "two",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "kube-system",
					Name:      "testservice",
					UID:       "test-uid",
					Annotations: map[string]string{
						ServiceAnnotationLoadBalancerInternal: "",
					},
				},
				Spec: v1.ServiceSpec{
					SessionAffinity: v1.ServiceAffinityNone,
					Ports: []v1.ServicePort{
						{
							Protocol: v1.ProtocolTCP,
							Port:     int32(80),
						},
					},
				},
			},
			expected: &LBSpec{
				Name:     "test-uid",
				Shape:    "100Mbps",
				Internal: true,
				Subnets:  []string{"one"},
				Listeners: map[string]loadbalancer.ListenerDetails{
					"TCP-80": {
						DefaultBackendSetName: common.String("TCP-80"),
						Port:                  common.Int(80),
						Protocol:              common.String("TCP"),
					},
				},
				BackendSets: map[string]loadbalancer.BackendSetDetails{
					"TCP-80": {
						Backends: []loadbalancer.BackendDetails{},
						HealthChecker: &loadbalancer.HealthCheckerDetails{
							Protocol:         common.String("HTTP"),
							Port:             common.Int(10256),
							UrlPath:          common.String("/healthz"),
							Retries:          common.Int(3),
							TimeoutInMillis:  common.Int(3000),
							IntervalInMillis: common.Int(10000),
						},
						Policy: common.String("ROUND_ROBIN"),
					},
				},
				SourceCIDRs: []string{"0.0.0.0/0"},
				Ports: map[string]portSpec{
					"TCP-80": {
						ListenerPort:      80,
						HealthCheckerPort: 10256,
					},
				},
				securityListManager: newSecurityListManagerNOOP(),
			},
		},
		"internal with overridden regional subnet1": {
			defaultSubnetOne: "one",
			defaultSubnetTwo: "two",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "kube-system",
					Name:      "testservice",
					UID:       "test-uid",
					Annotations: map[string]string{
						ServiceAnnotationLoadBalancerInternal: "",
						ServiceAnnotationLoadBalancerSubnet1:  "regional-subnet",
					},
				},
				Spec: v1.ServiceSpec{
					SessionAffinity: v1.ServiceAffinityNone,
					Ports: []v1.ServicePort{
						{
							Protocol: v1.ProtocolTCP,
							Port:     int32(80),
						},
					},
				},
			},
			expected: &LBSpec{
				Name:     "test-uid",
				Shape:    "100Mbps",
				Internal: true,
				Subnets:  []string{"regional-subnet"},
				Listeners: map[string]loadbalancer.ListenerDetails{
					"TCP-80": {
						DefaultBackendSetName: common.String("TCP-80"),
						Port:                  common.Int(80),
						Protocol:              common.String("TCP"),
					},
				},
				BackendSets: map[string]loadbalancer.BackendSetDetails{
					"TCP-80": {
						Backends: []loadbalancer.BackendDetails{},
						HealthChecker: &loadbalancer.HealthCheckerDetails{
							Protocol:         common.String("HTTP"),
							Port:             common.Int(10256),
							UrlPath:          common.String("/healthz"),
							Retries:          common.Int(3),
							TimeoutInMillis:  common.Int(3000),
							IntervalInMillis: common.Int(10000),
						},
						Policy: common.String("ROUND_ROBIN"),
					},
				},
				SourceCIDRs: []string{"0.0.0.0/0"},
				Ports: map[string]portSpec{
					"TCP-80": {
						ListenerPort:      80,
						HealthCheckerPort: 10256,
					},
				},
				securityListManager: newSecurityListManagerNOOP(),
			},
		},
		"internal with overridden regional subnet2": {
			defaultSubnetOne: "one",
			defaultSubnetTwo: "two",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "kube-system",
					Name:      "testservice",
					UID:       "test-uid",
					Annotations: map[string]string{
						ServiceAnnotationLoadBalancerInternal: "",
						ServiceAnnotationLoadBalancerSubnet2:  "regional-subnet",
					},
				},
				Spec: v1.ServiceSpec{
					SessionAffinity: v1.ServiceAffinityNone,
					Ports: []v1.ServicePort{
						{
							Protocol: v1.ProtocolTCP,
							Port:     int32(80),
						},
					},
				},
			},
			expected: &LBSpec{
				Name:     "test-uid",
				Shape:    "100Mbps",
				Internal: true,
				Subnets:  []string{"regional-subnet"},
				Listeners: map[string]loadbalancer.ListenerDetails{
					"TCP-80": {
						DefaultBackendSetName: common.String("TCP-80"),
						Port:                  common.Int(80),
						Protocol:              common.String("TCP"),
					},
				},
				BackendSets: map[string]loadbalancer.BackendSetDetails{
					"TCP-80": {
						Backends: []loadbalancer.BackendDetails{},
						HealthChecker: &loadbalancer.HealthCheckerDetails{
							Protocol:         common.String("HTTP"),
							Port:             common.Int(10256),
							UrlPath:          common.String("/healthz"),
							Retries:          common.Int(3),
							TimeoutInMillis:  common.Int(3000),
							IntervalInMillis: common.Int(10000),
						},
						Policy: common.String("ROUND_ROBIN"),
					},
				},
				SourceCIDRs: []string{"0.0.0.0/0"},
				Ports: map[string]portSpec{
					"TCP-80": {
						ListenerPort:      80,
						HealthCheckerPort: 10256,
					},
				},
				securityListManager: newSecurityListManagerNOOP(),
			},
		},
		"internal with no default subnets provide subnet1 via annotation": {
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "kube-system",
					Name:      "testservice",
					UID:       "test-uid",
					Annotations: map[string]string{
						ServiceAnnotationLoadBalancerInternal: "",
						ServiceAnnotationLoadBalancerSubnet1:  "annotation-one",
					},
				},
				Spec: v1.ServiceSpec{
					SessionAffinity: v1.ServiceAffinityNone,
					Ports: []v1.ServicePort{
						{
							Protocol: v1.ProtocolTCP,
							Port:     int32(80),
						},
					},
				},
			},
			expected: &LBSpec{
				Name:     "test-uid",
				Shape:    "100Mbps",
				Internal: true,
				Subnets:  []string{"annotation-one"},
				Listeners: map[string]loadbalancer.ListenerDetails{
					"TCP-80": {
						DefaultBackendSetName: common.String("TCP-80"),
						Port:                  common.Int(80),
						Protocol:              common.String("TCP"),
					},
				},
				BackendSets: map[string]loadbalancer.BackendSetDetails{
					"TCP-80": {
						Backends: []loadbalancer.BackendDetails{},
						HealthChecker: &loadbalancer.HealthCheckerDetails{
							Protocol:         common.String("HTTP"),
							Port:             common.Int(10256),
							UrlPath:          common.String("/healthz"),
							Retries:          common.Int(3),
							TimeoutInMillis:  common.Int(3000),
							IntervalInMillis: common.Int(10000),
						},
						Policy: common.String("ROUND_ROBIN"),
					},
				},
				SourceCIDRs: []string{"0.0.0.0/0"},
				Ports: map[string]portSpec{
					"TCP-80": {
						ListenerPort:      80,
						HealthCheckerPort: 10256,
					},
				},
				securityListManager: newSecurityListManagerNOOP(),
			},
		},
		"use default subnet in case of no subnet overrides via annotation": {
			defaultSubnetOne: "one",
			defaultSubnetTwo: "two",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "kube-system",
					Name:        "testservice",
					UID:         "test-uid",
					Annotations: map[string]string{},
				},
				Spec: v1.ServiceSpec{
					SessionAffinity: v1.ServiceAffinityNone,
					Ports: []v1.ServicePort{
						v1.ServicePort{
							Protocol: v1.ProtocolTCP,
							Port:     int32(80),
						},
					},
				},
			},
			expected: &LBSpec{
				Name:     "test-uid",
				Shape:    "100Mbps",
				Internal: false,
				Subnets:  []string{"one", "two"},
				Listeners: map[string]loadbalancer.ListenerDetails{
					"TCP-80": loadbalancer.ListenerDetails{
						DefaultBackendSetName: common.String("TCP-80"),
						Port:                  common.Int(80),
						Protocol:              common.String("TCP"),
					},
				},
				BackendSets: map[string]loadbalancer.BackendSetDetails{
					"TCP-80": loadbalancer.BackendSetDetails{
						Backends: []loadbalancer.BackendDetails{},
						HealthChecker: &loadbalancer.HealthCheckerDetails{
							Protocol:         common.String("HTTP"),
							Port:             common.Int(10256),
							UrlPath:          common.String("/healthz"),
							Retries:          common.Int(3),
							TimeoutInMillis:  common.Int(3000),
							IntervalInMillis: common.Int(10000),
						},
						Policy: common.String("ROUND_ROBIN"),
					},
				},
				SourceCIDRs: []string{"0.0.0.0/0"},
				Ports: map[string]portSpec{
					"TCP-80": portSpec{
						ListenerPort:      80,
						HealthCheckerPort: 10256,
					},
				},
				securityListManager: newSecurityListManagerNOOP(),
			},
		},
		"no default subnets provide subnet1 via annotation as regional-subnet": {
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "kube-system",
					Name:      "testservice",
					UID:       "test-uid",
					Annotations: map[string]string{
						ServiceAnnotationLoadBalancerSubnet1: "regional-subnet",
					},
				},
				Spec: v1.ServiceSpec{
					SessionAffinity: v1.ServiceAffinityNone,
					Ports: []v1.ServicePort{
						v1.ServicePort{
							Protocol: v1.ProtocolTCP,
							Port:     int32(80),
						},
					},
				},
			},
			expected: &LBSpec{
				Name:     "test-uid",
				Shape:    "100Mbps",
				Internal: false,
				Subnets:  []string{"regional-subnet"},
				Listeners: map[string]loadbalancer.ListenerDetails{
					"TCP-80": loadbalancer.ListenerDetails{
						DefaultBackendSetName: common.String("TCP-80"),
						Port:                  common.Int(80),
						Protocol:              common.String("TCP"),
					},
				},
				BackendSets: map[string]loadbalancer.BackendSetDetails{
					"TCP-80": loadbalancer.BackendSetDetails{
						Backends: []loadbalancer.BackendDetails{},
						HealthChecker: &loadbalancer.HealthCheckerDetails{
							Protocol:         common.String("HTTP"),
							Port:             common.Int(10256),
							UrlPath:          common.String("/healthz"),
							Retries:          common.Int(3),
							TimeoutInMillis:  common.Int(3000),
							IntervalInMillis: common.Int(10000),
						},
						Policy: common.String("ROUND_ROBIN"),
					},
				},
				SourceCIDRs: []string{"0.0.0.0/0"},
				Ports: map[string]portSpec{
					"TCP-80": portSpec{
						ListenerPort:      80,
						HealthCheckerPort: 10256,
					},
				},
				securityListManager: newSecurityListManagerNOOP(),
			},
		},
		"no default subnets provide subnet2 via annotation": {
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "kube-system",
					Name:      "testservice",
					UID:       "test-uid",
					Annotations: map[string]string{
						ServiceAnnotationLoadBalancerSubnet2: "annotation-two",
					},
				},
				Spec: v1.ServiceSpec{
					SessionAffinity: v1.ServiceAffinityNone,
					Ports: []v1.ServicePort{
						v1.ServicePort{
							Protocol: v1.ProtocolTCP,
							Port:     int32(80),
						},
					},
				},
			},
			expected: &LBSpec{
				Name:     "test-uid",
				Shape:    "100Mbps",
				Internal: false,
				Subnets:  []string{"", "annotation-two"},
				Listeners: map[string]loadbalancer.ListenerDetails{
					"TCP-80": loadbalancer.ListenerDetails{
						DefaultBackendSetName: common.String("TCP-80"),
						Port:                  common.Int(80),
						Protocol:              common.String("TCP"),
					},
				},
				BackendSets: map[string]loadbalancer.BackendSetDetails{
					"TCP-80": loadbalancer.BackendSetDetails{
						Backends: []loadbalancer.BackendDetails{},
						HealthChecker: &loadbalancer.HealthCheckerDetails{
							Protocol:         common.String("HTTP"),
							Port:             common.Int(10256),
							UrlPath:          common.String("/healthz"),
							Retries:          common.Int(3),
							TimeoutInMillis:  common.Int(3000),
							IntervalInMillis: common.Int(10000),
						},
						Policy: common.String("ROUND_ROBIN"),
					},
				},
				SourceCIDRs: []string{"0.0.0.0/0"},
				Ports: map[string]portSpec{
					"TCP-80": portSpec{
						ListenerPort:      80,
						HealthCheckerPort: 10256,
					},
				},
				securityListManager: newSecurityListManagerNOOP(),
			},
		},
		"override default subnet via subnet1 annotation as regional subnet": {
			defaultSubnetOne: "one",
			defaultSubnetTwo: "two",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "kube-system",
					Name:      "testservice",
					UID:       "test-uid",
					Annotations: map[string]string{
						ServiceAnnotationLoadBalancerSubnet1: "regional-subnet",
					},
				},
				Spec: v1.ServiceSpec{
					SessionAffinity: v1.ServiceAffinityNone,
					Ports: []v1.ServicePort{
						v1.ServicePort{
							Protocol: v1.ProtocolTCP,
							Port:     int32(80),
						},
					},
				},
			},
			expected: &LBSpec{
				Name:     "test-uid",
				Shape:    "100Mbps",
				Internal: false,
				Subnets:  []string{"regional-subnet"},
				Listeners: map[string]loadbalancer.ListenerDetails{
					"TCP-80": loadbalancer.ListenerDetails{
						DefaultBackendSetName: common.String("TCP-80"),
						Port:                  common.Int(80),
						Protocol:              common.String("TCP"),
					},
				},
				BackendSets: map[string]loadbalancer.BackendSetDetails{
					"TCP-80": loadbalancer.BackendSetDetails{
						Backends: []loadbalancer.BackendDetails{},
						HealthChecker: &loadbalancer.HealthCheckerDetails{
							Protocol:         common.String("HTTP"),
							Port:             common.Int(10256),
							UrlPath:          common.String("/healthz"),
							Retries:          common.Int(3),
							TimeoutInMillis:  common.Int(3000),
							IntervalInMillis: common.Int(10000),
						},
						Policy: common.String("ROUND_ROBIN"),
					},
				},
				SourceCIDRs: []string{"0.0.0.0/0"},
				Ports: map[string]portSpec{
					"TCP-80": portSpec{
						ListenerPort:      80,
						HealthCheckerPort: 10256,
					},
				},
				securityListManager: newSecurityListManagerNOOP(),
			},
		},
		"override default subnet via subnet2 annotation as regional subnet": {
			defaultSubnetOne: "one",
			defaultSubnetTwo: "two",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "kube-system",
					Name:      "testservice",
					UID:       "test-uid",
					Annotations: map[string]string{
						ServiceAnnotationLoadBalancerSubnet2: "regional-subnet",
					},
				},
				Spec: v1.ServiceSpec{
					SessionAffinity: v1.ServiceAffinityNone,
					Ports: []v1.ServicePort{
						v1.ServicePort{
							Protocol: v1.ProtocolTCP,
							Port:     int32(80),
						},
					},
				},
			},
			expected: &LBSpec{
				Name:     "test-uid",
				Shape:    "100Mbps",
				Internal: false,
				Subnets:  []string{"regional-subnet"},
				Listeners: map[string]loadbalancer.ListenerDetails{
					"TCP-80": loadbalancer.ListenerDetails{
						DefaultBackendSetName: common.String("TCP-80"),
						Port:                  common.Int(80),
						Protocol:              common.String("TCP"),
					},
				},
				BackendSets: map[string]loadbalancer.BackendSetDetails{
					"TCP-80": loadbalancer.BackendSetDetails{
						Backends: []loadbalancer.BackendDetails{},
						HealthChecker: &loadbalancer.HealthCheckerDetails{
							Protocol:         common.String("HTTP"),
							Port:             common.Int(10256),
							UrlPath:          common.String("/healthz"),
							Retries:          common.Int(3),
							TimeoutInMillis:  common.Int(3000),
							IntervalInMillis: common.Int(10000),
						},
						Policy: common.String("ROUND_ROBIN"),
					},
				},
				SourceCIDRs: []string{"0.0.0.0/0"},
				Ports: map[string]portSpec{
					"TCP-80": portSpec{
						ListenerPort:      80,
						HealthCheckerPort: 10256,
					},
				},
				securityListManager: newSecurityListManagerNOOP(),
			},
		},
		"override default subnet via subnet1 and subnet2 annotation": {
			defaultSubnetOne: "one",
			defaultSubnetTwo: "two",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "kube-system",
					Name:      "testservice",
					UID:       "test-uid",
					Annotations: map[string]string{
						ServiceAnnotationLoadBalancerSubnet1: "annotation-one",
						ServiceAnnotationLoadBalancerSubnet2: "annotation-two",
					},
				},
				Spec: v1.ServiceSpec{
					SessionAffinity: v1.ServiceAffinityNone,
					Ports: []v1.ServicePort{
						v1.ServicePort{
							Protocol: v1.ProtocolTCP,
							Port:     int32(80),
						},
					},
				},
			},
			expected: &LBSpec{
				Name:     "test-uid",
				Shape:    "100Mbps",
				Internal: false,
				Subnets:  []string{"annotation-one", "annotation-two"},
				Listeners: map[string]loadbalancer.ListenerDetails{
					"TCP-80": loadbalancer.ListenerDetails{
						DefaultBackendSetName: common.String("TCP-80"),
						Port:                  common.Int(80),
						Protocol:              common.String("TCP"),
					},
				},
				BackendSets: map[string]loadbalancer.BackendSetDetails{
					"TCP-80": loadbalancer.BackendSetDetails{
						Backends: []loadbalancer.BackendDetails{},
						HealthChecker: &loadbalancer.HealthCheckerDetails{
							Protocol:         common.String("HTTP"),
							Port:             common.Int(10256),
							UrlPath:          common.String("/healthz"),
							Retries:          common.Int(3),
							TimeoutInMillis:  common.Int(3000),
							IntervalInMillis: common.Int(10000),
						},
						Policy: common.String("ROUND_ROBIN"),
					},
				},
				SourceCIDRs: []string{"0.0.0.0/0"},
				Ports: map[string]portSpec{
					"TCP-80": portSpec{
						ListenerPort:      80,
						HealthCheckerPort: 10256,
					},
				},
				securityListManager: newSecurityListManagerNOOP(),
			},
		},
		//"security list manager annotation":
		"custom shape": {
			defaultSubnetOne: "one",
			defaultSubnetTwo: "two",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "kube-system",
					Name:      "testservice",
					UID:       "test-uid",
					Annotations: map[string]string{
						ServiceAnnotationLoadBalancerShape: "8000Mbps",
					},
				},
				Spec: v1.ServiceSpec{
					SessionAffinity: v1.ServiceAffinityNone,
					Ports: []v1.ServicePort{
						{
							Protocol: v1.ProtocolTCP,
							Port:     int32(80),
						},
					},
				},
			},
			expected: &LBSpec{
				Name:     "test-uid",
				Shape:    "8000Mbps",
				Internal: false,
				Subnets:  []string{"one", "two"},
				Listeners: map[string]loadbalancer.ListenerDetails{
					"TCP-80": {
						DefaultBackendSetName: common.String("TCP-80"),
						Port:                  common.Int(80),
						Protocol:              common.String("TCP"),
					},
				},
				BackendSets: map[string]loadbalancer.BackendSetDetails{
					"TCP-80": {
						Backends: []loadbalancer.BackendDetails{},
						HealthChecker: &loadbalancer.HealthCheckerDetails{
							Protocol:         common.String("HTTP"),
							Port:             common.Int(10256),
							UrlPath:          common.String("/healthz"),
							Retries:          common.Int(3),
							TimeoutInMillis:  common.Int(3000),
							IntervalInMillis: common.Int(10000),
						},
						Policy: common.String("ROUND_ROBIN"),
					},
				},
				SourceCIDRs: []string{"0.0.0.0/0"},
				Ports: map[string]portSpec{
					"TCP-80": {
						ListenerPort:      80,
						HealthCheckerPort: 10256,
					},
				},
				securityListManager: newSecurityListManagerNOOP(),
			},
		},
		"custom idle connection timeout": {
			defaultSubnetOne: "one",
			defaultSubnetTwo: "two",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "kube-system",
					Name:      "testservice",
					UID:       "test-uid",
					Annotations: map[string]string{
						ServiceAnnotationLoadBalancerConnectionIdleTimeout: "404",
					},
				},
				Spec: v1.ServiceSpec{
					SessionAffinity: v1.ServiceAffinityNone,
					Ports: []v1.ServicePort{
						{
							Protocol: v1.ProtocolTCP,
							Port:     int32(80),
						},
					},
				},
			},
			expected: &LBSpec{
				Name:     "test-uid",
				Shape:    "100Mbps",
				Internal: false,
				Subnets:  []string{"one", "two"},
				Listeners: map[string]loadbalancer.ListenerDetails{
					"TCP-80": {
						DefaultBackendSetName: common.String("TCP-80"),
						Port:                  common.Int(80),
						Protocol:              common.String("TCP"),
						ConnectionConfiguration: &loadbalancer.ConnectionConfiguration{
							IdleTimeout: common.Int64(404),
						},
					},
				},
				BackendSets: map[string]loadbalancer.BackendSetDetails{
					"TCP-80": {
						Backends: []loadbalancer.BackendDetails{},
						HealthChecker: &loadbalancer.HealthCheckerDetails{
							Protocol:         common.String("HTTP"),
							Port:             common.Int(10256),
							UrlPath:          common.String("/healthz"),
							Retries:          common.Int(3),
							TimeoutInMillis:  common.Int(3000),
							IntervalInMillis: common.Int(10000),
						},
						Policy: common.String("ROUND_ROBIN"),
					},
				},
				SourceCIDRs: []string{"0.0.0.0/0"},
				Ports: map[string]portSpec{
					"TCP-80": {
						ListenerPort:      80,
						HealthCheckerPort: 10256,
					},
				},
				securityListManager: newSecurityListManagerNOOP(),
			},
		},
		"protocol annotation set to http": {
			defaultSubnetOne: "one",
			defaultSubnetTwo: "two",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "kube-system",
					Name:      "testservice",
					UID:       "test-uid",
					Annotations: map[string]string{
						ServiceAnnotationLoadBalancerBEProtocol: "HTTP",
						ServiceAnnotationLoadBalancerSubnet1:    "annotation-one",
						ServiceAnnotationLoadBalancerSubnet2:    "annotation-two",
					},
				},
				Spec: v1.ServiceSpec{
					SessionAffinity: v1.ServiceAffinityNone,
					Ports: []v1.ServicePort{
						{
							Protocol: v1.ProtocolTCP,
							Port:     int32(80),
						},
					},
				},
			},
			expected: &LBSpec{
				Name:     "test-uid",
				Shape:    "100Mbps",
				Internal: false,
				Subnets:  []string{"annotation-one", "annotation-two"},
				Listeners: map[string]loadbalancer.ListenerDetails{
					"HTTP-80": {
						DefaultBackendSetName: common.String("TCP-80"),
						Port:                  common.Int(80),
						Protocol:              common.String("HTTP"),
					},
				},
				BackendSets: map[string]loadbalancer.BackendSetDetails{
					"TCP-80": {
						Backends: []loadbalancer.BackendDetails{},
						HealthChecker: &loadbalancer.HealthCheckerDetails{
							Protocol:         common.String("HTTP"),
							Port:             common.Int(10256),
							UrlPath:          common.String("/healthz"),
							Retries:          common.Int(3),
							TimeoutInMillis:  common.Int(3000),
							IntervalInMillis: common.Int(10000),
						},
						Policy: common.String("ROUND_ROBIN"),
					},
				},
				SourceCIDRs: []string{"0.0.0.0/0"},
				Ports: map[string]portSpec{
					"TCP-80": {
						ListenerPort:      80,
						HealthCheckerPort: 10256,
					},
				},
				securityListManager: newSecurityListManagerNOOP(),
			},
		},
		"protocol annotation set to tcp": {
			defaultSubnetOne: "one",
			defaultSubnetTwo: "two",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "kube-system",
					Name:      "testservice",
					UID:       "test-uid",
					Annotations: map[string]string{
						ServiceAnnotationLoadBalancerBEProtocol: "TCP",
						ServiceAnnotationLoadBalancerSubnet1:    "annotation-one",
						ServiceAnnotationLoadBalancerSubnet2:    "annotation-two",
					},
				},
				Spec: v1.ServiceSpec{
					SessionAffinity: v1.ServiceAffinityNone,
					Ports: []v1.ServicePort{
						{
							Protocol: v1.ProtocolTCP,
							Port:     int32(80),
						},
					},
				},
			},
			expected: &LBSpec{
				Name:     "test-uid",
				Shape:    "100Mbps",
				Internal: false,
				Subnets:  []string{"annotation-one", "annotation-two"},
				Listeners: map[string]loadbalancer.ListenerDetails{
					"TCP-80": {
						DefaultBackendSetName: common.String("TCP-80"),
						Port:                  common.Int(80),
						Protocol:              common.String("TCP"),
					},
				},
				BackendSets: map[string]loadbalancer.BackendSetDetails{
					"TCP-80": {
						Backends: []loadbalancer.BackendDetails{},
						HealthChecker: &loadbalancer.HealthCheckerDetails{
							Protocol:         common.String("HTTP"),
							Port:             common.Int(10256),
							UrlPath:          common.String("/healthz"),
							Retries:          common.Int(3),
							TimeoutInMillis:  common.Int(3000),
							IntervalInMillis: common.Int(10000),
						},
						Policy: common.String("ROUND_ROBIN"),
					},
				},
				SourceCIDRs: []string{"0.0.0.0/0"},
				Ports: map[string]portSpec{
					"TCP-80": {
						ListenerPort:      80,
						HealthCheckerPort: 10256,
					},
				},
				securityListManager: newSecurityListManagerNOOP(),
			},
		},
		"protocol annotation empty": {
			defaultSubnetOne: "one",
			defaultSubnetTwo: "two",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "kube-system",
					Name:      "testservice",
					UID:       "test-uid",
					Annotations: map[string]string{
						ServiceAnnotationLoadBalancerBEProtocol: "",
						ServiceAnnotationLoadBalancerSubnet1:    "annotation-one",
						ServiceAnnotationLoadBalancerSubnet2:    "annotation-two",
					},
				},
				Spec: v1.ServiceSpec{
					SessionAffinity: v1.ServiceAffinityNone,
					Ports: []v1.ServicePort{
						{
							Protocol: v1.ProtocolTCP,
							Port:     int32(80),
						},
					},
				},
			},
			expected: &LBSpec{
				Name:     "test-uid",
				Shape:    "100Mbps",
				Internal: false,
				Subnets:  []string{"annotation-one", "annotation-two"},
				Listeners: map[string]loadbalancer.ListenerDetails{
					"TCP-80": {
						DefaultBackendSetName: common.String("TCP-80"),
						Port:                  common.Int(80),
						Protocol:              common.String("TCP"),
					},
				},
				BackendSets: map[string]loadbalancer.BackendSetDetails{
					"TCP-80": {
						Backends: []loadbalancer.BackendDetails{},
						HealthChecker: &loadbalancer.HealthCheckerDetails{
							Protocol:         common.String("HTTP"),
							Port:             common.Int(10256),
							UrlPath:          common.String("/healthz"),
							Retries:          common.Int(3),
							TimeoutInMillis:  common.Int(3000),
							IntervalInMillis: common.Int(10000),
						},
						Policy: common.String("ROUND_ROBIN"),
					},
				},
				SourceCIDRs: []string{"0.0.0.0/0"},
				Ports: map[string]portSpec{
					"TCP-80": {
						ListenerPort:      80,
						HealthCheckerPort: 10256,
					},
				},
				securityListManager: newSecurityListManagerNOOP(),
			},
		},
		"LBSpec returned with proper SSLConfiguration": {
			defaultSubnetOne: "one",
			defaultSubnetTwo: "two",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "kube-system",
					Name:        "testservice",
					UID:         "test-uid",
					Annotations: map[string]string{},
				},
				Spec: v1.ServiceSpec{
					SessionAffinity: v1.ServiceAffinityNone,
					Ports: []v1.ServicePort{
						{
							Protocol: v1.ProtocolTCP,
							Port:     int32(443),
						},
					},
				},
			},
			expected: &LBSpec{
				Name:     "test-uid",
				Shape:    "100Mbps",
				Internal: false,
				Subnets:  []string{"one", "two"},
				Listeners: map[string]loadbalancer.ListenerDetails{
					fmt.Sprintf("TCP-443"): {
						DefaultBackendSetName: common.String("TCP-443"),
						Port:                  common.Int(443),
						Protocol:              common.String("TCP"),
						SslConfiguration: &loadbalancer.SslConfigurationDetails{
							CertificateName:       &listenerSecret,
							VerifyDepth:           common.Int(0),
							VerifyPeerCertificate: common.Bool(false),
						},
					},
				},
				BackendSets: map[string]loadbalancer.BackendSetDetails{
					"TCP-443": {
						Backends: []loadbalancer.BackendDetails{},
						HealthChecker: &loadbalancer.HealthCheckerDetails{
							Protocol:         common.String("TCP"),
							Port:             common.Int(10256),
							UrlPath:          common.String("/healthz"),
							Retries:          common.Int(3),
							TimeoutInMillis:  common.Int(3000),
							IntervalInMillis: common.Int(10000),
						},
						Policy: common.String("ROUND_ROBIN"),
						SslConfiguration: &loadbalancer.SslConfigurationDetails{
							CertificateName:       &backendSecret,
							VerifyDepth:           common.Int(0),
							VerifyPeerCertificate: common.Bool(false),
						},
					},
				},
				SourceCIDRs: []string{"0.0.0.0/0"},
				Ports: map[string]portSpec{
					"TCP-443": {
						ListenerPort:      443,
						HealthCheckerPort: 10256,
					},
				},
				securityListManager: newSecurityListManagerNOOP(),
				SSLConfig: &SSLConfig{
					Ports:                   sets.NewInt(443),
					ListenerSSLSecretName:   listenerSecret,
					BackendSetSSLSecretName: backendSecret,
				},
			},
			sslConfig: &SSLConfig{
				Ports:                   sets.NewInt(443),
				ListenerSSLSecretName:   listenerSecret,
				BackendSetSSLSecretName: backendSecret,
			},
		},
		"custom health check config": {
			defaultSubnetOne: "one",
			defaultSubnetTwo: "two",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "kube-system",
					Name:      "testservice",
					UID:       "test-uid",
					Annotations: map[string]string{
						ServiceAnnotationLoadBalancerHealthCheckRetries:  "1",
						ServiceAnnotationLoadBalancerHealthCheckTimeout:  "1000",
						ServiceAnnotationLoadBalancerHealthCheckInterval: "3000",
					},
				},
				Spec: v1.ServiceSpec{
					SessionAffinity: v1.ServiceAffinityNone,
					Ports: []v1.ServicePort{
						{
							Protocol: v1.ProtocolTCP,
							Port:     int32(80),
						},
					},
				},
			},
			expected: &LBSpec{
				Name:     "test-uid",
				Shape:    "100Mbps",
				Internal: false,
				Subnets:  []string{"one", "two"},
				Listeners: map[string]loadbalancer.ListenerDetails{
					"TCP-80": {
						DefaultBackendSetName: common.String("TCP-80"),
						Port:                  common.Int(80),
						Protocol:              common.String("TCP"),
					},
				},
				BackendSets: map[string]loadbalancer.BackendSetDetails{
					"TCP-80": {
						Backends: []loadbalancer.BackendDetails{},
						HealthChecker: &loadbalancer.HealthCheckerDetails{
							Protocol:         common.String("HTTP"),
							Port:             common.Int(10256),
							UrlPath:          common.String("/healthz"),
							Retries:          common.Int(1),
							TimeoutInMillis:  common.Int(1000),
							IntervalInMillis: common.Int(3000),
						},
						Policy: common.String("ROUND_ROBIN"),
					},
				},
				SourceCIDRs: []string{"0.0.0.0/0"},
				Ports: map[string]portSpec{
					"TCP-80": {
						ListenerPort:      80,
						HealthCheckerPort: 10256,
					},
				},
				securityListManager: newSecurityListManagerNOOP(),
			},
		},
	}

	cp := &CloudProvider{
		client: MockOCIClient{},
		config: &providercfg.Config{CompartmentID: "testCompartment"},
	}

	for name, tc := range testCases {
		logger := zap.L()
		t.Run(name, func(t *testing.T) {
			// we expect the service to be unchanged
			tc.expected.service = tc.service
			cp.config = &providercfg.Config{
				LoadBalancer: &providercfg.LoadBalancerConfig{
					Subnet1: tc.defaultSubnetOne,
					Subnet2: tc.defaultSubnetTwo,
				},
			}
			subnets, err := cp.getLoadBalancerSubnets(context.Background(), logger.Sugar(), tc.service)
			if err != nil {
				t.Error(err)
			}
			slManagerFactory := func(mode string) securityListManager {
				return newSecurityListManagerNOOP()
			}
			result, err := NewLBSpec(logger.Sugar(), tc.service, tc.nodes, subnets, tc.sslConfig, slManagerFactory)
			if err != nil {
				t.Error(err)
			}

			if !reflect.DeepEqual(result, tc.expected) {
				t.Errorf("Expected load balancer spec\n%+v\nbut got\n%+v", tc.expected, result)
			}
		})
	}
}

func TestNewLBSpecSingleAD(t *testing.T) {
	testCases := map[string]struct {
		defaultSubnetOne string
		defaultSubnetTwo string
		nodes            []*v1.Node
		service          *v1.Service
		expected         *LBSpec
	}{
		"single subnet for single AD": {
			defaultSubnetOne: "one",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "kube-system",
					Name:      "testservice",
					UID:       "test-uid",
					Annotations: map[string]string{
						ServiceAnnotationLoadBalancerBEProtocol: "",
						ServiceAnnotationLoadBalancerSubnet1:    "annotation-one",
					},
				},
				Spec: v1.ServiceSpec{
					SessionAffinity: v1.ServiceAffinityNone,
					Ports: []v1.ServicePort{
						{
							Protocol: v1.ProtocolTCP,
							Port:     int32(80),
						},
					},
				},
			},
			expected: &LBSpec{
				Name:     "test-uid",
				Shape:    "100Mbps",
				Internal: false,
				Subnets:  []string{"annotation-one"},
				Listeners: map[string]loadbalancer.ListenerDetails{
					"TCP-80": {
						DefaultBackendSetName: common.String("TCP-80"),
						Port:                  common.Int(80),
						Protocol:              common.String("TCP"),
					},
				},
				BackendSets: map[string]loadbalancer.BackendSetDetails{
					"TCP-80": {
						Backends: []loadbalancer.BackendDetails{},
						HealthChecker: &loadbalancer.HealthCheckerDetails{
							Protocol:         common.String("HTTP"),
							Port:             common.Int(10256),
							UrlPath:          common.String("/healthz"),
							Retries:          common.Int(3),
							TimeoutInMillis:  common.Int(3000),
							IntervalInMillis: common.Int(10000),
						},
						Policy: common.String("ROUND_ROBIN"),
					},
				},
				SourceCIDRs: []string{"0.0.0.0/0"},
				Ports: map[string]portSpec{
					"TCP-80": {
						ListenerPort:      80,
						HealthCheckerPort: 10256,
					},
				},
				securityListManager: newSecurityListManagerNOOP(),
			},
		},
	}

	cp := &CloudProvider{
		client: MockOCIClient{},
		config: &providercfg.Config{CompartmentID: "testCompartment"},
	}

	for name, tc := range testCases {
		logger := zap.L()
		t.Run(name, func(t *testing.T) {
			// we expect the service to be unchanged
			tc.expected.service = tc.service
			cp.config = &providercfg.Config{
				LoadBalancer: &providercfg.LoadBalancerConfig{
					Subnet1: tc.defaultSubnetOne,
					Subnet2: tc.defaultSubnetTwo,
				},
			}
			subnets, err := cp.getLoadBalancerSubnets(context.Background(), logger.Sugar(), tc.service)
			if err != nil {
				t.Error(err)
			}
			slManagerFactory := func(mode string) securityListManager {
				return newSecurityListManagerNOOP()
			}
			result, err := NewLBSpec(logger.Sugar(), tc.service, tc.nodes, subnets, nil, slManagerFactory)
			if err != nil {
				t.Error(err)
			}

			if !reflect.DeepEqual(result, tc.expected) {
				t.Errorf("Expected load balancer spec\n%+v\nbut got\n%+v", tc.expected, result)
			}
		})
	}
}

func TestNewLBSpecFailure(t *testing.T) {
	testCases := map[string]struct {
		defaultSubnetOne string
		defaultSubnetTwo string
		nodes            []*v1.Node
		service          *v1.Service
		//add cp or cp security list
		expectedErrMsg string
	}{
		"unsupported udp protocol": {
			service: &v1.Service{
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{Protocol: v1.ProtocolUDP},
					},
				},
			},
			expectedErrMsg: "invalid service: OCI load balancers do not support UDP",
		},
		"unsupported LB IP": {
			service: &v1.Service{
				Spec: v1.ServiceSpec{
					LoadBalancerIP:  "127.0.0.1",
					SessionAffinity: v1.ServiceAffinityNone,
					Ports: []v1.ServicePort{
						{Protocol: v1.ProtocolTCP},
					},
				},
			},
			expectedErrMsg: "invalid service: OCI does not support setting LoadBalancerIP",
		},
		"unsupported session affinity": {
			service: &v1.Service{
				Spec: v1.ServiceSpec{
					SessionAffinity: v1.ServiceAffinityClientIP,
					Ports: []v1.ServicePort{
						{Protocol: v1.ProtocolTCP},
					},
				},
			},
			expectedErrMsg: "invalid service: OCI only supports SessionAffinity \"None\" currently",
		},
		"invalid idle connection timeout": {
			defaultSubnetOne: "one",
			defaultSubnetTwo: "two",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "kube-system",
					Name:      "testservice",
					UID:       "test-uid",
					Annotations: map[string]string{
						ServiceAnnotationLoadBalancerConnectionIdleTimeout: "whoops",
					},
				},
				Spec: v1.ServiceSpec{
					SessionAffinity: v1.ServiceAffinityNone,
					Ports: []v1.ServicePort{
						{Protocol: v1.ProtocolTCP},
					},
				},
			},
			expectedErrMsg: "error parsing service annotation: service.beta.kubernetes.io/oci-load-balancer-connection-idle-timeout=whoops",
		},
		"internal lb missing subnet1": {
			defaultSubnetTwo: "two",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "kube-system",
					Name:      "testservice",
					UID:       "test-uid",
					Annotations: map[string]string{
						ServiceAnnotationLoadBalancerInternal: "",
					},
				},
				Spec: v1.ServiceSpec{
					SessionAffinity: v1.ServiceAffinityNone,
					Ports:           []v1.ServicePort{},
					//add security list mananger in spec
				},
			},
			expectedErrMsg: "a configuration for subnet1 must be specified for an internal load balancer",
		},
		"internal lb with empty subnet1 annotation": {
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "kube-system",
					Name:      "testservice",
					UID:       "test-uid",
					Annotations: map[string]string{
						ServiceAnnotationLoadBalancerInternal: "",
						ServiceAnnotationLoadBalancerSubnet1:  "",
					},
				},
				Spec: v1.ServiceSpec{
					SessionAffinity: v1.ServiceAffinityNone,
					Ports:           []v1.ServicePort{},
					//add security list mananger in spec
				},
			},
			expectedErrMsg: "a configuration for subnet1 must be specified for an internal load balancer",
		},
	}

	cp := &CloudProvider{
		client: MockOCIClient{},
		config: &providercfg.Config{CompartmentID: "testCompartment"},
	}

	for name, tc := range testCases {
		logger := zap.L()
		t.Run(name, func(t *testing.T) {
			cp.config = &providercfg.Config{
				LoadBalancer: &providercfg.LoadBalancerConfig{
					Subnet1: tc.defaultSubnetOne,
					Subnet2: tc.defaultSubnetTwo,
				},
			}
			subnets, err := cp.getLoadBalancerSubnets(context.Background(), logger.Sugar(), tc.service)
			if err == nil {
				slManagerFactory := func(mode string) securityListManager {
					return newSecurityListManagerNOOP()
				}
				_, err = NewLBSpec(logger.Sugar(), tc.service, tc.nodes, subnets, nil, slManagerFactory)
			}
			if err == nil || err.Error() != tc.expectedErrMsg {
				t.Errorf("Expected error with message %q but got %q", tc.expectedErrMsg, err)
			}
		})
	}
}

func TestNewSSLConfig(t *testing.T) {
	testCases := map[string]struct {
		secretListenerString   string
		secretBackendSetString string
		service                *v1.Service
		ports                  []int
		ssr                    sslSecretReader

		expectedResult *SSLConfig
	}{
		"noopSSLSecretReader if ssr is nil and uses the default service namespace": {
			secretListenerString:   "listenerSecretName",
			secretBackendSetString: "backendSetSecretName",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
			},
			ports: []int{8080},
			ssr:   nil,

			expectedResult: &SSLConfig{
				Ports:                        sets.NewInt(8080),
				ListenerSSLSecretName:        "listenerSecretName",
				ListenerSSLSecretNamespace:   "default",
				BackendSetSSLSecretName:      "backendSetSecretName",
				BackendSetSSLSecretNamespace: "default",
				sslSecretReader:              noopSSLSecretReader{},
			},
		},
		"ssr is assigned if provided and uses the default service namespace": {
			secretListenerString:   "listenerSecretName",
			secretBackendSetString: "backendSetSecretName",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
			},
			ports: []int{8080},
			ssr:   &mockSSLSecretReader{},

			expectedResult: &SSLConfig{
				Ports:                        sets.NewInt(8080),
				ListenerSSLSecretName:        "listenerSecretName",
				ListenerSSLSecretNamespace:   "default",
				BackendSetSSLSecretName:      "backendSetSecretName",
				BackendSetSSLSecretNamespace: "default",
				sslSecretReader:              &mockSSLSecretReader{},
			},
		},
		"If namespace is specified in secret string, use it": {
			secretListenerString:   "namespaceone/listenerSecretName",
			secretBackendSetString: "namespacetwo/backendSetSecretName",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
			},
			ports: []int{8080},
			ssr:   &mockSSLSecretReader{},

			expectedResult: &SSLConfig{
				Ports:                        sets.NewInt(8080),
				ListenerSSLSecretName:        "listenerSecretName",
				ListenerSSLSecretNamespace:   "namespaceone",
				BackendSetSSLSecretName:      "backendSetSecretName",
				BackendSetSSLSecretNamespace: "namespacetwo",
				sslSecretReader:              &mockSSLSecretReader{},
			},
		},
		"Empty secret string results in empty name and namespace": {
			ports: []int{8080},
			ssr:   &mockSSLSecretReader{},

			expectedResult: &SSLConfig{
				Ports:                        sets.NewInt(8080),
				ListenerSSLSecretName:        "",
				ListenerSSLSecretNamespace:   "",
				BackendSetSSLSecretName:      "",
				BackendSetSSLSecretNamespace: "",
				sslSecretReader:              &mockSSLSecretReader{},
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			result := NewSSLConfig(tc.secretListenerString, tc.secretBackendSetString, tc.service, tc.ports, tc.ssr)
			if !reflect.DeepEqual(result, tc.expectedResult) {
				t.Errorf("Expected SSlConfig \n%+v\nbut got\n%+v", tc.expectedResult, result)
			}
		})
	}
}

func TestCertificates(t *testing.T) {

	backendSecretCaCert := "cacert1"
	backendSecretPublicCert := "publiccert1"
	backendSecretPrivateKey := "privatekey1"
	backendSecretPassphrase := "passphrase1"

	listenerSecretCaCert := "cacert2"
	listenerSecretPublicCert := "publiccert2"
	listenerSecretPrivateKey := "privatekey2"
	listenerSecretPassphrase := "passphrase2"

	testCases := map[string]struct {
		lbSpec         *LBSpec
		expectedResult map[string]loadbalancer.CertificateDetails
		expectError    bool
	}{
		"No SSLConfig results in empty certificate details array": {
			expectError:    false,
			lbSpec:         &LBSpec{},
			expectedResult: make(map[string]loadbalancer.CertificateDetails),
		},
		"Return backend SSL secret": {
			expectError: false,
			lbSpec: &LBSpec{
				service: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "testnamespace",
					},
				},
				SSLConfig: &SSLConfig{
					BackendSetSSLSecretName:      backendSecret,
					BackendSetSSLSecretNamespace: "backendnamespace",
					sslSecretReader: &mockSSLSecretReader{
						returnError: false,
						returnMap: map[struct {
							namespaceArg string
							nameArg      string
						}]*certificateData{
							{namespaceArg: "backendnamespace", nameArg: backendSecret}: {
								Name:       "certificatename",
								CACert:     []byte(backendSecretCaCert),
								PublicCert: []byte(backendSecretPublicCert),
								PrivateKey: []byte(backendSecretPrivateKey),
								Passphrase: []byte(backendSecretPassphrase),
							},
						},
					},
				},
			},
			expectedResult: map[string]loadbalancer.CertificateDetails{
				backendSecret: {
					CertificateName:   &backendSecret,
					CaCertificate:     &backendSecretCaCert,
					Passphrase:        &backendSecretPassphrase,
					PrivateKey:        &backendSecretPrivateKey,
					PublicCertificate: &backendSecretPublicCert,
				},
			},
		},
		"Return both backend and listener SSL secret": {
			expectError: false,
			lbSpec: &LBSpec{
				service: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "testnamespace",
					},
				},
				SSLConfig: &SSLConfig{
					BackendSetSSLSecretName:      backendSecret,
					BackendSetSSLSecretNamespace: "backendnamespace",
					ListenerSSLSecretName:        listenerSecret,
					ListenerSSLSecretNamespace:   "listenernamespace",
					sslSecretReader: &mockSSLSecretReader{
						returnError: false,
						returnMap: map[struct {
							namespaceArg string
							nameArg      string
						}]*certificateData{
							{namespaceArg: "backendnamespace", nameArg: backendSecret}: {
								Name:       "backendcertificatename",
								CACert:     []byte(backendSecretCaCert),
								PublicCert: []byte(backendSecretPublicCert),
								PrivateKey: []byte(backendSecretPrivateKey),
								Passphrase: []byte(backendSecretPassphrase),
							},
							{namespaceArg: "listenernamespace", nameArg: listenerSecret}: {
								Name:       "listenercertificatename",
								CACert:     []byte(listenerSecretCaCert),
								PublicCert: []byte(listenerSecretPublicCert),
								PrivateKey: []byte(listenerSecretPrivateKey),
								Passphrase: []byte(listenerSecretPassphrase),
							},
						},
					},
				},
			},
			expectedResult: map[string]loadbalancer.CertificateDetails{
				backendSecret: {
					CertificateName:   &backendSecret,
					CaCertificate:     &backendSecretCaCert,
					Passphrase:        &backendSecretPassphrase,
					PrivateKey:        &backendSecretPrivateKey,
					PublicCertificate: &backendSecretPublicCert,
				},
				listenerSecret: {
					CertificateName:   &listenerSecret,
					CaCertificate:     &listenerSecretCaCert,
					Passphrase:        &listenerSecretPassphrase,
					PrivateKey:        &listenerSecretPrivateKey,
					PublicCertificate: &listenerSecretPublicCert,
				},
			},
		},
		"Error returned from SSL secret reader is handled gracefully": {
			expectError: true,
			lbSpec: &LBSpec{
				service: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "testnamespace",
					},
				},
				SSLConfig: &SSLConfig{
					BackendSetSSLSecretName: backendSecret,
					sslSecretReader: &mockSSLSecretReader{
						returnError: true,
					},
				},
			},
			expectedResult: nil,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			certDetails, err := tc.lbSpec.Certificates()
			if err != nil && !tc.expectError {
				t.Errorf("Was not expected an error to be returned, but got one:\n%+v", err)
			}
			if !reflect.DeepEqual(certDetails, tc.expectedResult) {
				t.Errorf("Expected certificate details \n%+v\nbut got\n%+v", tc.expectedResult, certDetails)
			}
		})
	}
}

func TestRequiresCertificate(t *testing.T) {
	testCases := map[string]struct {
		expected    bool
		annotations map[string]string
	}{
		"Contains the Load Balancer SSL Ports Annotation": {
			expected: true,
			annotations: map[string]string{
				ServiceAnnotationLoadBalancerSSLPorts: "443",
			},
		},
		"Does not container the Load Balancer SSL Ports Annotation": {
			expected:    false,
			annotations: make(map[string]string, 0),
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			result := requiresCertificate(&v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: tc.annotations,
				},
			})
			if result != tc.expected {
				t.Error("Did not get the correct result")
			}
		})
	}
}

func Test_getBackends(t *testing.T) {
	type args struct {
		nodes    []*v1.Node
		nodePort int32
	}
	var tests = []struct {
		name string
		args args
		want []loadbalancer.BackendDetails
	}{
		{
			name: "no nodes",
			args: args{nodePort: 80},
			want: []loadbalancer.BackendDetails{},
		},
		{
			name: "single node with assigned IP",
			args: args{
				nodes: []*v1.Node{
					{
						TypeMeta:   metav1.TypeMeta{},
						ObjectMeta: metav1.ObjectMeta{},
						Spec:       v1.NodeSpec{},
						Status: v1.NodeStatus{
							Capacity:    nil,
							Allocatable: nil,
							Phase:       "",
							Conditions:  nil,
							Addresses: []v1.NodeAddress{
								{
									Address: "0.0.0.0",
									Type:    "InternalIP",
								},
							},
							DaemonEndpoints: v1.NodeDaemonEndpoints{},
							NodeInfo:        v1.NodeSystemInfo{},
							Images:          nil,
							VolumesInUse:    nil,
							VolumesAttached: nil,
							Config:          nil,
						},
					},
				},
				nodePort: 80,
			},
			want: []loadbalancer.BackendDetails{
				{IpAddress: common.String("0.0.0.0"), Port: common.Int(80), Weight: common.Int(1)},
			},
		},
		{
			name: "single node with unassigned IP",
			args: args{
				nodes: []*v1.Node{
					{
						TypeMeta:   metav1.TypeMeta{},
						ObjectMeta: metav1.ObjectMeta{},
						Spec:       v1.NodeSpec{},
						Status: v1.NodeStatus{
							Capacity:        nil,
							Allocatable:     nil,
							Phase:           "",
							Conditions:      nil,
							Addresses:       []v1.NodeAddress{},
							DaemonEndpoints: v1.NodeDaemonEndpoints{},
							NodeInfo:        v1.NodeSystemInfo{},
							Images:          nil,
							VolumesInUse:    nil,
							VolumesAttached: nil,
							Config:          nil,
						},
					},
				},
				nodePort: 80,
			},
			want: []loadbalancer.BackendDetails{},
		},
		{
			name: "multiple nodes - all with assigned IP",
			args: args{
				nodes: []*v1.Node{
					{
						TypeMeta:   metav1.TypeMeta{},
						ObjectMeta: metav1.ObjectMeta{},
						Spec:       v1.NodeSpec{},
						Status: v1.NodeStatus{
							Capacity:    nil,
							Allocatable: nil,
							Phase:       "",
							Conditions:  nil,
							Addresses: []v1.NodeAddress{
								{
									Address: "0.0.0.0",
									Type:    "InternalIP",
								},
							},
							DaemonEndpoints: v1.NodeDaemonEndpoints{},
							NodeInfo:        v1.NodeSystemInfo{},
							Images:          nil,
							VolumesInUse:    nil,
							VolumesAttached: nil,
							Config:          nil,
						},
					},
					{
						TypeMeta:   metav1.TypeMeta{},
						ObjectMeta: metav1.ObjectMeta{},
						Spec:       v1.NodeSpec{},
						Status: v1.NodeStatus{
							Capacity:    nil,
							Allocatable: nil,
							Phase:       "",
							Conditions:  nil,
							Addresses: []v1.NodeAddress{
								{
									Address: "0.0.0.1",
									Type:    "InternalIP",
								},
							},
							DaemonEndpoints: v1.NodeDaemonEndpoints{},
							NodeInfo:        v1.NodeSystemInfo{},
							Images:          nil,
							VolumesInUse:    nil,
							VolumesAttached: nil,
							Config:          nil,
						},
					},
				},
				nodePort: 80,
			},
			want: []loadbalancer.BackendDetails{
				{IpAddress: common.String("0.0.0.0"), Port: common.Int(80), Weight: common.Int(1)},
				{IpAddress: common.String("0.0.0.1"), Port: common.Int(80), Weight: common.Int(1)},
			},
		},
		{
			name: "multiple nodes - all with unassigned IP",
			args: args{
				nodes: []*v1.Node{
					{
						TypeMeta:   metav1.TypeMeta{},
						ObjectMeta: metav1.ObjectMeta{},
						Spec:       v1.NodeSpec{},
						Status: v1.NodeStatus{
							Capacity:        nil,
							Allocatable:     nil,
							Phase:           "",
							Conditions:      nil,
							Addresses:       []v1.NodeAddress{},
							DaemonEndpoints: v1.NodeDaemonEndpoints{},
							NodeInfo:        v1.NodeSystemInfo{},
							Images:          nil,
							VolumesInUse:    nil,
							VolumesAttached: nil,
							Config:          nil,
						},
					},
					{
						TypeMeta:   metav1.TypeMeta{},
						ObjectMeta: metav1.ObjectMeta{},
						Spec:       v1.NodeSpec{},
						Status: v1.NodeStatus{
							Capacity:        nil,
							Allocatable:     nil,
							Phase:           "",
							Conditions:      nil,
							Addresses:       []v1.NodeAddress{},
							DaemonEndpoints: v1.NodeDaemonEndpoints{},
							NodeInfo:        v1.NodeSystemInfo{},
							Images:          nil,
							VolumesInUse:    nil,
							VolumesAttached: nil,
							Config:          nil,
						},
					},
				},
				nodePort: 80,
			},
			want: []loadbalancer.BackendDetails{},
		},
		{
			name: "multiple nodes - one with unassigned IP",
			args: args{
				nodes: []*v1.Node{
					{
						TypeMeta:   metav1.TypeMeta{},
						ObjectMeta: metav1.ObjectMeta{},
						Spec:       v1.NodeSpec{},
						Status: v1.NodeStatus{
							Capacity:    nil,
							Allocatable: nil,
							Phase:       "",
							Conditions:  nil,
							Addresses: []v1.NodeAddress{
								{
									Address: "0.0.0.0",
									Type:    "InternalIP",
								},
							},
							DaemonEndpoints: v1.NodeDaemonEndpoints{},
							NodeInfo:        v1.NodeSystemInfo{},
							Images:          nil,
							VolumesInUse:    nil,
							VolumesAttached: nil,
							Config:          nil,
						},
					},
					{
						TypeMeta:   metav1.TypeMeta{},
						ObjectMeta: metav1.ObjectMeta{},
						Spec:       v1.NodeSpec{},
						Status: v1.NodeStatus{
							Capacity:        nil,
							Allocatable:     nil,
							Phase:           "",
							Conditions:      nil,
							Addresses:       []v1.NodeAddress{},
							DaemonEndpoints: v1.NodeDaemonEndpoints{},
							NodeInfo:        v1.NodeSystemInfo{},
							Images:          nil,
							VolumesInUse:    nil,
							VolumesAttached: nil,
							Config:          nil,
						},
					},
					{
						TypeMeta:   metav1.TypeMeta{},
						ObjectMeta: metav1.ObjectMeta{},
						Spec:       v1.NodeSpec{},
						Status: v1.NodeStatus{
							Capacity:    nil,
							Allocatable: nil,
							Phase:       "",
							Conditions:  nil,
							Addresses: []v1.NodeAddress{
								{
									Address: "0.0.0.1",
									Type:    "InternalIP",
								},
							},
							DaemonEndpoints: v1.NodeDaemonEndpoints{},
							NodeInfo:        v1.NodeSystemInfo{},
							Images:          nil,
							VolumesInUse:    nil,
							VolumesAttached: nil,
							Config:          nil,
						},
					},
				},
				nodePort: 80,
			},
			want: []loadbalancer.BackendDetails{
				{IpAddress: common.String("0.0.0.0"), Port: common.Int(80), Weight: common.Int(1)},
				{IpAddress: common.String("0.0.0.1"), Port: common.Int(80), Weight: common.Int(1)},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zap.L()
			if got := getBackends(logger.Sugar(), tt.args.nodes, tt.args.nodePort); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getBackends() = %v, want %v", got, tt.want)
			}
		})
	}
}
