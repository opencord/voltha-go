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
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/keepalive"
	"k8s.io/api/core/v1"
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
	"k8s.io/client-go/tools/clientcmd"
)

type volthaPod struct {
	name       string
	ipAddr     string
	node       string
	devIds     map[string]struct{}
	backend    string
	connection string
}

type Configuration struct {
	DisplayVersionOnly *bool
}

var (
	// if k8s variables are undefined, will attempt to use in-cluster config
	k8sApiServer      = getStrEnv("K8S_API_SERVER", "")
	k8sKubeConfigPath = getStrEnv("K8S_KUBE_CONFIG_PATH", "")

	podNamespace          = getStrEnv("POD_NAMESPACE", "voltha")
	podLabelSelector      = getStrEnv("POD_LABEL_SELECTOR", "app=rw-core")
	podAffinityGroupLabel = getStrEnv("POD_AFFINITY_GROUP_LABEL", "affinity-group")

	podGrpcPort = uint64(getIntEnv("POD_GRPC_PORT", 0, math.MaxUint16, 50057))

	afrouterApiAddress = getStrEnv("AFROUTER_API_ADDRESS", "localhost:55554")

	afrouterRouterName    = getStrEnv("AFROUTER_ROUTER_NAME", "vcore")
	afrouterRouteName     = getStrEnv("AFROUTER_ROUTE_NAME", "dev_manager")
	afrouterRWClusterName = getStrEnv("AFROUTER_RW_CLUSTER_NAME", "vcore")

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
	var config *rest.Config
	if k8sApiServer != "" || k8sKubeConfigPath != "" {
		// use combination of URL & local kube-config file
		c, err := clientcmd.BuildConfigFromFlags(k8sApiServer, k8sKubeConfigPath)
		if err != nil {
			panic(err)
		}
		config = c
	} else {
		// use in-cluster config
		c, err := rest.InClusterConfig()
		if err != nil {
			log.Errorf("Unable to load in-cluster config.  Try setting K8S_API_SERVER and K8S_KUBE_CONFIG_PATH?")
			panic(err)
		}
		config = c
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	return clientset
}

func connect(ctx context.Context, addr string) (*grpc.ClientConn, error) {
	log.Debugf("Trying to connect to %s", addr)
	conn, err := grpc.DialContext(ctx, addr,
		grpc.WithInsecure(),
		grpc.WithBlock(),
		grpc.WithBackoffMaxDelay(time.Second*5),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: time.Second * 10, Timeout: time.Second * 5}))
	if err == nil {
		log.Debugf("Connection succeeded")
	}
	return conn, err
}

func getVolthaPods(cs *kubernetes.Clientset) ([]*volthaPod, error) {
	pods, err := cs.CoreV1().Pods(podNamespace).List(metav1.ListOptions{LabelSelector: podLabelSelector})
	if err != nil {
		return nil, err
	}

	var rwPods []*volthaPod
items:
	for _, v := range pods.Items {
		// only pods that are actually running should be considered
		if v.Status.Phase == v1.PodRunning {
			for _, condition := range v.Status.Conditions {
				if condition.Status != v1.ConditionTrue {
					continue items
				}
			}

			if group, have := v.Labels[podAffinityGroupLabel]; have {
				log.Debugf("Namespace: %s, PodName: %s, PodIP: %s, Host: %s\n", v.Namespace, v.Name, v.Status.PodIP, v.Spec.NodeName)
				rwPods = append(rwPods, &volthaPod{
					name:    v.Name,
					ipAddr:  v.Status.PodIP,
					node:    v.Spec.NodeName,
					devIds:  make(map[string]struct{}),
					backend: afrouterRWClusterName + group,
				})
			} else {
				log.Warnf("Pod %s found matching % without label %", v.Name, podLabelSelector, podAffinityGroupLabel)
			}
		}
	}
	return rwPods, nil
}

func reconcilePodDeviceIds(ctx context.Context, pod *volthaPod, ids map[string]struct{}) {
	ctxTimeout, _ := context.WithTimeout(ctx, time.Second*5)
	conn, err := connect(ctxTimeout, fmt.Sprintf("%s:%d", pod.ipAddr, podGrpcPort))
	if err != nil {
		log.Debugf("Could not reconcile devices from %s, could not connect: %s", pod.name, err)
		return
	}
	defer conn.Close()

	var idList cmn.IDs
	for k := range ids {
		idList.Items = append(idList.Items, &cmn.ID{Id: k})
	}

	client := vpb.NewVolthaServiceClient(conn)
	_, err = client.ReconcileDevices(ctx, &idList)
	if err != nil {
		log.Errorf("Attempt to reconcile ids on pod %s failed: %s", pod.name, err)
		return
	}
}

func queryPodDeviceIds(ctx context.Context, pod *volthaPod) map[string]struct{} {
	ctxTimeout, _ := context.WithTimeout(ctx, time.Second*5)
	conn, err := connect(ctxTimeout, fmt.Sprintf("%s:%d", pod.ipAddr, podGrpcPort))
	if err != nil {
		log.Debugf("Could not query devices from %s, could not connect: %s", pod.name, err)
		return nil
	}
	defer conn.Close()

	client := vpb.NewVolthaServiceClient(conn)
	devs, err := client.ListDeviceIds(ctx, &empty.Empty{})
	if err != nil {
		log.Error(err)
		return nil
	}

	var ret = make(map[string]struct{})
	for _, dv := range devs.Items {
		ret[dv.Id] = struct{}{}
	}
	return ret
}

func setAffinity(ctx context.Context, client pb.ConfigurationClient, deviceId string, backend string) {
	log.Debugf("Configuring backend %s with device id %s \n", backend, deviceId)
	if res, err := client.SetAffinity(ctx, &pb.Affinity{
		Router:  afrouterRouterName,
		Route:   afrouterRouteName,
		Cluster: afrouterRWClusterName,
		Backend: backend,
		Id:      deviceId,
	}); err != nil {
		log.Debugf("failed affinity RPC call: %s\n", err)
	} else {
		log.Debugf("Result: %v\n", res)
	}
}

func monitorDiscovery(ctx context.Context, client pb.ConfigurationClient, ch <-chan *ic.InterContainerMessage, doneCh chan<- struct{}) {
	defer close(doneCh)

monitorLoop:
	for {
		select {
		case <-ctx.Done():
		case msg := <-ch:
			log.Debug("Received a device discovery notification")
			device := &ic.DeviceDiscovered{}
			if err := ptypes.UnmarshalAny(msg.Body, device); err != nil {
				log.Errorf("Could not unmarshal received notification %v", msg)
			} else {
				// somewhat hackish solution, backend is known from the first digit found in the publisher name
				group := regexp.MustCompile(`\d`).FindString(device.Publisher)
				if group == "" {
					// set the affinity of the discovered device
					setAffinity(ctx, client, device.Id, afrouterRWClusterName+group)
				} else {
					log.Error("backend is unknown")
				}
			}
			break monitorLoop
		}
	}
}

func startDiscoveryMonitor(ctx context.Context, client pb.ConfigurationClient) (<-chan struct{}, error) {
	doneCh := make(chan struct{})
	// Connect to kafka for discovery events
	kc, err := newKafkaClient(kafkaClientType, kafkaHost, kafkaPort, kafkaInstanceID)
	if err != nil {
		panic(err)
	}
	kc.Start()
	defer kc.Stop()

	ch, err := kc.Subscribe(&kafka.Topic{Name: kafkaTopic})
	if err != nil {
		log.Errorf("Could not subscribe to the '%s' channel, discovery disabled", kafkaTopic)
		close(doneCh)
		return doneCh, err
	}

	go monitorDiscovery(ctx, client, ch, doneCh)
	return doneCh, nil
}

// coreMonitor polls the list of devices from all RW cores, pushes these devices
// into the affinity router, and ensures that all cores in a backend have their devices synced
func coreMonitor(ctx context.Context, client pb.ConfigurationClient, clientset *kubernetes.Clientset) {
	// map[backend]map[deviceId]struct{}
	deviceOwnership := make(map[string]map[string]struct{})
loop:
	for {
		// get the rw core list from k8s
		rwPods, err := getVolthaPods(clientset)
		if err != nil {
			log.Error(err)
			continue
		}

		// for every pod
		for _, pod := range rwPods {
			// get the devices for this pod's backend
			devices, have := deviceOwnership[pod.backend]
			if !have {
				devices = make(map[string]struct{})
				deviceOwnership[pod.backend] = devices
			}

			coreDevices := queryPodDeviceIds(ctx, pod)

			// handle devices that exist in the core, but we have just learned about
			for deviceId := range coreDevices {
				// if there's a new device
				if _, have := devices[deviceId]; !have {
					// add the device to our local list
					devices[deviceId] = struct{}{}
					// push the device into the affinity router
					setAffinity(ctx, client, deviceId, pod.backend)
				}
			}

			// ensure that the core knows about all devices in its backend
			toSync := make(map[string]struct{})
			for deviceId := range devices {
				// if the pod is missing any devices
				if _, have := coreDevices[deviceId]; !have {
					// we will reconcile them
					toSync[deviceId] = struct{}{}
				}
			}

			if len(toSync) != 0 {
				reconcilePodDeviceIds(ctx, pod, toSync)
			}
		}

		select {
		case <-ctx.Done():
			// if we're done, exit
			break loop
		case <-time.After(10 * time.Second): // wait a while
		}
	}
}

// endOnClose cancels the context when the connection closes
func connectionActiveContext(conn *grpc.ClientConn) context.Context {
	ctx, disconnected := context.WithCancel(context.Background())
	go func() {
		for state := conn.GetState(); state != connectivity.TransientFailure && state != connectivity.Shutdown; state = conn.GetState() {
			if !conn.WaitForStateChange(context.Background(), state) {
				break
			}
		}
		log.Infof("Connection to afrouter lost")
		disconnected()
	}()
	return ctx
}

func main() {
	config := &Configuration{}
	cmdParse := flag.NewFlagSet(path.Base(os.Args[0]), flag.ContinueOnError)
	config.DisplayVersionOnly = cmdParse.Bool("version", false, "Print version information and exit")

	if err := cmdParse.Parse(os.Args[1:]); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if *config.DisplayVersionOnly {
		fmt.Println("VOLTHA API Server (afrouterd)")
		fmt.Println(version.VersionInfo.String("  "))
		return
	}

	// Set up logging
	if _, err := log.SetDefaultLogger(log.JSON, 0, nil); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}

	// Set up kubernetes api
	clientset := k8sClientSet()

	for {
		// Connect to the affinity router
		conn, err := connect(context.Background(), afrouterApiAddress) // This is a sidecar container so communicating over localhost
		if err != nil {
			panic(err)
		}

		// monitor the connection status, end context if connection is lost
		ctx := connectionActiveContext(conn)

		// set up the client
		client := pb.NewConfigurationClient(conn)

		// start the discovery monitor and core monitor
		// these two processes do the majority of the work

		log.Info("Starting discovery monitoring")
		doneCh, _ := startDiscoveryMonitor(ctx, client)

		log.Info("Starting core monitoring")
		coreMonitor(ctx, client, clientset)

		//ensure the discovery monitor to quit
		<-doneCh

		conn.Close()
	}
}
