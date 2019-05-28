/*
 * Copyright 2018-present Open Networking Foundation

 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at

 * http://www.apache.org/licenses/LICENSE-2.0

 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path"
	"regexp"
	"strconv"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/common/version"
	"github.com/opencord/voltha-go/kafka"
	pb "github.com/opencord/voltha-protos/go/afrouter"
	cmn "github.com/opencord/voltha-protos/go/common"
	ic "github.com/opencord/voltha-protos/go/inter_container"
	vpb "github.com/opencord/voltha-protos/go/voltha"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type volthaPod struct {
	name       string
	ipAddr     string
	node       string
	devIds     map[string]struct{}
	cluster    string
	backend    string
	connection string
}

type podTrack struct {
	pod *volthaPod
	dn  bool
}

type Configuration struct {
	DisplayVersionOnly *bool
}

var (
	podNamespace = getStrEnv("POD_NAMESPACE", "voltha")
	podGrpcPort  = uint64(getIntEnv("POD_GRPC_PORT", 0, math.MaxUint16, 50057))

	numRWPods = getIntEnv("NUM_RW_PODS", 1, math.MaxInt32, 6)
	numROPods = getIntEnv("NUM_RO_PODS", 1, math.MaxInt32, 3)

	afrouterRouterName    = getStrEnv("AFROUTER_ROUTER_NAME", "vcore")
	afrouterRWClusterName = getStrEnv("AFROUTER_RW_CLUSTER_NAME", "vcore")
	afrouterROClusterName = getStrEnv("AFROUTER_RO_CLUSTER_NAME", "ro_vcore")

	kafkaTopic      = getStrEnv("KAFKA_TOPIC", "AffinityRouter")
	kafkaClientType = getStrEnv("KAFKA_CLIENT_TYPE", "sarama")
	kafkaHost       = getStrEnv("KAFKA_HOST", "kafka")
	kafkaPort       = getIntEnv("KAFKA_PORT", 0, math.MaxUint16, 9092)
	kafkaInstanceID = getStrEnv("KAFKA_INSTANCE_ID", "arouterd")
)

func getIntEnv(key string, min, max, defaultValue int) int {
	if val, have := os.LookupEnv(key); have {
		num, err := strconv.Atoi(val)
		if err != nil || !(min <= num && num <= max) {
			panic(fmt.Errorf("%s must be a number in the range [%d, %d]; default: %d", key, min, max, defaultValue))
		}
		return num
	}
	return defaultValue
}

func getStrEnv(key, defaultValue string) string {
	if val, have := os.LookupEnv(key); have {
		return val
	}
	return defaultValue
}

func newKafkaClient(clientType string, host string, port int, instanceID string) (kafka.Client, error) {

	log.Infow("kafka-client-type", log.Fields{"client": clientType})
	switch clientType {
	case "sarama":
		return kafka.NewSaramaClient(
			kafka.Host(host),
			kafka.Port(port),
			kafka.ConsumerType(kafka.GroupCustomer),
			kafka.ProducerReturnOnErrors(true),
			kafka.ProducerReturnOnSuccess(true),
			kafka.ProducerMaxRetries(6),
			kafka.NumPartitions(3),
			kafka.ConsumerGroupName(instanceID),
			kafka.ConsumerGroupPrefix(instanceID),
			kafka.AutoCreateTopic(false),
			kafka.ProducerFlushFrequency(5),
			kafka.ProducerRetryBackoff(time.Millisecond*30)), nil
	}
	return nil, errors.New("unsupported-client-type")
}

func k8sClientSet() *kubernetes.Clientset {
	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	return clientset
}

func connect(addr string) (*grpc.ClientConn, error) {
	for ctr := 0; ctr < 100; ctr++ {
		log.Debugf("Trying to connect to %s", addr)
		conn, err := grpc.Dial(addr, grpc.WithInsecure())
		if err != nil {
			log.Debugf("Attempt to connect failed, retrying %v:", err)
		} else {
			log.Debugf("Connection succeeded")
			return conn, err
		}
		time.Sleep(10 * time.Second)
	}
	log.Debugf("Too many connection attempts, giving up!")
	return nil, errors.New("Timeout attempting to conect")
}

func getVolthaPods(cs *kubernetes.Clientset, coreFilter *regexp.Regexp) []*volthaPod {
	var rtrn []*volthaPod

	pods, err := cs.CoreV1().Pods(podNamespace).List(metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	//log.Debugf("There are a total of %d pods in the cluster\n", len(pods.Items))

	for _, v := range pods.Items {
		if coreFilter.MatchString(v.Name) {
			log.Debugf("Namespace: %s, PodName: %s, PodIP: %s, Host: %s\n", v.Namespace, v.Name,
				v.Status.PodIP, v.Spec.NodeName)
			// Only add the pod if it has an IP address. If it doesn't then it likely crashed and
			// and is still in the process of getting re-started.
			if v.Status.PodIP != "" {
				rtrn = append(rtrn, &volthaPod{name: v.Name, ipAddr: v.Status.PodIP, node: v.Spec.NodeName,
					devIds: make(map[string]struct{}), backend: "", connection: ""})
			}
		}
	}
	return rtrn
}

func reconcilePodDeviceIds(pod *volthaPod, ids map[string]struct{}) bool {
	conn, err := connect(fmt.Sprintf("%s:%d", pod.ipAddr, podGrpcPort))
	defer conn.Close()
	if err != nil {
		log.Debugf("Could not query devices from %s, could not connect", pod.name)
		return false
	}

	var idList cmn.IDs
	for k := range ids {
		idList.Items = append(idList.Items, &cmn.ID{Id: k})
	}

	client := vpb.NewVolthaServiceClient(conn)
	_, err = client.ReconcileDevices(context.Background(), &idList)
	if err != nil {
		log.Error(err)
		return false
	}

	return true
}

func queryPodDeviceIds(pod *volthaPod) map[string]struct{} {
	var rtrn = make(map[string]struct{})
	// Open a connection to the pod
	conn, err := connect(fmt.Sprintf("%s:%d", pod.ipAddr, podGrpcPort))
	if err != nil {
		log.Debugf("Could not query devices from %s, could not connect", pod.name)
		return rtrn
	}
	defer conn.Close()
	client := vpb.NewVolthaServiceClient(conn)
	devs, err := client.ListDeviceIds(context.Background(), &empty.Empty{})
	if err != nil {
		log.Error(err)
		return rtrn
	}
	for _, dv := range devs.Items {
		rtrn[dv.Id] = struct{}{}
	}

	return rtrn
}

func queryDeviceIds(pods []*volthaPod) {
	for pk := range pods {
		// Keep the old Id list if a new list is not returned
		if idList := queryPodDeviceIds(pods[pk]); len(idList) != 0 {
			pods[pk].devIds = idList
		}
	}
}

func allEmpty(pods []*volthaPod) bool {
	for k := range pods {
		if len(pods[k].devIds) != 0 {
			return false
		}
	}
	return true
}

func rmPod(pods []*volthaPod, idx int) []*volthaPod {
	return append(pods[:idx], pods[idx+1:]...)
}

func groupIntersectingPods1(pods []*volthaPod, podCt int) ([][]*volthaPod, []*volthaPod) {
	var rtrn [][]*volthaPod
	var out []*volthaPod

	for {
		if len(pods) == 0 {
			break
		}
		if len(pods[0].devIds) == 0 { // Ignore pods with no devices
			////log.Debugf("%s empty pod", pd[k].pod.name)
			out = append(out, pods[0])
			pods = rmPod(pods, 0)
			continue
		}
		// Start a pod group with this pod
		var grp []*volthaPod
		grp = append(grp, pods[0])
		pods = rmPod(pods, 0)
		//log.Debugf("Creating new group %s", pd[k].pod.name)
		// Find the peer pod based on device overlap
		// It's ok if one isn't found, an empty one will be used instead
		for k := range pods {
			if len(pods[k].devIds) == 0 { // Skip pods with no devices
				//log.Debugf("%s empty pod", pd[k1].pod.name)
				continue
			}
			if intersect(grp[0].devIds, pods[k].devIds) {
				//log.Debugf("intersection found %s:%s", pd[k].pod.name, pd[k1].pod.name)
				if grp[0].node == pods[k].node {
					// This should never happen
					log.Errorf("pods %s and %s intersect and are on the same server!! Not pairing",
						grp[0].name, pods[k].name)
					continue
				}
				grp = append(grp, pods[k])
				pods = rmPod(pods, k)
				break

			}
		}
		rtrn = append(rtrn, grp)
		//log.Debugf("Added group %s", grp[0].name)
		// Check if the number of groups = half the pods, if so all groups are started.
		if len(rtrn) == podCt>>1 {
			// Append any remaining pods to out
			out = append(out, pods[0:]...)
			break
		}
	}
	return rtrn, out
}

func unallocPodCount(pd []*podTrack) int {
	var rtrn int = 0
	for _, v := range pd {
		if !v.dn {
			rtrn++
		}
	}
	return rtrn
}

func sameNode(pod *volthaPod, grps [][]*volthaPod) bool {
	for _, v := range grps {
		if v[0].node == pod.node {
			return true
		}
		if len(v) == 2 && v[1].node == pod.node {
			return true
		}
	}
	return false
}

func startRemainingGroups1(grps [][]*volthaPod, pods []*volthaPod, podCt int) ([][]*volthaPod, []*volthaPod) {
	var grp []*volthaPod

	for k := range pods {
		if sameNode(pods[k], grps) {
			continue
		}
		grp = []*volthaPod{}
		grp = append(grp, pods[k])
		pods = rmPod(pods, k)
		grps = append(grps, grp)
		if len(grps) == podCt>>1 {
			break
		}
	}
	return grps, pods
}

func hasSingleSecondNode(grp []*volthaPod) bool {
	var servers = make(map[string]struct{})
	for k := range grp {
		if k == 0 {
			continue // Ignore the first item
		}
		servers[grp[k].node] = struct{}{}
	}
	if len(servers) == 1 {
		return true
	}
	return false
}

func addNode(grps [][]*volthaPod, idx *volthaPod, item *volthaPod) [][]*volthaPod {
	for k := range grps {
		if grps[k][0].name == idx.name {
			grps[k] = append(grps[k], item)
			return grps
		}
	}
	// TODO: Error checking required here.
	return grps
}

func removeNode(grps [][]*volthaPod, item *volthaPod) [][]*volthaPod {
	for k := range grps {
		for k1 := range grps[k] {
			if grps[k][k1].name == item.name {
				grps[k] = append(grps[k][:k1], grps[k][k1+1:]...)
				break
			}
		}
	}
	return grps
}

func groupRemainingPods1(grps [][]*volthaPod, pods []*volthaPod) [][]*volthaPod {
	var lgrps [][]*volthaPod
	// All groups must be started when this function is called.
	// Copy incomplete groups
	for k := range grps {
		if len(grps[k]) != 2 {
			lgrps = append(lgrps, grps[k])
		}
	}

	// Add all pairing candidates to each started group.
	for k := range pods {
		for k2 := range lgrps {
			if lgrps[k2][0].node != pods[k].node {
				lgrps[k2] = append(lgrps[k2], pods[k])
			}
		}
	}

	//TODO: If any member of lgrps doesn't have at least 2
	// nodes something is wrong. Check for that here

	for {
		for { // Address groups with only a single server choice
			var ssn bool = false

			for k := range lgrps {
				// Now if any of the groups only have a single
				// node as the choice for the second member
				// address that one first.
				if hasSingleSecondNode(lgrps[k]) {
					ssn = true
					// Add this pairing to the groups
					grps = addNode(grps, lgrps[k][0], lgrps[k][1])
					// Since this node is now used, remove it from all
					// remaining tenative groups
					lgrps = removeNode(lgrps, lgrps[k][1])
					// Now remove this group completely since
					// it's been addressed
					lgrps = append(lgrps[:k], lgrps[k+1:]...)
					break
				}
			}
			if !ssn {
				break
			}
		}
		// Now address one of the remaining groups
		if len(lgrps) == 0 {
			break // Nothing left to do, exit the loop
		}
		grps = addNode(grps, lgrps[0][0], lgrps[0][1])
		lgrps = removeNode(lgrps, lgrps[0][1])
		lgrps = append(lgrps[:0], lgrps[1:]...)
	}
	return grps
}

func groupPods1(pods []*volthaPod) [][]*volthaPod {
	var rtrn [][]*volthaPod
	var podCt int = len(pods)

	rtrn, pods = groupIntersectingPods1(pods, podCt)
	// There are several outcomes here
	// 1) All pods have been paired and we're done
	// 2) Some un-allocated pods remain
	// 2.a) All groups have been started
	// 2.b) Not all groups have been started
	if len(pods) == 0 {
		return rtrn
	} else if len(rtrn) == podCt>>1 { // All groupings started
		// Allocate the remaining (presumably empty) pods to the started groups
		return groupRemainingPods1(rtrn, pods)
	} else { // Some groupings started
		// Start empty groups with remaining pods
		// each grouping is on a different server then
		// allocate remaining pods.
		rtrn, pods = startRemainingGroups1(rtrn, pods, podCt)
		return groupRemainingPods1(rtrn, pods)
	}
}

func intersect(d1 map[string]struct{}, d2 map[string]struct{}) bool {
	for k := range d1 {
		if _, ok := d2[k]; ok {
			return true
		}
	}
	return false
}

func setConnection(client pb.ConfigurationClient, cluster string, backend string, connection string, addr string, port uint64) {
	log.Debugf("Configuring backend %s : connection %s in cluster %s\n\n",
		backend, connection, cluster)
	cnf := &pb.Conn{Server: "grpc_command", Cluster: cluster, Backend: backend,
		Connection: connection, Addr: addr,
		Port: port}
	if res, err := client.SetConnection(context.Background(), cnf); err != nil {
		log.Debugf("failed SetConnection RPC call: %s", err)
	} else {
		log.Debugf("Result: %v", res)
	}
}

func setAffinity(client pb.ConfigurationClient, ids map[string]struct{}, backend string) {
	log.Debugf("Configuring backend %s : affinities \n", backend)
	aff := &pb.Affinity{Router: afrouterRouterName, Route: "dev_manager", Cluster: afrouterRWClusterName, Backend: backend}
	for k := range ids {
		log.Debugf("Setting affinity for id %s", k)
		aff.Id = k
		if res, err := client.SetAffinity(context.Background(), aff); err != nil {
			log.Debugf("failed affinity RPC call: %s", err)
		} else {
			log.Debugf("Result: %v", res)
		}
	}
}

func getBackendForCore(coreId string, coreGroups [][]*volthaPod) string {
	for _, v := range coreGroups {
		for _, v2 := range v {
			if v2.name == coreId {
				return v2.backend
			}
		}
	}
	log.Errorf("No backend found for core %s\n", coreId)
	return ""
}

func monitorDiscovery(client pb.ConfigurationClient,
	ch <-chan *ic.InterContainerMessage,
	coreGroups [][]*volthaPod) {
	var id = make(map[string]struct{})

	select {
	case msg := <-ch:
		log.Debugf("Received a device discovery notification")
		device := &ic.DeviceDiscovered{}
		if err := ptypes.UnmarshalAny(msg.Body, device); err != nil {
			log.Errorf("Could not unmarshal received notification %v", msg)
		} else {
			// Set the affinity of the discovered device.
			if be := getBackendForCore(device.Id, coreGroups); be != "" {
				id[device.Id] = struct{}{}
				setAffinity(client, id, be)
			} else {
				log.Error("Cant use an empty string as a backend name")
			}
		}
		break
	}
}

func startDiscoveryMonitor(client pb.ConfigurationClient,
	coreGroups [][]*volthaPod) error {
	var ch <-chan *ic.InterContainerMessage
	// Connect to kafka for discovery events
	topic := &kafka.Topic{Name: kafkaTopic}
	kc, err := newKafkaClient(kafkaClientType, kafkaHost, kafkaPort, kafkaInstanceID)
	kc.Start()

	if ch, err = kc.Subscribe(topic); err != nil {
		log.Errorf("Could not subscribe to the '%s' channel, discovery disabled", kafkaTopic)
		return err
	}
	go monitorDiscovery(client, ch, coreGroups)
	return nil
}

// Determines which items in core groups
// have changed based on the list provided
// and returns a coreGroup with only the changed
// items and a pod list with the new items
func getAddrDiffs(coreGroups [][]*volthaPod, rwPods []*volthaPod) ([][]*volthaPod, []*volthaPod) {
	var nList []*volthaPod
	var rtrn = make([][]*volthaPod, numRWPods>>1)
	var ipAddrs = make(map[string]struct{})

	log.Debug("Get addr diffs")

	// Start with an empty array
	for k := range rtrn {
		rtrn[k] = make([]*volthaPod, 2)
	}

	// Build a list with only the new items
	for _, v := range rwPods {
		if !hasIpAddr(coreGroups, v.ipAddr) {
			nList = append(nList, v)
		}
		ipAddrs[v.ipAddr] = struct{}{} // for the search below
	}

	// Now build the coreGroups with only the changed items
	for k1, v1 := range coreGroups {
		for k2, v2 := range v1 {
			if _, ok := ipAddrs[v2.ipAddr]; !ok {
				rtrn[k1][k2] = v2
			}
		}
	}
	return rtrn, nList
}

// Figure out where best to put the new pods
// in the coreGroup array based on the old
// pods being replaced. The criteria is that
// the new pod be on the same server as the
// old pod was.
func reconcileAddrDiffs(coreGroupDiffs [][]*volthaPod, rwPodDiffs []*volthaPod) [][]*volthaPod {
	var srvrs map[string][]*volthaPod = make(map[string][]*volthaPod)

	log.Debug("Reconciling diffs")
	log.Debug("Building server list")
	for _, v := range rwPodDiffs {
		log.Debugf("Adding %v to the server list", *v)
		srvrs[v.node] = append(srvrs[v.node], v)
	}

	for k1, v1 := range coreGroupDiffs {
		log.Debugf("k1:%v, v1:%v", k1, v1)
		for k2, v2 := range v1 {
			log.Debugf("k2:%v, v2:%v", k2, v2)
			if v2 == nil { // Nothing to do here
				continue
			}
			if _, ok := srvrs[v2.node]; ok {
				coreGroupDiffs[k1][k2] = srvrs[v2.node][0]
				if len(srvrs[v2.node]) > 1 { // remove one entry from the list
					srvrs[v2.node] = append(srvrs[v2.node][:0], srvrs[v2.node][1:]...)
				} else { // Delete the endtry from the map
					delete(srvrs, v2.node)
				}
			} else {
				log.Error("This should never happen, node appears to have changed names")
				// attempt to limp along by keeping this old entry
			}
		}
	}

	return coreGroupDiffs
}

func applyAddrDiffs(client pb.ConfigurationClient, coreList interface{}, nPods []*volthaPod) {
	var newEntries [][]*volthaPod

	log.Debug("Applying diffs")
	switch cores := coreList.(type) {
	case [][]*volthaPod:
		newEntries = reconcileAddrDiffs(getAddrDiffs(cores, nPods))

		// Now replace the information in coreGropus with the new
		// entries and then reconcile the device ids on the core
		// that's in the new entry with the device ids of it's
		// active-active peer.
		for k1, v1 := range cores {
			for k2, v2 := range v1 {
				if newEntries[k1][k2] != nil {
					// TODO: Missing is the case where bothe the primary
					// and the secondary core crash and come back.
					// Pull the device ids from the active-active peer
					ids := queryPodDeviceIds(cores[k1][k2^1])
					if len(ids) != 0 {
						if !reconcilePodDeviceIds(newEntries[k1][k2], ids) {
							log.Errorf("Attempt to reconcile ids on pod %v failed", newEntries[k1][k2])
						}
					}
					// Send the affininty router new connection information
					setConnection(client, afrouterRWClusterName, v2.backend, v2.connection, newEntries[k1][k2].ipAddr, podGrpcPort)
					// Copy the new entry information over
					cores[k1][k2].ipAddr = newEntries[k1][k2].ipAddr
					cores[k1][k2].name = newEntries[k1][k2].name
					cores[k1][k2].devIds = ids
				}
			}
		}
	case []*volthaPod:
		var mia []*volthaPod
		var found bool
		// TODO: Break this using functions to simplify
		// reading of the code.
		// Find the core(s) that have changed addresses
		for k1, v1 := range cores {
			found = false
			for _, v2 := range nPods {
				if v1.ipAddr == v2.ipAddr {
					found = true
					break
				}
			}
			if !found {
				mia = append(mia, cores[k1])
			}
		}
		// Now plug in the new addresses and set the connection
		for _, v1 := range nPods {
			found = false
			for _, v2 := range cores {
				if v1.ipAddr == v2.ipAddr {
					found = true
					break
				}
			}
			if found {
				continue
			}
			mia[0].ipAddr = v1.ipAddr
			mia[0].name = v1.name
			setConnection(client, afrouterROClusterName, mia[0].backend, mia[0].connection, v1.ipAddr, podGrpcPort)
			// Now get rid of the mia entry just processed
			mia = append(mia[:0], mia[1:]...)
		}
	default:
		log.Error("Internal: Unexpected type in call to applyAddrDiffs")
	}
}

func updateDeviceIds(coreGroups [][]*volthaPod, rwPods []*volthaPod) {
	var byName = make(map[string]*volthaPod)

	// Convenience
	for _, v := range rwPods {
		byName[v.name] = v
	}

	for k1, v1 := range coreGroups {
		for k2 := range v1 {
			coreGroups[k1][k2].devIds = byName[v1[k2].name].devIds
		}
	}
}

func startCoreMonitor(client pb.ConfigurationClient,
	clientset *kubernetes.Clientset,
	rwCoreFltr *regexp.Regexp,
	roCoreFltr *regexp.Regexp,
	coreGroups [][]*volthaPod,
	oRoPods []*volthaPod) error {
	// Now that initial allocation has been completed, monitor the pods
	// for IP changes
	// The main loop needs to do the following:
	// 1) Periodically query the pods and filter out
	//    the vcore ones
	// 2) Validate that the pods running are the same
	//    as the previous check
	// 3) Validate that the IP addresses are the same
	//    as the last check.
	// If the pod name(s) ha(s/ve) changed then remove
	// the unused pod names and add in the new pod names
	// maintaining the cluster/backend information.
	// If an IP address has changed (which shouldn't
	// happen unless a pod is re-started) it should get
	// caught by the pod name change.
	for {
		time.Sleep(10 * time.Second) // Wait a while
		// Get the rw core list from k8s
		rwPods := getVolthaPods(clientset, rwCoreFltr)
		queryDeviceIds(rwPods)
		updateDeviceIds(coreGroups, rwPods)
		// If we didn't get 2n+1 pods then wait since
		// something is down and will hopefully come
		// back up at some point.
		if len(rwPods) != numRWPods {
			continue
		}
		// We have all pods, check if any IP addresses
		// have changed.
		for _, v := range rwPods {
			if !hasIpAddr(coreGroups, v.ipAddr) {
				log.Debug("Address has changed...")
				applyAddrDiffs(client, coreGroups, rwPods)
				break
			}
		}

		roPods := getVolthaPods(clientset, roCoreFltr)

		if len(roPods) != numROPods {
			continue
		}
		for _, v := range roPods {
			if !hasIpAddr(oRoPods, v.ipAddr) {
				applyAddrDiffs(client, oRoPods, roPods)
				break
			}
		}

	}
}

func hasIpAddr(coreList interface{}, ipAddr string) bool {
	switch cores := coreList.(type) {
	case []*volthaPod:
		for _, v := range cores {
			if v.ipAddr == ipAddr {
				return true
			}
		}
	case [][]*volthaPod:
		for _, v1 := range cores {
			for _, v2 := range v1 {
				if v2.ipAddr == ipAddr {
					return true
				}
			}
		}
	default:
		log.Error("Internal: Unexpected type in call to hasIpAddr")
	}
	return false
}

func main() {
	// This is currently hard coded to a cluster with 3 servers
	//var connections map[string]configConn = make(map[string]configConn)
	//var rwCorePodsPrev map[string]rwPod = make(map[string]rwPod)
	var err error
	var conn *grpc.ClientConn

	config := &Configuration{}
	cmdParse := flag.NewFlagSet(path.Base(os.Args[0]), flag.ContinueOnError)
	config.DisplayVersionOnly = cmdParse.Bool("version", false, "Print version information and exit")

	err = cmdParse.Parse(os.Args[1:])
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if *config.DisplayVersionOnly {
		fmt.Println("VOLTHA API Server (afrouterd)")
		fmt.Println(version.VersionInfo.String("  "))
		return
	}

	// Set up the regular expression to identify the voltha cores
	rwCoreFltr := regexp.MustCompile(`rw-core[0-9]-`)
	roCoreFltr := regexp.MustCompile(`ro-core-`)

	// Set up logging
	if _, err := log.SetDefaultLogger(log.JSON, 0, nil); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}

	// Set up kubernetes api
	clientset := k8sClientSet()

	// Connect to the affinity router and set up the client
	conn, err = connect("localhost:55554") // This is a sidecar container so communicating over localhost
	defer conn.Close()
	if err != nil {
		panic(err.Error())
	}
	client := pb.NewConfigurationClient(conn)

	// Get the voltha rw-core podes
	rwPods := getVolthaPods(clientset, rwCoreFltr)

	// Fetch the devices held by each running core
	queryDeviceIds(rwPods)

	// For debugging... comment out l8r
	for _, v := range rwPods {
		log.Debugf("Pod list %v", *v)
	}

	coreGroups := groupPods1(rwPods)

	// Assign the groupings to the the backends and connections
	for k := range coreGroups {
		for k1 := range coreGroups[k] {
			coreGroups[k][k1].cluster = afrouterRWClusterName
			coreGroups[k][k1].backend = afrouterRWClusterName + strconv.Itoa(k+1)
			coreGroups[k][k1].connection = afrouterRWClusterName + strconv.Itoa(k+1) + strconv.Itoa(k1+1)
		}
	}
	log.Info("Core grouping completed")

	// TODO: Debugging code, comment out for production
	for k, v := range coreGroups {
		for k2, v2 := range v {
			log.Debugf("Core group %d,%d: %v", k, k2, v2)
		}
	}
	log.Info("Setting affinities")
	// Now set the affinities for exising devices in the cores
	for _, v := range coreGroups {
		setAffinity(client, v[0].devIds, v[0].backend)
		setAffinity(client, v[1].devIds, v[1].backend)
	}
	log.Info("Setting connections")
	// Configure the backeds based on the calculated core groups
	for _, v := range coreGroups {
		setConnection(client, afrouterRWClusterName, v[0].backend, v[0].connection, v[0].ipAddr, podGrpcPort)
		setConnection(client, afrouterRWClusterName, v[1].backend, v[1].connection, v[1].ipAddr, podGrpcPort)
	}

	// Process the read only pods
	roPods := getVolthaPods(clientset, roCoreFltr)
	for k, v := range roPods {
		log.Debugf("Processing ro_pod %v", v)
		vN := afrouterROClusterName + strconv.Itoa(k+1)
		log.Debugf("Setting connection %s, %s, %s", vN, vN+"1", v.ipAddr)
		roPods[k].cluster = afrouterROClusterName
		roPods[k].backend = vN
		roPods[k].connection = vN + "1"
		setConnection(client, afrouterROClusterName, v.backend, v.connection, v.ipAddr, podGrpcPort)
	}

	log.Info("Starting discovery monitoring")
	startDiscoveryMonitor(client, coreGroups)

	log.Info("Starting core monitoring")
	startCoreMonitor(client, clientset, rwCoreFltr,
		roCoreFltr, coreGroups, roPods) // Never returns
	return
}
