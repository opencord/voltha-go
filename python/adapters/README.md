# How to Build and Develop a Voltha Adapter

The build and development environment of a Voltha Adapter is left to the developer's choice.  

### Build

You can build the Voltha Adapter by:

```
cd adapters
. env.sh
make build
```

The above has generates a few docker images. An example is below:

```
$ docker images
REPOSITORY                  TAG                                        IMAGE ID            CREATED             SIZE
voltha-adapter-ponsim-onu   latest                                     3638b16b5262        36 seconds ago      774MB
voltha-adapter-ponsim-olt   latest                                     9e98a3a8e1aa        58 seconds ago      775MB
voltha-base                 latest                                     40ed93942a6a        23 minutes ago      771MB
voltha-rw-core              latest                                     648be4bc594a        About an hour ago   29.1MB
voltha-protos               latest                                     d458a391cc81        12 days ago         2.66MB
```

### Run the ponsim adapters 

The simplest way to run the containerized adapters is using the docker compose command:

```
docker-compose -f ../compose/adapters-ponsim.yml up -d
```
