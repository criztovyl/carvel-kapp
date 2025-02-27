// Copyright 2020 VMware, Inc.
// SPDX-License-Identifier: Apache-2.0

package resourcesmisc

import (
	"fmt"

	ctlconf "github.com/k14s/kapp/pkg/kapp/config"
	ctlres "github.com/k14s/kapp/pkg/kapp/resources"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type CustomWaitingResource struct {
	resource ctlres.Resource
	waitRule ctlconf.WaitRule
}

func NewCustomWaitingResource(resource ctlres.Resource, waitRules []ctlconf.WaitRule) *CustomWaitingResource {
	for _, rule := range waitRules {
		if rule.ResourceMatcher().Matches(resource) {
			return &CustomWaitingResource{resource, rule}
		}
	}
	return nil
}

type customWaitingResourceStruct struct {
	Metadata metav1.ObjectMeta
	Status   struct {
		ObservedGeneration int64
		Conditions         []customWaitingResourceCondition
	}
}

type customWaitingResourceCondition struct {
	Type               string
	Status             string
	Reason             string
	Message            string
	ObservedGeneration int64
}

func (s CustomWaitingResource) IsDoneApplying() DoneApplyState {
	obj := customWaitingResourceStruct{}

	err := s.resource.AsUncheckedTypedObj(&obj)
	if err != nil {
		return DoneApplyState{Done: true, Successful: false, Message: fmt.Sprintf(
			"Error: Failed obj conversion: %s", err)}
	}

	if s.waitRule.SupportsObservedGeneration && obj.Metadata.Generation != obj.Status.ObservedGeneration {
		return DoneApplyState{Done: false, Message: fmt.Sprintf(
			"Waiting for generation %d to be observed", obj.Metadata.Generation)}
	}

	if s.waitRule.Ytt != nil {
		configObj, err := WaitRuleContractV1{
			ResourceMatcher: ctlres.AnyMatcher{
				Matchers: ctlconf.ResourceMatchers(s.waitRule.ResourceMatchers).AsResourceMatchers()},
			Starlark: s.waitRule.Ytt.FuncContractV1.Resource,
		}.Apply(s.resource)
		if err != nil {
			return DoneApplyState{Done: true, Successful: false, Message: fmt.Sprintf(
				"Error: Applying ytt wait rule: %s", err.Error())}
		}
		return DoneApplyState{Done: configObj.Done, Successful: configObj.Successful,
			Message: configObj.Message}
	}

	hasConditionWaitingForGeneration := false
	// Check on failure conditions first
	for _, condMatcher := range s.waitRule.ConditionMatchers {
		for _, cond := range obj.Status.Conditions {
			if cond.Type == condMatcher.Type && cond.Status == condMatcher.Status {
				if condMatcher.SupportsObservedGeneration && obj.Metadata.Generation != cond.ObservedGeneration {
					hasConditionWaitingForGeneration = true
					continue
				}
				if condMatcher.Failure {
					return DoneApplyState{Done: true, Successful: false, Message: fmt.Sprintf(
						"Encountered failure condition %s == %s: %s (message: %s)",
						cond.Type, condMatcher.Status, cond.Reason, cond.Message)}
				}
			}
		}
	}

	// If no failure conditions found, check on successful ones
	for _, condMatcher := range s.waitRule.ConditionMatchers {
		for _, cond := range obj.Status.Conditions {
			if cond.Type == condMatcher.Type && cond.Status == condMatcher.Status {
				if condMatcher.SupportsObservedGeneration && obj.Metadata.Generation != cond.ObservedGeneration {
					hasConditionWaitingForGeneration = true
					continue
				}
				if condMatcher.Success {
					return DoneApplyState{Done: true, Successful: true, Message: fmt.Sprintf(
						"Encountered successful condition %s == %s: %s (message: %s)",
						cond.Type, condMatcher.Status, cond.Reason, cond.Message)}
				}
			}
		}
	}

	if hasConditionWaitingForGeneration {
		return DoneApplyState{Done: false, Message: fmt.Sprintf(
			"Waiting for generation %d to be observed by status condition(s)", obj.Metadata.Generation)}
	}
	return DoneApplyState{Done: false, Message: "No failing or successful conditions found"}
}
