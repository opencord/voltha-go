# How to Build and Run a Voltha Adapter

The build and development environment of a Voltha Adapter is left to the developer's choice.  The 
environment used below is the macOS. 

# Create fresh build setup
When creating a fresh build setup various packages, applications and libraries are required to Build successfully.
Please refer BUILD_ENV.md for details to create fresh setup on your choice of OS.
This file will increment as new setups are created on difference OSes.

### Build

To build the adapters (so far Ponsim OLT and Ponsim ONU adapters) and dependent containers follow 
the commands below. The base directory is voltha-go. 

```
cd python
source env.sh
VOLTHA_BUILD=docker make build
```

The above build command generates a few docker images. An example is below:

```
$ docker images
REPOSITORY                  TAG                                        IMAGE ID            CREATED             SIZE
voltha-adapter-ponsim-onu   latest                                     3638b16b5262        36 seconds ago      774MB
voltha-adapter-ponsim-olt   latest                                     9e98a3a8e1aa        58 seconds ago      775MB
voltha-base                 latest                                     40ed93942a6a        23 minutes ago      771MB
voltha-protos               latest                                     d458a391cc81        12 days ago         2.66MB
voltha-protoc               latest                                     a67dda73f695        2 months ago        1.41GB
```

Now build the Voltha Core from the voltha-go directory
```
make rw_core
```

Now build the Voltha CLI image from the voltha-go/python directory (used below for provisioning)
```
make cli
```

This will create the following images:
```
REPOSITORY                   TAG                                        IMAGE ID            CREATED             SIZE
voltha-rw-core               latest                                     eab4b288adea        47 seconds ago      36.8MB
voltha-cli                   latest                                     a0a4b8f25373        About an hour ago   827MB
```

### Run the Adapters locally (development environment)

We will use Ponsim as the example.  Ponsim has two containers, one being the Ponsim OLT Adapter and 
the other one the Ponsim ONU Adapter. We will use the docker-compose command to run these containers locally 
as it is straight forward.

#### Setup
Since an adapter communicates with the Voltha Core via the Kafka bus and uses the Etcd KV store then we 
need to have these containers running as well.   There is no dependency in the order in which they need to 
start as an adapter must be able to handle this scenario. 
 
First run the dependent containers from the voltha-go directory. In the commands below, replace the IP 
with the IP of the host.
```
DOCKER_HOST_IP=<Host IP> docker-compose -f compose/docker-compose-zk-kafka-test.yml up -d
DOCKER_HOST_IP=<Host IP> docker-compose -f compose/docker-compose-etcd.yml up -d
DOCKER_HOST_IP=<Host IP> docker-compose -f compose/rw_core.yml up -d
DOCKER_HOST_IP=<Host IP> docker-compose -f compose/cli.yml up -d
```
#### Running the Ponsim Adapters

Start the Ponsim OLT and ONU adapters
```
DOCKER_HOST_IP=<Host IP> docker-compose -f compose/adapters-ponsim.yml up -d
```

Start also the Ponsim OLT and ONU containers.  We are using only PonsimV2. You may want to change the 
image names from the yml files below if you are pulling the Ponsim OLT and ONU images from somewhere else.

```
docker-compose -f compose/ponsim_olt.yml up -d
docker-compose -f compose/ponsim_onu.yml up -d
```

#### Provisioning a device

First get the IP address of the Ponsim OLT container by using the docker inspect command.

Now, start the CLI. Password for 'voltha' user is 'admin'. Please see Dockerfile.cli for passwords

```$xslt
ssh -p 5022 voltha@localhost
```

Perform the provisioning

```$xslt
preprovision_olt -t ponsim_olt -H <IP of Ponsim OLT>:50060
enable <deviceId>  // Use the device ID returned in the previous command
```

At this point you can send flows to the devices using the test option in the CLI. 
```$xslt
test
install_eapol_flow <logical_device_id>
install_dhcp_flows  <logical_device_id>
install_all_controller_bound_flows <logical_device_id>
install_all_sample_flows <logical_device_id>
```

You can also see the metrics the Ponsim OLT and ONU adapters are pushing onto the kafka bus.

```$xslt
kafkacat -b <host IP>:9092 -t voltha.kpis -p 0  -o beginning
```

