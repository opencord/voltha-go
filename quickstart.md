# Quickstart VOLTHA 2.x Build Setup.

These notes describe the checking out and building from the multiple gerrit repositories needed to run a VOLTHA 2.x environment with docker-compose.  Starting point is a basic Ubuntu 16.04 or 18.04 installation with internet access.





## Install prerequisites

Patch and updated
```sh
sudo apt update
sudo apt dist-upgrade
```

Add `docker-ce` apt repo and install docker and build tools
```sh
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo apt-key add -
sudo add-apt-repository "deb [arch=amd64] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable"
sudo apt update
sudo apt install build-essential docker-ce git
```

Install current `docker-compose`.  Older versions may cause docker build problems: https://github.com/docker/docker-credential-helpers/issues/103
```sh
sudo curl -L "https://github.com/docker/compose/releases/download/1.25.0/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose
sudo chmod 755 /usr/local/bin/docker-compose
```

Install the Golang ppa apt repository and install **golang 1.12**.
```sh
sudo add-apt-repository ppa:longsleep/golang-backports
sudo apt update
sudo apt install golang-1.12
```


### Setup environment

Setup a local Golang and docker-compose environment, verifying the golang-1.12 binaries are in your path.  Also add your local GOPATH's bin folder to PATH.
Add to your `~/.profile` to persist.

```sh
mkdir $HOME/source
mkdir $HOME/go
export GO111MODULE=on
export GOPATH=$HOME/go
export DOCKER_TAG=latest
export PATH=$PATH:/usr/lib/go-1.12/bin:$GOPATH/bin
go version
```

Allow your current non-root user `$USER` docker system access
```sh
sudo usermod -a -G docker $USER
```
Logout/Login to assume new group membership needed for running docker as non-root user and verify any environment variables set in `~/.profile`.





## Checkout source and build images



### VOLTHA 2.x Core Containers

Checkout needed source from gerrit. Build the `voltha-rw-core` and `voltha-ofagent` docker images.
```sh
cd ~/source/
git clone https://gerrit.opencord.org/voltha-go.git
cd ~/source/voltha-go
make build
```
For more details regarding building and debugging the 2.x core outside of Docker refer to voltha-go BUILD.md.

https://github.com/opencord/voltha-go/blob/master/BUILD.md



### VOLTHA 2.x OpenOLT Container

Checkout needed source from gerrit.  Build the `voltha-openolt-adapter` docker image.
```sh
cd ~/source/
git clone https://gerrit.opencord.org/voltha-openolt-adapter.git
cd ~/source/voltha-openolt-adapter/
make build
```
For more details regarding building and debugging the openolt adapter container refer to voltha-openolt-adapter README.md.

https://github.com/opencord/voltha-openolt-adapter/blob/master/README.md



### VOLTHA 2.x OpenONU Container

Checkout needed source from gerrit.  Build the `voltha-openonu-adapter` docker image.
```sh
cd ~/source/
git clone https://gerrit.opencord.org/voltha-openonu-adapter.git
cd ~/source/voltha-openonu-adapter/
make build
```



### ONOS Container with VOLTHA Compatible Apps

By default the standard ONOS docker image does not contain nor start any apps needed by VOLTHA.  If you use the standard image then you need to use the ONOS restful API to load needed apps separately.  

For development convenience there is an ONOS docker image build that adds in the current compatible VOLTHA apps.  Checkout and build the ONOS image with added ONOS apps (olt, aaa, sadis, dhcpl2relay, and kafka).

```sh
cd ~/source/
git clone https://gerrit.opencord.org/voltha-onos.git
cd ~/source/voltha-onos
make build
```



### Install voltctl VOLTHA Command Line Management Tool

A working Golang build environment is required as `voltctl` is build and run directly from the host.  Build the `voltctl` executable and install in ~/go/bin which should already be in your $PATH
```sh
cd ~/source/
git clone https://gerrit.opencord.org/voltctl.git
cd ~/source/voltctl
make build
make install
```

Configure the `voltctl` environment configuration files `~/.volt/config` and `~/.volt/command_options` to point at the local `voltha-rw-core` instance that will be running.
```sh
mkdir ~/.volt/

cat << EOF > ~/.volt/config
apiVersion: v2
server: localhost:50057
tls:
  useTls: false
  caCert: ""
  cert: ""
  key: ""
  verify: ""
grpc:
  timeout: 10s
EOF

cat << EOF > ~/.volt/command_options
device-list:
  format: table{{.Id}}\t{{.Type}}\t{{.Root}}\t{{.ParentId}}\t{{.SerialNumber}}\t{{.Address}}\t{{.AdminState}}\t{{.OperStatus}}\t{{.ConnectStatus}}\t{{.Reason}}
  order: -Root,SerialNumber

device-ports:
  order: PortNo

device-flows:
  order: Priority,EthType

logical-device-list:
  order: RootDeviceId,DataPathId

logical-device-ports:
  order: Id

logical-device-flows:
  order: Priority,EthType

adapter-list:
  order: Id

component-list:
  order: Component,Name,Id

loglevel-get:
  order: ComponentName,PackageName,Level

loglevel-list:
  order: ComponentName,PackageName,Level
EOF

```



### Install VOLTHA bbsim olt/onu Simulator (Optional)

If you do not have physical OLT/ONU hardware you can build a simulator.
```sh
cd ~/source/
git clone https://gerrit.opencord.org/bbsim.git
cd ~/source/bbsim
make docker-build
```





## Test

### Startup

Run the combined docker-compose yaml configuration file that starts the core, its dependent systems (etcd, zookeeper, and kafka) and the openonu and openolt adapters.  Export the `DOCKER_HOST_IP` environment variable to your non-localhost IP address needed for inter-container communication.  This can be the IP assigned to `eth0` or the `docker0` bridge (typically 172.17.0.1)

For convenience you can also export `DOCKER_TAG` to signify the docker images tag you would like to use.  Though for typical development you may have to edit `compose/system-test.yml` to override the specific docker image `DOCKER_TAG` needed.  The `DOCKER_REGISTRY` and `DOCKER_REPOSITORY` variables are not needed unless you wish to override the docker image path.  See the `system-test.yml` file for details on the docker image path creation string.
```sh
export DOCKER_HOST_IP=172.17.0.1
export DOCKER_TAG=latest

cd ~/source/voltha-go
docker-compose -f compose/system-test.yml up -d


WARNING: The DOCKER_REGISTRY variable is not set. Defaulting to a blank string.
WARNING: The DOCKER_REPOSITORY variable is not set. Defaulting to a blank string.
Creating network "compose_default" with driver "bridge"
Pulling zookeeper (wurstmeister/zookeeper:latest)...
latest: Pulling from wurstmeister/zookeeper
a3ed95caeb02: Pull complete
ef38b711a50f: Pull complete
e057c74597c7: Pull complete
666c214f6385: Pull complete
c3d6a96f1ffc: Pull complete
3fe26a83e0ca: Pull complete
3d3a7dd3a3b1: Pull complete
f8cc938abe5f: Pull complete
9978b75f7a58: Pull complete
4d4dbcc8f8cc: Pull complete
8b130a9baa49: Pull complete
6b9611650a73: Pull complete
5df5aac51927: Pull complete
76eea4448d9b: Pull complete
8b66990876c6: Pull complete
f0dd38204b6f: Pull complete
Digest: sha256:7a7fd44a72104bfbd24a77844bad5fabc86485b036f988ea927d1780782a6680
Status: Downloaded newer image for wurstmeister/zookeeper:latest
Pulling kafka (wurstmeister/kafka:2.11-2.0.1)...
2.11-2.0.1: Pulling from wurstmeister/kafka
4fe2ade4980c: Pull complete
6fc58a8d4ae4: Pull complete
819f4a45746c: Pull complete
a3133bc2e3e5: Pull complete
72f0dc369677: Pull complete
1e1130fc942d: Pull complete
Digest: sha256:20d08a6849383b124bccbe58bc9c48ec202eefb373d05e0a11e186459b84f2a0
Status: Downloaded newer image for wurstmeister/kafka:2.11-2.0.1
Pulling etcd (quay.io/coreos/etcd:v3.2.9)...
v3.2.9: Pulling from coreos/etcd
88286f41530e: Pull complete
2fa4a2c3ffb5: Pull complete
539b8e6ccce1: Pull complete
79e70e608afa: Pull complete
f1bf8f503bff: Pull complete
c4abfc27d146: Pull complete
Digest: sha256:1913dd980d55490fa50640bbef0f4540d124e5c66d6db271b0b4456e9370a272
Status: Downloaded newer image for quay.io/coreos/etcd:v3.2.9
Creating compose_kafka_1           ... done
Creating compose_cli_1             ... done
Creating compose_adapter_openolt_1 ... done
Creating compose_rw_core_1         ... done
Creating compose_adapter_openonu_1 ... done
Creating compose_etcd_1            ... done
Creating compose_onos_1            ... done
Creating compose_ofagent_1         ... done
Creating compose_zookeeper_1       ... done
```



Verify containers have continuous uptime and no restarts

```sh
docker-compose -f compose/system-test.yml ps


          Name                         Command               State                                             Ports                                           
---------------------------------------------------------------------------------------------------------------------------------------------------------------
compose_adapter_openolt_1   /app/openolt --kafka_adapt ...   Up      0.0.0.0:50062->50062/tcp                                                                  
compose_adapter_openonu_1   /voltha/adapters/brcm_open ...   Up                                                                                                
compose_cli_1               /voltha/python/cli/setup.s ...   Up      0.0.0.0:5022->22/tcp                                                                      
compose_etcd_1              etcd --name=etcd0 --advert ...   Up      0.0.0.0:2379->2379/tcp, 0.0.0.0:32773->2380/tcp, 0.0.0.0:32772->4001/tcp                  
compose_kafka_1             start-kafka.sh                   Up      0.0.0.0:9092->9092/tcp                                                                    
compose_ofagent_1           /ofagent/ofagent/main.py - ...   Up                                                                                                
compose_onos_1              ./bin/onos-service server        Up      6640/tcp, 0.0.0.0:6653->6653/tcp, 0.0.0.0:8101->8101/tcp, 0.0.0.0:8181->8181/tcp, 9876/tcp
compose_rw_core_1           /app/rw_core -kv_store_typ ...   Up      0.0.0.0:50057->50057/tcp                                                                  
compose_zookeeper_1         /bin/sh -c /usr/sbin/sshd  ...   Up      0.0.0.0:2181->2181/tcp, 22/tcp, 2888/tcp, 3888/tcp                                        
```

```sh
docker ps


CONTAINER ID        IMAGE                           COMMAND                  CREATED             STATUS              PORTS                                                                                        NAMES
829c3cb4c6c5        voltha-onos:latest              "./bin/onos-service …"   22 seconds ago      Up 16 seconds       0.0.0.0:6653->6653/tcp, 0.0.0.0:8101->8101/tcp, 6640/tcp, 9876/tcp, 0.0.0.0:8181->8181/tcp   compose_onos_1
e6f2db007bea        voltha-openonu-adapter:latest   "/voltha/adapters/br…"   22 seconds ago      Up 18 seconds                                                                                                    compose_adapter_openonu_1
11be84da6d23        voltha-rw-core:latest           "/app/rw_core -kv_st…"   22 seconds ago      Up 12 seconds       0.0.0.0:50057->50057/tcp                                                                     compose_rw_core_1
e497985c70ac        quay.io/coreos/etcd:v3.2.9      "etcd --name=etcd0 -…"   22 seconds ago      Up 13 seconds       0.0.0.0:2379->2379/tcp, 0.0.0.0:32773->2380/tcp, 0.0.0.0:32772->4001/tcp                     compose_etcd_1
48fc5680e947        voltha-ofagent:latest           "/ofagent/ofagent/ma…"   22 seconds ago      Up 18 seconds                                                                                                    compose_ofagent_1
1ba37663929a        voltha-cli:latest               "/voltha/python/cli/…"   22 seconds ago      Up 17 seconds       0.0.0.0:5022->22/tcp                                                                         compose_cli_1
34d7ea255778        wurstmeister/kafka:2.11-2.0.1   "start-kafka.sh"         22 seconds ago      Up 19 seconds       0.0.0.0:9092->9092/tcp                                                                       compose_kafka_1
c92d2e52ad96        wurstmeister/zookeeper:latest   "/bin/sh -c '/usr/sb…"   22 seconds ago      Up 18 seconds       22/tcp, 2888/tcp, 3888/tcp, 0.0.0.0:2181->2181/tcp                                           compose_zookeeper_1
69d3409fdf10        voltha-openolt-adapter:latest   "/app/openolt --kafk…"   22 seconds ago      Up 14 seconds       0.0.0.0:50062->50062/tcp 
```



### Verify Cluster Communication

Use `voltctl` commands to verify core and adapters are running.

```sh
voltctl adapter list
ID                   VENDOR            VERSION
brcm_openomci_onu    Voltha project    2.0
openolt              VOLTHA OpenOLT    2.2.3-dev
```

List "devices" to verify no devices exist.
```sh
voltctl device list
ID    TYPE    ROOT    PARENTID    SERIALNUMBER    ADDRESS    ADMINSTATE    OPERSTATUS    CONNECTSTATUS    REASON
```

At this point create/preprovision and enable an olt device and add flows via onos and ofagent.



### Physical OLT/ONU Testing with Passing Traffic

Start a physical OLT and ONU.  Tested with Edgecore OLT, Broadcom based ONU, and RG capable of EAPoL.  Create/Preprovision the OLT and enable.  The create command returns the device ID needed for enable and subsequent commands.



**Add device to VOLTHA**

```sh
voltctl device create -t openolt -H 10.64.1.206:9191
ce7c6fc7bf8ce675d0ce9f21

voltctl device enable ce7c6fc7bf8ce675d0ce9f21
```



**Verify device state**

```sh
voltctl device list
ID                          TYPE                 ROOT     PARENTID                    SERIALNUMBER    ADDRESS             ADMINSTATE    OPERSTATUS    CONNECTSTATUS    REASON
ce7c6fc7bf8ce675d0ce9f21    openolt              true     a82bb53678ae                EC1721000221    10.64.1.206:9191    ENABLED       ACTIVE        REACHABLE
1815c74841da338cb48e1034    brcm_openomci_onu    false    ce7c6fc7bf8ce675d0ce9f21    ALPHe3d1cf57    unknown             ENABLED       ACTIVE        REACHABLE        omci-flows-pushed
```

```sh
voltctl device ports ce7c6fc7bf8ce675d0ce9f21
PORTNO       LABEL            TYPE            ADMINSTATE    OPERSTATUS    DEVICEID    PEERS
1048576      nni-1048576      ETHERNET_NNI    ENABLED       ACTIVE                    []
536870912    pon-536870912    PON_OLT         ENABLED       ACTIVE                    [{1815c74841da338cb48e1034 536870912}]
536870913    pon-536870913    PON_OLT         ENABLED       ACTIVE                    []
536870914    pon-536870914    PON_OLT         ENABLED       ACTIVE                    []
536870915    pon-536870915    PON_OLT         ENABLED       ACTIVE                    []
536870916    pon-536870916    PON_OLT         ENABLED       ACTIVE                    []
536870917    pon-536870917    PON_OLT         ENABLED       ACTIVE                    []
536870918    pon-536870918    PON_OLT         ENABLED       ACTIVE                    []
536870919    pon-536870919    PON_OLT         ENABLED       ACTIVE                    []
536870920    pon-536870920    PON_OLT         ENABLED       ACTIVE                    []
536870921    pon-536870921    PON_OLT         ENABLED       ACTIVE                    []
536870922    pon-536870922    PON_OLT         ENABLED       ACTIVE                    []
536870923    pon-536870923    PON_OLT         ENABLED       ACTIVE                    []
536870924    pon-536870924    PON_OLT         ENABLED       ACTIVE                    []
536870925    pon-536870925    PON_OLT         ENABLED       ACTIVE                    []
536870926    pon-536870926    PON_OLT         ENABLED       ACTIVE                    []
536870927    pon-536870927    PON_OLT         ENABLED       ACTIVE                    []
```

```sh
voltctl device ports 1815c74841da338cb48e1034
PORTNO       LABEL       TYPE            ADMINSTATE    OPERSTATUS    DEVICEID    PEERS
32           uni-32      ETHERNET_UNI    ENABLED       ACTIVE                    []
33           uni-33      ETHERNET_UNI    ENABLED       ACTIVE                    []
34           uni-34      ETHERNET_UNI    ENABLED       ACTIVE                    []
35           uni-35      ETHERNET_UNI    ENABLED       ACTIVE                    []
36           uni-36      ETHERNET_UNI    ENABLED       ACTIVE                    []
536870912    PON port    PON_ONU         ENABLED       ACTIVE                    [{ce7c6fc7bf8ce675d0ce9f21 536870912}]
```


Verify ONOS device state and eventual EAPoL authentication.  ONOS default Username is `karaf`, Password is `karaf`
```sh
ssh -p 8101 karaf@localhost
```

Display the device and ports discovered
```sh
onos> ports
id=of:0000a82bb53678ae, available=true, local-status=connected 2m18s ago, role=MASTER, type=SWITCH, mfr=VOLTHA Project, hw=open_pon, sw=open_pon, serial=EC1721000221, chassis=a82bb53678ae, driver=voltha, channelId=172.30.0.1:39080, managementAddress=172.30.0.1, protocol=OF_13
  port=32, state=enabled, type=fiber, speed=0 , adminState=enabled, portMac=08:00:00:00:00:20, portName=ALPHe3d1cf57-1
  port=33, state=enabled, type=fiber, speed=0 , adminState=enabled, portMac=08:00:00:00:00:21, portName=ALPHe3d1cf57-2
  port=34, state=enabled, type=fiber, speed=0 , adminState=enabled, portMac=08:00:00:00:00:22, portName=ALPHe3d1cf57-3
  port=35, state=enabled, type=fiber, speed=0 , adminState=enabled, portMac=08:00:00:00:00:23, portName=ALPHe3d1cf57-4
  port=36, state=enabled, type=fiber, speed=0 , adminState=enabled, portMac=08:00:00:00:00:24, portName=ALPHe3d1cf57-5
  port=1048576, state=enabled, type=fiber, speed=0 , adminState=enabled, portMac=a8:2b:b5:36:78:ae, portName=nni-1048576
```

EAPoL may take up to 30 seconds to complete.
```sh
onos> aaa-users
UserName=94:CC:B9:DA:AB:D1,CurrentState=AUTHORIZED,DeviceId=of:0000a82bb53678ae,MAC=94:CC:B9:DA:AB:D1,PortNumber=32,SubscriberId=PON 1/1/3/1:2.1.1
```



**Provision subscriber flows**

```sh
onos> volt-subscribers
port=of:0000a82bb53678ae/32, svlan=13, cvlan=22

onos> volt-add-subscriber-access of:0000a82bb53678ae 32

onos> volt-programmed-subscribers
location=of:of:0000a82bb53678ae/32 subscriber=[id:ALPHe3d1cf57-1,cTag:20,sTag:11,nasPortId:PON 1/1/3/1:2.1.1,uplinkPort:-1,slot:-1,hardwareIdentifier:null,ipaddress:null,nasId:null,circuitId:PON 1/1/3/1:2.1.1-CID,remoteId:ATLEDGEVOLT1-RID]
```

After about 30 seconds the RG should attempt DHCP which should be visible in onos.  At this point the RG should be able to pass database traffic via the ONU/OLT.
```sh
onos> dhcpl2relay-allocations
SubscriberId=PON 1/1/3/1:2.1.1,ConnectPoint=of:0000a82bb53678ae/32,State=DHCPACK,MAC=94:CC:B9:DA:AB:D1,CircuitId=None,IP Allocated=29.29.206.20,Allocation Timestamp=2019-11-28T18:59:05.472Z
```



### BBSIM Simulated OLT/ONU Testing Control Plane Traffic

If you do not have physical OLT/ONU hardware you can start VOLTHA containers and the bbsim olt/onu hardware simulator using a different docker-compose yaml file.  Verify containers are running as above with the addition of the bbsim and radius server containers.
```sh
export DOCKER_HOST_IP=172.17.0.1
export DOCKER_TAG=latest

cd ~/source/voltha-go
docker-compose -f compose/system-test-bbsim.yml up -d
```

Create/Preprovision and enable similarly to the physical OLT above, providing the local bbsim IP and listening port
```sh
voltctl device create -t openolt -H 172.17.0.1:50060
ece94c86e93c6e06dd0a544b

voltctl device enable ece94c86e93c6e06dd0a544b
ece94c86e93c6e06dd0a544b
```

Proceed with the verification and ONOS provisioning commands similar to the physical OLT described above.
