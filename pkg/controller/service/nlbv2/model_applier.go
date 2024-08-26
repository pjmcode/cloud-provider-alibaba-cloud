package nlbv2

import (
	"context"
	"fmt"
	v1 "k8s.io/api/core/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrlCfg "k8s.io/cloud-provider-alibaba-cloud/pkg/config"
	"k8s.io/cloud-provider-alibaba-cloud/pkg/controller/helper"
	svcCtx "k8s.io/cloud-provider-alibaba-cloud/pkg/controller/service/reconcile/context"
	nlbmodel "k8s.io/cloud-provider-alibaba-cloud/pkg/model/nlb"
	"k8s.io/cloud-provider-alibaba-cloud/pkg/model/tag"
	"k8s.io/cloud-provider-alibaba-cloud/pkg/provider/alibaba/base"
	"k8s.io/cloud-provider-alibaba-cloud/pkg/provider/dryrun"
	"k8s.io/cloud-provider-alibaba-cloud/pkg/util"
)

func NewModelApplier(nlbMgr *NLBManager, lisMgr *ListenerManager, sgMgr *ServerGroupManager) *ModelApplier {
	return &ModelApplier{
		nlbMgr: nlbMgr,
		lisMgr: lisMgr,
		sgMgr:  sgMgr,
	}
}

type ModelApplier struct {
	nlbMgr *NLBManager
	lisMgr *ListenerManager
	sgMgr  *ServerGroupManager
}

func (m *ModelApplier) Apply(reqCtx *svcCtx.RequestContext, local *nlbmodel.NetworkLoadBalancer) (*nlbmodel.NetworkLoadBalancer, error) {
	remote := &nlbmodel.NetworkLoadBalancer{
		NamespacedName:                  util.NamespacedName(reqCtx.Service),
		LoadBalancerAttribute:           &nlbmodel.LoadBalancerAttribute{},
		ContainsPotentialReadyEndpoints: local.ContainsPotentialReadyEndpoints,
	}

	err := m.nlbMgr.BuildRemoteModel(reqCtx, remote)
	if err != nil {
		return remote, fmt.Errorf("get nlb attribute from cloud error: %s", err.Error())
	}
	reqCtx.Ctx = context.WithValue(reqCtx.Ctx, dryrun.ContextNLB, remote.GetLoadBalancerId())

	if remote.LoadBalancerAttribute.LoadBalancerId != "" && local.LoadBalancerAttribute.PreserveOnDelete {
		reqCtx.Recorder.Eventf(reqCtx.Service, v1.EventTypeWarning, helper.PreservedOnDelete,
			"The lb [%s] will be preserved after the service is deleted.", remote.LoadBalancerAttribute.LoadBalancerId)
	}

	serviceHashChanged := helper.IsServiceHashChanged(reqCtx.Service)
	errs := []error{}
	if serviceHashChanged || ctrlCfg.ControllerCFG.DryRun {
		if err := m.applyLoadBalancerAttribute(reqCtx, local, remote); err != nil {
			_, ok := err.(utilerrors.Aggregate)
			if ok {
				// if lb attr update failed, continue to sync vgroup & listener
				errs = append(errs, fmt.Errorf("update nlb attribute error: %s", err.Error()))
			} else {
				return nil, err
			}
		}
	}

	if err := m.sgMgr.BuildRemoteModel(reqCtx, remote); err != nil {
		errs = append(errs, fmt.Errorf("get server group from remote error: %s", err.Error()))
		return remote, utilerrors.NewAggregate(errs)
	}
	if err := m.applyVGroups(reqCtx, local, remote); err != nil {
		errs = append(errs, fmt.Errorf("reconcile backends error: %s", err.Error()))
		return remote, utilerrors.NewAggregate(errs)
	}

	if serviceHashChanged || ctrlCfg.ControllerCFG.DryRun {
		if remote.LoadBalancerAttribute.LoadBalancerId != "" {
			if err := m.lisMgr.BuildRemoteModel(reqCtx, remote); err != nil {
				errs = append(errs, fmt.Errorf("get lb listeners from cloud, error: %s", err.Error()))
				return remote, utilerrors.NewAggregate(errs)
			}
			if err := m.applyListeners(reqCtx, local, remote); err != nil {
				errs = append(errs, fmt.Errorf("reconcile listeners error: %s", err.Error()))
				return remote, utilerrors.NewAggregate(errs)
			}
		} else {
			if !helper.NeedDeleteLoadBalancer(reqCtx.Service) {
				errs = append(errs, fmt.Errorf("alicloud: can not find loadbalancer by tag [%s:%s]",
					helper.TAGKEY, reqCtx.Anno.GetDefaultLoadBalancerName()))
				return remote, utilerrors.NewAggregate(errs)
			}
		}
	}

	if err := m.cleanup(reqCtx, local, remote); err != nil {
		errs = append(errs, fmt.Errorf("update lb listeners error: %s", err.Error()))
		return remote, utilerrors.NewAggregate(errs)
	}

	return remote, utilerrors.NewAggregate(errs)
}

func (m *ModelApplier) applyLoadBalancerAttribute(reqCtx *svcCtx.RequestContext, local, remote *nlbmodel.NetworkLoadBalancer) error {
	if local == nil || remote == nil {
		return fmt.Errorf("local or remote mdl is nil")
	}

	if local.NamespacedName.String() != remote.NamespacedName.String() {
		return fmt.Errorf("models for different svc, local [%s], remote [%s]",
			local.NamespacedName, remote.NamespacedName)
	}

	// delete nlb
	if helper.NeedDeleteLoadBalancer(reqCtx.Service) {
		if !local.LoadBalancerAttribute.IsUserManaged {
			if local.LoadBalancerAttribute.PreserveOnDelete {
				err := m.nlbMgr.SetProtectionsOff(reqCtx, remote)
				if err != nil {
					return fmt.Errorf("set loadbalancer [%s] protections off error: %s",
						remote.LoadBalancerAttribute.LoadBalancerId, err.Error())
				}

				err = m.nlbMgr.CleanupLoadBalancerTags(reqCtx, remote)
				if err != nil {
					return fmt.Errorf("cleanup loadbalancer [%s] tags error: %s",
						remote.LoadBalancerAttribute.LoadBalancerId, err.Error())
				}
				reqCtx.Log.Info(fmt.Sprintf("successfully cleanup preserved nlb %s", remote.LoadBalancerAttribute.LoadBalancerId))
			} else {
				err := m.nlbMgr.Delete(reqCtx, remote)
				if err != nil {
					return fmt.Errorf("delete nlb [%s] error: %s",
						remote.LoadBalancerAttribute.LoadBalancerId, err.Error())
				}
				reqCtx.Log.Info(fmt.Sprintf("successfully delete nlb %s", remote.LoadBalancerAttribute.LoadBalancerId))
			}

			remote.LoadBalancerAttribute.LoadBalancerId = ""
			remote.LoadBalancerAttribute.DNSName = ""
			return nil
		}
		reqCtx.Log.Info(fmt.Sprintf("nlb %s is reused, skip delete it", remote.LoadBalancerAttribute.LoadBalancerId))
		return nil
	}

	// create nlb
	if remote.LoadBalancerAttribute.LoadBalancerId == "" {
		if helper.IsServiceOwnIngress(reqCtx.Service) {
			return fmt.Errorf("alicloud: can not find loadbalancer, but it's defined in service [%v] "+
				"this may happen when you delete the loadbalancer", reqCtx.Service.Status.LoadBalancer.Ingress[0].IP)
		}

		if err := m.nlbMgr.Create(reqCtx, local); err != nil {
			return fmt.Errorf("create nlb error: %s", err.Error())
		}
		reqCtx.Log.Info(fmt.Sprintf("successfully create lb %s", local.LoadBalancerAttribute.LoadBalancerId))
		// update remote model
		remote.LoadBalancerAttribute.LoadBalancerId = local.LoadBalancerAttribute.LoadBalancerId
		if err := m.nlbMgr.Find(reqCtx, remote); err != nil {
			return fmt.Errorf("update remote model for lbId %s, error: %s",
				remote.LoadBalancerAttribute.LoadBalancerId, err.Error())
		}
		// need update nlb security groups
		// or ipv6 address type
		if len(local.LoadBalancerAttribute.SecurityGroupIds) != 0 ||
			local.LoadBalancerAttribute.IPv6AddressType != "" {
			err := m.nlbMgr.Update(reqCtx, local, remote)
			if err != nil {
				return err
			}
		}

		return nil
	}

	tags, err := m.nlbMgr.cloud.ListNLBTagResources(reqCtx.Ctx, remote.LoadBalancerAttribute.LoadBalancerId)
	if err != nil {
		return fmt.Errorf("ListNLBTagResources: %s", err.Error())
	}
	remote.LoadBalancerAttribute.Tags = tags

	// check whether slb can be reused
	if !helper.NeedDeleteLoadBalancer(reqCtx.Service) && local.LoadBalancerAttribute.IsUserManaged {
		if ok, reason := isNLBReusable(reqCtx.Service, tags, remote.LoadBalancerAttribute.DNSName); !ok {
			return fmt.Errorf("the loadbalancer %s can not be reused, %s",
				remote.LoadBalancerAttribute.LoadBalancerId, reason)
		}
	}

	return m.nlbMgr.Update(reqCtx, local, remote)

}

func (m *ModelApplier) applyVGroups(reqCtx *svcCtx.RequestContext, local, remote *nlbmodel.NetworkLoadBalancer) error {
	var errs []error
	for i := range local.ServerGroups {
		found := false
		var old nlbmodel.ServerGroup
		for _, rv := range remote.ServerGroups {
			// for reuse vgroup case, find by vgroup id first
			if local.ServerGroups[i].ServerGroupId != "" &&
				local.ServerGroups[i].ServerGroupId == rv.ServerGroupId {
				found = true
				old = *rv
				break
			}
			// find by vgroup name
			if local.ServerGroups[i].ServerGroupId == "" &&
				local.ServerGroups[i].ServerGroupName == rv.ServerGroupName {
				found = true
				local.ServerGroups[i].ServerGroupId = rv.ServerGroupId
				old = *rv
				break
			}
		}

		// update
		if found {
			// if server group type changed, need to recreate
			if local.ServerGroups[i].ServerGroupType != "" &&
				local.ServerGroups[i].ServerGroupType != old.ServerGroupType {
				if local.ServerGroups[i].IsUserManaged {
					return fmt.Errorf("ServerGroupType of user managed server group %s should be [%s], but [%s]",
						local.ServerGroups[i].ServerGroupId, local.ServerGroups[i].ServerGroupType, old.ServerGroupType)
				}
				reqCtx.Log.Info(fmt.Sprintf("ServerGroupType changed [%s] - [%s], need to recreate server group",
					old.ServerGroupType, local.ServerGroups[i].ServerGroupType),
					"sgId", old.ServerGroupId, "sgName", old.ServerGroupName)
				found = false
			} else {
				if err := m.sgMgr.UpdateServerGroup(reqCtx, local.ServerGroups[i], &old); err != nil {
					errs = append(errs, fmt.Errorf("EnsureServerGroupUpdated error: %s", err.Error()))
					continue
				}
			}
		}

		// create
		if !found {
			reqCtx.Log.Info(fmt.Sprintf("create server group %s", local.ServerGroups[i].ServerGroupName))
			if remote.LoadBalancerAttribute.VpcId != "" {
				local.ServerGroups[i].VPCId = remote.LoadBalancerAttribute.VpcId
			}
			// to avoid add too many backends in one action, create server group with empty backends,
			// then use AddNLBServers to add backends
			if err := m.sgMgr.CreateServerGroup(reqCtx, local.ServerGroups[i]); err != nil {
				errs = append(errs, fmt.Errorf("EnsureServerGroupCreated error: %s", err.Error()))
				continue
			}
			if err := m.sgMgr.BatchAddServers(reqCtx, local.ServerGroups[i],
				local.ServerGroups[i].Servers); err != nil {
				errs = append(errs, fmt.Errorf("BatchAddServers error: %s", err.Error()))
				continue
			}
			remote.ServerGroups = append(remote.ServerGroups, local.ServerGroups[i])
		}
	}

	return utilerrors.NewAggregate(errs)
}

func (m *ModelApplier) applyListeners(reqCtx *svcCtx.RequestContext, local, remote *nlbmodel.NetworkLoadBalancer) error {
	if local.LoadBalancerAttribute.IsUserManaged {
		if !reqCtx.Anno.IsForceOverride() {
			reqCtx.Log.Info("listener override is false, skip reconcile listeners")
			return nil
		}
	}

	// associate listener and vGroup
	for i := range local.Listeners {
		if local.Listeners[i].ServerGroupId != "" {
			continue
		}
		if err := findServerGroup(local.ServerGroups, local.Listeners[i]); err != nil {
			return fmt.Errorf("find servergroup error: %s", err.Error())
		}
	}

	// delete
	for _, r := range remote.Listeners {
		found := false
		for i, l := range local.Listeners {
			if r.ListenerPort == l.ListenerPort && r.ListenerProtocol == l.ListenerProtocol {
				found = true
				local.Listeners[i].ListenerId = r.ListenerId
			}
		}

		if !found {
			if local.LoadBalancerAttribute.IsUserManaged {
				if r.NamedKey == nil || !r.NamedKey.IsManagedByService(reqCtx.Service, base.CLUSTER_ID) {
					reqCtx.Log.V(5).Info(fmt.Sprintf("listener %s is managed by user, skip delete", r.ListenerId))
					continue
				}
			}

			reqCtx.Log.Info(fmt.Sprintf("delete listener: %s [%d]", r.ListenerProtocol, r.ListenerPort))
			if err := m.lisMgr.DeleteListener(reqCtx, r.ListenerId); err != nil {
				return fmt.Errorf("EnsureListenerDeleted error: %s", err.Error())
			}
		}
	}

	for i := range local.Listeners {
		found := false
		for j := range remote.Listeners {
			if local.Listeners[i].ListenerId == remote.Listeners[j].ListenerId {
				found = true
				if err := m.lisMgr.UpdateNLBListener(reqCtx, local.Listeners[i], remote.Listeners[j]); err != nil {
					return fmt.Errorf("EnsureListenerUpdated error: %s", err.Error())
				}
			}
		}

		// create
		if !found {
			reqCtx.Log.Info(fmt.Sprintf("create listener: %s [%d]", local.Listeners[i].ListenerProtocol, local.Listeners[i].ListenerPort))
			if err := m.lisMgr.CreateListener(reqCtx, remote.LoadBalancerAttribute.LoadBalancerId, local.Listeners[i]); err != nil {
				return fmt.Errorf("EnsureListenerCreated error: %s", err.Error())
			}
		}
	}

	return nil
}

func (m *ModelApplier) cleanup(reqCtx *svcCtx.RequestContext, local, remote *nlbmodel.NetworkLoadBalancer) error {
	// delete server groups
	for _, r := range remote.ServerGroups {
		found := false
		for _, l := range local.ServerGroups {
			if l.ServerGroupId == r.ServerGroupId {
				found = true
				break
			}
		}

		// delete unused vgroup
		if !found {
			// if the loadbalancer is preserved, and the service is deleting,
			// remove the server group tag instead of deleting the server group.
			if local.LoadBalancerAttribute.PreserveOnDelete {
				if err := m.sgMgr.CleanupServerGroupTags(reqCtx, r); err != nil {
					return err
				}
				continue
			}

			// do not delete user managed server group, but need to clean the backends
			if r.NamedKey == nil || r.IsUserManaged || !r.NamedKey.IsManagedByService(reqCtx.Service, base.CLUSTER_ID) {
				reqCtx.Log.Info(fmt.Sprintf("try to delete vgroup: [%s] description [%s] is managed by user, skip delete",
					r.ServerGroupName, r.ServerGroupId))
				var del []nlbmodel.ServerGroupServer
				for _, remote := range r.Servers {
					if !remote.IsUserManaged {
						del = append(del, remote)
					}
				}
				if len(del) > 0 {
					if err := m.sgMgr.BatchRemoveServers(reqCtx, r, del); err != nil {
						return err
					}
				}
				continue
			}

			reqCtx.Log.Info(fmt.Sprintf("delete server group [%s], %s", r.ServerGroupName, r.ServerGroupId))
			err := m.sgMgr.DeleteServerGroup(reqCtx, r.ServerGroupId)
			if err != nil {
				return fmt.Errorf("delete server group %s failed, error: %s", r.ServerGroupId, err.Error())
			}
		}
	}
	return nil
}

func isNLBReusable(service *v1.Service, tags []tag.Tag, dnsName string) (bool, string) {
	for _, t := range tags {
		// the tag of the apiserver slb is "ack.aliyun.com": "${clusterid}",
		// so can not reuse slbs which have ack.aliyun.com tag key.
		if t.Key == helper.TAGKEY || t.Key == util.ClusterTagKey {
			return false, "can not reuse loadbalancer created by kubernetes."
		}
	}

	if len(service.Status.LoadBalancer.Ingress) > 0 {
		found := false
		for _, ingress := range service.Status.LoadBalancer.Ingress {
			if ingress.Hostname == dnsName {
				found = true
			}
		}
		if !found {
			return false, fmt.Sprintf("service has been associated with dnsname [%v], cannot be bound to dnsname [%s]",
				service.Status.LoadBalancer.Ingress[0].Hostname, dnsName)
		}
	}

	return true, ""
}

func findServerGroup(sgs []*nlbmodel.ServerGroup, lis *nlbmodel.ListenerAttribute) error {
	for _, sg := range sgs {
		if sg.ServerGroupName == lis.ServerGroupName {
			lis.ServerGroupId = sg.ServerGroupId
			return nil
		}
	}
	return fmt.Errorf("can not find server group by name %s", lis.ServerGroupName)

}
