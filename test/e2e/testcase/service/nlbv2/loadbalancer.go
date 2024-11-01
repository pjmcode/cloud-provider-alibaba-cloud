package nlbv2

import (
	"context"
	"strings"
	"time"

	"github.com/alibabacloud-go/tea/tea"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cloud-provider-alibaba-cloud/pkg/controller/helper"
	"k8s.io/cloud-provider-alibaba-cloud/pkg/controller/service/reconcile/annotation"
	"k8s.io/cloud-provider-alibaba-cloud/pkg/model"
	nlb_model "k8s.io/cloud-provider-alibaba-cloud/pkg/model/nlb"
	"k8s.io/cloud-provider-alibaba-cloud/test/e2e/framework"
	"k8s.io/cloud-provider-alibaba-cloud/test/e2e/options"
	"k8s.io/klog/v2"
)

func RunLoadBalancerTestCases(f *framework.Framework) {

	ginkgo.Describe("nlb service controller: loadbalancer", func() {

		ginkgo.AfterEach(func() {
			ginkgo.By("delete service")
			err := f.AfterEachNlb()
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})

		ginkgo.Context("create nlb service", func() {
			ginkgo.It("create by loadbalancerClass", func() {
				svc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
					annotation.Annotation(annotation.ZoneMaps): options.TestConfig.NLBZoneMaps,
				})
				gomega.Expect(err).To(gomega.BeNil())
				err = f.ExpectNetworkLoadBalancerEqual(svc)
				gomega.Expect(err).To(gomega.BeNil())
			})
		})
		ginkgo.Context("after the service is deleted, the LB is retained", func() {
			ginkgo.It("delete service & preserve LB", func() {
				svc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
					annotation.Annotation(annotation.ZoneMaps):           options.TestConfig.NLBZoneMaps,
					annotation.Annotation(annotation.PreserveLBOnDelete): string(model.OnFlag),
				})
				gomega.Expect(err).To(gomega.BeNil())
				err = f.ExpectNetworkLoadBalancerEqual(svc)
				gomega.Expect(err).To(gomega.BeNil())
				_, nlb, err := f.FindNetworkLoadBalancer()
				gomega.Expect(err).To(gomega.BeNil())
				defer func(id string) {
					gomega.Expect(f.DeleteNetworkLoadBalancer(id)).To(gomega.BeNil())
				}(nlb.LoadBalancerAttribute.LoadBalancerId)

				ginkgo.By("delete service")
				gomega.Expect(f.Client.KubeClient.DeleteService()).To(gomega.BeNil())
				gomega.Consistently(func(g gomega.Gomega) {
					lb, err := framework.BuildNLBRemoteModel(f, svc)
					g.Expect(err).To(gomega.BeNil())
					g.Expect(lb.LoadBalancerAttribute.LoadBalancerId).To(gomega.Equal(nlb.LoadBalancerAttribute.LoadBalancerId))
				}, 10*time.Second, 30*time.Second).Should(gomega.Succeed())

			})

		})
		ginkgo.Context("bandwidth-package-id", func() {
			if options.TestConfig.CommonBandwidthPackageId != "" &&
				framework.CompareVersion(options.TestConfig.CCM_Version, "2.9.1") {
				ginkgo.It("Bind a shared bandwidth package", func() {
					svc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
						annotation.Annotation(annotation.ZoneMaps):           options.TestConfig.NLBZoneMaps,
						annotation.Annotation(annotation.BandwidthPackageId): options.TestConfig.CommonBandwidthPackageId,
					})
					gomega.Expect(err).To(gomega.BeNil())
					err = f.ExpectNetworkLoadBalancerEqual(svc)
					gomega.Expect(err).To(gomega.BeNil())
				})
				ginkgo.It("Bind a invalid bandwidth-package-id", func() {
					svc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
						annotation.Annotation(annotation.ZoneMaps):           options.TestConfig.NLBZoneMaps,
						annotation.Annotation(annotation.BandwidthPackageId): "cbwp-xxxxxx",
					})
					gomega.Expect(err).To(gomega.BeNil())
					err = f.ExpectNetworkLoadBalancerEqual(svc)
					gomega.Expect(err).NotTo(gomega.BeNil())
				})
				if options.TestConfig.InternetNetworkLoadBalancerID != "" && options.TestConfig.IntranetNetworkLoadBalancerID != "" {
					ginkgo.It("reuse internet lb", func() {
						svc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
							annotation.Annotation(annotation.ZoneMaps):           options.TestConfig.NLBZoneMaps,
							annotation.Annotation(annotation.AddressType):        string(model.InternetAddressType),
							annotation.Annotation(annotation.LoadBalancerId):     options.TestConfig.InternetNetworkLoadBalancerID,
							annotation.Annotation(annotation.BandwidthPackageId): options.TestConfig.CommonBandwidthPackageId,
						})
						gomega.Expect(err).To(gomega.BeNil())
						err = f.ExpectNetworkLoadBalancerEqual(svc)
						gomega.Expect(err).To(gomega.BeNil())
					})
					ginkgo.It("reuse intranet lb", func() {
						svc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
							annotation.Annotation(annotation.ZoneMaps):           options.TestConfig.NLBZoneMaps,
							annotation.Annotation(annotation.AddressType):        string(model.IntranetAddressType),
							annotation.Annotation(annotation.LoadBalancerId):     options.TestConfig.IntranetNetworkLoadBalancerID,
							annotation.Annotation(annotation.BandwidthPackageId): options.TestConfig.CommonBandwidthPackageId,
						})
						gomega.Expect(err).To(gomega.BeNil())
						err = f.ExpectNetworkLoadBalancerEqual(svc)
						gomega.Expect(err).NotTo(gomega.BeNil())
					})
				}

			}

		})
		ginkgo.Context("readinessGate", func() {
			if framework.EnableReadinessGate() {
				ginkgo.It("readinessGate", func() {
					oldsvc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
						annotation.Annotation(annotation.ZoneMaps): options.TestConfig.NLBZoneMaps,
					})
					gomega.Expect(err).To(gomega.BeNil())
					err = f.ExpectNetworkLoadBalancerEqual(oldsvc)
					gomega.Expect(err).To(gomega.BeNil())
					ginkgo.By("readinessGate")
					app, err := f.Client.KubeClient.GetDefaultDeployment()
					gomega.Expect(err).To(gomega.BeNil())
					cond := "service.readiness.alibabacloud.com/" + oldsvc.Name
					app.Spec.Template.Spec.ReadinessGates = append(
						app.Spec.Template.Spec.ReadinessGates, v1.PodReadinessGate{
							ConditionType: helper.BuildReadinessGatePodConditionType(
								cond,
							),
						})
					app, err = f.Client.KubeClient.UpdateDeployment(app)
					gomega.Expect(err).To(gomega.BeNil())
					gomega.Eventually(func(g gomega.Gomega) {
						pods, err := f.Client.KubeClient.ListPods(metav1.FormatLabelSelector(app.Spec.Selector))
						g.Expect(err).To(gomega.BeNil())
						g.Expect(pods.Items).To(gomega.HaveLen(3))
					}).WithPolling(5*time.Second).WithTimeout(1*time.Minute).Should(gomega.Succeed(), "test app should has 3 pods")
					pods, err := f.Client.KubeClient.ListPods(metav1.FormatLabelSelector(app.Spec.Selector))
					gomega.Expect(err).To(gomega.BeNil())
					// All ready pods should have a readinessGate
					ok := func() bool {
						for _, pod := range pods.Items {
							if !f.Client.KubeClient.IsPodHasReadinessGate(&pod, cond) {
								klog.Errorf("pod %s not has readinessGate %s cond", pod.Name, cond)
								return false
							}
						}
						return true
					}
					gomega.Eventually(ok, 10*time.Second, 30*time.Second).Should(gomega.BeTrue(), "all ready pods should has readinessGate")
					err = f.ExpectNetworkLoadBalancerEqual(oldsvc)
					gomega.Expect(err).To(gomega.BeNil())

				})
			}

		})

		ginkgo.Context("nlb address-type", func() {
			ginkgo.It("address-type=internet", func() {
				svc, err := f.Client.KubeClient.CreateNLBServiceByAnno(
					map[string]string{
						annotation.Annotation(annotation.AddressType): string(model.InternetAddressType),
						annotation.Annotation(annotation.ZoneMaps):    options.TestConfig.NLBZoneMaps,
					})
				gomega.Expect(err).To(gomega.BeNil())

				err = f.ExpectNetworkLoadBalancerEqual(svc)
				gomega.Expect(err).To(gomega.BeNil())
			})

			ginkgo.It("address-type=intranet", func() {
				svc, err := f.Client.KubeClient.CreateNLBServiceByAnno(
					map[string]string{
						annotation.Annotation(annotation.AddressType): string(model.IntranetAddressType),
						annotation.Annotation(annotation.ZoneMaps):    options.TestConfig.NLBZoneMaps,
					})
				gomega.Expect(err).To(gomega.BeNil())

				err = f.ExpectNetworkLoadBalancerEqual(svc)
				gomega.Expect(err).To(gomega.BeNil())

			})

			ginkgo.It("address-type: intranet->internet", func() {
				oldSvc, err := f.Client.KubeClient.CreateNLBServiceByAnno(
					map[string]string{
						annotation.Annotation(annotation.AddressType): string(model.IntranetAddressType),
						annotation.Annotation(annotation.ZoneMaps):    options.TestConfig.NLBZoneMaps,
					})
				gomega.Expect(err).To(gomega.BeNil())
				err = f.ExpectNetworkLoadBalancerEqual(oldSvc)
				gomega.Expect(err).To(gomega.BeNil())

				newSvc := oldSvc.DeepCopy()
				newSvc.Annotations[annotation.Annotation(annotation.AddressType)] = string(model.InternetAddressType)
				newSvc, err = f.Client.KubeClient.PatchService(oldSvc, newSvc)
				gomega.Expect(err).To(gomega.BeNil())

				err = f.ExpectNetworkLoadBalancerEqual(newSvc)
				gomega.Expect(err).To(gomega.BeNil())
			})

			ginkgo.It("address-type: internet->intranet", func() {
				oldSvc, err := f.Client.KubeClient.CreateNLBServiceByAnno(
					map[string]string{
						annotation.Annotation(annotation.AddressType): string(model.InternetAddressType),
						annotation.Annotation(annotation.ZoneMaps):    options.TestConfig.NLBZoneMaps,
					})
				gomega.Expect(err).To(gomega.BeNil())
				err = f.ExpectNetworkLoadBalancerEqual(oldSvc)
				gomega.Expect(err).To(gomega.BeNil())

				newSvc := oldSvc.DeepCopy()
				newSvc.Annotations[annotation.Annotation(annotation.AddressType)] = string(model.IntranetAddressType)
				newSvc, err = f.Client.KubeClient.PatchService(oldSvc, newSvc)
				gomega.Expect(err).To(gomega.BeNil())

				err = f.ExpectNetworkLoadBalancerEqual(newSvc)
				gomega.Expect(err).To(gomega.BeNil())
			})

			if options.TestConfig.IPv6 && tea.StringValue(options.TestConfig.AckCluster.ProxyMode) == "ipvs" {
				ginkgo.It("ipv6-address-type=internet", func() {
					svc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
						annotation.Annotation(annotation.ZoneMaps):        options.TestConfig.NLBZoneMaps,
						annotation.Annotation(annotation.IPVersion):       string(model.DualStack),
						annotation.Annotation(annotation.IPv6AddressType): string(model.InternetAddressType),
					})
					gomega.Expect(err).To(gomega.BeNil())

					err = f.ExpectNetworkLoadBalancerEqual(svc)
					gomega.Expect(err).To(gomega.BeNil())
				})

				ginkgo.It("ipv6-address-type=intranet", func() {
					svc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
						annotation.Annotation(annotation.ZoneMaps):        options.TestConfig.NLBZoneMaps,
						annotation.Annotation(annotation.IPVersion):       string(model.DualStack),
						annotation.Annotation(annotation.IPv6AddressType): string(model.IntranetAddressType),
					})
					gomega.Expect(err).To(gomega.BeNil())

					err = f.ExpectNetworkLoadBalancerEqual(svc)
					gomega.Expect(err).To(gomega.BeNil())
				})

				ginkgo.It("ipv6-address-type: intranet->internet", func() {
					oldSvc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
						annotation.Annotation(annotation.ZoneMaps):        options.TestConfig.NLBZoneMaps,
						annotation.Annotation(annotation.IPVersion):       string(model.DualStack),
						annotation.Annotation(annotation.IPv6AddressType): string(model.IntranetAddressType),
					})
					gomega.Expect(err).To(gomega.BeNil())
					err = f.ExpectNetworkLoadBalancerEqual(oldSvc)
					gomega.Expect(err).To(gomega.BeNil())
					newSvc := oldSvc.DeepCopy()
					newSvc.Annotations[annotation.Annotation(annotation.IPv6AddressType)] = string(model.InternetAddressType)
					newSvc, err = f.Client.KubeClient.PatchService(oldSvc, newSvc)
					gomega.Expect(err).To(gomega.BeNil())
					err = f.ExpectNetworkLoadBalancerEqual(newSvc)
					gomega.Expect(err).To(gomega.BeNil())
				})

				ginkgo.It("ipv6-address-type: internet->intranet", func() {
					oldSvc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
						annotation.Annotation(annotation.ZoneMaps):        options.TestConfig.NLBZoneMaps,
						annotation.Annotation(annotation.IPVersion):       string(model.DualStack),
						annotation.Annotation(annotation.IPv6AddressType): string(model.InternetAddressType),
					})
					gomega.Expect(err).To(gomega.BeNil())
					err = f.ExpectNetworkLoadBalancerEqual(oldSvc)
					gomega.Expect(err).To(gomega.BeNil())
					newSvc := oldSvc.DeepCopy()
					newSvc.Annotations[annotation.Annotation(annotation.IPv6AddressType)] = string(model.IntranetAddressType)
					newSvc, err = f.Client.KubeClient.PatchService(oldSvc, newSvc)
					gomega.Expect(err).To(gomega.BeNil())
					err = f.ExpectNetworkLoadBalancerEqual(newSvc)
					gomega.Expect(err).To(gomega.BeNil())
				})
			}
		})

		ginkgo.Context("nlb reuse lb", func() {
			if options.TestConfig.InternetNetworkLoadBalancerID != "" && options.TestConfig.IntranetNetworkLoadBalancerID != "" {
				ginkgo.It("reuse internet lb", func() {
					svc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
						annotation.Annotation(annotation.ZoneMaps):       options.TestConfig.NLBZoneMaps,
						annotation.Annotation(annotation.AddressType):    string(model.InternetAddressType),
						annotation.Annotation(annotation.LoadBalancerId): options.TestConfig.InternetNetworkLoadBalancerID,
					})
					gomega.Expect(err).To(gomega.BeNil())
					err = f.ExpectNetworkLoadBalancerEqual(svc)
					gomega.Expect(err).To(gomega.BeNil())
				})
				ginkgo.It("reuse internet lb with override-listener=false", func() {
					svc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
						annotation.Annotation(annotation.ZoneMaps):         options.TestConfig.NLBZoneMaps,
						annotation.Annotation(annotation.AddressType):      string(model.InternetAddressType),
						annotation.Annotation(annotation.LoadBalancerId):   options.TestConfig.InternetNetworkLoadBalancerID,
						annotation.Annotation(annotation.OverrideListener): "false",
					})
					gomega.Expect(err).To(gomega.BeNil())
					err = f.ExpectNetworkLoadBalancerEqual(svc)
					gomega.Expect(err).To(gomega.BeNil())
				})
				ginkgo.It("reuse internet lb with override-listener=true", func() {
					svc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
						annotation.Annotation(annotation.ZoneMaps):         options.TestConfig.NLBZoneMaps,
						annotation.Annotation(annotation.AddressType):      string(model.InternetAddressType),
						annotation.Annotation(annotation.LoadBalancerId):   options.TestConfig.InternetNetworkLoadBalancerID,
						annotation.Annotation(annotation.OverrideListener): "true",
					})
					gomega.Expect(err).To(gomega.BeNil())
					err = f.ExpectNetworkLoadBalancerEqual(svc)
					gomega.Expect(err).To(gomega.BeNil())
				})
				ginkgo.It("ccm created nlb -> reused nlb", func() {
					oldSvc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
						annotation.Annotation(annotation.ZoneMaps): options.TestConfig.NLBZoneMaps,
					})
					gomega.Expect(err).To(gomega.BeNil())
					_, oldlb, err := f.FindNetworkLoadBalancer()
					gomega.Expect(err).To(gomega.BeNil())

					lbid := oldlb.LoadBalancerAttribute.LoadBalancerId
					defer func(id string) {
						err := f.DeleteNetworkLoadBalancer(id)
						gomega.Expect(err).To(gomega.BeNil())
						err = f.ExpectNetworkLoadBalancerDeleted(oldSvc)
						gomega.Expect(err).To(gomega.BeNil())

					}(lbid)

					newsvc := oldSvc.DeepCopy()
					newsvc.Annotations = map[string]string{
						annotation.Annotation(annotation.ZoneMaps):       options.TestConfig.NLBZoneMaps,
						annotation.Annotation(annotation.LoadBalancerId): options.TestConfig.InternetNetworkLoadBalancerID,
					}
					newsvc, err = f.Client.KubeClient.PatchService(oldSvc, newsvc)
					gomega.Expect(err).To(gomega.BeNil())

					err = f.ExpectNetworkLoadBalancerEqual(newsvc)
					gomega.Expect(err).NotTo(gomega.BeNil())
				})

				ginkgo.It("reuse intranet nlb with override-listener=true", func() {
					svc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
						annotation.Annotation(annotation.ZoneMaps):         options.TestConfig.NLBZoneMaps,
						annotation.Annotation(annotation.AddressType):      string(model.IntranetAddressType),
						annotation.Annotation(annotation.LoadBalancerId):   options.TestConfig.IntranetNetworkLoadBalancerID,
						annotation.Annotation(annotation.OverrideListener): "true",
					})
					gomega.Expect(err).To(gomega.BeNil())
					err = f.ExpectNetworkLoadBalancerEqual(svc)
					gomega.Expect(err).To(gomega.BeNil())
				})

				ginkgo.It("reuse not exist nlb", func() {
					svc, err := f.Client.KubeClient.CreateServiceByAnno(map[string]string{
						annotation.Annotation(annotation.ZoneMaps):         options.TestConfig.NLBZoneMaps,
						annotation.Annotation(annotation.LoadBalancerId):   "nlb-xxxxxxxxxx",
						annotation.Annotation(annotation.OverrideListener): "true",
					})
					gomega.Expect(err).To(gomega.BeNil())
					err = f.ExpectLoadBalancerEqual(svc)
					gomega.Expect(err).NotTo(gomega.BeNil())
				})

				//ginkgo.It("reuse nlb created by CCM", func() {
				//	anotherSvc := f.Client.KubeClient.DefaultService()
				//	anotherSvc.Annotations = map[string]string{
				//		annotation.Annotation(annotation.ZoneMaps): options.TestConfig.NLBZoneMaps,
				//	}
				//	anotherSvc.Name = "test-another-nlb"
				//	anotherSvc, err := f.Client.KubeClient.CreateService(anotherSvc)
				//	gomega.Expect(err).To(gomega.BeNil())
				//
				//	defer func() {
				//		err := f.Client.KubeClient.DeleteServiceByName(anotherSvc.Name)
				//		gomega.Expect(err).To(gomega.BeNil())
				//	}()
				//
				//
				//})
			}

		})

		ginkgo.Context("nlb name", func() {
			ginkgo.It("lb-name: test-lb-name", func() {
				svc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
					annotation.Annotation(annotation.ZoneMaps):         options.TestConfig.NLBZoneMaps,
					annotation.Annotation(annotation.LoadBalancerName): "test-lb-name",
				})
				gomega.Expect(err).To(gomega.BeNil())
				err = f.ExpectNetworkLoadBalancerEqual(svc)
				gomega.Expect(err).To(gomega.BeNil())
			})

			ginkgo.It("lb-name: lb-name-1 -> lb-name-2", func() {
				oldSvc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
					annotation.Annotation(annotation.ZoneMaps):         options.TestConfig.NLBZoneMaps,
					annotation.Annotation(annotation.LoadBalancerName): "test-lb-name",
				})
				gomega.Expect(err).To(gomega.BeNil())
				err = f.ExpectNetworkLoadBalancerEqual(oldSvc)
				gomega.Expect(err).To(gomega.BeNil())

				newSvc := oldSvc.DeepCopy()
				newSvc.Annotations[annotation.Annotation(annotation.LoadBalancerName)] = "modify-lb-name"
				newSvc, err = f.Client.KubeClient.PatchService(oldSvc, newSvc)
				gomega.Expect(err).To(gomega.BeNil())

				err = f.ExpectNetworkLoadBalancerEqual(newSvc)
				gomega.Expect(err).To(gomega.BeNil())
			})
		})

		ginkgo.Context("nlb IP version", func() {
			ginkgo.It("ip-version: ipv4", func() {
				svc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
					annotation.Annotation(annotation.ZoneMaps):  options.TestConfig.NLBZoneMaps,
					annotation.Annotation(annotation.IPVersion): string(model.IPv4),
				})
				gomega.Expect(err).To(gomega.BeNil())
				err = f.ExpectNetworkLoadBalancerEqual(svc)
				gomega.Expect(err).To(gomega.BeNil())
			})
			if options.TestConfig.IPv6 {
				ginkgo.It("ip-version: dualstack", func() {
					svc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
						annotation.Annotation(annotation.ZoneMaps):  options.TestConfig.NLBZoneMaps,
						annotation.Annotation(annotation.IPVersion): string(model.DualStack),
					})
					gomega.Expect(err).To(gomega.BeNil())
					err = f.ExpectNetworkLoadBalancerEqual(svc)
					gomega.Expect(err).To(gomega.BeNil())
				})

				ginkgo.It("lb-version: ipv4 -> dualstack", func() {
					oldSvc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
						annotation.Annotation(annotation.ZoneMaps): options.TestConfig.NLBZoneMaps,
					})
					gomega.Expect(err).To(gomega.BeNil())
					err = f.ExpectNetworkLoadBalancerEqual(oldSvc)
					gomega.Expect(err).To(gomega.BeNil())

					newSvc := oldSvc.DeepCopy()
					newSvc.Annotations[annotation.Annotation(annotation.IPVersion)] = string(model.DualStack)
					newSvc, err = f.Client.KubeClient.PatchService(oldSvc, newSvc)
					gomega.Expect(err).To(gomega.BeNil())

					err = f.ExpectNetworkLoadBalancerEqual(newSvc)
					gomega.Expect(err).NotTo(gomega.BeNil())
				})

				ginkgo.It("lb-version: dualstack -> ipv4", func() {
					oldSvc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
						annotation.Annotation(annotation.ZoneMaps):  options.TestConfig.NLBZoneMaps,
						annotation.Annotation(annotation.IPVersion): string(model.DualStack),
					})
					gomega.Expect(err).To(gomega.BeNil())
					err = f.ExpectNetworkLoadBalancerEqual(oldSvc)
					gomega.Expect(err).To(gomega.BeNil())

					newSvc := oldSvc.DeepCopy()
					newSvc.Annotations[annotation.Annotation(annotation.IPVersion)] = string(model.IPv4)
					newSvc, err = f.Client.KubeClient.PatchService(oldSvc, newSvc)
					gomega.Expect(err).To(gomega.BeNil())

					err = f.ExpectNetworkLoadBalancerEqual(newSvc)
					gomega.Expect(err).NotTo(gomega.BeNil())
				})
			}
		})

		if options.TestConfig.ResourceGroupID != "" {
			ginkgo.Context("nlb resource group", func() {
				ginkgo.It("create lb with resource-group-id", func() {
					svc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
						annotation.Annotation(annotation.ZoneMaps):        options.TestConfig.NLBZoneMaps,
						annotation.Annotation(annotation.ResourceGroupId): options.TestConfig.ResourceGroupID,
					})
					gomega.Expect(err).To(gomega.BeNil())
					err = f.ExpectNetworkLoadBalancerEqual(svc)
					gomega.Expect(err).To(gomega.BeNil())
				})

				ginkgo.It("change resource-group-id", func() {
					oldSvc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
						annotation.Annotation(annotation.ZoneMaps): options.TestConfig.NLBZoneMaps,
					})
					gomega.Expect(err).To(gomega.BeNil())
					err = f.ExpectNetworkLoadBalancerEqual(oldSvc)
					gomega.Expect(err).To(gomega.BeNil())

					newSvc := oldSvc.DeepCopy()
					newSvc.Annotations[annotation.Annotation(annotation.ResourceGroupId)] = options.TestConfig.ResourceGroupID
					_, err = f.Client.KubeClient.PatchService(oldSvc, newSvc)
					gomega.Expect(err).To(gomega.BeNil())

					err = f.ExpectNetworkLoadBalancerEqual(newSvc)
					gomega.Expect(err).NotTo(gomega.BeNil())
				})
				ginkgo.It("reuse lb and set resource-group-id is inconsistent with lb", func() {
					svc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
						annotation.Annotation(annotation.ZoneMaps):        options.TestConfig.NLBZoneMaps,
						annotation.Annotation(annotation.ResourceGroupId): options.TestConfig.ResourceGroupID,
						annotation.Annotation(annotation.LoadBalancerId):  options.TestConfig.InternetNetworkLoadBalancerID,
					})
					gomega.Expect(err).To(gomega.BeNil())
					err = f.ExpectNetworkLoadBalancerEqual(svc)
					gomega.Expect(err).NotTo(gomega.BeNil())
				})
			})
		}

		ginkgo.Context("nlb add tags", func() {
			ginkgo.It("add tag for nlb", func() {
				svc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
					annotation.Annotation(annotation.ZoneMaps):       options.TestConfig.NLBZoneMaps,
					annotation.Annotation(annotation.AdditionalTags): "Key1=Value1,Key2=Value2",
				})
				gomega.Expect(err).To(gomega.BeNil())
				err = f.ExpectNetworkLoadBalancerEqual(svc)
				gomega.Expect(err).To(gomega.BeNil())
			})

			ginkgo.It("update tags", func() {
				oldSvc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
					annotation.Annotation(annotation.ZoneMaps): options.TestConfig.NLBZoneMaps,
				})
				gomega.Expect(err).To(gomega.BeNil())
				err = f.ExpectNetworkLoadBalancerEqual(oldSvc)
				gomega.Expect(err).To(gomega.BeNil())

				newSvc := oldSvc.DeepCopy()
				newSvc.Annotations[annotation.Annotation(annotation.AdditionalTags)] = "Key1=Value1,Key2=Value2"
				newSvc, err = f.Client.KubeClient.PatchService(oldSvc, newSvc)
				gomega.Expect(err).To(gomega.BeNil())

				err = f.ExpectNetworkLoadBalancerEqual(newSvc)
				gomega.Expect(err).To(gomega.BeNil())
			})

			ginkgo.It("update and delete tags", func() {
				oldSvc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
					annotation.Annotation(annotation.ZoneMaps):       options.TestConfig.NLBZoneMaps,
					annotation.Annotation(annotation.AdditionalTags): "Key1=Value1,Key2=Value2",
				})
				gomega.Expect(err).To(gomega.BeNil())
				err = f.ExpectNetworkLoadBalancerEqual(oldSvc)
				gomega.Expect(err).To(gomega.BeNil())

				newSvc := oldSvc.DeepCopy()
				newSvc.Annotations[annotation.Annotation(annotation.AdditionalTags)] = "Key1=Value3"
				newSvc, err = f.Client.KubeClient.PatchService(oldSvc, newSvc)
				gomega.Expect(err).To(gomega.BeNil())

				err = f.ExpectNetworkLoadBalancerEqual(newSvc)
				gomega.Expect(err).To(gomega.BeNil())
			})

			ginkgo.It("add tag for reused lb", func() {
				svc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
					annotation.Annotation(annotation.ZoneMaps):       options.TestConfig.NLBZoneMaps,
					annotation.Annotation(annotation.AdditionalTags): "Key1=Value1,Key2=Value2",
					annotation.Annotation(annotation.LoadBalancerId): options.TestConfig.InternetNetworkLoadBalancerID,
				})
				gomega.Expect(err).To(gomega.BeNil())
				err = f.ExpectNetworkLoadBalancerEqual(svc)
				gomega.Expect(err).To(gomega.BeNil())
			})
		})

		ginkgo.Context("service type", func() {
			ginkgo.It("type: NodePort -> LoadBalancer", func() {
				svc := f.Client.KubeClient.DefaultService()
				svc.Spec.Type = v1.ServiceTypeNodePort
				_, err := f.Client.KubeClient.CreateService(svc)
				gomega.Expect(err).To(gomega.BeNil())

				svc, _, err = f.FindNetworkLoadBalancer()
				// should not create slb
				gomega.Expect(err).NotTo(gomega.BeNil())

				lbClass := helper.NLBClass

				lbSvc := svc.DeepCopy()
				lbSvc.Spec.Type = v1.ServiceTypeLoadBalancer
				lbSvc.Spec.LoadBalancerClass = &lbClass
				lbSvc.Annotations = make(map[string]string)
				lbSvc.Annotations[annotation.Annotation(annotation.ZoneMaps)] = options.TestConfig.NLBZoneMaps
				lbSvc, err = f.Client.KubeClient.PatchService(svc, lbSvc)
				gomega.Expect(err).To(gomega.BeNil())

				err = f.ExpectNetworkLoadBalancerEqual(lbSvc)
				gomega.Expect(err).To(gomega.BeNil())
			})
			ginkgo.It("type: LoadBalancer->NodePort", func() {
				oldSvc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
					annotation.Annotation(annotation.ZoneMaps): options.TestConfig.NLBZoneMaps,
				})
				gomega.Expect(err).To(gomega.BeNil())
				err = f.ExpectNetworkLoadBalancerEqual(oldSvc)
				gomega.Expect(err).To(gomega.BeNil())

				npSvc := oldSvc.DeepCopy()
				npSvc.Spec.Type = v1.ServiceTypeNodePort
				_, err = f.Client.KubeClient.PatchService(oldSvc, npSvc)
				gomega.Expect(err).To(gomega.BeNil())

				err = f.ExpectNetworkLoadBalancerDeleted(npSvc)
				gomega.Expect(err).To(gomega.BeNil())

				gomega.Eventually(func(g gomega.Gomega) {
					svc, err := f.Client.KubeClient.GetService()
					g.Expect(err).To(gomega.BeNil())
					g.Expect(svc.Labels).ShouldNot(gomega.HaveKey(helper.LabelServiceHash))
					g.Expect(svc.Labels).ShouldNot(gomega.HaveKey(helper.LabelLoadBalancerId))
				}).WithPolling(10 * time.Second).WithTimeout(2 * time.Minute).Should(gomega.Succeed())

			})
			ginkgo.It("type: ClusterIP -> LoadBalancer", func() {
				svc := f.Client.KubeClient.DefaultService()
				svc.Spec.Type = v1.ServiceTypeClusterIP
				_, err := f.Client.KubeClient.CreateService(svc)
				gomega.Expect(err).To(gomega.BeNil())

				_, _, err = f.FindNetworkLoadBalancer()
				// should not create slb
				gomega.Expect(err).NotTo(gomega.BeNil())

				lbClass := helper.NLBClass

				lbSvc := svc.DeepCopy()
				lbSvc.Spec.Type = v1.ServiceTypeLoadBalancer
				lbSvc.Spec.LoadBalancerClass = &lbClass
				lbSvc.Annotations = make(map[string]string)
				lbSvc.Annotations[annotation.Annotation(annotation.ZoneMaps)] = options.TestConfig.NLBZoneMaps
				lbSvc, err = f.Client.KubeClient.PatchService(svc, lbSvc)
				gomega.Expect(err).To(gomega.BeNil())

				err = f.ExpectNetworkLoadBalancerEqual(lbSvc)
				gomega.Expect(err).To(gomega.BeNil())
			})
			ginkgo.It("type: LoadBalancer -> ClusterIP", func() {
				oldSvc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
					annotation.Annotation(annotation.ZoneMaps): options.TestConfig.NLBZoneMaps,
				})
				gomega.Expect(err).To(gomega.BeNil())
				err = f.ExpectNetworkLoadBalancerEqual(oldSvc)
				gomega.Expect(err).To(gomega.BeNil())

				npSvc := oldSvc.DeepCopy()
				npSvc.Spec.Type = v1.ServiceTypeClusterIP
				_, err = f.Client.KubeClient.PatchService(oldSvc, npSvc)
				gomega.Expect(err).To(gomega.BeNil())

				gomega.Eventually(func(g gomega.Gomega) {
					_, lb, err := f.FindNetworkLoadBalancer()
					g.Expect(lb).To(gomega.BeNil())
					g.Expect(err).NotTo(gomega.BeNil())
				}, "2m").Should(gomega.Succeed())
			})
		})

		ginkgo.Context("can not find nlb", func() {
			ginkgo.It("auto-created nlb & delete nlb", func() {
				oldSvc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
					annotation.Annotation(annotation.ZoneMaps): options.TestConfig.NLBZoneMaps,
				})
				gomega.Expect(err).To(gomega.BeNil())
				err = f.ExpectNetworkLoadBalancerEqual(oldSvc)
				gomega.Expect(err).To(gomega.BeNil())

				_, slb, err := f.FindNetworkLoadBalancer()
				gomega.Expect(err).To(gomega.BeNil())
				err = f.DeleteNetworkLoadBalancer(slb.LoadBalancerAttribute.LoadBalancerId)
				gomega.Expect(err).To(gomega.BeNil())

				err = f.Client.KubeClient.ScaleDeployment(0)
				gomega.Expect(err).To(gomega.BeNil())
				defer func() {
					err = f.Client.KubeClient.ScaleDeployment(3)
					gomega.Expect(err).To(gomega.BeNil())
				}()
				// can not find nlb
				err = f.ExpectNetworkLoadBalancerEqual(oldSvc)
				gomega.Expect(err).NotTo(gomega.BeNil())
			})

			ginkgo.It("auto-created nlb & delete tag", func() {
				oldSvc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
					annotation.Annotation(annotation.ZoneMaps): options.TestConfig.NLBZoneMaps,
				})
				gomega.Expect(err).To(gomega.BeNil())
				err = f.ExpectNetworkLoadBalancerEqual(oldSvc)
				gomega.Expect(err).To(gomega.BeNil())

				_, nlb, err := f.FindNetworkLoadBalancer()
				gomega.Expect(err).To(gomega.BeNil())
				key := helper.TAGKEY
				err = f.Client.CloudClient.UntagNLBResources(context.TODO(), nlb.LoadBalancerAttribute.LoadBalancerId, nlb_model.LoadBalancerTagType, []*string{&key})
				gomega.Expect(err).To(gomega.BeNil())

				err = f.Client.KubeClient.ScaleDeployment(0)
				gomega.Expect(err).To(gomega.BeNil())
				defer func() {
					err = f.Client.KubeClient.ScaleDeployment(3)
					gomega.Expect(err).To(gomega.BeNil())
					err = f.DeleteNetworkLoadBalancer(nlb.LoadBalancerAttribute.LoadBalancerId)
					gomega.Expect(err).To(gomega.BeNil())
				}()

				// find slb by name
				err = f.ExpectNetworkLoadBalancerEqual(oldSvc)
				gomega.Expect(err).To(gomega.BeNil())
			})

			// fixme: this case failed
			//   Expected
			//      <*errors.errorString | 0xc000256a40>: {
			//          s: "timed out waiting for the condition",
			//      }
			//  to be nil
			//  cloud-provider-alibaba-cloud/test/e2e/e2e_test.go:58
			ginkgo.It("auto-created nlb & delete tag & change name", func() {
				oldSvc, err := f.Client.KubeClient.CreateNLBServiceByAnno(
					map[string]string{
						annotation.Annotation(annotation.ZoneMaps):         options.TestConfig.NLBZoneMaps,
						annotation.Annotation(annotation.LoadBalancerName): "test-lb-name",
					})
				gomega.Expect(err).To(gomega.BeNil())
				err = f.ExpectNetworkLoadBalancerEqual(oldSvc)
				gomega.Expect(err).To(gomega.BeNil())

				_, nlb, err := f.FindNetworkLoadBalancer()
				gomega.Expect(err).To(gomega.BeNil())
				key := helper.TAGKEY
				err = f.Client.CloudClient.UntagNLBResources(context.TODO(), nlb.LoadBalancerAttribute.LoadBalancerId, nlb_model.LoadBalancerTagType, []*string{&key})
				gomega.Expect(err).To(gomega.BeNil())

				err = f.Client.KubeClient.ScaleDeployment(0)
				gomega.Expect(err).To(gomega.BeNil())
				defer func() {
					err = f.Client.KubeClient.ScaleDeployment(3)
					gomega.Expect(err).To(gomega.BeNil())
					err = f.DeleteNetworkLoadBalancer(nlb.LoadBalancerAttribute.LoadBalancerId)
					gomega.Expect(err).To(gomega.BeNil())
				}()

				// can not find nlb
				err = f.ExpectNetworkLoadBalancerEqual(oldSvc)
				gomega.Expect(err).NotTo(gomega.BeNil())
			})
		})

		if options.TestConfig.SecurityGroupIDs != "" {
			securityGroupIDs := strings.Split(options.TestConfig.SecurityGroupIDs, ",")
			ginkgo.Context("nlb security group ids", func() {
				ginkgo.It("one security group", func() {
					svc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
						annotation.Annotation(annotation.ZoneMaps):         options.TestConfig.NLBZoneMaps,
						annotation.Annotation(annotation.SecurityGroupIds): securityGroupIDs[0],
					})
					gomega.Expect(err).To(gomega.BeNil())
					err = f.ExpectNetworkLoadBalancerEqual(svc)
					gomega.Expect(err).To(gomega.BeNil())
				})

				ginkgo.It("none to one security group", func() {
					oldSvc, err := f.Client.KubeClient.CreateNLBServiceWithoutSelector(map[string]string{
						annotation.Annotation(annotation.ZoneMaps): options.TestConfig.NLBZoneMaps,
					})
					gomega.Expect(err).To(gomega.BeNil())
					err = f.ExpectNetworkLoadBalancerEqual(oldSvc)
					gomega.Expect(err).To(gomega.BeNil())

					newSvc := oldSvc.DeepCopy()
					newSvc.Annotations[annotation.Annotation(annotation.SecurityGroupIds)] = securityGroupIDs[0]
					newSvc, err = f.Client.KubeClient.PatchService(oldSvc, newSvc)
					gomega.Expect(err).To(gomega.BeNil())
					err = f.ExpectNetworkLoadBalancerEqual(newSvc)
					gomega.Expect(err).To(gomega.BeNil())
				})

				ginkgo.It("set security group annotation to empty", func() {
					oldSvc, err := f.Client.KubeClient.CreateNLBServiceWithoutSelector(map[string]string{
						annotation.Annotation(annotation.ZoneMaps):         options.TestConfig.NLBZoneMaps,
						annotation.Annotation(annotation.SecurityGroupIds): securityGroupIDs[0],
					})
					gomega.Expect(err).To(gomega.BeNil())
					err = f.ExpectNetworkLoadBalancerEqual(oldSvc)
					gomega.Expect(err).To(gomega.BeNil())

					newSvc := oldSvc.DeepCopy()
					newSvc.Annotations[annotation.Annotation(annotation.SecurityGroupIds)] = ""
					newSvc, err = f.Client.KubeClient.PatchService(oldSvc, newSvc)
					gomega.Expect(err).To(gomega.BeNil())
					err = f.ExpectNetworkLoadBalancerEqual(newSvc)
					gomega.Expect(err).To(gomega.BeNil())
				})

				ginkgo.It("delete security group annotation", func() {
					oldSvc, err := f.Client.KubeClient.CreateNLBServiceWithoutSelector(map[string]string{
						annotation.Annotation(annotation.ZoneMaps):         options.TestConfig.NLBZoneMaps,
						annotation.Annotation(annotation.SecurityGroupIds): securityGroupIDs[0],
					})
					gomega.Expect(err).To(gomega.BeNil())
					err = f.ExpectNetworkLoadBalancerEqual(oldSvc)
					gomega.Expect(err).To(gomega.BeNil())

					newSvc := oldSvc.DeepCopy()
					delete(newSvc.Annotations, annotation.Annotation(annotation.SecurityGroupIds))
					newSvc, err = f.Client.KubeClient.PatchService(oldSvc, newSvc)
					gomega.Expect(err).To(gomega.BeNil())
					err = f.ExpectNetworkLoadBalancerEqual(oldSvc)
					gomega.Expect(err).To(gomega.BeNil())
				})

				if len(securityGroupIDs) >= 2 {
					ids := strings.Join(securityGroupIDs[0:2], ",")
					ginkgo.It("two security groups", func() {
						svc, err := f.Client.KubeClient.CreateNLBServiceByAnno(map[string]string{
							annotation.Annotation(annotation.ZoneMaps):         options.TestConfig.NLBZoneMaps,
							annotation.Annotation(annotation.SecurityGroupIds): ids,
						})
						gomega.Expect(err).To(gomega.BeNil())
						err = f.ExpectNetworkLoadBalancerEqual(svc)
						gomega.Expect(err).To(gomega.BeNil())
					})

					ginkgo.It("security group a -> b", func() {
						oldSvc, err := f.Client.KubeClient.CreateNLBServiceWithoutSelector(map[string]string{
							annotation.Annotation(annotation.ZoneMaps):         options.TestConfig.NLBZoneMaps,
							annotation.Annotation(annotation.SecurityGroupIds): securityGroupIDs[0],
						})
						gomega.Expect(err).To(gomega.BeNil())
						err = f.ExpectNetworkLoadBalancerEqual(oldSvc)
						gomega.Expect(err).To(gomega.BeNil())

						newSvc := oldSvc.DeepCopy()
						newSvc.Annotations[annotation.Annotation(annotation.SecurityGroupIds)] = securityGroupIDs[1]
						newSvc, err = f.Client.KubeClient.PatchService(oldSvc, newSvc)
						gomega.Expect(err).To(gomega.BeNil())
						err = f.ExpectNetworkLoadBalancerEqual(newSvc)
						gomega.Expect(err).To(gomega.BeNil())
					})

					ginkgo.It("security group a -> a&b", func() {
						oldSvc, err := f.Client.KubeClient.CreateNLBServiceWithoutSelector(map[string]string{
							annotation.Annotation(annotation.ZoneMaps):         options.TestConfig.NLBZoneMaps,
							annotation.Annotation(annotation.SecurityGroupIds): securityGroupIDs[0],
						})
						gomega.Expect(err).To(gomega.BeNil())
						err = f.ExpectNetworkLoadBalancerEqual(oldSvc)
						gomega.Expect(err).To(gomega.BeNil())

						newSvc := oldSvc.DeepCopy()
						newSvc.Annotations[annotation.Annotation(annotation.SecurityGroupIds)] = ids
						newSvc, err = f.Client.KubeClient.PatchService(oldSvc, newSvc)
						gomega.Expect(err).To(gomega.BeNil())
						err = f.ExpectNetworkLoadBalancerEqual(newSvc)
						gomega.Expect(err).To(gomega.BeNil())
					})

				}
			})
		}
	})
}
