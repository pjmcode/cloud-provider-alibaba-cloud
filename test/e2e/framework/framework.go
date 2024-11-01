package framework

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/alibabacloud-go/tea/tea"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/cloud-provider-alibaba-cloud/pkg/model"
	nlbmodel "k8s.io/cloud-provider-alibaba-cloud/pkg/model/nlb"
	"k8s.io/cloud-provider-alibaba-cloud/pkg/util"
	"k8s.io/cloud-provider-alibaba-cloud/test/e2e/client"
	"k8s.io/cloud-provider-alibaba-cloud/test/e2e/options"
	"k8s.io/klog/v2"
)

type ResourceType string

const (
	SLBResource = "SLB"
	NLBResource = "NLB"
	ACLResource = "ACL"
	CBPResource = "CommonBandwidthPackage"
)

type Framework struct {
	Client          *client.E2EClient
	CreatedResource map[string]string
}

func NewFrameWork(c *client.E2EClient) *Framework {
	return &Framework{
		Client:          c,
		CreatedResource: make(map[string]string, 0),
	}
}

func (f *Framework) BeforeSuit() error {
	err := f.Client.KubeClient.CreateNamespace()
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			err = f.Client.KubeClient.DeleteService()
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}
	if err := f.Client.KubeClient.CreateDeployment(); err != nil {
		return err
	}

	if options.TestConfig.EnableVK {
		if err := f.Client.KubeClient.CreateECIPod(); err != nil {
			return err
		}
		if _, err := f.Client.KubeClient.GetVkNodes(10); err != nil {
			return err
		}
		if _, err := f.Client.KubeClient.GetVkPods(10); err != nil {
			return err
		}
	}

	return nil
}

func (f *Framework) AfterSuit() error {
	err := f.Client.KubeClient.DeleteNamespace()
	if err != nil {
		return err
	}
	return f.CleanCloudResources()
}

func (f *Framework) AfterEachClb() error {
	svc, err := f.Client.KubeClient.GetService()
	if err != nil {
		if apierrors.IsNotFound(err) {
			klog.Infof("skip delete service, service not found")
			return nil
		}
		return err
	}
	if err = f.Client.KubeClient.DeleteService(); err != nil {
		return fmt.Errorf("delete test service error: %s", err.Error())
	}

	return f.ExpectLoadBalancerDeleted(svc)
}

func (f *Framework) AfterEachNlb() error {
	svc, err := f.Client.KubeClient.GetService()
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if err = f.Client.KubeClient.DeleteService(); err != nil {
		return fmt.Errorf("delete service failed: %s", err)
	}

	return f.ExpectNetworkLoadBalancerDeleted(svc)
}

func (f *Framework) CreateCloudResource() error {
	f.CreatedResource = make(map[string]string, 0)
	region, err := f.Client.CloudClient.Region()
	if err != nil {
		return err
	}
	if options.TestConfig.InternetLoadBalancerID == "" {
		slbM := &model.LoadBalancer{
			LoadBalancerAttribute: model.LoadBalancerAttribute{
				AddressType:      model.InternetAddressType,
				LoadBalancerSpec: model.S1Small,
				RegionId:         region,
				LoadBalancerName: fmt.Sprintf("%s-%s-slb", options.TestConfig.ClusterId, "internet"),
			},
		}
		if err := f.Client.CloudClient.FindLoadBalancerByName(slbM); err != nil {
			return err
		}
		if slbM.LoadBalancerAttribute.LoadBalancerId == "" {
			if err := f.Client.CloudClient.CreateLoadBalancer(context.TODO(), slbM, ""); err != nil {
				return fmt.Errorf("create internet slb error: %s", err.Error())
			}
		}
		options.TestConfig.InternetLoadBalancerID = slbM.LoadBalancerAttribute.LoadBalancerId
		f.CreatedResource[options.TestConfig.InternetLoadBalancerID] = SLBResource

		vsg := []model.VServerGroup{
			{
				VGroupName: "test1",
			},
			{
				VGroupName: "test2",
			},
		}
		remotevsg, err := f.Client.CloudClient.DescribeVServerGroups(context.TODO(), slbM.LoadBalancerAttribute.LoadBalancerId)
		if err != nil {
			return err
		}
		for i, lvs := range vsg {
			found := false
			for _, rvs := range remotevsg {
				if lvs.VGroupName == rvs.VGroupName {
					found = true
					vsg[i].VGroupId = rvs.VGroupId
					break
				}
			}
			if found {
				continue
			} else {
				err = f.Client.CloudClient.CreateVServerGroup(context.TODO(), &vsg[i], options.TestConfig.InternetLoadBalancerID)
				if err != nil {
					return fmt.Errorf("create vserver group error: %s", err.Error())
				}
			}

		}

		options.TestConfig.VServerGroupID = vsg[0].VGroupId
		options.TestConfig.VServerGroupID2 = vsg[1].VGroupId
	}

	if options.TestConfig.IntranetLoadBalancerID == "" {
		vswId, err := f.Client.CloudClient.VswitchID()
		if err != nil {
			return fmt.Errorf("get vsw id error: %s", err.Error())
		}
		slbM := &model.LoadBalancer{
			LoadBalancerAttribute: model.LoadBalancerAttribute{
				AddressType:      model.IntranetAddressType,
				LoadBalancerSpec: model.S1Small,
				RegionId:         region,
				VSwitchId:        vswId,
				LoadBalancerName: fmt.Sprintf("%s-%s-slb", options.TestConfig.ClusterId, "intranet"),
			},
		}
		if err := f.Client.CloudClient.FindLoadBalancerByName(slbM); err != nil {
			return err
		}
		if slbM.LoadBalancerAttribute.LoadBalancerId == "" {
			if err := f.Client.CloudClient.CreateLoadBalancer(context.TODO(), slbM, ""); err != nil {
				return fmt.Errorf("create intranet slb error: %s", err.Error())
			}
		}
		options.TestConfig.IntranetLoadBalancerID = slbM.LoadBalancerAttribute.LoadBalancerId
		f.CreatedResource[options.TestConfig.IntranetLoadBalancerID] = SLBResource
	}

	if options.TestConfig.EnableNLBTest {
		zoneMappings, err := parseZoneMappings(options.TestConfig.NLBZoneMaps)
		if err != nil {
			return err
		}
		if options.TestConfig.InternetNetworkLoadBalancerID == "" {
			slbM := &nlbmodel.NetworkLoadBalancer{
				LoadBalancerAttribute: &nlbmodel.LoadBalancerAttribute{
					AddressType:  nlbmodel.InternetAddressType,
					ZoneMappings: zoneMappings,
					VpcId:        options.TestConfig.VPCID,
					Name:         fmt.Sprintf("%s-%s-nlb", options.TestConfig.ClusterId, "internet"),
				},
			}

			if err := f.Client.CloudClient.FindNLBByName(context.TODO(), slbM); err != nil {
				return err
			}
			if slbM.LoadBalancerAttribute.LoadBalancerId == "" {
				if err := f.Client.CloudClient.CreateNLB(context.TODO(), slbM); err != nil {
					return fmt.Errorf("create internet nlb error: %s", err.Error())
				}
			}
			options.TestConfig.InternetNetworkLoadBalancerID = slbM.LoadBalancerAttribute.LoadBalancerId
			f.CreatedResource[options.TestConfig.InternetNetworkLoadBalancerID] = NLBResource
		}

		if options.TestConfig.IntranetNetworkLoadBalancerID == "" {
			slbM := &nlbmodel.NetworkLoadBalancer{
				LoadBalancerAttribute: &nlbmodel.LoadBalancerAttribute{
					AddressType:  nlbmodel.IntranetAddressType,
					ZoneMappings: zoneMappings,
					VpcId:        options.TestConfig.VPCID,
					Name:         fmt.Sprintf("%s-%s-nlb", options.TestConfig.ClusterId, "intranet"),
				},
			}

			if err := f.Client.CloudClient.FindNLBByName(context.TODO(), slbM); err != nil {
				return err
			}
			if slbM.LoadBalancerAttribute.LoadBalancerId == "" {
				if err := f.Client.CloudClient.CreateNLB(context.TODO(), slbM); err != nil {
					return fmt.Errorf("create intranet nlb error: %s", err.Error())
				}
			}
			options.TestConfig.IntranetNetworkLoadBalancerID = slbM.LoadBalancerAttribute.LoadBalancerId
			f.CreatedResource[options.TestConfig.IntranetNetworkLoadBalancerID] = NLBResource
		}
		if options.TestConfig.CommonBandwidthPackageId == "" {
			CBP, err := f.Client.CloudClient.CreateCommonBandwidthPackage()
			if err != nil {
				return fmt.Errorf("create common bandwidth package error: %s", err.Error())
			}
			options.TestConfig.CommonBandwidthPackageId = CBP
			f.CreatedResource[options.TestConfig.CommonBandwidthPackageId] = CBPResource
		}
	}

	if options.TestConfig.AclID == "" {
		aclName := fmt.Sprintf("%s-acl-%s", options.TestConfig.ClusterId, "a")
		aclId, err := f.Client.CloudClient.DescribeAccessControlList(context.TODO(), aclName)
		if err != nil {
			return fmt.Errorf("DescribeAccessControlList error: %s", err.Error())
		}
		if aclId == "" {
			aclId, err = f.Client.CloudClient.CreateAccessControlList(context.TODO(), aclName)
			if err != nil {
				return fmt.Errorf("CreateAccessControlList error: %s", err.Error())
			}
		}
		options.TestConfig.AclID = aclId
		f.CreatedResource[aclId] = ACLResource
	}

	if options.TestConfig.AclID2 == "" {
		aclName := fmt.Sprintf("%s-acl-%s", options.TestConfig.ClusterId, "b")
		aclId, err := f.Client.CloudClient.DescribeAccessControlList(context.TODO(), aclName)
		if err != nil {
			return fmt.Errorf("DescribeAccessControlList error: %s", err.Error())
		}
		if aclId == "" {
			aclId, err = f.Client.CloudClient.CreateAccessControlList(context.TODO(), aclName)
			if err != nil {
				return fmt.Errorf("CreateAccessControlList error: %s", err.Error())
			}
		}
		options.TestConfig.AclID2 = aclId
		f.CreatedResource[aclId] = ACLResource
	}

	klog.Infof("created resource: %s", util.PrettyJson(f.CreatedResource))
	return nil
}

func (f *Framework) DeleteLoadBalancer(lbid string) error {
	region, err := f.Client.CloudClient.Region()
	if err != nil {
		return err
	}
	slbM := &model.LoadBalancer{
		LoadBalancerAttribute: model.LoadBalancerAttribute{
			LoadBalancerId: lbid,
			RegionId:       region,
		},
	}
	err = f.Client.CloudClient.SetLoadBalancerDeleteProtection(context.TODO(), lbid, string(model.OffFlag))
	if err != nil {
		return err
	}

	err = f.Client.CloudClient.DeleteLoadBalancer(context.TODO(), slbM)
	if err != nil {
		return err
	}
	return nil
}

func (f *Framework) CleanCloudResources() error {
	klog.Infof("try to clean cloud resources: %+v", f.CreatedResource)
	for key, value := range f.CreatedResource {
		switch value {
		case SLBResource:
			if err := f.DeleteLoadBalancer(key); err != nil {
				return err
			}
		case NLBResource:
			if err := f.DeleteNetworkLoadBalancer(key); err != nil {
				return err
			}
		case ACLResource:
			if err := f.Client.CloudClient.DeleteAccessControlList(context.TODO(), key); err != nil {
				return err
			}
		case CBPResource:
			if err := f.Client.CloudClient.DeleteCommonBandwidthPackage(context.TODO(), key, "true"); err != nil {
				return err
			}
		}
	}
	return nil
}

func (f *Framework) DeleteNetworkLoadBalancer(lbid string) error {
	slbM := &nlbmodel.NetworkLoadBalancer{
		LoadBalancerAttribute: &nlbmodel.LoadBalancerAttribute{
			LoadBalancerId: lbid,
		},
	}
	if lbid == "" {
		klog.Infof("nlb id is empty, skip delete")
		return fmt.Errorf("DeleteNetworkLoadBalancer but nlb id is empty")
	}
	err := f.Client.CloudClient.UpdateLoadBalancerProtection(context.TODO(), lbid,
		&nlbmodel.DeletionProtectionConfig{Enabled: false}, nil)
	if err != nil {
		klog.Errorf("update nlb %s deletion protection error: %s", lbid, err.Error())
		return err
	}
	err = f.Client.CloudClient.DeleteNLB(context.TODO(), slbM)
	if err != nil {
		klog.Errorf("delete nlb %s error: %s", lbid, err.Error())
		return err
	}
	return nil
}

// format: <major>.<minor>.<patch>
// return versionA >= versionB
func CompareVersion(versionA, versionB string) bool {
	defer func() {
		if r := recover(); r != nil {
			klog.Errorf("%v %v compareVersion panic %v", versionA, versionB, r)
			os.Exit(1)
		}
	}()
	klog.Infof("compareVersion %s %s", versionA, versionB)
	A := strings.TrimLeft(strings.TrimSpace(versionA), "v")
	B := strings.TrimLeft(strings.TrimSpace(versionB), "v")
	A = strings.TrimSuffix(A, "-aliyun.1")
	B = strings.TrimSuffix(B, "-aliyun.1")

	KubeVersion_A := strings.SplitN(A, ".", 3)
	KubeVersion_B := strings.SplitN(B, ".", 3)
	for len(KubeVersion_A) < 3 {
		KubeVersion_A = append(KubeVersion_A, "0")
	}
	for len(KubeVersion_B) < 3 {
		KubeVersion_B = append(KubeVersion_B, "0")
	}

	klog.Infof("compareVersion %s %s", KubeVersion_A, KubeVersion_B)
	major_a, err := strconv.Atoi(KubeVersion_A[0])
	if err != nil {
		klog.Errorf("compareVersion Atoi %s", err)
	}
	major_b, err := strconv.Atoi(KubeVersion_B[0])
	if err != nil {
		klog.Errorf("compareVersion Atoi %s", err)
	}
	if major_a > major_b {
		klog.Infof("compareVersion %s >= %s true", KubeVersion_A, KubeVersion_B)
		return true
	} else if major_a < major_b {
		return false
	} else {
		minor_a, err := strconv.Atoi(KubeVersion_A[1])
		if err != nil {
			klog.Errorf("compareVersion Atoi %s", err)
		}
		minor_b, err := strconv.Atoi(KubeVersion_B[1])
		if err != nil {
			klog.Errorf("compareVersion Atoi %s", err)
		}
		if minor_a > minor_b {
			klog.Infof("compareVersion %s >= %s true", KubeVersion_A, KubeVersion_B)
			return true
		} else if minor_a < minor_b {
			return false
		} else {
			patch_a, err := strconv.Atoi(KubeVersion_A[2])
			if err != nil {
				klog.Errorf("compareVersion Atoi %s", err)
			}
			patch_b, err := strconv.Atoi(KubeVersion_B[2])
			if err != nil {
				klog.Errorf("compareVersion Atoi %s", err)
			}
			if patch_a >= patch_b {
				klog.Infof("compareVersion %s >= %s true", KubeVersion_A, KubeVersion_B)
				return true
			} else {
				klog.Infof("compareVersion %s >= %s false", KubeVersion_A, KubeVersion_B)
				return false
			}
		}
	}

}

func EnableReadinessGate() bool {
	// terway
	// Cloud Controller Manager >= v2.10.0
	// cluster >= v1.24.0
	network := tea.StringValue(options.TestConfig.AckCluster.Parameters["Network"])
	clusterVersion := tea.StringValue(options.TestConfig.AckCluster.CurrentVersion)
	ccmVersion := options.TestConfig.CCM_Version
	return strings.Contains(network, "terway") && CompareVersion(clusterVersion, "v1.24.0") && CompareVersion(ccmVersion, "v2.10.0")
}
