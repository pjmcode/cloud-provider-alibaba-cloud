package e2e

import (
	"strings"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"k8s.io/cloud-provider-alibaba-cloud/pkg/util"
	"k8s.io/cloud-provider-alibaba-cloud/test/e2e/client"
	"k8s.io/cloud-provider-alibaba-cloud/test/e2e/framework"
	"k8s.io/cloud-provider-alibaba-cloud/test/e2e/options"
	"k8s.io/cloud-provider-alibaba-cloud/test/e2e/testcase/node"
	"k8s.io/cloud-provider-alibaba-cloud/test/e2e/testcase/route"
	"k8s.io/cloud-provider-alibaba-cloud/test/e2e/testcase/service/clbv1"
	"k8s.io/cloud-provider-alibaba-cloud/test/e2e/testcase/service/nlbv2"
	"k8s.io/klog/v2"
)

func init() {
	options.TestConfig.BindFlags()
}

func TestE2E(t *testing.T) {
	err := options.TestConfig.Validate()
	if err != nil {
		t.Fatalf("test config validate failed: %s", err.Error())
	}

	c, err := client.NewClient()
	if err != nil {
		t.Fatalf("create client error: %s", err.Error())
	}
	f := framework.NewFrameWork(c)
	if err := f.Client.InitOptions(); err != nil {
		t.Fatalf("init option error: %s", err.Error())
	}
	if options.TestConfig.AllowCreateCloudResource {
		if err := f.CreateCloudResource(); err != nil {
			t.Fatalf("create cloud resource error: %s", err.Error())
		}
	}
	klog.Infof("test config: %s", util.PrettyJson(options.TestConfig))

	ginkgo.BeforeSuite(func() {
		err = f.BeforeSuit()
		gomega.Expect(err).To(gomega.BeNil())
	})

	ginkgo.AfterSuite(func() {
		err = f.AfterSuit()
		gomega.Expect(err).To(gomega.BeNil())
	})

	gomega.RegisterFailHandler(ginkgo.Fail)

	ginkgo.Describe("Run cloud controller manager e2e tests", func() {
		AddControllerTests(f)
	})

	ginkgo.RunSpecs(t, "run ccm e2e test")
}

func AddControllerTests(f *framework.Framework) {
	controllers := strings.Split(options.TestConfig.Controllers, ",")
	if len(controllers) == 0 {
		klog.Info("no controller tests need to run, finished")
		return
	}
	if !options.TestConfig.EnableNLBTest && !options.TestConfig.EnableCLBTest {
		klog.Warningf("enableCLBTest and enableNLBTest are both false, skip service controller tests")
	}
	for _, c := range controllers {
		switch c {
		case "service":
			if options.TestConfig.EnableCLBTest {
				ginkgo.Describe("clb service controller tests", func() {
					clbv1.RunLoadBalancerTestCases(f)
					clbv1.RunListenerTestCases(f)
					clbv1.RunBackendTestCases(f)
				})
			}

			if options.TestConfig.NLBZoneMaps != "" && options.TestConfig.EnableNLBTest {
				ginkgo.Describe("nlb service controller tests", func() {
					nlbv2.RunLoadBalancerTestCases(f)
					nlbv2.RunListenerTestCases(f)
					nlbv2.RunBackendTestCases(f)
				})
			} else {
				klog.Warningf("NLBZoneMaps is empty or not EnableNLBTest, skip NLB service tests")
			}

		case "node":
			ginkgo.Describe("node controller tests", func() {
				node.RunNodeControllerTestCases(f)
			})
		case "route":
			if options.TestConfig.Network == options.Flannel {
				ginkgo.Describe("route controller tests", func() {
					route.RunRouteControllerTestCases(f)
				})
			}
		default:
			klog.Infof("%s controller is not supported", c)
		}

	}
}
