package client

import (
	"encoding/json"
	"fmt"
	"time"

	"k8s.io/cloud-provider-alibaba-cloud/test/e2e/options"

	cs "github.com/alibabacloud-go/cs-20151215/v5/client"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/tea"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cloud-provider-alibaba-cloud/pkg/provider/alibaba/base"
	"k8s.io/klog/v2"
)

func NewACKClient() (*ACKClient, error) {
	ak, sk, err := base.LoadAK()
	if err != nil {
		return nil, fmt.Errorf("create ack client error: load ak error: %s", err.Error())
	}
	region := options.TestConfig.RegionId
	config := &openapi.Config{
		AccessKeyId:     &ak,
		AccessKeySecret: &sk,
		RegionId:        &region,
	}
	c, err := cs.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("create ack client error: %s", err.Error())
	}
	return &ACKClient{client: c}, nil
}

type ACKClient struct {
	client *cs.Client
}

func (e *ACKClient) DescribeClusterDetail(clusterId string) (*cs.DescribeClusterDetailResponseBody, error) {
	resp, err := e.client.DescribeClusterDetail(&clusterId)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("DescribeClusterDetail resp is nil")
	}
	return resp.Body, err
}

func (e *ACKClient) getDefaultNodePool(clusterId string) (*string, error) {
	detail, err := e.client.DescribeClusterNodePools(tea.String(clusterId), &cs.DescribeClusterNodePoolsRequest{})
	if err != nil {
		return nil, err
	}
	for _, np := range detail.Body.Nodepools {
		if np.NodepoolInfo == nil {
			continue
		}
		if np.NodepoolInfo.IsDefault != nil && *np.NodepoolInfo.IsDefault {
			return np.NodepoolInfo.NodepoolId, nil
		}
		if np.NodepoolInfo.Name != nil && *np.NodepoolInfo.Name == "default-nodepool" {
			return np.NodepoolInfo.NodepoolId, nil
		}
	}
	return nil, fmt.Errorf("get defalt NodepoolId fail")
}

func (e *ACKClient) ScaleOutCluster(clusterId string, nodeCount int64) error {
	nodePoolId, err := e.getDefaultNodePool(clusterId)
	if err != nil {
		return err
	}
	scaleOutNodePoolRequest := &cs.ScaleClusterNodePoolRequest{
		Count: &nodeCount,
	}
	resp, err := e.client.ScaleClusterNodePool(tea.String(clusterId), nodePoolId, scaleOutNodePoolRequest)
	if err != nil {
		return err
	}
	if resp == nil {
		return fmt.Errorf("DescribeClusterDetail resp is nil")
	}

	return wait.PollImmediate(30*time.Second, 5*time.Minute, func() (done bool, err error) {
		detail, err := e.DescribeClusterDetail(clusterId)
		if err != nil {
			return false, nil
		}
		if detail == nil || detail.State == nil {
			return false, nil
		}
		klog.Infof("waiting cluster state to be running, now %s", *detail.State)
		return *detail.State == "running", nil
	})
}

func (e *ACKClient) DeleteClusterNodes(clusterId, nodeName string) error {
	deleteClusterNodesRequest := &cs.DeleteClusterNodesRequest{
		DrainNode:   tea.Bool(true),
		ReleaseNode: tea.Bool(true),
		Nodes:       []*string{tea.String(nodeName)},
	}
	_, err := e.client.DeleteClusterNodes(tea.String(clusterId), deleteClusterNodesRequest)
	if err != nil {
		return err
	}
	return wait.PollImmediate(30*time.Second, 5*time.Minute, func() (done bool, err error) {
		detail, err := e.DescribeClusterDetail(clusterId)
		if err != nil {
			return false, err
		}
		if *detail.State == "running" {
			return true, nil
		} else {
			klog.Infof("waiting cluster state to be running, now is %s", *detail.State)
		}
		return false, err
	})
}

type AddonInfo struct {
	ComponentName string
	Version       string
}

func (e *ACKClient) GetClusterAddonInstance(clusterId string, componentId string) (*AddonInfo, error) {
	resp, err := e.client.GetClusterAddonInstance(tea.String(clusterId), tea.String(componentId))
	if err != nil {
		return nil, fmt.Errorf("GetClusterAddonInstance %s failed %s", componentId, err)
	}

	return &AddonInfo{
		ComponentName: *resp.Body.Name,
		Version:       *resp.Body.Version,
	}, nil
}

func (e *ACKClient) ModifyClusterConfiguration(clusterId string, addonName string, configs map[string]string) error {
	bytes, _err := json.Marshal(configs)
	if _err != nil {
		return _err
	}

	req := &cs.ModifyClusterAddonRequest{
		Config: tea.String(string(bytes)),
	}
	_, err := e.client.ModifyClusterAddon(tea.String(clusterId), tea.String(addonName), req)
	return err
}
