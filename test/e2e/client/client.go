package client

import (
	"context"
	"fmt"
	"os"
	"strings"

	vpcsdk "github.com/aliyun/alibaba-cloud-sdk-go/services/vpc"
	"k8s.io/client-go/kubernetes"
	ctrlCfg "k8s.io/cloud-provider-alibaba-cloud/pkg/config"
	"k8s.io/cloud-provider-alibaba-cloud/pkg/provider/alibaba"
	"k8s.io/cloud-provider-alibaba-cloud/pkg/provider/alibaba/base"
	"k8s.io/cloud-provider-alibaba-cloud/pkg/provider/alibaba/cas"
	"k8s.io/cloud-provider-alibaba-cloud/pkg/provider/alibaba/ecs"
	"k8s.io/cloud-provider-alibaba-cloud/pkg/provider/alibaba/nlb"
	"k8s.io/cloud-provider-alibaba-cloud/pkg/provider/alibaba/pvtz"
	"k8s.io/cloud-provider-alibaba-cloud/pkg/provider/alibaba/slb"
	"k8s.io/cloud-provider-alibaba-cloud/pkg/provider/alibaba/vpc"
	"k8s.io/cloud-provider-alibaba-cloud/test/e2e/options"
	"k8s.io/klog/v2"
	runtime "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

type E2EClient struct {
	CloudClient   *alibaba.AlibabaCloud
	KubeClient    *KubeClient
	RuntimeClient runtime.Client
	ACKClient     *ACKClient
}

var DefaultRetryTimes = 3

func NewClient() (*E2EClient, error) {
	ctrlCfg.ControllerCFG.CloudConfigPath = options.TestConfig.CloudConfig

	ackClient, err := NewACKClient()
	if err != nil {
		panic(fmt.Sprintf("initialize alibaba client: %s", err.Error()))
	}

	if err := InitCloudConfig(ackClient); err != nil {
		panic(fmt.Sprintf("init cloud config error: %s", err.Error()))
	}
	mgr, err := base.NewClientMgr()
	if err != nil || mgr == nil {
		return nil, fmt.Errorf("initialize alibaba cloud client auth error: %v", err)
	}
	err = mgr.Start(base.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("refresh token: %s", err.Error())
	}
	cc := &alibaba.AlibabaCloud{
		IMetaData:    mgr.Meta,
		ECSProvider:  ecs.NewECSProvider(mgr),
		SLBProvider:  slb.NewLBProvider(mgr),
		PVTZProvider: pvtz.NewPVTZProvider(mgr),
		VPCProvider:  vpc.NewVPCProvider(mgr),
		NLBProvider:  nlb.NewNLBProvider(mgr),
		CASProvider:  cas.NewCASProvider(mgr),
	}

	cfg := config.GetConfigOrDie()
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		panic(fmt.Sprintf("new client : %s", err.Error()))
	}

	runtimeClient, err := runtime.New(cfg, runtime.Options{})
	if err != nil {
		panic(fmt.Sprintf("new runtime client error: %s", err.Error()))
	}

	return &E2EClient{
		CloudClient:   cc,
		KubeClient:    NewKubeClient(kubeClient),
		RuntimeClient: runtimeClient,
		ACKClient:     ackClient,
	}, nil
}

func InitCloudConfig(client *ACKClient) error {
	ack, err := client.DescribeClusterDetail(options.TestConfig.ClusterId)
	if err != nil {
		return err
	}
	if ctrlCfg.CloudCFG.Global.Region == "" {
		ctrlCfg.CloudCFG.Global.Region = *ack.RegionId
	}
	if ctrlCfg.CloudCFG.Global.ClusterID == "" {
		ctrlCfg.CloudCFG.Global.ClusterID = *ack.ClusterId
	}
	if ctrlCfg.CloudCFG.Global.VswitchID == "" {
		vswitchIds := strings.Split(*ack.VswitchId, ",")
		if len(vswitchIds) > 1 {
			ctrlCfg.CloudCFG.Global.VswitchID = vswitchIds[0]
		} else {
			ctrlCfg.CloudCFG.Global.VswitchID = *ack.VswitchId
		}
	}
	if ctrlCfg.CloudCFG.Global.VpcID == "" {
		ctrlCfg.CloudCFG.Global.VpcID = *ack.VpcId
	}

	return nil
}

func (client *E2EClient) InitOptions() error {
	ack, err := client.ACKClient.DescribeClusterDetail(options.TestConfig.ClusterId)
	if err != nil {
		return err
	}
	options.TestConfig.AckCluster = ack
	options.TestConfig.ClusterType = *ack.ClusterType
	if ack.SubnetCidr == nil || *ack.SubnetCidr == "" {
		options.TestConfig.Network = options.Terway
		if err := os.Setenv("SERVICE_FORCE_BACKEND_ENI", "true"); err != nil {
			return err
		}
	} else {
		options.TestConfig.Network = options.Flannel
		if err := os.Setenv("SERVICE_FORCE_BACKEND_ENI", "false"); err != nil {
			return err
		}
	}
	// ccm version
	addon, err := client.ACKClient.GetClusterAddonInstance(options.TestConfig.ClusterId, "cloud-controller-manager")
	if err != nil || addon.Version == "" {
		return fmt.Errorf("GetClusterAddonInstance ccm error: %s", err.Error())
	}
	if addon.Version == "" {
		return fmt.Errorf("GetClusterAddonInstance ccm error:version is empty %v", addon)
	}
	klog.Info(addon)
	options.TestConfig.CCM_Version = addon.Version

	if options.TestConfig.VSwitchID == "" {
		vswId, err := client.CloudClient.VswitchID()
		if err != nil {
			return err
		}
		if vswId != "" {
			options.TestConfig.VSwitchID = vswId
		} else {
			vswitchIds := strings.Split(*ack.VswitchId, ",")
			if len(vswitchIds) > 1 {
				options.TestConfig.VSwitchID = vswitchIds[0]
			} else {
				options.TestConfig.VSwitchID = *ack.VswitchId
			}
		}
	}

	if options.TestConfig.VPCID == "" {
		vpcId, err := client.CloudClient.VpcID()
		if err != nil {
			return err
		}
		if vpcId != "" {
			options.TestConfig.VPCID = vpcId
		} else {
			options.TestConfig.VPCID = *ack.VpcId
		}
	}
	vsws, err := client.CloudClient.DescribeVSwitches(context.TODO(), options.TestConfig.VPCID)
	if err != nil {
		return err
	}
	if options.TestConfig.VSwitchID2 == "" {
		found := false
		for _, v := range vsws {
			if v.VSwitchId != options.TestConfig.VSwitchID {
				options.TestConfig.VSwitchID2 = v.VSwitchId
				found = true
				break
			}
		}
		if !found {
			klog.Warningf("vpc %s has no available vsws, VSwitchID2 is nil", options.TestConfig.VPCID)
		}
	}
	if options.TestConfig.IPv6 {
		for _, v := range vsws {
			if v.Ipv6CidrBlock == "" {
				options.TestConfig.IPv6 = false
				klog.Warningf("vpc %s has no available ipv6 cidr, IPv6 is false", options.TestConfig.VPCID)
				break
			}
		}
		ipv6Gateways, err := client.CloudClient.DescribeIpv6Gateways(context.TODO(), options.TestConfig.VPCID)
		if err != nil {
			return err
		}
		if len(ipv6Gateways) == 0 {
			klog.Warningf("vpc %s has no available ipv6 gateway, IPv6 is false", options.TestConfig.VPCID)
			options.TestConfig.IPv6 = false
			if options.TestConfig.AllowCreateCloudResource {
				ipv6_Gateway, err := client.CloudClient.CreateIpv6Gateway(context.TODO(), options.TestConfig.VPCID)
				if err != nil {
					klog.Warningf("create ipv6 gateway error: %s", err.Error())
					options.TestConfig.IPv6 = false
				}
				klog.Infof("for test create ipv6 gateway: %s", ipv6_Gateway)
				options.TestConfig.IPv6 = true
			}
		} else {
			for _, g := range ipv6Gateways {
				if g.Status != "Available" {
					options.TestConfig.IPv6 = false
					klog.Warningf("vpc %s has no available ipv6 gateway, IPv6 is false", options.TestConfig.VPCID)
				}
			}
		}
	}

	if options.TestConfig.MasterZoneID == "" || options.TestConfig.SlaveZoneID == "" {
		resources, err := client.CloudClient.DescribeAvailableResource(context.TODO(), "classic_internet", "ipv4")
		if err != nil {
			return fmt.Errorf("describe available slb resources error: %s", err.Error())
		}
		if len(resources) < 2 {
			return fmt.Errorf("no available slb resource, skip create internet slb")
		}
		options.TestConfig.MasterZoneID = resources[0].MasterZoneId
		options.TestConfig.SlaveZoneID = resources[0].SlaveZoneId
	}

	addon, err = client.ACKClient.GetClusterAddonInstance(options.TestConfig.ClusterId, "ack-virtual-node")
	if err != nil {
		if strings.Contains(err.Error(), "AddonNotFound") {
			options.TestConfig.EnableVK = false
		} else {
			return fmt.Errorf("GetClusterAddonInstance error: %s", err.Error())
		}
	} else {
		if addon.Version != "" {
			options.TestConfig.EnableVK = true
		}
	}
	klog.Info(addon)

	if options.TestConfig.CertID == "" || options.TestConfig.CertID2 == "" {
		certs, err := client.CloudClient.DescribeServerCertificates(context.TODO())
		if err != nil {
			return fmt.Errorf("DescribeServerCertificates error: %s", err.Error())
		}
		if len(certs) != 0 {
			for _, cert := range certs {
				if options.TestConfig.CertID == "" {
					options.TestConfig.CertID = cert
					continue
				}
				if options.TestConfig.CertID2 == "" && cert != options.TestConfig.CertID {
					options.TestConfig.CertID2 = cert
				}
			}
		}
	}

	if options.TestConfig.CACertID == "" {
		cacerts, err := client.CloudClient.DescribeCACertificates(context.TODO())
		if err != nil {
			return fmt.Errorf("DescribeCACertificates error: %s", err.Error())
		}
		if len(cacerts) > 0 {
			options.TestConfig.CACertID = cacerts[0]
		}
	}
	// NLB test
	if options.TestConfig.EnableNLBTest {
		if options.TestConfig.NLBZoneMaps == "" {
			regions, err := client.CloudClient.NLBRegionIds()
			if err != nil {
				return err
			}

			found := false
			for _, r := range regions {
				if r == *ack.RegionId {
					found = true
					break
				}
			}
			if found {
				zones, err := client.CloudClient.NLBZoneIds(*ack.RegionId)
				vsws, err := client.CloudClient.DescribeVSwitches(context.TODO(), *ack.VpcId)
				if err != nil {
					return err
				}

				var results []vpcsdk.VSwitch
				zoneMaps := make(map[string]bool)
				for _, vsw := range vsws {
					for _, zone := range zones {
						if vsw.ZoneId == zone && !zoneMaps[zone] {
							results = append(results, vsw)
							zoneMaps[zone] = true
							break
						}
					}

					if len(results) >= 2 {
						break
					}
				}

				if len(results) >= 2 {
					var mappings []string
					for _, vsw := range results {
						mappings = append(mappings, fmt.Sprintf("%s:%s", vsw.ZoneId, vsw.VSwitchId))
					}

					options.TestConfig.NLBZoneMaps = strings.Join(mappings, ",")
					klog.Infof("NLBZoneMaps set to [%s]", options.TestConfig.NLBZoneMaps)
				} else {
					klog.Warningf("no enough vswitches in nlb supported zones, supported zones in region %s are [%v]", *ack.RegionId, zones)
					klog.Warningln("no enough vswitches in nlb supported zones, close nlb test")
					options.TestConfig.EnableNLBTest = false
				}

			} else {
				klog.Warningf("region %s does not support nlb, ZoneMaps is empty", *ack.RegionId)
				klog.Warningln("cluster region %s does not support nlb, should select from %+v", ack.RegionId, regions)
				options.TestConfig.EnableNLBTest = false
			}
		}
		if options.TestConfig.NLBCertID == "" || options.TestConfig.NLBCertID2 == "" {
			ssl_certs, err := client.CloudClient.DescribeSSLCertificateList(context.TODO())
			if err != nil {
				return fmt.Errorf("DescribeSSLCertificateList error: %s", err.Error())
			}
			if len(ssl_certs) != 0 {
				for _, ssl_cert := range ssl_certs {
					if options.TestConfig.NLBCertID == "" {
						options.TestConfig.NLBCertID = ssl_cert.CertIdentifier
						continue
					}
					if options.TestConfig.NLBCertID2 == "" && ssl_cert.CertIdentifier != options.TestConfig.NLBCertID {
						options.TestConfig.NLBCertID2 = ssl_cert.CertIdentifier
					}
				}
			}
		}
		if options.TestConfig.CommonBandwidthPackageId == "" {
			CBP, _ := client.CloudClient.DescribeCommonBandwidthPackages()
			if len(CBP) > 0 {
				options.TestConfig.CommonBandwidthPackageId = CBP[0].BandwidthPackageId
			}
		}
	}
	return nil
}
