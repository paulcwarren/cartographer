// Copyright 2021 VMware
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

package delivery_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/vmware-tanzu/cartographer/pkg/apis/v1alpha1"
	"github.com/vmware-tanzu/cartographer/pkg/utils"
)

var _ = Describe("Deliveries", func() {
	var (
		ctx      context.Context
		delivery *unstructured.Unstructured
		cleanups []client.Object
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("I can define a delivery with a resource", func() {
		BeforeEach(func() {
			deliveryYaml := utils.HereYaml(`
				---
				apiVersion: carto.run/v1alpha1
				kind: ClusterDelivery
				metadata:
				  name: my-delivery
				spec:
				  selector:
					"some-key": "some-value"
			      resources:
			        - name: my-first-resource
					  templateRef:
				        kind: ClusterSourceTemplate
				        name: my-source-template
			`)

			delivery = &unstructured.Unstructured{}
			err := yaml.Unmarshal([]byte(deliveryYaml), delivery)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			for _, obj := range cleanups {
				_ = c.Delete(ctx, obj, &client.DeleteOptions{})
			}
		})

		Context("the referenced resource exists", func() {
			BeforeEach(func() {
				clusterSourceTemplateYaml := utils.HereYaml(`
					---
					apiVersion: carto.run/v1alpha1
					kind: ClusterSourceTemplate
					metadata:
					  name: my-source-template
					spec:
					  urlPath: .spec.value.foo
					  revisionPath: .spec.value.foo
					  template:
					    apiVersion: test.run/v1alpha1
					    kind: Test
					    metadata:
					      name: test-deliverable-source
					    spec:
					      value:
					        foo: bar
			    `)

				clusterSourceTemplate := &unstructured.Unstructured{}
				err := yaml.Unmarshal([]byte(clusterSourceTemplateYaml), clusterSourceTemplate)
				Expect(err).NotTo(HaveOccurred())

				err = c.Create(ctx, clusterSourceTemplate, &client.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())

				cleanups = append(cleanups, clusterSourceTemplate)
			})

			It("sets the status to Ready True", func() {
				err := c.Create(ctx, delivery, &client.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())

				cleanups = append(cleanups, delivery)

				Eventually(func() []metav1.Condition {
					persistedDelivery := &v1alpha1.ClusterDelivery{}
					err := c.Get(ctx, client.ObjectKey{Name: "my-delivery"}, persistedDelivery)
					Expect(err).NotTo(HaveOccurred())
					return persistedDelivery.Status.Conditions
				}, 5*time.Second).Should(
					ContainElements(
						MatchFields(
							IgnoreExtras,
							Fields{
								"Type":   Equal("Ready"),
								"Status": Equal(metav1.ConditionTrue),
								"Reason": Equal("Ready"),
							},
						),
						MatchFields(
							IgnoreExtras,
							Fields{
								"Type":   Equal("TemplatesReady"),
								"Status": Equal(metav1.ConditionTrue),
								"Reason": Equal("Ready"),
							},
						),
					),
				)
			})
		})

		Context("the referenced resource does not exist", func() {
			It("sets the status to Ready False", func() {
				err := c.Create(ctx, delivery, &client.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())

				cleanups = append(cleanups, delivery)

				Eventually(func() []metav1.Condition {
					persistedDelivery := &v1alpha1.ClusterDelivery{}
					err := c.Get(ctx, client.ObjectKey{Name: "my-delivery"}, persistedDelivery)
					Expect(err).NotTo(HaveOccurred())
					return persistedDelivery.Status.Conditions
				}, 5*time.Second).Should(
					ContainElements(
						MatchFields(
							IgnoreExtras,
							Fields{
								"Type":   Equal("Ready"),
								"Status": Equal(metav1.ConditionFalse),
								"Reason": Equal("TemplatesNotFound"),
							},
						),
						MatchFields(
							IgnoreExtras,
							Fields{
								"Type":   Equal("TemplatesReady"),
								"Status": Equal(metav1.ConditionFalse),
								"Reason": Equal("TemplatesNotFound"),
							},
						),
					),
				)
			})
		})
	})

	Describe("I cannot define identical resource names", func() {
		It("rejects the delivery with an error", func() {
			deliveryYaml := utils.HereYaml(`
				---
				apiVersion: carto.run/v1alpha1
				kind: ClusterDelivery
				metadata:
				  name: my-delivery
				spec:
				  selector:
					foo: bar
			      resources:
			        - name: my-first-resource
					  templateRef:
						kind: ClusterSourceTemplate
						name: my-source-template
			        - name: my-first-resource
					  templateRef:
						kind: ClusterSourceTemplate
						name: my-other-template
			`)

			delivery = &unstructured.Unstructured{}
			err := yaml.Unmarshal([]byte(deliveryYaml), delivery)
			Expect(err).NotTo(HaveOccurred())

			err = c.Create(ctx, delivery, &client.CreateOptions{})
			Expect(err).To(HaveOccurred())

			cleanups = append(cleanups, delivery)

			Expect(err).To(MatchError(ContainSubstring(`spec.resources[1].name "my-first-resource" cannot appear twice`)))
		})
	})

	Describe("I can expect ClusterDelivery to not keep updating it's status", func() {

		var (
			lastConditions []metav1.Condition
		)

		BeforeEach(func() {
			deliveryYaml := utils.HereYaml(`
				---
				apiVersion: carto.run/v1alpha1
				kind: ClusterDelivery
				metadata:
				  name: my-delivery
				spec:
				  selector:
				    "some-key": "some-value"
				  resources:
				    - name: my-first-resource
				      templateRef:
				        kind: ClusterSourceTemplate
				        name: my-source-template
			`)

			delivery = &unstructured.Unstructured{}
			err := yaml.Unmarshal([]byte(deliveryYaml), delivery)
			Expect(err).NotTo(HaveOccurred())

			err = c.Create(ctx, delivery, &client.CreateOptions{})
			cleanups = append(cleanups, delivery)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool {
				persistedDelivery := &v1alpha1.ClusterDelivery{}
				err := c.Get(ctx, client.ObjectKey{Name: "my-delivery"}, persistedDelivery)
				Expect(err).NotTo(HaveOccurred())
				lastConditions = persistedDelivery.Status.Conditions

				return persistedDelivery.Status.ObservedGeneration == persistedDelivery.Generation
			}, 5*time.Second).Should(BeTrue())
		})

		It("does not update the lastTransitionTime on subsequent reconciliation if the status does not change", func() {
			time.Sleep(1 * time.Second) //metav1.Time unmarshals with 1 second accuracy so this sleep avoids a race condition

			persistedDelivery := &v1alpha1.ClusterDelivery{}
			err := c.Get(context.Background(), client.ObjectKey{Name: "my-delivery"}, persistedDelivery)
			Expect(err).NotTo(HaveOccurred())

			persistedDelivery.Spec.Selector = map[string]string{"some-key": "blah"}
			err = c.Update(context.Background(), persistedDelivery)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() int64 {
				persistedDelivery = &v1alpha1.ClusterDelivery{}
				err := c.Get(context.Background(), client.ObjectKey{Name: "my-delivery"}, persistedDelivery)
				Expect(err).NotTo(HaveOccurred())
				return persistedDelivery.Status.ObservedGeneration
			}).Should(Equal(persistedDelivery.Generation))

			err = c.Get(context.Background(), client.ObjectKey{Name: "my-delivery"}, persistedDelivery)
			Expect(err).NotTo(HaveOccurred())
			Expect(persistedDelivery.Status.Conditions).To(Equal(lastConditions))
		})
	})
})
