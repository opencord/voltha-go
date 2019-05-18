# Quickstart VOLTHA 2.x Build Setup.

These notes describe the checking out and building from the multiple gerrit repositories needed to run a VOLTHA 2.x environment with docker-compose.  Starting point is a basic Ubuntu 16.04 installation with internet access.





## Install prerequisites

Patch and updated
```sh
sudo apt update
sudo apt dist-upgrade
```

Add docker-ce apt repo and install docker and build tools
```sh
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo apt-key add -
sudo add-apt-repository "deb [arch=amd64] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable"
sudo apt update
sudo apt install build-essential docker-ce docker-compose virtualenv git python-setuptools python-dev libpcap-dev libffi-dev libssl-dev tox
```

Snap install golang 1.12.  Inform apparmor (if being used) of the change.  The apparmor_parser step may need to be run again after reboot.  This appears to be a snapd bug.
```sh
sudo snap install --classic go
snap list
sudo apparmor_parser -r /var/lib/snapd/apparmor/profiles/snap*
```

Setup a local Golang environment, verifying the golang-1.12 binaries are in your path.  Also add your local GOPATH's bin folder to PATH.
Add to your ~/.profile to persist.

```sh
go version
mkdir $HOME/go
export GOPATH=$HOME/go
export PATH=$PATH:$GOPATH/bin
```

Allow non-root user docker system access
```sh
sudo usermod -a -G docker $USER
```
Logout/Login to assume new group membership needed for running docker as non-root user.





## Checkout source and build images



### VOLTHA Protos

Library containing all VOLTHA gRPC Protobuf definitions and the build system to produce needed stubs in Python and Golang.  Stable versions of this package is available via python's pip or golang's "dep" or "go get".   If you need to **edit protos and test those changes locally** you will need to refer to the voltha-protos README and export needed environment variables to include the local build.

https://github.com/opencord/voltha-protos/blob/master/README.md


Below is a very quick summary of the steps needed to setup.  Specific versions of protobuf and libprotoc/protoc are needed to generate identical stubs:
```sh
mkdir -p ~/source
cd ~/source
git clone https://gerrit.opencord.org/voltha-protos

mkdir -p $GOPATH/src/github.com/opencord
ln -s ~/source/voltha-protos $GOPATH/src/github.com/opencord/voltha-protos

go get -d github.com/golang/protobuf/
cd $GOPATH/src/github.com/golang/protobuf
git checkout v1.3.1   # needed to match stubs already generated
make install

cd $GOPATH/src/github.com/opencord/voltha-protos
make install-protoc   # installs libprotoc/protoc 3.7.0
make build
```

After following notes above verify local artifactes are generated.  After building the python and golang voltha-protos dev environment, set and environment variable to indicate the local voltha-protos for golang and python if editing/testing protos changes is needed:
```sh
cd ~/source/voltha-protos/
ls dist/    #python pip tarball output
ls go/      #golang stubs
```

Set an environment variable for below Golang and Python builds to inform the Makefile to copy files into the local vendor folder or to use the local pip tar.gz.  Useful for development testing.
```sh
export LOCAL_PROTOS=true
```



### PyVoltha PIP Library

Python library of common core functions.  Stable versions of this package is available via python's pip.  If you need to **edit this library and test those changes locally** you will need to export needed environment variables to include the local build..

```sh
cd ~/source/
git clone https://gerrit.opencord.org/pyvoltha.git
```

Generate the local tar.gz that is the dev version of pyvoltha:
```sh
cd ~/source/pyvoltha/
make dist
ls dist/    #python pip tarball output
```

Set an environment variable for below Python builds to inform the Makefile to use the local pip tar.gz.  Useful for development testing.
```sh
export LOCAL_PYVOLTHA=true
```



### VOLTHA 2.0 Go Core

For more details regarding building and debugging the 2.0 core outside of Docker refer to voltha-go BUILD.md.

https://github.com/opencord/voltha-go/blob/master/BUILD.md


Below is a very quick summary of the steps needed to setup:
```sh
mkdir -p ~/source
cd ~/source
git clone https://gerrit.opencord.org/voltha-go

mkdir -p $GOPATH/src/github.com/opencord
ln -s ~/source/voltha-go $GOPATH/src/github.com/opencord/voltha-go

cd $GOPATH/src/github.com/opencord/voltha-go
go build rw_core/main.go    # verify vendor and other dependancies. output binary not actually used in container builds
```

The steps below generate the needed docker images and the Docker build system sets up the Go environment within a container image.  Build Go docker images, rw_core being whats most needed for now.
This should work without setting up a golang environment.  This also builds needed ofagent and cli docker images:
```sh
export DOCKER_TAG=latest
cd ~/source/voltha-go
make build
```

Set an environment variable for other Golang builds to inform the Makefile to copy files into the local vendor folder.  Useful for development testing.
```sh
export LOCAL_VOLTHAGO=true
```



### VOLTHA 2.0 OpenOLT (Golang and Python)

Checkout and link openolt source into GOPATH for openolt development.  This is similar to voltha-go above.
```sh
cd ~/source/
git clone https://gerrit.opencord.org/voltha-openolt-adapter.git

mkdir -p $GOPATH/src/github.com/opencord
ln -s ~/source/voltha-openolt-adapter $GOPATH/src/github.com/opencord/voltha-openolt-adapter

cd $GOPATH/src/github.com/opencord/voltha-openolt-adapter
go build main.go    # verify vendor and other dependancies. output binary not actually used in container builds
```

Build the openolt container.  Above LOCAL environment variables can be used to include local library builds of PyVoltha, voltha-go, and voltha-protos.  This will copy the pyvoltha tar.gz and voltha-protos from their respective build tree and include in the openolt build tree.

Golang and Python Openolt
```sh
export DOCKER_TAG=latest
cd ~/source/voltha-openolt-adapter/
make build
```



### VOLTHA 2.0 OpenONU (Python)

```sh
cd ~/source/
git clone https://gerrit.opencord.org/voltha-openonu-adapter.git
```

Build the openonu container.  Above LOCAL environment variables can be used to include local builds of PyVoltha and voltha-protos.  This will copy the pyvoltha tar.gz and voltha-protos from their respective build tree and include in the openonu build tree.
```sh
export DOCKER_TAG=latest
cd ~/source/voltha-openonu-adapter/
make build
```



### ONOS with VOLTHA Compatible Apps

By default the standard onos docker image does not contain nor start any apps needed by voltha.  If you use the standard image then you need to use the onos restful api to load needed apps.  For development convienence there is an onos docker image build that adds in the current compatible voltha apps.
```sh
cd ~/source/
git clone https://gerrit.opencord.org/voltha-onos.git
```

Build the onos image with added onos apps (olt, aaa, sadis, dhcpl2relay).
```sh
export DOCKER_TAG=latest
cd ~/source/voltha-onos
make build
```



## Test

Run the combined compose file that starts the core, its dependent systems and the openonu and openolt adapters.  Export an environment variable of your non-localhost ip address needed for inter-container communication.  For convienence you can also export DOCKER_TAG to signify the docker images tag you would like to use.  Though for typical development you may have to edit compose/system-test.yml to override the specific docker image DOCKER_TAG needed.
```sh
export DOCKER_HOST_IP=##YOUR_LOCAL_IP##
export DOCKER_TAG=latest

cd ~/source/voltha-go
docker-compose -f compose/system-test.yml up -d
```

Verify containers have continuous uptime and no restarts
```sh
docker-compose -f compose/system-test.yml ps
docker ps
```

Login to cli and verify.  Password is admin
```sh
ssh -p 5022 voltha@localhost
```

Run voltha "devices" command to verify communication to etcd
"table empty" is good
```sh
(voltha) devices
Devices:
table empty
```

At this point preprovision and enable olt, add flows using the CLI or ofagent.



### Physical OLT/ONU Testing with Passing Traffic
Start a physical OLT and ONU.  Tested with Edgecore OLT, Broadcom based ONU, and RG capable of EAPoL

Preprovision and enable
```sh
(voltha) preprovision_olt -t openolt -H 10.64.1.207:9191
success (device id = c6efa171c13056d36e69d1ab)
(voltha) enable
enabling c6efa171c13056d36e69d1ab
waiting for device to be enabled...
waiting for device to be enabled...
success (device id = c6efa171c13056d36e69d1ab)
```

Verify device state
```sh
(voltha) devices
Devices:
+--------------------------+-------------------+------+--------------------------+---------------+-------------------+-------------+-------------+----------------+----------------+------------------+--------------------------------------+
|                       id |              type | root |                parent_id | serial_number |       mac_address | admin_state | oper_status | connect_status | parent_port_no |    host_and_port |                               reason |
+--------------------------+-------------------+------+--------------------------+---------------+-------------------+-------------+-------------+----------------+----------------+------------------+--------------------------------------+
| c6efa171c13056d36e69d1ab |           openolt | True |             00000a4001cf |  EC1829000886 | 00:00:0a:40:01:cf |     ENABLED |      ACTIVE |      REACHABLE |                | 10.64.1.207:9191 |                                      |
| 51e092cb40883b796c77a8f2 | brcm_openomci_onu |      | c6efa171c13056d36e69d1ab |  BRCM33333333 |                   |     ENABLED |      ACTIVE |      REACHABLE |      536870912 |                  | tech-profile-config-download-success |
+--------------------------+-------------------+------+--------------------------+---------------+-------------------+-------------+-------------+----------------+----------------+------------------+--------------------------------------+

(voltha) device c6efa171c13056d36e69d1ab
(device c6efa171c13056d36e69d1ab) ports
Device ports:
+-----------+-----------+--------------+-------------+-------------+--------------------------------------------------------------------+
|   port_no |     label |         type | admin_state | oper_status |                                                              peers |
+-----------+-----------+--------------+-------------+-------------+--------------------------------------------------------------------+
| 536870912 |      pon0 |      PON_OLT |     ENABLED |      ACTIVE | [{'port_no': 536870912, 'device_id': u'51e092cb40883b796c77a8f2'}] |
|     65536 | nni-65536 | ETHERNET_NNI |     ENABLED |      ACTIVE |                                                                    |
| 536870913 |      pon1 |      PON_OLT |     ENABLED |      ACTIVE |                                                                    |
| 536870914 |      pon2 |      PON_OLT |     ENABLED |      ACTIVE |                                                                    |
| 536870915 |      pon3 |      PON_OLT |     ENABLED |      ACTIVE |                                                                    |
| 536870916 |      pon4 |      PON_OLT |     ENABLED |      ACTIVE |                                                                    |
| 536870917 |      pon5 |      PON_OLT |     ENABLED |      ACTIVE |                                                                    |
| 536870918 |      pon6 |      PON_OLT |     ENABLED |      ACTIVE |                                                                    |
| 536870919 |      pon7 |      PON_OLT |     ENABLED |      ACTIVE |                                                                    |
| 536870920 |      pon8 |      PON_OLT |     ENABLED |      ACTIVE |                                                                    |
+-----------+-----------+--------------+-------------+-------------+--------------------------------------------------------------------+
| 536870921 |      pon9 |      PON_OLT |     ENABLED |      ACTIVE |                                                                    |
| 536870922 |     pon10 |      PON_OLT |     ENABLED |      ACTIVE |                                                                    |
| 536870923 |     pon11 |      PON_OLT |     ENABLED |      ACTIVE |                                                                    |
| 536870924 |     pon12 |      PON_OLT |     ENABLED |      ACTIVE |                                                                    |
| 536870925 |     pon13 |      PON_OLT |     ENABLED |      ACTIVE |                                                                    |
| 536870926 |     pon14 |      PON_OLT |     ENABLED |      ACTIVE |                                                                    |
| 536870927 |     pon15 |      PON_OLT |     ENABLED |      ACTIVE |                                                                    |
+-----------+-----------+--------------+-------------+-------------+--------------------------------------------------------------------+
(device c6efa171c13056d36e69d1ab) quit


(voltha) device 51e092cb40883b796c77a8f2
(device 51e092cb40883b796c77a8f2) ports
Device ports:
+-----------+----------+--------------+-------------+-------------+--------------------------------------------------------------------+
|   port_no |    label |         type | admin_state | oper_status |                                                              peers |
+-----------+----------+--------------+-------------+-------------+--------------------------------------------------------------------+
| 536870912 | PON port |      PON_ONU |     ENABLED |      ACTIVE | [{'port_no': 536870912, 'device_id': u'c6efa171c13056d36e69d1ab'}] |
|        16 |   uni-16 | ETHERNET_UNI |     ENABLED |      ACTIVE |                                                                    |
|        17 |   uni-17 | ETHERNET_UNI |     ENABLED |      ACTIVE |                                                                    |
|        18 |   uni-18 | ETHERNET_UNI |     ENABLED |      ACTIVE |                                                                    |
|        19 |   uni-19 | ETHERNET_UNI |     ENABLED |      ACTIVE |                                                                    |
|        20 |   uni-20 | ETHERNET_UNI |     ENABLED |      ACTIVE |                                                                    |
+-----------+----------+--------------+-------------+-------------+--------------------------------------------------------------------+
```

Verify onos device state and EAPoL authentication.  Password is karaf
```sh
ssh -p 8101 karaf@localhost
```

```sh
onos> ports
id=of:000000000a4001cf, available=true, local-status=connected 1h15m ago, role=MASTER, type=SWITCH, mfr=, hw=asfvolt16, sw=BAL.2.6.0__Openolt.2018.10.04, serial=EC1829000886, chassis=a4001cf, driver=voltha, channelId=172.18.0.1:38368, locType=none, managementAddress=172.18.0.1, name=of:000000000a4001cf, protocol=OF_13
  port=16, state=enabled, type=fiber, speed=0 , adminState=enabled, portMac=08:00:00:00:00:10, portName=BRCM33333333-1
  port=17, state=enabled, type=fiber, speed=0 , adminState=enabled, portMac=08:00:00:00:00:11, portName=BRCM33333333-2
  port=18, state=enabled, type=fiber, speed=0 , adminState=enabled, portMac=08:00:00:00:00:12, portName=BRCM33333333-3
  port=19, state=enabled, type=fiber, speed=0 , adminState=enabled, portMac=08:00:00:00:00:13, portName=BRCM33333333-4
  port=20, state=enabled, type=fiber, speed=0 , adminState=enabled, portMac=08:00:00:00:00:14, portName=BRCM33333333-5
  port=65536, state=enabled, type=fiber, speed=0 , adminState=enabled, portMac=00:00:00:01:00:00, portName=nni-65536

onos> aaa-users
UserName=E0:B7:0A:70:E6:C1,CurrentState=AUTHORIZED,DeviceId=of:000000000a4001cf,MAC=E0:B7:0A:70:E6:C1,PortNumber=16,SubscriberId=PON 1/1/3/1:3.1.1
```

Provision subscriber flows.  Verify traffic
```sh
onos> volt-subscribers
port=of:000000000a4001cf/16, svlan=13, cvlan=22

onos> volt-add-subscriber-access of:000000000a4001cf 16

onos> volt-programmed-subscribers
location=of:000000000a4001cf/16 subscriber=[id:BRCM33333333-1,cTag:22,sTag:13,nasPortId:PON 1/1/3/1:3.1.1,uplinkPort:-1,slot:-1,hardwareIdentifier:null,ipaddress:null,nasId:null,circuitId:PON 1/1/3/1:3.1.1-CID,remoteId:ATLEDGEVOLT1-RID]

```


### Test with BBSIM or Ponsim
if you don't have a real OLT device and want to test with a simulator BBSIM or PONSIM can be used.
```compose/system-test-bbsim.yml``` file includes BBSIM image and ```compose/system-test-ponsim.yml``` includes PONSIM. Please note that since PONSIM uses its own ```ponsim_adapter``` you need to run the preprovision command like this:
```preprovision_olt -t ponsim_olt -H <IP of Ponsim OLT>:50060```
