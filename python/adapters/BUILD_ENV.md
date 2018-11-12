
Build Environment for Ubuntu
============================

1. The following commands were executed on a Ubuntu 16.04 (b4 bits) VM with following configuation (OpenStack Flavor: m1.medium):
	- vCPUs - 2
	- RAM - 4 GB
	- HDD - 40 GB
	- Network - Floating IP attached to Internal IP NIC. Internet access available


2. login as root as docker command needs root access. When logged-in as root 'sudo' becomes optional in each command below

git clone https://gerrit.opencord.org/voltha-go
sudo apt-get install make
sudo apt-get install python
sudo apt-get install virtualenv
sudo apt-get install gcc
sudo apt-get install python-dev
sudo apt-get install g++
sudo apt-get install libpcap-dev
sudo apt-get install apt-utils
sudo apt-get install docker-compose


3. Install docker CE for Ubuntu as mentioned in this link: https://docs.docker.com/install/linux/docker-ce/ubuntu/#set-up-the-repository. These are commands executed to install docker

sudo apt-get update

sudo apt-get install \
    apt-transport-https \
    ca-certificates \
    curl \
    software-properties-common

curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo apt-key add -

sudo apt-key fingerprint 0EBFCD88

sudo add-apt-repository \
   "deb [arch=amd64] https://download.docker.com/linux/ubuntu \
   $(lsb_release -cs) \
   stable"

sudo apt-get update

sudo apt-get install docker-ce


4. Verify that Docker CE is installed correctly by running the hello-world image. The following command downloads a test image and runs it in a container. When the container runs, it prints an informational message and exits.

sudo docker run hello-world

