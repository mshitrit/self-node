package master

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/medik8s/self-node-remediation/pkg/peers"
	clientv3 "go.etcd.io/etcd/client/v3"
	corev1 "k8s.io/api/core/v1"
	"math/rand"
	"net"
	"os/exec"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sync"
	"time"
)

const (
	initErrorText         = "error initializing master handler"
	expectedKubeletStatus = "active(running)"
)

var (
	initError    = errors.New(initErrorText)
	processError = errors.New("an error occurred during master remediation process")
)

//Manager contains logic and info needed to fence and remediate master nodes
type Manager struct {
	nodeName string
	nodeRole peers.Role
	//TODO mshitrit remove if not used
	nodeNameRoleMapping map[string]peers.Role
	isHasInternetAccess bool
	client              client.Client
	log                 logr.Logger
}

//NewManager inits a new Manager return nil if init fails
func NewManager(nodeName string, myClient client.Client) *Manager {
	return &Manager{
		nodeName:            nodeName,
		nodeNameRoleMapping: map[string]peers.Role{},
		client:              myClient,
		isHasInternetAccess: false,
		log:                 ctrl.Log.WithName("master").WithName("Manager"),
	}
}

func (manager *Manager) Start(ctx context.Context) error {
	if err := manager.initializeManager(); err != nil {
		return err
	}
	//TODO mshitrit remove later, only for debug
	manager.log.Info("[DEBUG] current node role is:", "role", manager.nodeRole)
	manager.log.Info("[DEBUG] node name -> role mapping: ", "mapping", manager.nodeNameRoleMapping)
	go func() {
		for {
			_ = manager.isEtcdRunning()
			//_ = manager.isKubeletServiceRunning()
			time.Sleep(time.Second * 5)
		}
	}()

	return nil
}

func (manager *Manager) IsMaster() bool {
	return manager.nodeRole == peers.Master
}

func (manager *Manager) IsMasterHealthy(workerPeerResponse peers.Response, isOtherMastersCanBeReached bool) bool {
	switch workerPeerResponse.Reason {
	//reported unhealthy by worker peers
	case peers.UnHealthyBecauseCRFound:
		return false
	case peers.UnHealthyBecauseNodeIsIsolated:
		return isOtherMastersCanBeReached
	//reported healthy by worker peers
	case peers.HealthyBecauseErrorsThresholdNotReached, peers.HealthyBecauseCRNotFound:
		return true
	//master node has connection to most workers, we assume it's not isolated (or at least that the master node that does not have worker peers quorum will reboot)
	case peers.HealthyBecauseMostPeersCantAccessAPIServer:
		//TODO mshitrit error is ignored
		isHealthy, _ := manager.isDiagnosticsPassed()
		return isHealthy
	case peers.HealthyBecauseNoPeersWereFound:
		if isHealthy, _ := manager.isDiagnosticsPassed(); !isHealthy {
			return false
		}
		return isOtherMastersCanBeReached

	default:
		manager.log.Error(processError, "node is considered unhealthy by worker peers for an unknown reason", "reason", workerPeerResponse.Reason, "node name", manager.nodeName)
		return false
	}

}

func (manager *Manager) isDiagnosticsPassed() (bool, error) {
	//TODO mshitrit implement check external communication, kubelet service etc
	if isLostInternetConnection := manager.isHasInternetAccess && !isHasInternetAccess(); isLostInternetConnection {
		return false, nil
	} else if !manager.isKubeletServiceRunning() {
		return false, nil
	}

	randomBool := rand.Intn(2) == 0
	return randomBool, nil
}

func wrapWithInitError(err error) error {
	return fmt.Errorf(initErrorText+" [%w]", err)
}

func (manager *Manager) initializeManager() error {
	nodesList := &corev1.NodeList{}
	if err := manager.client.List(context.TODO(), nodesList, &client.ListOptions{}); err != nil {
		manager.log.Error(err, "could not retrieve nodes")
		return wrapWithInitError(err)
	}

	for _, node := range nodesList.Items {

		isNodeRoleFound := false
		if _, isWorker := node.Labels[peers.WorkerLabelName]; isWorker {
			manager.nodeNameRoleMapping[node.Name] = peers.Worker
			isNodeRoleFound = true
		} else {
			peers.SetControlPlaneLabelType(&node)
			if _, isMaster := node.Labels[peers.GetUsedControlPlaneLabel()]; isMaster {
				manager.nodeNameRoleMapping[node.Name] = peers.Master
				isNodeRoleFound = true
			}
		}

		if !isNodeRoleFound {
			manager.log.Error(initError, "could not find role for node", "node name", node.Name)
			return initError
		}
	}

	if _, isFound := manager.nodeNameRoleMapping[manager.nodeName]; !isFound {
		manager.log.Error(initError, "could not find role for current node", "node name", manager.nodeName)
		return initError
	} else {
		manager.nodeRole = manager.nodeNameRoleMapping[manager.nodeName]
		manager.isHasInternetAccess = isHasInternetAccess()
		manager.log.Info("[DEBUG] internet connection status is:", "status", manager.isHasInternetAccess)
		return nil
	}

}

func isHasInternetAccess() bool {
	con, err := net.DialTimeout("tcp", "google.com:80", 5*time.Second)
	defer func(con net.Conn) {
		_ = con.Close()
	}(con)
	return err == nil
}

func (manager *Manager) isKubeletServiceRunning() bool {
	url := fmt.Sprintf("https://%s:10250/pods", manager.nodeName)
	cmd := exec.Command("curl", "-k", "-X", "GET", url)
	if err := cmd.Run(); err != nil {
		manager.log.Error(err, "kubelet service is down", "node name", manager.nodeName)
		return false
	}
	return true
}

func (manager *Manager) isEtcdRunning() bool {
	defer func() {
		// recover from panic if one occurred. Set err to nil otherwise.
		if r := recover(); r != nil {
			manager.log.Info("[DEBUG] etcd isEtcdRunning has panicked", "panic", r)
		} else {
			manager.log.Info("[DEBUG] etcd isEtcdRunning finished successfully")
		}
	}()
	manager.log.Info("[DEBUG] etcd isEtcdRunning started")
	ep1 := fmt.Sprintf("%s:2379", manager.nodeName)

	cli, err := clientv3.New(clientv3.Config{
		Endpoints: []string{ep1, "192.168.111.20:2379", "192.168.111.21:2379", "192.168.111.22:2379",
			"192.168.111.20:9979", "192.168.111.21:9979", "192.168.111.22:9979"},
		DialTimeout: 20 * time.Second,
	})
	manager.log.Info("[DEBUG] etcd isEtcdRunning client created successfully", "isSuccessfully", err == nil)
	if err != nil {
		manager.log.Info("[DEBUG] etcd is tested and down", "error", err)
		return false
	}

	defer cli.Close()

	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		manager.log.Info("[DEBUG] etcd isEtcdRunning about to check status")
		status, err := cli.Status(ctx, "192.168.111.20:2379")
		cancel()
		manager.log.Info("[DEBUG] etcd health results", "error", err, "status", status)
	}()

	timeout := time.Second * 60
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		manager.log.Info("[DEBUG] etcd isEtcdRunning about to get Role List")
		res, err := cli.RoleList(ctx)
		cancel()
		manager.log.Info("[DEBUG] etcd role list", "error", err, "result", res)
	}()

	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		manager.log.Info("[DEBUG] etcd isEtcdRunning about to get User List")
		res, err := cli.UserList(ctx)
		cancel()
		manager.log.Info("[DEBUG] etcd user list", "error", err, "result", res)
	}()

	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		manager.log.Info("[DEBUG] etcd isEtcdRunning about to get member List")
		res, err := cli.MemberList(ctx)
		cancel()
		manager.log.Info("[DEBUG] etcd member list", "error", err, "result", res)
	}()
	wg.Wait()
	//manager.log.Info("[DEBUG] etcd is tested and running")
	return true
}
