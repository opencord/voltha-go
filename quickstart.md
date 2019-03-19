# Quickstart VOLTHA 2.x Build Setup.

Starting point is a basic Ubuntu 16.04 installation with internet access.





## Install prerequisites

Patch and updated
```sh
sudo apt update
sudo apt dist-upgrade
```

Add docker-ce repo and install docker and build tools
```sh
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo apt-key add -
sudo add-apt-repository "deb [arch=amd64] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable"
sudo apt update
sudo apt install build-essential docker-ce docker-compose virtualenv git python-setuptools python-dev libpcap-dev libffi-dev libssl-dev tox
```

Allow non-root user docker system access
```
sudo usermod -a -G docker $USER
```
Logout/Login to assume new group membership needed for running docker as non-root user.





## Checkout source and build images



### VOLTHA Protos

Library containing all VOLTHA gRPC Protobuf definitions and the build system to produce needed stubs in Python and Golang.

```sh
mkdir ~/source
cd ~/source/
git clone https://gerrit.opencord.org/voltha-protos.git
```

Setup local voltha-protos for go if editing protos is needed
Follow voltha-protos README for setup, then: 
```
export LOCAL_PROTOS=true
```

Generate the local tar.gz that is the dev version of voltha-protos:
```sh
cd ~/source/voltha-protos/
make build
ls dist/
```



### VOLTHA 2.0 Go Core

```sh
cd ~/source/
git clone https://gerrit.opencord.org/voltha-go.git
```

Build Go docker images, rwcore being whats most needed for now:
```sh
cd ~/source/voltha-go
make build
```

Build Python CLI and OFAgent docker images.  Python ofagent, cli, and ponsim build system needs to be told to create protos docker image using environment variable.  This will no longer be needed when python components use pyvoltha and voltha-protos packages
```sh
export VOLTHA_BUILD=docker
cd ~/source/voltha-go/python
make build
```



### PyVoltha PIP Library

Python library of common core functions.  Once this stabilizes then you will no longer need a local build, the needed version will be in pip.  Currently the plan is to push the pyvoltha pip library once a sprint into PyPi.   Currently PyVoltha includes generated python stubs of voltha gRPC protos.

```sh
cd ~/source/
git clone https://gerrit.opencord.org/pyvoltha.git
```

Generate the local tar.gz that is the dev version of pyvoltha:
```sh
cd ~/source/pyvoltha/
make dist
ls dist/
```



### VOLTHA 2.0 OpenOLT (python)

```sh
cd ~/source/
git clone https://gerrit.opencord.org/voltha-openolt-adapter.git
```

Build the openolt container.  Inform the Makefile to use a local build of PyVoltha and voltha-protos.  This will copy the pyvoltha tar.gz and voltha-protos from their respective build tree and include in the openolt build tree.  Once PyVoltha and voltha-protos is stable this will not be needed.
```sh
export LOCAL_PYVOLTHA=true
cd ~/source/voltha-openolt-adapter/python/
make build
```



### VOLTHA 2.0 OpenONU (python)

```sh
cd ~/source/
git clone https://gerrit.opencord.org/voltha-openonu-adapter.git
```

Build the openonu container.  Inform the Makefile to use a local build of PyVoltha and voltha-protos.  This will copy the pyvoltha tar.gz and voltha-protos from their respective build tree and include in the openonu build tree.  Once PyVoltha and voltha-protos is stable this will not be needed.
```sh
export LOCAL_PYVOLTHA=true
cd ~/source/voltha-openonu-adapter/python
make build
```





## Test

Run the combined compose file that starts the core, its dependent systems and the openonu and openolt adapters.  Export an environment variable of your non-localhost ip address needed for inter-container communication.

```sh
export DOCKER_HOST_IP=##YOUR_LOCAL_IP##
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

### Test with BBSIM or Ponsim
if you don't have a real OLT device and want to test with a simulator BBSIM or PONSIM can be used.
```compose/system-test-bbsim.yml``` file includes BBSIM image and ```compose/system-test-ponsim.yml``` includes PONSIM. Please note that since PONSIM uses its own ```ponsim_adapter``` you need to run the preprovision command like this:
```preprovision_olt -t ponsim_olt -H <IP of Ponsim OLT>:50060``` 
