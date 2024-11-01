package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/alibabacloud-go/tea/tea"

	appv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/cloud-provider-alibaba-cloud/pkg/controller/helper"
	"k8s.io/cloud-provider-alibaba-cloud/test/e2e/options"
	"k8s.io/klog/v2"
)

const (
	Namespace        = "ccm-e2e-test"
	Service          = "basic-service"
	Deployment       = "ccm-nginx"
	VKDeployment     = "ccm-nginx-vk"
	NodeLabel        = "ccme2etest"
	ExcludeNodeLabel = "service.beta.kubernetes.io/exclude-node"
)

type KubeClient struct {
	kubernetes.Interface
}

func NewKubeClient(client kubernetes.Interface) *KubeClient {
	return &KubeClient{client}
}

func IsOversea() bool {
	region := options.TestConfig.RegionId
	if region != "" {
		klog.Infof("check region is domestic or oversea: %s", region)
		if strings.Contains(region, "cn-hongkong") {
			return true
		}
		return !strings.Contains(region, "cn-")
	}
	return false
}
func GetTestImage() string {
	if os.Getenv("TEST_IMAGE") != "" {
		return os.Getenv("TEST_IMAGE")
	}
	image := "ack-cn-hangzhou-registry.cn-hangzhou.cr.aliyuncs.com/test/nginx-multiarch:latest"
	if IsOversea() {
		image = "ack-cn-hongkong-registry.cn-hongkong.cr.aliyuncs.com/test/nginx-multiarch:latest"
	}
	return image
}

// service
func (client *KubeClient) DefaultService() *v1.Service {
	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      Service,
			Namespace: Namespace,
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt(80),
					Protocol:   v1.ProtocolTCP,
				},
				{
					Name:       "https",
					Port:       443,
					TargetPort: intstr.FromInt(443),
					Protocol:   v1.ProtocolTCP,
				},
			},
			Type:            v1.ServiceTypeLoadBalancer,
			SessionAffinity: v1.ServiceAffinityNone,
			Selector:        map[string]string{"run": "nginx"},
		},
	}
}

func (client *KubeClient) DefaultNLBService() *v1.Service {
	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      Service,
			Namespace: Namespace,
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt(80),
					Protocol:   v1.ProtocolTCP,
				},
				{
					Name:       "https",
					Port:       443,
					TargetPort: intstr.FromInt(443),
					Protocol:   v1.ProtocolTCP,
				},
			},
			Type:              v1.ServiceTypeLoadBalancer,
			SessionAffinity:   v1.ServiceAffinityNone,
			Selector:          map[string]string{"run": "nginx"},
			LoadBalancerClass: tea.String(helper.NLBClass),
		},
	}
}

func (client *KubeClient) CreateServiceByAnno(anno map[string]string) (*v1.Service, error) {
	svc := client.DefaultService()
	svc.Annotations = anno
	return client.CoreV1().Services(Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})
}

func (client *KubeClient) CreateNLBServiceByAnno(anno map[string]string) (*v1.Service, error) {
	svc := client.DefaultNLBService()
	svc.Annotations = anno

	return client.CoreV1().Services(Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})
}

func (client *KubeClient) CreateServiceWithStringTargetPort(anno map[string]string) (*v1.Service, error) {
	svc := client.DefaultService()
	svc.Annotations = anno
	svc.Spec.Ports = []v1.ServicePort{
		{
			Name:       "http",
			Port:       80,
			TargetPort: intstr.FromString("http"),
			Protocol:   v1.ProtocolTCP,
		},
		{
			Name:       "https",
			Port:       443,
			TargetPort: intstr.FromString("https"),
			Protocol:   v1.ProtocolTCP,
		},
	}
	return client.CoreV1().Services(Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})
}

func (client *KubeClient) CreateNLBServiceWithStringTargetPort(anno map[string]string) (*v1.Service, error) {
	lbClass := helper.NLBClass
	svc := client.DefaultService()
	svc.Annotations = anno
	svc.Spec.LoadBalancerClass = &lbClass
	svc.Spec.Ports = []v1.ServicePort{
		{
			Name:       "http",
			Port:       80,
			TargetPort: intstr.FromString("http"),
			Protocol:   v1.ProtocolTCP,
		},
		{
			Name:       "https",
			Port:       443,
			TargetPort: intstr.FromString("https"),
			Protocol:   v1.ProtocolTCP,
		},
	}
	return client.CoreV1().Services(Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})
}

func (client *KubeClient) CreateService(svc *v1.Service) (*v1.Service, error) {
	if svc == nil {
		return nil, fmt.Errorf("svc is nil")
	}
	return client.CoreV1().Services(Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})
}

func (client *KubeClient) PatchService(oldSvc, newSvc *v1.Service) (*v1.Service, error) {
	oldStr, _ := json.Marshal(oldSvc)
	newStr, _ := json.Marshal(newSvc)
	patchBytes, patchErr := strategicpatch.CreateTwoWayMergePatch(oldStr, newStr, &v1.Service{})
	if patchErr != nil {
		return nil, fmt.Errorf("create merge patch: %s", patchErr.Error())
	}
	return client.CoreV1().Services(Namespace).Patch(context.TODO(), Service, types.StrategicMergePatchType,
		patchBytes, metav1.PatchOptions{})
}

func (client *KubeClient) CreateServiceWithoutSelector(anno map[string]string) (*v1.Service, error) {
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        Service,
			Namespace:   Namespace,
			Annotations: anno,
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt(80),
					Protocol:   v1.ProtocolTCP,
				},
				{
					Name:       "https",
					Port:       443,
					TargetPort: intstr.FromInt(80),
					Protocol:   v1.ProtocolTCP,
				},
			},
			Type: v1.ServiceTypeLoadBalancer,
		},
	}

	return client.CoreV1().Services(Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})
}

func (client *KubeClient) CreateNLBServiceWithoutSelector(anno map[string]string) (*v1.Service, error) {
	lbClass := helper.NLBClass
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        Service,
			Namespace:   Namespace,
			Annotations: anno,
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt(80),
					Protocol:   v1.ProtocolTCP,
				},
				{
					Name:       "https",
					Port:       443,
					TargetPort: intstr.FromInt(80),
					Protocol:   v1.ProtocolTCP,
				},
			},
			Type:              v1.ServiceTypeLoadBalancer,
			LoadBalancerClass: &lbClass,
		},
	}

	return client.CoreV1().Services(Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})
}

func (client *KubeClient) DeleteService() error {
	return wait.PollImmediate(3*time.Second, 5*time.Minute, func() (done bool, err error) {
		err = client.CoreV1().Services(Namespace).Delete(context.TODO(), Service, metav1.DeleteOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
		}
		return false, nil
	})
}

func (client *KubeClient) DeleteServiceByName(name string) error {
	return wait.PollImmediate(3*time.Second, 3*time.Minute, func() (done bool, err error) {
		err = client.CoreV1().Services(Namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
		}
		return false, nil
	})
}

func (client *KubeClient) GetService() (*v1.Service, error) {
	return client.CoreV1().Services(Namespace).Get(context.TODO(), Service, metav1.GetOptions{})
}

// endpoints

func (client *KubeClient) GetEndpoint() (*v1.Endpoints, error) {
	return client.CoreV1().Endpoints(Namespace).Get(context.TODO(), Service, metav1.GetOptions{})
}

func (client *KubeClient) CreateEndpointsWithoutNodeName() (*v1.Endpoints, error) {
	ep := &v1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      Service,
			Namespace: Namespace,
		},
		Subsets: []v1.EndpointSubset{
			{
				Addresses: []v1.EndpointAddress{
					{
						IP: "123.123.123.123",
					},
				},
				Ports: []v1.EndpointPort{
					{
						Port:     80,
						Protocol: v1.ProtocolTCP,
					},
				},
			},
		},
	}
	return client.CoreV1().Endpoints(Namespace).Create(context.TODO(), ep, metav1.CreateOptions{})
}

func (client *KubeClient) CreateEndpointsWithNotExistNode() (*v1.Endpoints, error) {
	nodeName := "cn-hangzhou.123.123.123.123"
	ep := &v1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      Service,
			Namespace: Namespace,
		},
		Subsets: []v1.EndpointSubset{
			{
				Addresses: []v1.EndpointAddress{
					{
						IP:       "123.123.123.123",
						NodeName: &nodeName,
					},
				},
				Ports: []v1.EndpointPort{
					{
						Port:     80,
						Protocol: v1.ProtocolTCP,
					},
				},
			},
		},
	}
	return client.CoreV1().Endpoints(Namespace).Create(context.TODO(), ep, metav1.CreateOptions{})
}

func ConditionsToString(conditions []v1.PodCondition) string {
	var buffer bytes.Buffer
	for _, condition := range conditions {
		buffer.WriteString(fmt.Sprintf("%s: %s", string(condition.Type), condition.Status))
		if condition.Message != "" {
			buffer.WriteString(fmt.Sprintf("(%s)", condition.Message))
		}
		buffer.WriteString("; ")
	}
	return buffer.String()
}

// 等待 pod running
func (kc *KubeClient) WaitPodRunningOrComplete(timeout time.Duration, labelSelector, namespace string) error {
	// waiting for pod to be completed
	waitErr := wait.PollImmediate(time.Second*2, timeout, func() (done bool, err error) {
		pods, err := kc.CoreV1().Pods(Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			klog.Infof("LabelSelector %s get pod err: %s", labelSelector, err.Error())
			return false, nil
		}
		for _, pod := range pods.Items {
			pod, err := kc.CoreV1().Pods(namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})
			if err != nil {
				klog.Warningf("retry get pod %s err: %s", pod.Name, err.Error())
				return false, nil
			}
			phase := pod.Status.Phase
			klog.Infof("pod %s phase: %s", pod.Name, phase)
			switch pod.Status.Phase {
			case v1.PodRunning:
				klog.Infof("pod %s status: %s", pod.Name, pod.Status.Phase)
				continue
			case v1.PodPending:
				klog.Errorf("pod Pending: %s", ConditionsToString(pod.Status.Conditions))
				return false, nil
			case v1.PodFailed:
				req := kc.CoreV1().Pods(namespace).GetLogs(pod.Name, &v1.PodLogOptions{})
				stream, err := req.Stream(context.TODO())
				if err != nil {
					klog.Errorf("get log of pods:%s return err:%s", pod.Name, err.Error())
				}
				defer stream.Close()
				data, err := io.ReadAll(stream)
				if err != nil {
					klog.Errorf("get log of pods:%s return err:%s", pod.Name, err.Error())
				}
				klog.Infof("pod:%s,  pod log:%s", pod.Name, string(data))
				return false, fmt.Errorf("pod:%s,  pod log:%s", pod.Name, string(data))
			default:
				klog.Errorf("pod %s status: %s", pod.Name, pod.Status.Phase)
				return false, nil
			}
		}
		return true, nil

	})

	return waitErr
}

// deployment
func (client *KubeClient) CreateDeployment() error {
	var replica int32 = 3
	test_iamge := GetTestImage()
	nginx := &appv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      Deployment,
			Namespace: Namespace,
			Labels: map[string]string{
				"run": "nginx",
			},
		},
		Spec: appv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"run": "nginx",
				},
			},
			Replicas: &replica,
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"run": "nginx",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:            "nginx",
							Image:           test_iamge,
							ImagePullPolicy: "Always",
							Ports: []v1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: 80,
									Protocol:      v1.ProtocolTCP,
								},
								{
									Name:          "https",
									ContainerPort: 443,
									Protocol:      v1.ProtocolTCP,
								},
							},
						},
					},
				},
			},
		},
	}

	_, err := client.AppsV1().Deployments(Namespace).Create(context.Background(), nginx, metav1.CreateOptions{})
	if err != nil {
		if !strings.Contains(err.Error(), "exists") {
			return fmt.Errorf("create nginx error: %s", err.Error())
		}
	}
	return client.WaitPodRunningOrComplete(2*time.Minute, "run=nginx", Namespace)
}

func (client *KubeClient) ScaleDeployment(replica int32) error {
	deploy, err := client.AppsV1().Deployments(Namespace).Get(context.TODO(), Deployment, metav1.GetOptions{})
	if err != nil {
		return err
	}
	deploy.Spec.Replicas = &replica
	_, err = client.AppsV1().Deployments(Namespace).Update(context.TODO(), deploy, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	return wait.PollImmediate(5*time.Second, 2*time.Minute, func() (done bool, err error) {
		pods, err := client.CoreV1().Pods(Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "run=nginx"})
		if err != nil {
			klog.Infof("wait for nginx pod ready: %s", err.Error())
			return false, nil
		}
		if len(pods.Items) != int(replica) {
			klog.Infof("wait for nginx pod replicas: cur %d expect %d", len(pods.Items), replica)
			return false, nil
		}
		for _, pod := range pods.Items {
			if pod.Status.Phase != "Running" {
				klog.Infof("wait for nginx pod Running: %s cur %v", pod.Name, pod.Status.Phase)
				cond := ConditionsToString(pod.Status.Conditions)
				klog.Infof("pod %s conditions: %s", pod.Name, cond)
				return false, nil
			}
		}
		klog.Infof("ScaleDeployment %s replica %d ok.", Deployment, replica)
		return true, nil
	})
}

func (client *KubeClient) CreateECIPod() error {
	test_image := GetTestImage()
	nginx := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      VKDeployment,
			Namespace: Namespace,
			Labels: map[string]string{
				"app":                  "nginx-vk",
				"alibabacloud.com/eci": "true",
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:            "nginx",
					Image:           test_image,
					ImagePullPolicy: v1.PullIfNotPresent,
					Ports: []v1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: 80,
							Protocol:      v1.ProtocolTCP,
						},
						{
							Name:          "https",
							ContainerPort: 443,
							Protocol:      v1.ProtocolTCP,
						},
					},
				},
			},
			Tolerations: []v1.Toleration{
				{
					Operator: v1.TolerationOpExists,
				},
			},
		},
	}

	_, err := client.CoreV1().Pods(Namespace).Create(context.Background(), nginx, metav1.CreateOptions{})
	if err != nil {
		if !strings.Contains(err.Error(), "exists") {
			return fmt.Errorf("create nginx error: %s", err.Error())
		}
	}
	return client.WaitPodRunningOrComplete(5*time.Minute, "app=nginx-vk", Namespace)

}

func (client *KubeClient) CreateVKDeployment() error {
	var replica int32 = 2
	test_iamge := GetTestImage()
	nginx := &appv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      VKDeployment,
			Namespace: Namespace,
			Labels: map[string]string{
				"run": "nginx",
				"app": "nginx-vk",
			},
		},
		Spec: appv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"run": "nginx",
					"app": "nginx-vk",
				},
			},
			Replicas: &replica,
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"run": "nginx",
						"app": "nginx-vk",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:            "nginx",
							Image:           test_iamge,
							ImagePullPolicy: "Always",
							Ports: []v1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: 80,
									Protocol:      v1.ProtocolTCP,
								},
								{
									Name:          "https",
									ContainerPort: 443,
									Protocol:      v1.ProtocolTCP,
								},
							},
						},
					},
					NodeSelector: map[string]string{
						"type": "virtual-kubelet",
					},
					Tolerations: []v1.Toleration{
						{
							Operator: v1.TolerationOpExists,
						},
					},
				},
			},
		},
	}

	_, err := client.AppsV1().Deployments(Namespace).Create(context.Background(), nginx, metav1.CreateOptions{})
	if err != nil {
		if !strings.Contains(err.Error(), "exists") {
			return fmt.Errorf("create nginx error: %s", err.Error())
		}
	}
	return wait.Poll(5*time.Second, 2*time.Minute, func() (done bool, err error) {
		pods, err := client.CoreV1().Pods(nginx.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "app=nginx-vk"})
		if err != nil {
			klog.Infof("wait for nginx pod ready: %s", err.Error())
			return false, nil
		}
		if len(pods.Items) != int(*nginx.Spec.Replicas) {
			klog.Infof("wait for nginx pod replicas: %d", len(pods.Items))
			return false, nil
		}
		for _, pod := range pods.Items {
			if pod.Status.Phase != "Running" {
				klog.Infof("wait for nginx pod Running: %s", pod.Name)
				return false, nil
			}
		}
		return true, nil
	},
	)

}

func (client *KubeClient) GetVkNodes(retry int) ([]v1.Node, error) {
	nodes, err := client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{LabelSelector: "type=virtual-kubelet"})
	if err != nil {
		return nil, err
	}
	if len(nodes.Items) == 0 && retry > 0 {
		retry--
		time.Sleep(5 * time.Second)
		return client.GetVkNodes(retry)
	}
	if len(nodes.Items) == 0 {
		return nil, fmt.Errorf("no vk nodes")
	}
	return nodes.Items, nil
}

func (client *KubeClient) GetVkPods(retry int) (vkPods []v1.Pod, err error) {
	nodeName := "virtual-kubelet-" + options.TestConfig.RegionId
	pods, err := client.CoreV1().Pods(Namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: "app=nginx-vk"})
	if err != nil {
		return nil, err
	}
	for _, pod := range pods.Items {
		if strings.HasPrefix(pod.Spec.NodeName, nodeName) {
			vkPods = append(vkPods, pod)
		}
	}
	if len(vkPods) == 0 && retry > 0 {
		retry--
		time.Sleep(5 * time.Second)
		return client.GetVkPods(retry)
	}
	if len(vkPods) == 0 {
		return nil, fmt.Errorf("no vk pods")
	}

	return
}

// namespace
func (client *KubeClient) CreateNamespace() error {
	ns := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      Namespace,
			Namespace: Namespace,
		},
	}
	_, err := client.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
	return err
}

func (client *KubeClient) DeleteNamespace() error {
	return wait.PollImmediate(5*time.Second, 3*time.Minute,
		func() (done bool, err error) {
			err = client.CoreV1().Namespaces().Delete(context.TODO(), Namespace, metav1.DeleteOptions{})
			if err != nil {
				if apierrors.IsNotFound(err) {
					return true, nil
				}
			}
			return false, nil
		})
}

// node
func (client *KubeClient) LabelNode(nodeName string, key string, value string) error {
	return wait.PollImmediate(2*time.Second, time.Minute, func() (done bool, err error) {
		n, err := client.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
		n.ObjectMeta.Labels[key] = value
		_, err = client.CoreV1().Nodes().Update(context.TODO(), n, metav1.UpdateOptions{})
		if err != nil {
			return false, nil
		}
		return true, nil
	})
}

func (client *KubeClient) UnLabelNode(nodeName string, key string) error {
	return wait.PollImmediate(2*time.Second, time.Minute, func() (done bool, err error) {
		n, err := client.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
		delete(n.ObjectMeta.Labels, key)
		_, err = client.CoreV1().Nodes().Update(context.TODO(), n, metav1.UpdateOptions{})
		if err != nil {
			return false, nil
		}
		return true, nil
	})
}

func (client *KubeClient) UnscheduledNode(nodeName string) error {
	return wait.PollImmediate(2*time.Second, time.Minute, func() (done bool, err error) {
		n, err := client.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
		if err != nil || n == nil {
			return false, nil
		}
		n.Spec.Unschedulable = true
		_, err = client.CoreV1().Nodes().Update(context.TODO(), n, metav1.UpdateOptions{})
		if err != nil {
			return false, nil
		}
		return true, nil
	})

}

func (client *KubeClient) ScheduledNode(nodeName string) error {
	return wait.PollImmediate(2*time.Second, time.Minute, func() (done bool, err error) {
		n, err := client.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
		if err != nil || n == nil {
			return false, nil
		}
		n.Spec.Unschedulable = false
		_, err = client.CoreV1().Nodes().Update(context.TODO(), n, metav1.UpdateOptions{})
		if err != nil {
			return false, nil
		}
		return true, nil
	})

}

func (client *KubeClient) AddTaint(nodeName string, taint v1.Taint) error {
	return wait.PollImmediate(2*time.Second, 30*time.Second, func() (done bool, err error) {
		n, err := client.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		for _, taint := range n.Spec.Taints {
			if taint.Key == taint.Key {
				return true, nil
			}
		}
		n.Spec.Taints = append(n.Spec.Taints, taint)
		_, err = client.CoreV1().Nodes().Update(context.TODO(), n, metav1.UpdateOptions{})
		if err != nil {
			return false, nil
		}
		return true, nil
	})
}

func (client *KubeClient) RemoveTaint(nodeName string, taint v1.Taint) error {
	return wait.PollImmediate(2*time.Second, 30*time.Second, func() (done bool, err error) {
		n, err := client.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		var updateTaints []v1.Taint
		for _, t := range n.Spec.Taints {
			if t.Key == taint.Key {
				continue
			}
			updateTaints = append(updateTaints, t)
		}
		_, err = client.CoreV1().Nodes().Update(context.TODO(), n, metav1.UpdateOptions{})
		if err != nil {
			return false, nil
		}
		return true, nil
	})
}

func (client *KubeClient) ListNodes() ([]v1.Node, error) {
	nodeList, err := client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return nodeList.Items, nil
}

func (client *KubeClient) GetLatestNode() (*v1.Node, error) {
	nodeList, err := client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	if len(nodeList.Items) == 0 {
		return nil, nil
	}

	var ret v1.Node
	for _, node := range nodeList.Items {
		if helper.HasExcludeLabel(&node) {
			continue
		}
		if _, exclude := node.Labels[helper.LabelNodeExcludeBalancer]; exclude {
			continue
		}
		if _, exclude := node.Labels[helper.LabelNodeExcludeBalancerDeprecated]; exclude {
			continue
		}
		if _, isVK := node.Labels[helper.LabelNodeTypeVK]; isVK {
			continue
		}
		if ret.Name == "" {
			ret = node
		} else if ret.CreationTimestamp.Before(&node.CreationTimestamp) {
			ret = node
		}
	}
	klog.Infof("return node:%s", ret.Name)
	return &ret, nil
}

func (client *KubeClient) PatchNodeStatus(oldNode, newNode *v1.Node) (*v1.Node, error) {
	oldStr, _ := json.Marshal(oldNode)
	newStr, _ := json.Marshal(newNode)
	patchBytes, patchErr := strategicpatch.CreateTwoWayMergePatch(oldStr, newStr, &v1.Node{})
	if patchErr != nil {
		return nil, fmt.Errorf("create merge patch: %s", patchErr.Error())
	}
	return client.CoreV1().Nodes().PatchStatus(context.TODO(), oldNode.Name, patchBytes)
}

func (client *KubeClient) PatchNode(oldNode, newNode *v1.Node) (*v1.Node, error) {
	oldStr, _ := json.Marshal(oldNode)
	newStr, _ := json.Marshal(newNode)
	patchBytes, patchErr := strategicpatch.CreateTwoWayMergePatch(oldStr, newStr, &v1.Node{})
	if patchErr != nil {
		return nil, fmt.Errorf("create merge patch: %s", patchErr.Error())
	}
	return client.CoreV1().Nodes().Patch(context.TODO(), oldNode.Name,
		types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{})
}

func (client *KubeClient) GetDefaultDeployment() (*appv1.Deployment, error) {
	app, err := client.AppsV1().Deployments(Namespace).Get(context.TODO(), Deployment, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get deployment %s error: %s", Deployment, err)
	}
	return app, nil
}

func (client *KubeClient) UpdateDeployment(app *appv1.Deployment) (*appv1.Deployment, error) {
	app, err := client.AppsV1().Deployments(Namespace).Update(context.TODO(), app, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("update deployment %s error: %s", Deployment, err)
	}
	labelSelector := metav1.FormatLabelSelector(app.Spec.Selector)
	return app, client.WaitPodRunningOrComplete(5*time.Minute, labelSelector, Namespace)
}

func (client *KubeClient) ListPods(labelSelector string) (*v1.PodList, error) {
	pods, err := client.CoreV1().Pods(Namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("list pods %s error: %s", labelSelector, err)
	}
	return pods, nil
}

func reverse(s []v1.Event) []v1.Event {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
	return s
}

func (client *KubeClient) GetEvents(name string) (*v1.EventList, error) {
	fieldSelector := "involvedObject.name=" + name
	return client.CoreV1().Events(Namespace).List(context.TODO(), metav1.ListOptions{FieldSelector: fieldSelector})
}

func (client *KubeClient) GetSvcEventsMessages(name string) (messgaes []string, err error) {
	events, err := client.GetEvents(name)
	if err != nil {
		return nil, err
	}
	items := reverse(events.Items)
	for _, v := range items {
		if strings.Contains(v.Reason, "Failed") {
			errMess := fmt.Sprintf("[%v][%s][%s]", v.LastTimestamp, v.Reason, v.Message)
			messgaes = append(messgaes, errMess)
		}
		if len(messgaes) > 1 {
			break
		}
	}
	return
}
func (client *KubeClient) IsPodHasReadinessGate(pod *v1.Pod, cond string) bool {
	conditionType := v1.PodConditionType(cond)
	for _, cd := range pod.Status.Conditions {
		if cd.Type == conditionType && cd.Status == v1.ConditionTrue {
			return true
		}
	}
	return false
}
